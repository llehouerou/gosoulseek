package client

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/llehouerou/gosoulseek/messages/peer"
	"github.com/llehouerou/gosoulseek/protocol"
)

// uploadOrchestrator handles a single upload execution.
type uploadOrchestrator struct {
	slots     *SlotManager
	transfer  *Transfer
	username  string
	ctx       context.Context
	cancel    context.CancelFunc
	queueMgr  *QueueManager
	transfers *TransferRegistry
}

// acquireSlots acquires both per-user and global upload slots.
func (o *uploadOrchestrator) acquireSlots() error {
	return o.slots.AcquireUploadSlot(o.ctx, o.username)
}

// sendTransferRequest sends a TransferRequest(Upload) message to the peer.
func (o *uploadOrchestrator) sendTransferRequest(conn net.Conn) error {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	req := &peer.TransferRequest{
		Direction: peer.TransferUpload,
		Token:     o.transfer.Token,
		Filename:  o.transfer.Filename,
		FileSize:  o.transfer.Size,
	}
	req.Encode(w)
	if err := w.Error(); err != nil {
		return err
	}

	// Write length prefix + message
	msgLen := uint32(buf.Len())
	lenBuf := make([]byte, 4)
	lenBuf[0] = byte(msgLen)
	lenBuf[1] = byte(msgLen >> 8)
	lenBuf[2] = byte(msgLen >> 16)
	lenBuf[3] = byte(msgLen >> 24)

	if _, err := conn.Write(lenBuf); err != nil {
		return err
	}
	if _, err := conn.Write(buf.Bytes()); err != nil {
		return err
	}

	return nil
}

// fail marks the transfer as failed with the given error and updates state.
func (o *uploadOrchestrator) fail(err error) {
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

// complete marks the upload as successfully completed.
func (o *uploadOrchestrator) complete() {
	o.transfer.mu.Lock()
	o.transfer.Transferred = o.transfer.Size
	o.transfer.mu.Unlock()

	o.transfer.SetState(TransferStateCompleted | TransferStateSucceeded)
	o.transfer.emitProgress()
}

// cleanup releases resources when the upload completes or fails.
func (o *uploadOrchestrator) cleanup() {
	if o.cancel != nil {
		o.cancel()
	}

	// Release slots
	if o.slots != nil {
		o.slots.ReleaseUploadSlot(o.username)
	}

	// Remove from queue
	if o.queueMgr != nil {
		o.queueMgr.DequeueUpload(o.transfer.Token)
	}

	// Remove from registry (also closes progress channel)
	if o.transfers != nil {
		o.transfers.Remove(o.transfer.Token)
	}
}

// waitForResponse waits for a TransferResponse from the peer.
func (o *uploadOrchestrator) waitForResponse(conn net.Conn) (*peer.TransferResponse, error) {
	// Set read deadline based on context or default
	deadline, ok := o.ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	if err := conn.SetReadDeadline(deadline); err != nil {
		return nil, err
	}

	for {
		// Read message length
		lenBuf := make([]byte, 4)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return nil, err
		}
		msgLen := binary.LittleEndian.Uint32(lenBuf)

		// Read message
		msgBuf := make([]byte, msgLen)
		if _, err := io.ReadFull(conn, msgBuf); err != nil {
			return nil, err
		}

		// Check if it's a TransferResponse
		if len(msgBuf) < 4 {
			continue
		}
		code := binary.LittleEndian.Uint32(msgBuf[:4])
		if code == uint32(protocol.PeerTransferResponse) {
			resp, err := peer.DecodeTransferResponse(msgBuf)
			if err != nil {
				return nil, err
			}
			// Check token matches
			if resp.Token == o.transfer.Token {
				return resp, nil
			}
		}
	}
}

// streamFile reads the offset from peer and streams the file data.
func (o *uploadOrchestrator) streamFile(conn net.Conn) error {
	// Set read deadline for offset
	if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return err
	}

	// Read 8-byte offset from peer (resume support)
	var offsetBuf [8]byte
	if _, err := io.ReadFull(conn, offsetBuf[:]); err != nil {
		return fmt.Errorf("read offset: %w", err)
	}
	offset := int64(binary.LittleEndian.Uint64(offsetBuf[:]))

	// Validate offset
	if offset > o.transfer.Size {
		return fmt.Errorf("invalid offset %d > size %d", offset, o.transfer.Size)
	}

	// Update transfer state
	o.transfer.mu.Lock()
	o.transfer.StartOffset = offset
	o.transfer.Transferred = offset
	o.transfer.mu.Unlock()

	// Get shared file
	sf := o.transfer.SharedFile()
	if sf == nil {
		return errors.New("no shared file set")
	}

	// Open file
	file, err := os.Open(sf.LocalPath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	// Seek to offset
	if offset > 0 {
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return fmt.Errorf("seek: %w", err)
		}
	}

	// Clear deadlines for streaming
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return err
	}

	// Stream file data
	toSend := o.transfer.Size - offset
	sent := int64(0)
	buf := make([]byte, 64*1024)

	for sent < toSend {
		select {
		case <-o.ctx.Done():
			return o.ctx.Err()
		default:
		}

		remaining := toSend - sent
		toRead := min(int64(len(buf)), remaining)

		n, err := file.Read(buf[:toRead])
		if err != nil && err != io.EOF {
			return fmt.Errorf("read file: %w", err)
		}
		if n == 0 {
			break
		}

		if _, err := conn.Write(buf[:n]); err != nil {
			return fmt.Errorf("write: %w", err)
		}

		sent += int64(n)
		o.transfer.UpdateProgress(offset + sent)
		o.transfer.emitProgress()
	}

	return nil
}

const uploadProcessInterval = 1 * time.Second

// uploadProcessor is a background goroutine that processes queued uploads.
type uploadProcessor struct {
	slots      *SlotManager
	queueMgr   *QueueManager
	transfers  *TransferRegistry
	fileSharer *FileSharer
	stopCh     chan struct{}
	doneCh     chan struct{}
	started    bool
	stopped    bool
}

// newUploadProcessor creates a new upload processor.
func newUploadProcessor(slots *SlotManager, queueMgr *QueueManager, transfers *TransferRegistry, fileSharer *FileSharer) *uploadProcessor {
	return &uploadProcessor{
		slots:      slots,
		queueMgr:   queueMgr,
		transfers:  transfers,
		fileSharer: fileSharer,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}
}

// Start begins processing queued uploads.
func (p *uploadProcessor) Start() {
	p.started = true
	go p.run()
}

// Stop stops the processor and waits for it to finish.
func (p *uploadProcessor) Stop() {
	if p.stopped || !p.started {
		return
	}
	p.stopped = true
	close(p.stopCh)
	<-p.doneCh
}

// run is the main processing loop.
func (p *uploadProcessor) run() {
	defer close(p.doneCh)

	ticker := time.NewTicker(uploadProcessInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.processNextUpload()
		}
	}
}

// processNextUpload finds the next queued upload and starts it.
func (p *uploadProcessor) processNextUpload() {
	// Get first queued upload
	p.queueMgr.mu.RLock()
	if len(p.queueMgr.queue) == 0 {
		p.queueMgr.mu.RUnlock()
		return
	}
	entry := p.queueMgr.queue[0]
	p.queueMgr.mu.RUnlock()

	// Get the transfer
	tr, ok := p.transfers.GetByToken(entry.Token)
	if !ok {
		// Transfer was cancelled/removed, dequeue
		p.queueMgr.DequeueUpload(entry.Token)
		return
	}

	// Check if already being processed
	state := tr.State()
	if state != (TransferStateQueued | TransferStateLocally) {
		return // Already processing or in different state
	}

	// Get shared file
	var sharedFile *SharedFile
	if p.fileSharer != nil {
		sharedFile = p.fileSharer.GetFile(tr.Filename)
	}
	if sharedFile == nil {
		// File no longer shared, fail the transfer
		tr.mu.Lock()
		tr.Error = errors.New("file no longer shared")
		tr.mu.Unlock()
		tr.SetState(TransferStateCompleted | TransferStateErrored)
		tr.emitProgress()
		p.queueMgr.DequeueUpload(entry.Token)
		p.transfers.Remove(entry.Token)
		return
	}

	// Set shared file on transfer
	tr.SetSharedFile(sharedFile)
}
