package client

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/llehouerou/gosoulseek/connection"
	"github.com/llehouerou/gosoulseek/messages/peer"
	"github.com/llehouerou/gosoulseek/messages/server"
	"github.com/llehouerou/gosoulseek/protocol"
)

// transferToken is an atomic counter for generating unique transfer tokens.
var transferToken uint32

// TransferState represents the current state of a download.
type TransferState int

const (
	// TransferStateQueued indicates the download is queued locally.
	TransferStateQueued TransferState = iota
	// TransferStateRequested indicates the download request has been sent.
	TransferStateRequested
	// TransferStateQueuedRemotely indicates the file is in the peer's upload queue.
	TransferStateQueuedRemotely
	// TransferStateConnecting indicates we're establishing the transfer connection.
	TransferStateConnecting
	// TransferStateTransferring indicates the file is being transferred.
	TransferStateTransferring
	// TransferStateCompleted indicates the download completed successfully.
	TransferStateCompleted
	// TransferStateFailed indicates the download failed.
	TransferStateFailed
	// TransferStateCancelled indicates the download was cancelled.
	TransferStateCancelled
)

// String returns a human-readable state name.
func (s TransferState) String() string {
	switch s {
	case TransferStateQueued:
		return "queued"
	case TransferStateRequested:
		return "requested"
	case TransferStateQueuedRemotely:
		return "queued_remotely"
	case TransferStateConnecting:
		return "connecting"
	case TransferStateTransferring:
		return "transferring"
	case TransferStateCompleted:
		return "completed"
	case TransferStateFailed:
		return "failed"
	case TransferStateCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// DownloadProgress represents the current state and progress of a download.
type DownloadProgress struct {
	State            TransferState
	BytesTransferred int64
	FileSize         int64
	QueuePosition    uint32 // Position in remote queue (0 if not queued)
	Error            error  // Set when State is Failed
}

// PercentComplete returns the download progress as a percentage.
func (p *DownloadProgress) PercentComplete() float64 {
	if p.FileSize == 0 {
		return 0
	}
	return float64(p.BytesTransferred) / float64(p.FileSize) * 100
}

// activeDownload tracks an ongoing download.
type activeDownload struct {
	username string
	filename string
	token    uint32
	fileSize int64

	// Progress channel - sends updates and closes on completion
	progressCh chan DownloadProgress

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc

	// Destination writer
	writer io.Writer

	// State tracking
	mu    sync.Mutex
	state TransferState
}

// downloadRegistry manages active downloads.
type downloadRegistry struct {
	mu        sync.RWMutex
	downloads map[uint32]*activeDownload // by token
	byFile    map[string]*activeDownload // by "username/filename"
}

func newDownloadRegistry() *downloadRegistry {
	return &downloadRegistry{
		downloads: make(map[uint32]*activeDownload),
		byFile:    make(map[string]*activeDownload),
	}
}

func (r *downloadRegistry) add(dl *activeDownload) {
	r.mu.Lock()
	r.downloads[dl.token] = dl
	r.byFile[dl.username+"/"+dl.filename] = dl
	r.mu.Unlock()
}

func (r *downloadRegistry) remove(dl *activeDownload) {
	r.mu.Lock()
	delete(r.downloads, dl.token)
	delete(r.byFile, dl.username+"/"+dl.filename)
	r.mu.Unlock()
}

func (r *downloadRegistry) closeAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, dl := range r.downloads {
		dl.cancel()
		close(dl.progressCh)
	}
	r.downloads = make(map[uint32]*activeDownload)
	r.byFile = make(map[string]*activeDownload)
}

// Download initiates a file download from a peer.
// It returns a channel that receives progress updates and closes when the download completes.
//
// The download flow:
// 1. Request the file from the peer (TransferRequest)
// 2. Wait for acceptance or queue position
// 3. Establish transfer connection when peer is ready
// 4. Receive file data
//
// Example:
//
//	progress, err := client.Download(ctx, "username", "@@music/file.mp3", outputFile)
//	if err != nil {
//	    return err
//	}
//	for p := range progress {
//	    fmt.Printf("%.1f%% complete\n", p.PercentComplete())
//	    if p.Error != nil {
//	        return p.Error
//	    }
//	}
func (c *Client) Download(ctx context.Context, username, filename string, w io.Writer) (<-chan DownloadProgress, error) {
	c.mu.Lock()
	if !c.loggedIn {
		c.mu.Unlock()
		return nil, errors.New("not logged in")
	}
	c.mu.Unlock()

	// Generate unique token
	token := atomic.AddUint32(&transferToken, 1)

	// Create progress channel
	progressCh := make(chan DownloadProgress, 100)

	// Create cancellable context
	dlCtx, cancel := context.WithCancel(ctx)

	dl := &activeDownload{
		username:   username,
		filename:   filename,
		token:      token,
		progressCh: progressCh,
		ctx:        dlCtx,
		cancel:     cancel,
		writer:     w,
		state:      TransferStateQueued,
	}

	// Register the download
	c.downloads.add(dl)

	// Start the download process in a goroutine
	go c.runDownload(dl)

	return progressCh, nil
}

// runDownload handles the download flow.
func (c *Client) runDownload(dl *activeDownload) {
	defer func() {
		c.downloads.remove(dl)
		close(dl.progressCh)
	}()

	// Send initial progress
	dl.sendProgress(TransferStateQueued, 0, 0, 0, nil)

	// Step 1: Get peer address from server
	peerAddr, err := c.getPeerAddress(dl.ctx, dl.username)
	if err != nil {
		dl.sendProgress(TransferStateFailed, 0, 0, 0, fmt.Errorf("get peer address: %w", err))
		return
	}

	// Step 2: Connect to peer and send transfer request
	dl.sendProgress(TransferStateRequested, 0, 0, 0, nil)

	peerConn, err := c.connectToPeerForDownload(dl.ctx, peerAddr, dl)
	if err != nil {
		dl.sendProgress(TransferStateFailed, 0, 0, 0, fmt.Errorf("connect to peer: %w", err))
		return
	}
	defer peerConn.Close()

	// Step 3: Send TransferRequest
	if err := c.sendTransferRequest(peerConn, dl); err != nil {
		dl.sendProgress(TransferStateFailed, 0, 0, 0, fmt.Errorf("send transfer request: %w", err))
		return
	}

	// Step 4: Wait for response
	resp, err := c.waitForTransferResponse(dl.ctx, peerConn, dl.token)
	if err != nil {
		dl.sendProgress(TransferStateFailed, 0, 0, 0, fmt.Errorf("wait for response: %w", err))
		return
	}

	if err := c.handleTransferResponse(resp, peerAddr, peerConn, dl); err != nil {
		dl.sendProgress(TransferStateFailed, dl.fileSize, 0, 0, err)
		return
	}

	dl.sendProgress(TransferStateCompleted, dl.fileSize, dl.fileSize, 0, nil)
}

// handleTransferResponse processes the peer's transfer response.
func (c *Client) handleTransferResponse(resp *peer.TransferResponse, peerAddr string, peerConn *connection.Conn, dl *activeDownload) error {
	if resp.Allowed {
		// Immediate transfer
		dl.fileSize = resp.FileSize
		return c.performTransfer(dl.ctx, peerAddr, dl)
	}

	if resp.Reason != "Queued" {
		return fmt.Errorf("transfer denied: %s", resp.Reason)
	}

	// File is queued - wait for peer to initiate transfer
	dl.sendProgress(TransferStateQueuedRemotely, 0, 0, 0, nil)
	return c.waitForPeerTransfer(dl.ctx, peerConn, dl)
}

// sendProgress sends a progress update to the channel (non-blocking).
func (dl *activeDownload) sendProgress(state TransferState, fileSize, transferred int64, queuePos uint32, err error) {
	dl.mu.Lock()
	dl.state = state
	dl.mu.Unlock()

	progress := DownloadProgress{
		State:            state,
		FileSize:         fileSize,
		BytesTransferred: transferred,
		QueuePosition:    queuePos,
		Error:            err,
	}

	select {
	case dl.progressCh <- progress:
	default:
		// Channel full, skip update
	}
}

// getPeerAddress requests the peer's address from the server.
func (c *Client) getPeerAddress(ctx context.Context, username string) (string, error) {
	// Create a channel to receive the response
	type addrResult struct {
		addr string
		err  error
	}
	resultCh := make(chan addrResult, 1)

	// Register temporary handler for GetPeerAddress response
	handlerID := c.router.Register(uint32(protocol.ServerGetPeerAddress), func(_ uint32, payload []byte) {
		resp, err := server.DecodeGetPeerAddress(protocol.NewReader(bytes.NewReader(payload)))
		if err != nil {
			return
		}
		if resp.Username == username {
			select {
			case resultCh <- addrResult{addr: fmt.Sprintf("%s:%d", resp.IPAddress, resp.Port)}:
			default:
			}
		}
	})
	defer c.router.Unregister(uint32(protocol.ServerGetPeerAddress), handlerID)

	// Send request
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	req := &server.GetPeerAddress{Username: username}
	req.Encode(w)
	if err := w.Error(); err != nil {
		return "", err
	}
	if err := c.WriteMessage(buf.Bytes()); err != nil {
		return "", err
	}

	// Wait for response
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-resultCh:
		return result.addr, result.err
	}
}

// connectToPeerForDownload establishes a peer connection for downloading.
func (c *Client) connectToPeerForDownload(ctx context.Context, addr string, dl *activeDownload) (*connection.Conn, error) {
	conn, err := connection.Dial(ctx, addr)
	if err != nil {
		return nil, err
	}

	// Send PeerInit
	c.mu.Lock()
	username := c.username
	c.mu.Unlock()

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	init := &peer.Init{
		Username: username,
		Type:     "P",
		Token:    dl.token,
	}
	init.Encode(w)
	if err := w.Error(); err != nil {
		conn.Close()
		return nil, err
	}

	if err := conn.WriteMessage(buf.Bytes()); err != nil {
		conn.Close()
		return nil, err
	}

	return conn, nil
}

// sendTransferRequest sends a download request to the peer.
func (c *Client) sendTransferRequest(conn *connection.Conn, dl *activeDownload) error {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	req := &peer.TransferRequest{
		Direction: peer.TransferDownload,
		Token:     dl.token,
		Filename:  dl.filename,
	}
	req.Encode(w)
	if err := w.Error(); err != nil {
		return err
	}

	return conn.WriteMessage(buf.Bytes())
}

// waitForTransferResponse waits for the peer's response to our transfer request.
func (c *Client) waitForTransferResponse(ctx context.Context, conn *connection.Conn, token uint32) (*peer.TransferResponse, error) {
	// Set read deadline based on context
	deadline, ok := ctx.Deadline()
	if ok {
		if err := conn.SetReadDeadline(deadline); err != nil {
			return nil, err
		}
	} else {
		if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			return nil, err
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		payload, err := conn.ReadMessage()
		if err != nil {
			return nil, err
		}

		if len(payload) < 4 {
			continue
		}

		code := binary.LittleEndian.Uint32(payload[:4])

		if code == uint32(protocol.PeerTransferResponse) {
			resp, err := peer.DecodeTransferResponse(payload)
			if err != nil {
				return nil, err
			}
			if resp.Token == token {
				return resp, nil
			}
		}
	}
}

// waitForPeerTransfer waits for the peer to initiate the transfer (when queued).
func (c *Client) waitForPeerTransfer(ctx context.Context, conn *connection.Conn, dl *activeDownload) error {
	// Set a longer deadline for queued downloads
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Minute)); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		payload, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read peer message: %w", err)
		}

		if len(payload) < 4 {
			continue
		}

		code := binary.LittleEndian.Uint32(payload[:4])

		switch code {
		case uint32(protocol.PeerTransferRequest):
			// Peer is ready to send
			req, err := peer.DecodeTransferRequest(payload)
			if err != nil {
				return err
			}
			if req.Direction == peer.TransferUpload && req.Token == dl.token {
				dl.fileSize = req.FileSize

				// Send TransferResponse accepting the transfer
				var buf bytes.Buffer
				w := protocol.NewWriter(&buf)
				resp := &peer.TransferResponse{
					Token:    dl.token,
					Allowed:  true,
					FileSize: req.FileSize,
				}
				resp.Encode(w)
				if err := w.Error(); err != nil {
					return err
				}
				if err := conn.WriteMessage(buf.Bytes()); err != nil {
					return err
				}

				// Now perform the transfer
				peerAddr := conn.RemoteAddr().String()
				return c.performTransfer(ctx, peerAddr, dl)
			}

		case uint32(protocol.PeerPlaceInQueueResponse):
			resp, err := peer.DecodePlaceInQueueResponse(payload)
			if err != nil {
				continue
			}
			if resp.Filename == dl.filename {
				dl.sendProgress(TransferStateQueuedRemotely, 0, 0, resp.Place, nil)
			}

		case uint32(protocol.PeerUploadDenied):
			denied, err := peer.DecodeUploadDenied(payload)
			if err != nil {
				continue
			}
			if denied.Filename == dl.filename {
				return fmt.Errorf("upload denied: %s", denied.Reason)
			}

		case uint32(protocol.PeerUploadFailed):
			failed, err := peer.DecodeUploadFailed(payload)
			if err != nil {
				continue
			}
			if failed.Filename == dl.filename {
				return errors.New("upload failed")
			}
		}
	}
}

// performTransfer establishes a transfer connection and receives the file data.
func (c *Client) performTransfer(ctx context.Context, peerAddr string, dl *activeDownload) error {
	dl.sendProgress(TransferStateConnecting, dl.fileSize, 0, 0, nil)

	// Extract host from peer address (may include port)
	host, _, err := net.SplitHostPort(peerAddr)
	if err != nil {
		// Try as host without port
		host = peerAddr
	}

	// Get peer's transfer port (usually same as message port)
	// In practice, we reconnect to the same address
	transferAddr := peerAddr

	// Establish transfer connection with timeout
	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	conn, err := connection.Dial(dialCtx, transferAddr)
	if err != nil {
		return fmt.Errorf("dial transfer connection to %s: %w", host, err)
	}
	defer conn.Close()

	// Send PeerInit with type "F" for file transfer
	c.mu.Lock()
	username := c.username
	c.mu.Unlock()

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	init := &peer.Init{
		Username: username,
		Type:     "F",
		Token:    dl.token,
	}
	init.Encode(w)
	if err := w.Error(); err != nil {
		return err
	}

	if err := conn.WriteMessage(buf.Bytes()); err != nil {
		return fmt.Errorf("send init: %w", err)
	}

	// Send 8-byte offset (0 for full file)
	var offsetBuf [8]byte
	// Offset 0 = start from beginning
	if _, err := conn.Write(offsetBuf[:]); err != nil {
		return fmt.Errorf("send offset: %w", err)
	}

	// Receive file data
	dl.sendProgress(TransferStateTransferring, dl.fileSize, 0, 0, nil)

	return c.receiveFileData(ctx, conn, dl)
}

// receiveFileData reads file data from the transfer connection.
func (c *Client) receiveFileData(ctx context.Context, conn *connection.Conn, dl *activeDownload) error {
	// Buffer for reading
	buf := make([]byte, 64*1024) // 64KB buffer

	var received int64
	lastUpdate := time.Now()

	for received < dl.fileSize {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Set read deadline
		if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			return err
		}

		// Calculate how much to read
		remaining := dl.fileSize - received
		toRead := min(int64(len(buf)), remaining)

		n, err := conn.Read(buf[:toRead])
		if err != nil {
			if errors.Is(err, io.EOF) && received == dl.fileSize {
				break // Expected EOF at end of file
			}
			return fmt.Errorf("read file data: %w", err)
		}

		// Write to destination
		if _, err := dl.writer.Write(buf[:n]); err != nil {
			return fmt.Errorf("write file data: %w", err)
		}

		received += int64(n)

		// Send progress update (throttled to avoid flooding)
		if time.Since(lastUpdate) > 100*time.Millisecond {
			dl.sendProgress(TransferStateTransferring, dl.fileSize, received, 0, nil)
			lastUpdate = time.Now()
		}
	}

	return nil
}
