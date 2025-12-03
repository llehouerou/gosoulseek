package client

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
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

// transferReadyInfo contains info from TransferRequest(Upload) message.
type transferReadyInfo struct {
	remoteToken uint32
	fileSize    int64
}

// activeDownload tracks an ongoing download.
type activeDownload struct {
	username string
	filename string
	token    uint32 // Our local token
	fileSize int64

	// Remote token - set when peer sends TransferRequest(Upload)
	remoteToken    uint32
	hasRemoteToken bool

	// Progress channel - sends updates and closes on completion
	progressCh chan DownloadProgress

	// Transfer connection channel - receives the "F" type connection
	transferConnCh chan *connection.Conn

	// Transfer ready channel - signaled when TransferRequest(Upload) arrives on ANY connection
	transferReadyCh chan transferReadyInfo

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
	mu            sync.RWMutex
	downloads     map[uint32]*activeDownload // by our token
	byFile        map[string]*activeDownload // by "username/filename"
	byRemoteToken map[string]*activeDownload // by "username/remoteToken"
}

func newDownloadRegistry() *downloadRegistry {
	return &downloadRegistry{
		downloads:     make(map[uint32]*activeDownload),
		byFile:        make(map[string]*activeDownload),
		byRemoteToken: make(map[string]*activeDownload),
	}
}

func (r *downloadRegistry) add(dl *activeDownload) {
	r.mu.Lock()
	r.downloads[dl.token] = dl
	r.byFile[dl.username+"/"+dl.filename] = dl
	r.mu.Unlock()
}

// setRemoteToken sets the remote token for a download and indexes it.
func (r *downloadRegistry) setRemoteToken(dl *activeDownload, remoteToken uint32) {
	r.mu.Lock()
	dl.remoteToken = remoteToken
	dl.hasRemoteToken = true
	r.byRemoteToken[fmt.Sprintf("%s/%d", dl.username, remoteToken)] = dl
	r.mu.Unlock()
}

// getByRemoteToken finds a download by username and remote token.
func (r *downloadRegistry) getByRemoteToken(username string, remoteToken uint32) *activeDownload {
	r.mu.RLock()
	dl := r.byRemoteToken[fmt.Sprintf("%s/%d", username, remoteToken)]
	r.mu.RUnlock()
	return dl
}

// getByFile finds a download by username and filename.
func (r *downloadRegistry) getByFile(username, filename string) *activeDownload {
	r.mu.RLock()
	dl := r.byFile[username+"/"+filename]
	r.mu.RUnlock()
	return dl
}

func (r *downloadRegistry) remove(dl *activeDownload) {
	r.mu.Lock()
	delete(r.downloads, dl.token)
	delete(r.byFile, dl.username+"/"+dl.filename)
	if dl.hasRemoteToken {
		delete(r.byRemoteToken, fmt.Sprintf("%s/%d", dl.username, dl.remoteToken))
	}
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
	r.byRemoteToken = make(map[string]*activeDownload)
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

	// Create transfer connection channel (receives the "F" connection)
	transferConnCh := make(chan *connection.Conn, 1)

	// Create transfer ready channel (signaled when TransferRequest(Upload) arrives)
	transferReadyCh := make(chan transferReadyInfo, 1)

	// Create cancellable context
	dlCtx, cancel := context.WithCancel(ctx)

	dl := &activeDownload{
		username:        username,
		filename:        filename,
		token:           token,
		progressCh:      progressCh,
		transferConnCh:  transferConnCh,
		transferReadyCh: transferReadyCh,
		ctx:             dlCtx,
		cancel:          cancel,
		writer:          w,
		state:           TransferStateQueued | TransferStateLocally,
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
	dl.sendProgress(TransferStateQueued|TransferStateLocally, 0, 0, 0, nil)

	// Step 1: Get peer address from server
	peerAddr, err := c.getPeerAddress(dl.ctx, dl.username)
	if err != nil {
		dl.sendProgress(TransferStateCompleted|TransferStateErrored, 0, 0, 0, fmt.Errorf("get peer address: %w", err))
		return
	}

	// Step 2: Connect to peer and send transfer request
	dl.sendProgress(TransferStateRequested, 0, 0, 0, nil)

	peerConn, err := c.connectToPeerForDownload(dl.ctx, peerAddr, dl)
	if err != nil {
		dl.sendProgress(TransferStateCompleted|TransferStateErrored, 0, 0, 0, fmt.Errorf("connect to peer: %w", err))
		return
	}
	defer peerConn.Close()

	// Step 3: Send TransferRequest
	if err := c.sendTransferRequest(peerConn, dl); err != nil {
		dl.sendProgress(TransferStateCompleted|TransferStateErrored, 0, 0, 0, fmt.Errorf("send transfer request: %w", err))
		return
	}

	// Step 4: Wait for response
	resp, err := c.waitForTransferResponse(dl.ctx, peerConn, dl.token)
	if err != nil {
		dl.sendProgress(TransferStateCompleted|TransferStateErrored, 0, 0, 0, fmt.Errorf("wait for response: %w", err))
		return
	}

	if err := c.handleTransferResponse(resp, peerAddr, peerConn, dl); err != nil {
		dl.sendProgress(TransferStateCompleted|TransferStateErrored, dl.fileSize, 0, 0, err)
		return
	}

	dl.sendProgress(TransferStateCompleted|TransferStateSucceeded, dl.fileSize, dl.fileSize, 0, nil)
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
	dl.sendProgress(TransferStateQueued|TransferStateRemotely, 0, 0, 0, nil)
	return c.waitForPeerTransfer(dl.ctx, peerAddr, peerConn, dl)
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
// Uses the connection manager for caching and parallel direct+indirect strategy.
func (c *Client) connectToPeerForDownload(ctx context.Context, addr string, dl *activeDownload) (*connection.Conn, error) {
	// Use the connection manager which handles:
	// - Connection caching per username
	// - Parallel direct+indirect strategy
	// - Connection deduplication for concurrent requests
	conn, isNew, err := c.peerConnMgr.GetOrCreateEx(ctx, dl.username, addr)
	if err != nil {
		return nil, err
	}

	// Start message handler only if we created a new connection
	if isNew {
		go c.handleIncomingPeerMessages(conn, dl.username)
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
// The peer will send TransferRequest(Upload) with their token (remoteToken),
// then connect to us (or we receive a ConnectToPeer) with an "F" type connection.
//
// IMPORTANT: TransferRequest(Upload) can arrive on ANY message connection from the peer,
// not just the one we established. We use a channel-based approach to handle this.
func (c *Client) waitForPeerTransfer(ctx context.Context, peerAddr string, conn *connection.Conn, dl *activeDownload) error {
	log.Printf("[DEBUG] waitForPeerTransfer: starting, waiting for TransferRequest(Upload) from %s", dl.username)

	// Start a goroutine to read messages from the connection we established.
	// This handles messages like PlaceInQueue, UploadDenied, UploadFailed.
	// TransferRequest(Upload) might also arrive here, but it could arrive on a different connection too.
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.readPeerMessagesUntilReady(conn, dl)
	}()

	// Wait for either:
	// 1. TransferRequest(Upload) signal via transferReadyCh (from ANY connection)
	// 2. Error from reading the connection we established
	// 3. Context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()

	case err := <-errCh:
		// Connection read returned - either error or we got TransferRequest on this connection
		if err != nil {
			// Check if we got signaled via channel in the meantime
			select {
			case info := <-dl.transferReadyCh:
				log.Printf("[DEBUG] waitForPeerTransfer: got signal despite error, remoteToken=%d", info.remoteToken)
				dl.fileSize = info.fileSize
				dl.sendProgress(TransferStateInitializing, dl.fileSize, 0, 0, nil)
				return c.waitForTransferConnection(ctx, peerAddr, dl)
			default:
				return err
			}
		}
		// No error means we got TransferRequest on this connection and it was handled
		dl.sendProgress(TransferStateInitializing, dl.fileSize, 0, 0, nil)
		return c.waitForTransferConnection(ctx, peerAddr, dl)

	case info := <-dl.transferReadyCh:
		// TransferRequest(Upload) arrived on some connection (maybe this one, maybe another)
		log.Printf("[DEBUG] waitForPeerTransfer: got transferReadyCh signal, remoteToken=%d, fileSize=%d", info.remoteToken, info.fileSize)
		dl.fileSize = info.fileSize
		dl.sendProgress(TransferStateInitializing, dl.fileSize, 0, 0, nil)
		return c.waitForTransferConnection(ctx, peerAddr, dl)
	}
}

// readPeerMessagesUntilReady reads messages from the P-type connection.
// Returns nil when TransferRequest(Upload) is received, or error on failure.
func (c *Client) readPeerMessagesUntilReady(conn *connection.Conn, dl *activeDownload) error {
	// Set a longer deadline for queued downloads
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Minute)); err != nil {
		return err
	}

	for {
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
			// Peer is ready to send - handle it via the global handler
			req, err := peer.DecodeTransferRequest(payload)
			if err != nil {
				continue
			}
			log.Printf("[DEBUG] readPeerMessagesUntilReady: got TransferRequest direction=%d, filename=%s, token=%d",
				req.Direction, req.Filename, req.Token)

			// Must be Upload direction and match our filename
			if req.Direction != peer.TransferUpload || req.Filename != dl.filename {
				continue
			}

			// Store the remote token
			c.downloads.setRemoteToken(dl, req.Token)
			dl.fileSize = req.FileSize

			// Send TransferResponse accepting the transfer
			var buf bytes.Buffer
			w := protocol.NewWriter(&buf)
			resp := &peer.TransferResponse{
				Token:    req.Token,
				Allowed:  true,
				FileSize: req.FileSize,
			}
			resp.Encode(w)
			if err := w.Error(); err != nil {
				return err
			}
			log.Printf("[DEBUG] readPeerMessagesUntilReady: sending TransferResponse Allowed=true for token=%d", req.Token)
			if err := conn.WriteMessage(buf.Bytes()); err != nil {
				return err
			}

			// Signal via channel (in case waitForPeerTransfer is waiting on it)
			select {
			case dl.transferReadyCh <- transferReadyInfo{remoteToken: req.Token, fileSize: req.FileSize}:
			default:
			}

			return nil // Success - ready for transfer

		case uint32(protocol.PeerPlaceInQueueResponse):
			resp, err := peer.DecodePlaceInQueueResponse(payload)
			if err != nil {
				continue
			}
			if resp.Filename == dl.filename {
				dl.sendProgress(TransferStateQueued|TransferStateRemotely, 0, 0, resp.Place, nil)
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

// waitForTransferConnection gets the "F" type transfer connection.
// This uses a dual strategy - trying both simultaneously:
// 1. Waiting for peer to connect to our listener (inbound)
// 2. Connecting directly to the peer (outbound)
// The first successful connection wins.
func (c *Client) waitForTransferConnection(ctx context.Context, peerAddr string, dl *activeDownload) error {
	log.Printf("[DEBUG] waitForTransferConnection: starting, peerAddr=%s, remoteToken=%d", peerAddr, dl.remoteToken)
	conn, err := c.getTransferConnection(ctx, peerAddr, dl)
	if err != nil {
		log.Printf("[DEBUG] waitForTransferConnection: getTransferConnection failed: %v", err)
		return err
	}
	defer conn.Close()
	log.Printf("[DEBUG] waitForTransferConnection: got connection, sending offset")

	// Send 8-byte offset (0 for full file)
	var offsetBuf [8]byte
	if _, err := conn.Write(offsetBuf[:]); err != nil {
		log.Printf("[DEBUG] waitForTransferConnection: send offset failed: %v", err)
		return fmt.Errorf("send offset: %w", err)
	}
	log.Printf("[DEBUG] waitForTransferConnection: offset sent, receiving file data")

	// Receive file data
	dl.sendProgress(TransferStateInProgress, dl.fileSize, 0, 0, nil)
	return c.receiveFileData(ctx, conn, dl)
}

// getTransferConnection gets a transfer connection using triple strategy.
// It tries three methods simultaneously:
// 1. Wait for peer to connect to us directly (inbound)
// 2. Connect directly to peer (outbound)
// 3. Ask server to tell peer to connect to us (indirect/solicited)
func (c *Client) getTransferConnection(ctx context.Context, peerAddr string, dl *activeDownload) (*connection.Conn, error) {
	log.Printf("[DEBUG] getTransferConnection: starting triple strategy for peerAddr=%s", peerAddr)
	type connResult struct {
		conn   *connection.Conn
		method string
		err    error
	}
	resultCh := make(chan connResult, 3)

	// Create cancellation contexts for each method
	inboundCtx, cancelInbound := context.WithCancel(ctx)
	defer cancelInbound()
	outboundCtx, cancelOutbound := context.WithCancel(ctx)
	defer cancelOutbound()
	indirectCtx, cancelIndirect := context.WithCancel(ctx)
	defer cancelIndirect()

	// Method 1: Wait for peer to connect to us (inbound)
	go func() {
		log.Printf("[DEBUG] getTransferConnection: starting inbound wait")
		conn, err := c.waitForInboundTransferConnection(inboundCtx, dl)
		log.Printf("[DEBUG] getTransferConnection: inbound result: conn=%v, err=%v", conn != nil, err)
		select {
		case resultCh <- connResult{conn: conn, method: "inbound", err: err}:
		case <-inboundCtx.Done():
			if conn != nil {
				conn.Close()
			}
		}
	}()

	// Method 2: Connect directly to peer (outbound)
	go func() {
		log.Printf("[DEBUG] getTransferConnection: starting outbound connect to %s", peerAddr)
		conn, err := c.connectTransferDirect(outboundCtx, peerAddr, dl)
		log.Printf("[DEBUG] getTransferConnection: outbound result: conn=%v, err=%v", conn != nil, err)
		select {
		case resultCh <- connResult{conn: conn, method: "outbound", err: err}:
		case <-outboundCtx.Done():
			if conn != nil {
				conn.Close()
			}
		}
	}()

	// Method 3: Ask server to tell peer to connect to us (indirect)
	go func() {
		log.Printf("[DEBUG] getTransferConnection: starting indirect solicitation")
		conn, err := c.solicitTransferConnection(indirectCtx, dl)
		log.Printf("[DEBUG] getTransferConnection: indirect result: conn=%v, err=%v", conn != nil, err)
		select {
		case resultCh <- connResult{conn: conn, method: "indirect", err: err}:
		case <-indirectCtx.Done():
			if conn != nil {
				conn.Close()
			}
		}
	}()

	// Wait for first successful connection
	var inboundErr, outboundErr, indirectErr error
	for range 3 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case result := <-resultCh:
			if result.err == nil && result.conn != nil {
				// Success! Cancel all other methods
				cancelInbound()
				cancelOutbound()
				cancelIndirect()
				log.Printf("[DEBUG] getTransferConnection: success via %s", result.method)
				return result.conn, nil
			}
			// Track errors by method
			switch result.method {
			case "inbound":
				inboundErr = result.err
			case "outbound":
				outboundErr = result.err
			case "indirect":
				indirectErr = result.err
			}
		}
	}

	// All failed
	return nil, errors.Join(
		fmt.Errorf("inbound: %w", inboundErr),
		fmt.Errorf("outbound: %w", outboundErr),
		fmt.Errorf("indirect: %w", indirectErr),
	)
}

// waitForInboundTransferConnection waits for the peer to connect to our listener.
func (c *Client) waitForInboundTransferConnection(ctx context.Context, dl *activeDownload) (*connection.Conn, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case conn := <-dl.transferConnCh:
		if conn == nil {
			return nil, errors.New("transfer connection closed")
		}
		return conn, nil
	}
}

// solicitTransferConnection asks the server to tell the peer to connect to us.
// This is used when the peer cannot connect to us directly (e.g., due to NAT).
func (c *Client) solicitTransferConnection(ctx context.Context, dl *activeDownload) (*connection.Conn, error) {
	// Check if we have a listener running
	if c.ListenerPort() == 0 {
		return nil, errors.New("no listener running for indirect connections")
	}

	// Generate a solicitation token
	solicitToken := atomic.AddUint32(&transferToken, 1)
	log.Printf("[DEBUG] solicitTransferConnection: sending ConnectToPeerRequest type=F, token=%d, username=%s", solicitToken, dl.username)

	// Create a channel to receive the incoming connection
	connCh := make(chan *connection.Conn, 1)

	// Register the pending solicitation
	c.solicitations.mu.Lock()
	c.solicitations.pending[solicitToken] = connCh
	c.solicitations.mu.Unlock()

	// Clean up on exit
	defer func() {
		c.solicitations.mu.Lock()
		delete(c.solicitations.pending, solicitToken)
		c.solicitations.mu.Unlock()
	}()

	// Send ConnectToPeerRequest(type=F) to the server
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	req := &server.ConnectToPeerRequest{
		Token:    solicitToken,
		Username: dl.username,
		Type:     server.ConnectionTypeTransfer, // "F" for file transfer
	}
	req.Encode(w)
	if err := w.Error(); err != nil {
		return nil, err
	}

	if err := c.WriteMessage(buf.Bytes()); err != nil {
		return nil, fmt.Errorf("send connect request: %w", err)
	}

	// Wait for the peer to connect to us (via PierceFirewall)
	var conn *connection.Conn
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case conn = <-connCh:
		if conn == nil {
			return nil, errors.New("connection closed")
		}
		log.Printf("[DEBUG] solicitTransferConnection: received connection via solicitation")
	case <-time.After(30 * time.Second):
		return nil, errors.New("indirect connection timeout")
	}

	// After PierceFirewall, the peer sends their remoteToken as 4 bytes
	// We need to read this and verify it matches our expected remoteToken
	var tokenBuf [4]byte
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set deadline: %w", err)
	}
	if _, err := io.ReadFull(conn, tokenBuf[:]); err != nil {
		conn.Close()
		return nil, fmt.Errorf("read remote token: %w", err)
	}
	remoteToken := binary.LittleEndian.Uint32(tokenBuf[:])
	log.Printf("[DEBUG] solicitTransferConnection: read remoteToken=%d, expected=%d", remoteToken, dl.remoteToken)

	// Verify the token matches (if we have one)
	if dl.hasRemoteToken && remoteToken != dl.remoteToken {
		conn.Close()
		return nil, fmt.Errorf("remote token mismatch: got %d, expected %d", remoteToken, dl.remoteToken)
	}

	// Clear the deadline
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("clear deadline: %w", err)
	}

	return conn, nil
}

// connectTransferDirect connects directly to the peer for file transfer.
func (c *Client) connectTransferDirect(ctx context.Context, peerAddr string, dl *activeDownload) (*connection.Conn, error) {
	// Wait until we have the remote token (peer sends TransferRequest with their token)
	// We need this token to identify ourselves to the peer
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(30 * time.Second)
	for {
		c.downloads.mu.RLock()
		hasToken := dl.hasRemoteToken
		remoteToken := dl.remoteToken
		c.downloads.mu.RUnlock()

		if hasToken {
			return c.dialTransferConnection(ctx, peerAddr, remoteToken)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return nil, errors.New("timeout waiting for remote token")
		case <-ticker.C:
			// Continue polling
		}
	}
}

// dialTransferConnection establishes an outbound transfer connection to the peer.
func (c *Client) dialTransferConnection(ctx context.Context, peerAddr string, remoteToken uint32) (*connection.Conn, error) {
	log.Printf("[DEBUG] dialTransferConnection: dialing %s with remoteToken=%d", peerAddr, remoteToken)
	conn, err := connection.Dial(ctx, peerAddr)
	if err != nil {
		log.Printf("[DEBUG] dialTransferConnection: dial failed: %v", err)
		return nil, fmt.Errorf("dial: %w", err)
	}
	log.Printf("[DEBUG] dialTransferConnection: connected, sending PeerInit")

	// Send PeerInit with type "F" for file transfer, using the remote token
	c.mu.Lock()
	username := c.username
	c.mu.Unlock()

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	init := &peer.Init{
		Username: username,
		Type:     "F",
		Token:    remoteToken, // Use the peer's token so they can identify this transfer
	}
	init.Encode(w)
	if err := w.Error(); err != nil {
		conn.Close()
		return nil, err
	}

	log.Printf("[DEBUG] dialTransferConnection: PeerInit message: %x", buf.Bytes())
	if err := conn.WriteMessage(buf.Bytes()); err != nil {
		conn.Close()
		log.Printf("[DEBUG] dialTransferConnection: send init failed: %v", err)
		return nil, fmt.Errorf("send init: %w", err)
	}
	log.Printf("[DEBUG] dialTransferConnection: PeerInit sent, sending token bytes")

	// After PeerInit, send the token as 4 raw bytes
	// This is required by the protocol for direct transfer connections
	var tokenBuf [4]byte
	binary.LittleEndian.PutUint32(tokenBuf[:], remoteToken)
	log.Printf("[DEBUG] dialTransferConnection: token bytes: %x", tokenBuf[:])
	if _, err := conn.Write(tokenBuf[:]); err != nil {
		conn.Close()
		log.Printf("[DEBUG] dialTransferConnection: send token failed: %v", err)
		return nil, fmt.Errorf("send token: %w", err)
	}
	log.Printf("[DEBUG] dialTransferConnection: token sent, connection ready")

	return conn, nil
}

// performTransfer establishes a transfer connection and receives the file data.
// This is used for immediate transfers (when the uploader responds with Allowed=true).
// In this case, WE initiate the F-type connection using OUR token.
func (c *Client) performTransfer(ctx context.Context, peerAddr string, dl *activeDownload) error {
	dl.sendProgress(TransferStateInitializing, dl.fileSize, 0, 0, nil)

	log.Printf("[DEBUG] performTransfer: starting immediate transfer to %s with our token=%d", peerAddr, dl.token)

	// Establish transfer connection with timeout
	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	conn, err := connection.Dial(dialCtx, peerAddr)
	if err != nil {
		log.Printf("[DEBUG] performTransfer: dial failed: %v", err)
		return fmt.Errorf("dial transfer connection: %w", err)
	}
	defer conn.Close()

	log.Printf("[DEBUG] performTransfer: connected, sending PeerInit")

	// Send PeerInit with type "F" for file transfer
	// For immediate transfers, we use OUR token since there's no remoteToken yet
	c.mu.Lock()
	username := c.username
	c.mu.Unlock()

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	init := &peer.Init{
		Username: username,
		Type:     "F",
		Token:    dl.token, // Use OUR token for immediate transfers
	}
	init.Encode(w)
	if err := w.Error(); err != nil {
		return err
	}

	if err := conn.WriteMessage(buf.Bytes()); err != nil {
		return fmt.Errorf("send init: %w", err)
	}

	// After PeerInit, send the token as 4 raw bytes
	// This is required by the protocol for direct transfer connections
	var tokenBuf [4]byte
	binary.LittleEndian.PutUint32(tokenBuf[:], dl.token)
	if _, err := conn.Write(tokenBuf[:]); err != nil {
		return fmt.Errorf("send token: %w", err)
	}

	log.Printf("[DEBUG] performTransfer: PeerInit and token sent, sending offset")

	// Send 8-byte offset (0 for full file)
	var offsetBuf [8]byte
	// Offset 0 = start from beginning
	if _, err := conn.Write(offsetBuf[:]); err != nil {
		return fmt.Errorf("send offset: %w", err)
	}

	log.Printf("[DEBUG] performTransfer: offset sent, receiving file data")

	// Receive file data
	dl.sendProgress(TransferStateInProgress, dl.fileSize, 0, 0, nil)

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
			dl.sendProgress(TransferStateInProgress, dl.fileSize, received, 0, nil)
			lastUpdate = time.Now()
		}
	}

	return nil
}
