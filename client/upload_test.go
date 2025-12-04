package client

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/llehouerou/gosoulseek/messages/peer"
	"github.com/llehouerou/gosoulseek/protocol"
)

func TestTransfer_SharedFile(t *testing.T) {
	// Transfer should have a sharedFile field for uploads
	tr := NewTransfer(peer.TransferUpload, "testuser", "@@Music\\test.mp3", 12345)

	// Initially nil
	if tr.SharedFile() != nil {
		t.Error("expected SharedFile to be nil initially")
	}

	// Can be set
	sf := &SharedFile{
		LocalPath:    "/tmp/test.mp3",
		SoulseekPath: "@@Music\\test.mp3",
		Size:         1024,
	}
	tr.SetSharedFile(sf)

	if tr.SharedFile() != sf {
		t.Error("expected SharedFile to be set")
	}
}

func TestUploadOrchestrator_AcquireSlots(t *testing.T) {
	// Create slot manager with limit of 1
	slots := NewSlotManager(0, 1)
	defer slots.Close()

	// Create orchestrator
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	orch := &uploadOrchestrator{
		slots:    slots,
		username: "testuser",
		ctx:      ctx,
	}

	// First acquisition should succeed
	err := orch.acquireSlots()
	if err != nil {
		t.Fatalf("first slot acquisition failed: %v", err)
	}

	// Create second orchestrator - should block/fail due to slot limit
	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()

	orch2 := &uploadOrchestrator{
		slots:    slots,
		username: "testuser2",
		ctx:      ctx2,
	}

	err = orch2.acquireSlots()
	if err == nil {
		t.Error("expected second slot acquisition to fail due to timeout")
	}
}

func TestUploadOrchestrator_SendTransferRequest(t *testing.T) {
	// Create a pipe to simulate a connection
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	transfer := NewTransfer(peer.TransferUpload, "testuser", "@@Music\\test.mp3", 12345)
	transfer.Size = 1024

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	orch := &uploadOrchestrator{
		transfer: transfer,
		ctx:      ctx,
	}

	// Read in background
	done := make(chan struct{})
	var readErr error
	var req *peer.TransferRequest
	go func() {
		defer close(done)
		// Read message length (4 bytes)
		lenBuf := make([]byte, 4)
		if _, err := io.ReadFull(serverConn, lenBuf); err != nil {
			readErr = err
			return
		}
		msgLen := binary.LittleEndian.Uint32(lenBuf)

		// Read message
		msgBuf := make([]byte, msgLen)
		if _, err := io.ReadFull(serverConn, msgBuf); err != nil {
			readErr = err
			return
		}

		req, readErr = peer.DecodeTransferRequest(msgBuf)
	}()

	// Send transfer request
	err := orch.sendTransferRequest(clientConn)
	if err != nil {
		t.Fatalf("sendTransferRequest failed: %v", err)
	}

	// Wait for read
	<-done
	if readErr != nil {
		t.Fatalf("failed to read request: %v", readErr)
	}

	// Verify request
	if req.Direction != peer.TransferUpload {
		t.Errorf("expected direction Upload, got %d", req.Direction)
	}
	if req.Token != 12345 {
		t.Errorf("expected token 12345, got %d", req.Token)
	}
	if req.Filename != "@@Music\\test.mp3" {
		t.Errorf("expected filename @@Music\\test.mp3, got %s", req.Filename)
	}
	if req.FileSize != 1024 {
		t.Errorf("expected file size 1024, got %d", req.FileSize)
	}
}

func TestUploadOrchestrator_WaitForResponse_Allowed(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	transfer := NewTransfer(peer.TransferUpload, "testuser", "@@Music\\test.mp3", 12345)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	orch := &uploadOrchestrator{
		transfer: transfer,
		ctx:      ctx,
	}

	// Send response in background
	go func() {
		var buf bytes.Buffer
		w := protocol.NewWriter(&buf)
		resp := &peer.TransferResponse{
			Token:    12345,
			Allowed:  true,
			FileSize: 1024,
		}
		resp.Encode(w)

		// Write length prefix + message
		msgLen := uint32(buf.Len())
		lenBuf := make([]byte, 4)
		binary.LittleEndian.PutUint32(lenBuf, msgLen)
		_, _ = serverConn.Write(lenBuf)
		_, _ = serverConn.Write(buf.Bytes())
	}()

	// Wait for response
	resp, err := orch.waitForResponse(clientConn)
	if err != nil {
		t.Fatalf("waitForResponse failed: %v", err)
	}

	if !resp.Allowed {
		t.Error("expected response to be allowed")
	}
	if resp.Token != 12345 {
		t.Errorf("expected token 12345, got %d", resp.Token)
	}
}

func TestUploadOrchestrator_WaitForResponse_Rejected(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	transfer := NewTransfer(peer.TransferUpload, "testuser", "@@Music\\test.mp3", 12345)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	orch := &uploadOrchestrator{
		transfer: transfer,
		ctx:      ctx,
	}

	// Send rejection in background
	go func() {
		var buf bytes.Buffer
		w := protocol.NewWriter(&buf)
		resp := &peer.TransferResponse{
			Token:   12345,
			Allowed: false,
			Reason:  "File not available",
		}
		resp.Encode(w)

		// Write length prefix + message
		msgLen := uint32(buf.Len())
		lenBuf := make([]byte, 4)
		binary.LittleEndian.PutUint32(lenBuf, msgLen)
		_, _ = serverConn.Write(lenBuf)
		_, _ = serverConn.Write(buf.Bytes())
	}()

	// Wait for response
	resp, err := orch.waitForResponse(clientConn)
	if err != nil {
		t.Fatalf("waitForResponse failed: %v", err)
	}

	if resp.Allowed {
		t.Error("expected response to be rejected")
	}
	if resp.Reason != "File not available" {
		t.Errorf("expected reason 'File not available', got %s", resp.Reason)
	}
}

func TestUploadOrchestrator_StreamFile(t *testing.T) {
	// Create a temporary file with known content
	tmpFile, err := os.CreateTemp("", "upload_test_*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write test content
	testContent := []byte("Hello, World! This is a test file for upload.")
	if _, err := tmpFile.Write(testContent); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	// Create transfer and shared file
	transfer := NewTransfer(peer.TransferUpload, "testuser", "@@Music\\test.txt", 12345)
	transfer.Size = int64(len(testContent))
	sf := &SharedFile{
		LocalPath:    tmpFile.Name(),
		SoulseekPath: "@@Music\\test.txt",
		Size:         int64(len(testContent)),
	}
	transfer.SetSharedFile(sf)

	// Create pipe
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	orch := &uploadOrchestrator{
		transfer: transfer,
		ctx:      ctx,
	}

	// Read data in background
	receivedData := make(chan []byte, 1)
	go func() {
		// First send offset (8 bytes, little-endian) - request full file
		offsetBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(offsetBuf, 0)
		_, _ = serverConn.Write(offsetBuf)

		// Read all data
		data, _ := io.ReadAll(serverConn)
		receivedData <- data
	}()

	// Stream file
	err = orch.streamFile(clientConn)
	if err != nil {
		t.Fatalf("streamFile failed: %v", err)
	}
	clientConn.Close()

	// Check received data
	received := <-receivedData
	if !bytes.Equal(received, testContent) {
		t.Errorf("data mismatch: got %q, want %q", received, testContent)
	}
}

func TestUploadOrchestrator_StreamFile_WithOffset(t *testing.T) {
	// Create a temporary file with known content
	tmpFile, err := os.CreateTemp("", "upload_test_*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write test content
	testContent := []byte("Hello, World! This is a test file for upload.")
	if _, err := tmpFile.Write(testContent); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	// Create transfer and shared file
	transfer := NewTransfer(peer.TransferUpload, "testuser", "@@Music\\test.txt", 12345)
	transfer.Size = int64(len(testContent))
	sf := &SharedFile{
		LocalPath:    tmpFile.Name(),
		SoulseekPath: "@@Music\\test.txt",
		Size:         int64(len(testContent)),
	}
	transfer.SetSharedFile(sf)

	// Create pipe
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	orch := &uploadOrchestrator{
		transfer: transfer,
		ctx:      ctx,
	}

	// Read data in background - request resume from offset 14 ("World!...")
	offset := int64(14)
	receivedData := make(chan []byte, 1)
	go func() {
		offsetBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(offsetBuf, uint64(offset))
		_, _ = serverConn.Write(offsetBuf)

		// Read all data
		data, _ := io.ReadAll(serverConn)
		receivedData <- data
	}()

	// Stream file
	err = orch.streamFile(clientConn)
	if err != nil {
		t.Fatalf("streamFile failed: %v", err)
	}
	clientConn.Close()

	// Check received data - should be content from offset onwards
	expectedData := testContent[offset:]
	received := <-receivedData
	if !bytes.Equal(received, expectedData) {
		t.Errorf("data mismatch: got %q, want %q", received, expectedData)
	}

	// Check transfer state was updated
	if transfer.StartOffset != offset {
		t.Errorf("expected StartOffset %d, got %d", offset, transfer.StartOffset)
	}
}

func TestUploadOrchestrator_StreamFile_InvalidOffset(t *testing.T) {
	// Create a temporary file with known content
	tmpFile, err := os.CreateTemp("", "upload_test_*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write test content
	testContent := []byte("Hello")
	if _, err := tmpFile.Write(testContent); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	// Create transfer and shared file
	transfer := NewTransfer(peer.TransferUpload, "testuser", "@@Music\\test.txt", 12345)
	transfer.Size = int64(len(testContent))
	sf := &SharedFile{
		LocalPath:    tmpFile.Name(),
		SoulseekPath: "@@Music\\test.txt",
		Size:         int64(len(testContent)),
	}
	transfer.SetSharedFile(sf)

	// Create pipe
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	orch := &uploadOrchestrator{
		transfer: transfer,
		ctx:      ctx,
	}

	// Send invalid offset (larger than file size)
	go func() {
		offsetBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(offsetBuf, 1000) // Way past file size
		_, _ = serverConn.Write(offsetBuf)
	}()

	// Stream file should fail
	err = orch.streamFile(clientConn)
	if err == nil {
		t.Error("expected error for invalid offset, got nil")
	}
}

func TestUploadOrchestrator_Fail_SetsState(t *testing.T) {
	transfer := NewTransfer(peer.TransferUpload, "testuser", "@@Music\\test.mp3", 12345)

	ctx := t.Context()

	orch := &uploadOrchestrator{
		transfer: transfer,
		ctx:      ctx,
	}

	// Test error sets Errored state
	testErr := errors.New("test error")
	orch.fail(testErr)

	state := transfer.State()
	if state&TransferStateCompleted == 0 {
		t.Error("expected Completed flag to be set")
	}
	if state&TransferStateErrored == 0 {
		t.Error("expected Errored flag to be set")
	}
	if !errors.Is(transfer.Error, testErr) {
		t.Errorf("expected error to be set, got %v", transfer.Error)
	}
}

func TestUploadOrchestrator_Fail_Cancelled(t *testing.T) {
	transfer := NewTransfer(peer.TransferUpload, "testuser", "@@Music\\test.mp3", 12345)

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Cancel immediately

	orch := &uploadOrchestrator{
		transfer: transfer,
		ctx:      ctx,
	}

	// Test cancellation sets Cancelled state
	orch.fail(context.Canceled)

	state := transfer.State()
	if state&TransferStateCompleted == 0 {
		t.Error("expected Completed flag to be set")
	}
	if state&TransferStateCancelled == 0 {
		t.Error("expected Cancelled flag to be set")
	}
}

func TestUploadOrchestrator_Fail_Rejected(t *testing.T) {
	transfer := NewTransfer(peer.TransferUpload, "testuser", "@@Music\\test.mp3", 12345)

	ctx := t.Context()

	orch := &uploadOrchestrator{
		transfer: transfer,
		ctx:      ctx,
	}

	// Test rejection sets Rejected state
	orch.fail(&TransferRejectedError{Reason: "Not allowed"})

	state := transfer.State()
	if state&TransferStateCompleted == 0 {
		t.Error("expected Completed flag to be set")
	}
	if state&TransferStateRejected == 0 {
		t.Error("expected Rejected flag to be set")
	}
}

func TestUploadOrchestrator_Complete(t *testing.T) {
	transfer := NewTransfer(peer.TransferUpload, "testuser", "@@Music\\test.mp3", 12345)
	transfer.Size = 1024

	ctx := t.Context()

	orch := &uploadOrchestrator{
		transfer: transfer,
		ctx:      ctx,
	}

	orch.complete()

	state := transfer.State()
	if state&TransferStateCompleted == 0 {
		t.Error("expected Completed flag to be set")
	}
	if state&TransferStateSucceeded == 0 {
		t.Error("expected Succeeded flag to be set")
	}
	if transfer.Transferred != 1024 {
		t.Errorf("expected Transferred to be 1024, got %d", transfer.Transferred)
	}
}

func TestUploadOrchestrator_Cleanup_ReleasesSlots(t *testing.T) {
	// Create slot manager with limit of 1
	slots := NewSlotManager(0, 1)
	defer slots.Close()

	queueMgr := NewQueueManager()
	transfers := NewTransferRegistry()

	// Use RegisterUpload to add to registry
	transfer, err := transfers.RegisterUpload("testuser", "@@Music\\test.mp3", 12345, 1024)
	if err != nil {
		t.Fatalf("failed to register upload: %v", err)
	}
	queueMgr.EnqueueUpload("testuser", "@@Music\\test.mp3", 12345)

	ctx, cancel := context.WithCancel(context.Background())

	orch := &uploadOrchestrator{
		slots:     slots,
		transfer:  transfer,
		username:  "testuser",
		ctx:       ctx,
		cancel:    cancel,
		queueMgr:  queueMgr,
		transfers: transfers,
	}

	// Acquire slot
	err = orch.acquireSlots()
	if err != nil {
		t.Fatalf("failed to acquire slot: %v", err)
	}

	// Verify slot is held
	stats := slots.Stats()
	if stats.UploadsActive != 1 {
		t.Errorf("expected 1 active upload, got %d", stats.UploadsActive)
	}

	// Cleanup should release slot
	orch.cleanup()

	// Verify slot is released
	stats = slots.Stats()
	if stats.UploadsActive != 0 {
		t.Errorf("expected 0 active uploads after cleanup, got %d", stats.UploadsActive)
	}

	// Verify transfer removed from registry
	_, ok := transfers.GetByToken(12345)
	if ok {
		t.Error("expected transfer to be removed from registry")
	}

	// Verify dequeued
	if queueMgr.Len() != 0 {
		t.Errorf("expected queue to be empty, got %d", queueMgr.Len())
	}
}

func TestUploadProcessor_StartStop(_ *testing.T) {
	slots := NewSlotManager(0, 10)
	defer slots.Close()

	queueMgr := NewQueueManager()
	transfers := NewTransferRegistry()

	processor := newUploadProcessor(slots, queueMgr, transfers, nil)
	processor.Start()

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Stop should be clean
	processor.Stop()

	// Double stop should not panic
	processor.Stop()
}

func TestUploadProcessor_ProcessesQueue(t *testing.T) {
	// This is an integration-level test verifying the processor picks up queued uploads
	// We'll use a mock peer connection approach

	slots := NewSlotManager(0, 10)
	defer slots.Close()

	queueMgr := NewQueueManager()
	transfers := NewTransferRegistry()

	// Create a shared file
	tmpFile, err := os.CreateTemp("", "upload_processor_test_*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	testContent := []byte("test content")
	_, _ = tmpFile.Write(testContent)
	tmpFile.Close()

	fileSharer := NewFileSharer()
	fileSharer.files["@@Music\\test.mp3"] = &SharedFile{
		LocalPath:    tmpFile.Name(),
		SoulseekPath: "@@Music\\test.mp3",
		Size:         int64(len(testContent)),
	}

	// Register an upload
	transfer, err := transfers.RegisterUpload("testuser", "@@Music\\test.mp3", 12345, int64(len(testContent)))
	if err != nil {
		t.Fatalf("failed to register upload: %v", err)
	}
	transfer.SetState(TransferStateQueued | TransferStateLocally)
	queueMgr.EnqueueUpload("testuser", "@@Music\\test.mp3", 12345)

	// Create processor
	processor := newUploadProcessor(slots, queueMgr, transfers, fileSharer)

	// Process should find the queued upload and set shared file
	processor.processNextUpload()

	// Check that SharedFile was set
	if transfer.SharedFile() == nil {
		t.Error("expected SharedFile to be set on transfer")
	}
}
