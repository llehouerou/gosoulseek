package client

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"sync/atomic"
	"time"

	"github.com/llehouerou/gosoulseek/connection"
	"github.com/llehouerou/gosoulseek/messages/peer"
	"github.com/llehouerou/gosoulseek/messages/server"
	"github.com/llehouerou/gosoulseek/protocol"
)

// transferToken is an atomic counter for generating unique transfer tokens.
var transferToken uint32

// getPeerAddress requests a peer's IP address and port from the server.
func (c *Client) getPeerAddress(ctx context.Context, username string) (string, error) {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	(&server.GetPeerAddress{Username: username}).Encode(w)
	if err := w.Error(); err != nil {
		return "", fmt.Errorf("encode: %w", err)
	}

	if err := c.WriteMessage(buf.Bytes()); err != nil {
		return "", fmt.Errorf("send: %w", err)
	}

	// Wait for response
	respCh := make(chan *server.GetPeerAddressResponse, 1)
	handlerID := c.router.Register(uint32(protocol.ServerGetPeerAddress), func(_ uint32, payload []byte) {
		resp, err := server.DecodeGetPeerAddress(protocol.NewReader(bytes.NewReader(payload)))
		if err != nil || resp.Username != username {
			return
		}
		select {
		case respCh <- resp:
		default:
		}
	})
	defer c.router.Unregister(uint32(protocol.ServerGetPeerAddress), handlerID)

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case resp := <-respCh:
		return fmt.Sprintf("%s:%d", resp.IPAddress, resp.Port), nil
	}
}

// Download initiates a file download from a peer.
// It returns a channel that receives progress updates and closes when the download completes.
//
// Example:
//
//	progress, err := client.Download(ctx, "username", "@@music/file.mp3", outputFile)
//	if err != nil {
//	    return err
//	}
//	for p := range progress {
//	    fmt.Printf("%.1f%% complete\n", float64(p.BytesTransferred)/float64(p.FileSize)*100)
//	    if p.Error != nil {
//	        return p.Error
//	    }
//	}
func (c *Client) Download(ctx context.Context, username, filename string, w io.Writer, opts ...DownloadOption) (<-chan TransferProgress, error) {
	c.mu.Lock()
	if !c.loggedIn {
		c.mu.Unlock()
		return nil, errors.New("not logged in")
	}
	c.mu.Unlock()

	// Apply options
	cfg := &downloadConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Generate unique token
	token := atomic.AddUint32(&transferToken, 1)

	// Register with TransferRegistry (duplicate detection happens here)
	transfer, err := c.transfers.RegisterDownload(username, filename, token)
	if err != nil {
		return nil, err
	}

	// Initialize transfer fields
	transfer.StartOffset = cfg.startOffset
	transfer.InitDownloadChannels()

	// Store writer in transfer (need to add this field)
	transfer.mu.Lock()
	transfer.writer = w
	transfer.mu.Unlock()

	// Create cancellable context
	dlCtx, cancel := context.WithCancel(ctx)

	// Create and start orchestrator
	orch := &downloadOrchestrator{
		client:   c,
		transfer: transfer,
		ctx:      dlCtx,
		cancel:   cancel,
	}
	go orch.run()

	return transfer.Progress(), nil
}

// downloadOrchestrator handles a single download using the new infrastructure.
type downloadOrchestrator struct {
	client   *Client
	transfer *Transfer
	peerAddr string
	ctx      context.Context
	cancel   context.CancelFunc
}

// run executes the download flow.
func (o *downloadOrchestrator) run() {
	defer o.cleanup()

	// Send initial progress
	o.transfer.SetState(TransferStateQueued | TransferStateLocally)
	o.transfer.emitProgress()

	// Phase 1: Get peer address
	if err := o.getPeerAddress(); err != nil {
		o.fail(fmt.Errorf("get peer address: %w", err))
		return
	}

	// Phase 2: Connect to peer and send transfer request
	o.transfer.SetState(TransferStateRequested)
	o.transfer.emitProgress()

	peerConn, err := o.connectToPeer()
	if err != nil {
		o.fail(fmt.Errorf("connect to peer: %w", err))
		return
	}
	defer peerConn.Close()

	// Phase 3: Send TransferRequest
	if err := o.sendTransferRequest(peerConn); err != nil {
		o.fail(fmt.Errorf("send transfer request: %w", err))
		return
	}

	// Phase 4: Wait for response
	resp, err := o.waitForTransferResponse(peerConn)
	if err != nil {
		o.fail(fmt.Errorf("wait for response: %w", err))
		return
	}

	// Phase 5: Handle response (immediate or queued)
	if err := o.handleTransferResponse(resp, peerConn); err != nil {
		o.fail(err)
		return
	}

	// Success
	o.complete()
}

// getPeerAddress requests the peer's address from the server.
func (o *downloadOrchestrator) getPeerAddress() error {
	addr, err := o.client.getPeerAddress(o.ctx, o.transfer.Username)
	if err != nil {
		return err
	}
	o.peerAddr = addr
	return nil
}

// connectToPeer establishes a P-type message connection to the peer.
// Note: We don't start the global message handler here because the download
// orchestrator will read from this connection directly in waitForTransferResponse
// and readPeerMessagesUntilReady. Starting a handler would cause two readers
// to compete for messages on the same connection.
func (o *downloadOrchestrator) connectToPeer() (*connection.Conn, error) {
	conn, _, err := o.client.peerConnMgr.GetOrCreateEx(o.ctx, o.transfer.Username, o.peerAddr)
	if err != nil {
		return nil, err
	}

	return conn, nil
}

// sendTransferRequest sends a download request to the peer.
func (o *downloadOrchestrator) sendTransferRequest(conn *connection.Conn) error {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	req := &peer.TransferRequest{
		Direction: peer.TransferDownload,
		Token:     o.transfer.Token,
		Filename:  o.transfer.Filename,
	}
	req.Encode(w)
	if err := w.Error(); err != nil {
		return err
	}
	return conn.WriteMessage(buf.Bytes())
}

// waitForTransferResponse waits for the peer's response.
// It races between reading from the P-type connection and receiving a signal
// via the transfer's ready channel (in case peer connects to us instead).
func (o *downloadOrchestrator) waitForTransferResponse(conn *connection.Conn) (*peer.TransferResponse, error) {
	deadline, ok := o.ctx.Deadline()
	if ok {
		if err := conn.SetReadDeadline(deadline); err != nil {
			return nil, err
		}
	} else {
		if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			return nil, err
		}
	}

	// Channel for response from P-type connection
	type readResult struct {
		resp *peer.TransferResponse
		err  error
	}
	readCh := make(chan readResult, 1)

	go func() {
		for {
			payload, err := conn.ReadMessage()
			if err != nil {
				readCh <- readResult{nil, err}
				return
			}

			if len(payload) < 4 {
				continue
			}

			code := binary.LittleEndian.Uint32(payload[:4])
			if code == uint32(protocol.PeerTransferResponse) {
				resp, err := peer.DecodeTransferResponse(payload)
				if err != nil {
					readCh <- readResult{nil, err}
					return
				}
				if resp.Token == o.transfer.Token {
					readCh <- readResult{resp, nil}
					return
				}
			}
		}
	}()

	// Race: either we get a response on the P-type connection,
	// or the peer connects to us and signals via the ready channel
	select {
	case <-o.ctx.Done():
		return nil, o.ctx.Err()

	case result := <-readCh:
		if result.err != nil {
			// Connection failed - check if peer signaled us via listener
			select {
			case info := <-o.transfer.TransferReadyCh():
				// Peer connected to us instead! Treat as "queued and ready"
				o.transfer.mu.Lock()
				o.transfer.Size = info.FileSize
				o.transfer.RemoteToken = info.RemoteToken
				o.transfer.mu.Unlock()
				_ = o.client.transfers.SetRemoteToken(o.transfer.Token, info.RemoteToken)
				// Return a synthetic "Queued" response so handleTransferResponse proceeds correctly
				return &peer.TransferResponse{Token: o.transfer.Token, Allowed: false, Reason: "Queued"}, nil
			default:
				return nil, result.err
			}
		}
		return result.resp, nil

	case info := <-o.transfer.TransferReadyCh():
		// Peer connected to us directly! Treat as "queued and ready"
		o.transfer.mu.Lock()
		o.transfer.Size = info.FileSize
		o.transfer.RemoteToken = info.RemoteToken
		o.transfer.mu.Unlock()
		_ = o.client.transfers.SetRemoteToken(o.transfer.Token, info.RemoteToken)
		// Return a synthetic "Queued" response
		return &peer.TransferResponse{Token: o.transfer.Token, Allowed: false, Reason: "Queued"}, nil
	}
}

// handleTransferResponse processes the peer's response and performs the transfer.
func (o *downloadOrchestrator) handleTransferResponse(resp *peer.TransferResponse, peerConn *connection.Conn) error {
	if resp.Allowed {
		// Immediate transfer
		o.transfer.mu.Lock()
		o.transfer.Size = resp.FileSize
		o.transfer.mu.Unlock()
		return o.performImmediateTransfer()
	}

	if resp.Reason != "Queued" {
		return &TransferRejectedError{Reason: resp.Reason}
	}

	// File is queued - wait for peer to initiate transfer
	o.transfer.SetState(TransferStateQueued | TransferStateRemotely)
	o.transfer.emitProgress()

	return o.waitForQueuedTransfer(peerConn)
}

// performImmediateTransfer handles immediate transfers where the peer is ready to send.
func (o *downloadOrchestrator) performImmediateTransfer() error {
	o.transfer.SetState(TransferStateInitializing)
	o.transfer.emitProgress()

	log.Printf("[DEBUG] performImmediateTransfer: connecting to %s with token=%d", o.peerAddr, o.transfer.Token)

	// Establish transfer connection
	dialCtx, cancel := context.WithTimeout(o.ctx, 30*time.Second)
	defer cancel()

	conn, err := connection.Dial(dialCtx, o.peerAddr)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	// Send PeerInit with F-type and our token
	o.client.mu.Lock()
	username := o.client.username
	o.client.mu.Unlock()

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	init := &peer.Init{
		Username: username,
		Type:     "F",
		Token:    o.transfer.Token,
	}
	init.Encode(w)
	if err := w.Error(); err != nil {
		return err
	}
	if err := conn.WriteMessage(buf.Bytes()); err != nil {
		return fmt.Errorf("send init: %w", err)
	}

	// Send token as 4 raw bytes
	var tokenBuf [4]byte
	binary.LittleEndian.PutUint32(tokenBuf[:], o.transfer.Token)
	if _, err := conn.Write(tokenBuf[:]); err != nil {
		return fmt.Errorf("send token: %w", err)
	}

	return o.transferData(conn)
}

// waitForQueuedTransfer waits for the peer to initiate the transfer.
func (o *downloadOrchestrator) waitForQueuedTransfer(peerConn *connection.Conn) error {
	// Check if we already have the remote token (set by waitForTransferResponse when peer connected to us)
	o.transfer.mu.RLock()
	alreadyReady := o.transfer.RemoteToken != 0
	o.transfer.mu.RUnlock()

	if alreadyReady {
		log.Printf("[DEBUG] waitForQueuedTransfer: already have remote token, proceeding to transfer connection")
		return o.waitForTransferConnection()
	}

	log.Printf("[DEBUG] waitForQueuedTransfer: waiting for TransferRequest(Upload) from %s", o.transfer.Username)

	// Start reading messages from the P-type connection
	errCh := make(chan error, 1)
	go func() {
		errCh <- o.readPeerMessagesUntilReady(peerConn)
	}()

	// Wait for signal or error
	select {
	case <-o.ctx.Done():
		return o.ctx.Err()

	case err := <-errCh:
		if err != nil {
			// Check if we got signaled via channel
			select {
			case info := <-o.transfer.TransferReadyCh():
				o.transfer.mu.Lock()
				o.transfer.Size = info.FileSize
				o.transfer.RemoteToken = info.RemoteToken
				o.transfer.mu.Unlock()
				_ = o.client.transfers.SetRemoteToken(o.transfer.Token, info.RemoteToken)
				return o.waitForTransferConnection()
			default:
				return err
			}
		}
		return o.waitForTransferConnection()

	case info := <-o.transfer.TransferReadyCh():
		o.transfer.mu.Lock()
		o.transfer.Size = info.FileSize
		o.transfer.RemoteToken = info.RemoteToken
		o.transfer.mu.Unlock()
		_ = o.client.transfers.SetRemoteToken(o.transfer.Token, info.RemoteToken)
		return o.waitForTransferConnection()
	}
}

// readPeerMessagesUntilReady reads messages from the P-type connection.
func (o *downloadOrchestrator) readPeerMessagesUntilReady(conn *connection.Conn) error {
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
			req, err := peer.DecodeTransferRequest(payload)
			if err != nil {
				continue
			}
			log.Printf("[DEBUG] readPeerMessagesUntilReady: got TransferRequest direction=%d, filename=%s, token=%d",
				req.Direction, req.Filename, req.Token)

			if req.Direction != peer.TransferUpload || req.Filename != o.transfer.Filename {
				continue
			}

			// Store remote token
			o.transfer.mu.Lock()
			o.transfer.RemoteToken = req.Token
			o.transfer.Size = req.FileSize
			o.transfer.mu.Unlock()
			_ = o.client.transfers.SetRemoteToken(o.transfer.Token, req.Token)

			// Send TransferResponse
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
			if err := conn.WriteMessage(buf.Bytes()); err != nil {
				return err
			}

			// Signal via channel
			select {
			case o.transfer.TransferReadyCh() <- TransferReadyInfo{RemoteToken: req.Token, FileSize: req.FileSize}:
			default:
			}

			return nil

		case uint32(protocol.PeerPlaceInQueueResponse):
			resp, err := peer.DecodePlaceInQueueResponse(payload)
			if err != nil {
				continue
			}
			if resp.Filename == o.transfer.Filename {
				// Update queue position in progress
				o.transfer.emitProgress()
			}

		case uint32(protocol.PeerUploadDenied):
			denied, err := peer.DecodeUploadDenied(payload)
			if err != nil {
				continue
			}
			if denied.Filename == o.transfer.Filename {
				return &TransferRejectedError{Reason: denied.Reason}
			}

		case uint32(protocol.PeerUploadFailed):
			failed, err := peer.DecodeUploadFailed(payload)
			if err != nil {
				continue
			}
			if failed.Filename == o.transfer.Filename {
				return errors.New("upload failed")
			}
		}
	}
}

// waitForTransferConnection gets the F-type transfer connection using TransferConnectionManager.
func (o *downloadOrchestrator) waitForTransferConnection() error {
	o.transfer.SetState(TransferStateInitializing)
	o.transfer.emitProgress()

	log.Printf("[DEBUG] waitForTransferConnection: using TransferConnectionManager, peerAddr=%s, remoteToken=%d",
		o.peerAddr, o.transfer.RemoteToken)

	conn, err := o.client.transferConnMgr.AwaitConnection(o.ctx, o.transfer, o.peerAddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	return o.transferData(conn)
}

// transferData streams data from the connection to the writer.
func (o *downloadOrchestrator) transferData(conn *connection.Conn) error {
	// Send offset
	var offsetBuf [8]byte
	binary.LittleEndian.PutUint64(offsetBuf[:], uint64(o.transfer.StartOffset)) //nolint:gosec // StartOffset is always non-negative
	if _, err := conn.Write(offsetBuf[:]); err != nil {
		return fmt.Errorf("send offset: %w", err)
	}

	o.transfer.SetState(TransferStateInProgress)
	o.transfer.emitProgress()

	// Receive data
	o.transfer.mu.RLock()
	writer := o.transfer.writer
	fileSize := o.transfer.Size
	startOffset := o.transfer.StartOffset
	o.transfer.mu.RUnlock()

	toReceive := fileSize - startOffset
	received := int64(0)
	buf := make([]byte, 64*1024)

	for received < toReceive {
		select {
		case <-o.ctx.Done():
			return o.ctx.Err()
		default:
		}

		if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			return err
		}

		remaining := toReceive - received
		toRead := min(int64(len(buf)), remaining)

		n, err := conn.Read(buf[:toRead])
		if err != nil {
			if errors.Is(err, io.EOF) && received == toReceive {
				break
			}
			return fmt.Errorf("read: %w", err)
		}

		if _, err := writer.Write(buf[:n]); err != nil {
			return fmt.Errorf("write: %w", err)
		}

		received += int64(n)
		o.transfer.UpdateProgress(startOffset + received)
		o.transfer.emitProgress()
	}

	return nil
}

// fail marks the transfer as failed with the given error.
func (o *downloadOrchestrator) fail(err error) {
	o.transfer.mu.Lock()
	o.transfer.Error = err
	o.transfer.mu.Unlock()

	state := TransferStateCompleted
	switch {
	case errors.Is(err, context.Canceled):
		state |= TransferStateCancelled
	case errors.Is(err, context.DeadlineExceeded):
		state |= TransferStateTimedOut
	default:
		var rejected *TransferRejectedError
		if errors.As(err, &rejected) {
			state |= TransferStateRejected
		} else {
			state |= TransferStateErrored
		}
	}

	o.transfer.SetState(state)
	o.transfer.emitProgress()
}

// complete marks the transfer as successfully completed.
func (o *downloadOrchestrator) complete() {
	o.transfer.mu.Lock()
	o.transfer.Transferred = o.transfer.Size
	o.transfer.mu.Unlock()

	o.transfer.SetState(TransferStateCompleted | TransferStateSucceeded)
	o.transfer.emitProgress()
}

// cleanup releases resources when the download completes or fails.
func (o *downloadOrchestrator) cleanup() {
	o.cancel()
	// Remove from registry - this also closes the progress channel
	o.client.transfers.Remove(o.transfer.Token)
}

// deliverTransferConnection delivers an F-type connection to a pending download.
// This is called when a peer connects to our listener with a transfer connection.
func (c *Client) deliverTransferConnection(username string, remoteToken uint32, conn *connection.Conn) {
	tr, ok := c.transfers.GetByRemoteToken(username, remoteToken)
	if !ok {
		log.Printf("[DEBUG] deliverTransferConnection: no pending transfer for %s token=%d", username, remoteToken)
		return
	}

	connCh := tr.TransferConnCh()
	if connCh == nil {
		log.Printf("[WARN] deliverTransferConnection: transfer conn channel not initialized")
		return
	}

	select {
	case connCh <- conn:
		log.Printf("[DEBUG] deliverTransferConnection: delivered connection for %s token=%d", username, remoteToken)
	default:
		log.Printf("[WARN] deliverTransferConnection: transfer conn channel full")
	}
}

// GetDownloadPlaceInQueue returns the current queue position for a pending download.
// Returns an error if not logged in or if no download exists for the given file.
// Note: This method returns the last known queue position; to poll for updates,
// use the transfer's Progress() channel which emits updates when PlaceInQueueResponse
// messages are received.
func (c *Client) GetDownloadPlaceInQueue(_ context.Context, username, filename string) (uint32, error) {
	c.mu.Lock()
	if !c.loggedIn {
		c.mu.Unlock()
		return 0, errors.New("not logged in")
	}
	c.mu.Unlock()

	// Look up the download
	tr, ok := c.transfers.GetByFile(username, filename, peer.TransferDownload)
	if !ok {
		return 0, fmt.Errorf("no download for %s from %s", filename, username)
	}

	return tr.QueuePosition(), nil
}

// handleTransferRequest handles TransferRequest messages from peers.
// When direction is Upload, it signals the corresponding download transfer
// and sends a TransferResponse to acknowledge we're ready to receive.
// This is called when the peer is ready to send us a file we requested.
func (c *Client) handleTransferRequest(payload []byte, username string, conn *connection.Conn) {
	req, err := peer.DecodeTransferRequest(payload)
	if err != nil {
		log.Printf("[WARN] handleTransferRequest: decode error: %v", err)
		return
	}

	// Only handle Upload direction (peer wants to send file to us)
	if req.Direction != peer.TransferUpload {
		return
	}

	// Look up the download transfer
	tr, ok := c.transfers.GetByFile(username, req.Filename, peer.TransferDownload)
	if !ok {
		log.Printf("[DEBUG] handleTransferRequest: no pending download for %s from %s", req.Filename, username)
		return
	}

	// Send TransferResponse to acknowledge we're ready
	if conn != nil {
		var buf bytes.Buffer
		w := protocol.NewWriter(&buf)
		resp := &peer.TransferResponse{
			Token:    req.Token,
			Allowed:  true,
			FileSize: req.FileSize,
		}
		resp.Encode(w)
		if err := w.Error(); err != nil {
			log.Printf("[WARN] handleTransferRequest: encode response error: %v", err)
		} else if err := conn.WriteMessage(buf.Bytes()); err != nil {
			log.Printf("[WARN] handleTransferRequest: send response error: %v", err)
		} else {
			log.Printf("[DEBUG] handleTransferRequest: sent TransferResponse for %s", req.Filename)
		}
	}

	// Signal the transfer with remote token and file size
	readyCh := tr.TransferReadyCh()
	if readyCh == nil {
		log.Printf("[WARN] handleTransferRequest: transfer ready channel not initialized")
		return
	}

	select {
	case readyCh <- TransferReadyInfo{RemoteToken: req.Token, FileSize: req.FileSize}:
		log.Printf("[DEBUG] handleTransferRequest: signaled transfer for %s, token=%d, size=%d",
			req.Filename, req.Token, req.FileSize)
	default:
		log.Printf("[WARN] handleTransferRequest: transfer ready channel full")
	}
}
