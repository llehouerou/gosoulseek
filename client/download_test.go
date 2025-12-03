package client

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/llehouerou/gosoulseek/connection"
	"github.com/llehouerou/gosoulseek/messages/peer"
	"github.com/llehouerou/gosoulseek/protocol"
)

// TestDownload_RegistersWithTransferRegistry verifies that Download
// registers the transfer with the TransferRegistry.
func TestDownload_RegistersWithTransferRegistry(t *testing.T) {
	client := newTestClient(t)
	client.loggedIn = true
	defer func() { _ = client.Disconnect() }()

	var buf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	progress, err := client.Download(ctx, "testuser", "@@music/song.mp3", &buf)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	// Verify transfer is registered (check immediately, before orchestrator might remove it)
	tr, ok := client.transfers.GetByFile("testuser", "@@music/song.mp3", peer.TransferDownload)
	if !ok {
		t.Fatal("Transfer should be registered in TransferRegistry")
	}
	if tr.Username != "testuser" {
		t.Errorf("Username = %q, want %q", tr.Username, "testuser")
	}
	if tr.Filename != "@@music/song.mp3" {
		t.Errorf("Filename = %q, want %q", tr.Filename, "@@music/song.mp3")
	}

	// Drain progress channel to let orchestrator complete
	for range progress { //nolint:revive // drain channel
	}
}

// TestDownload_WithStartOffset verifies resume capability.
func TestDownload_WithStartOffset(t *testing.T) {
	client := newTestClient(t)
	client.loggedIn = true
	defer func() { _ = client.Disconnect() }()

	var buf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	progress, err := client.Download(ctx, "testuser", "@@music/song.mp3", &buf, WithStartOffset(1024))
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	tr, ok := client.transfers.GetByFile("testuser", "@@music/song.mp3", peer.TransferDownload)
	if !ok {
		t.Fatal("Transfer should be registered")
	}
	if tr.StartOffset != 1024 {
		t.Errorf("StartOffset = %d, want 1024", tr.StartOffset)
	}

	for range progress { //nolint:revive // drain channel
	}
}

// TestDownload_NotLoggedIn verifies error when not logged in.
func TestDownload_NotLoggedIn(t *testing.T) {
	client := newTestClient(t)
	defer func() { _ = client.Disconnect() }()

	var buf bytes.Buffer
	_, err := client.Download(context.Background(), "testuser", "@@music/song.mp3", &buf)
	if err == nil {
		t.Fatal("Download() should fail when not logged in")
	}
}

// TestDownload_DuplicateTransfer verifies duplicate transfer detection.
func TestDownload_DuplicateTransfer(t *testing.T) {
	client := newTestClient(t)
	client.loggedIn = true
	defer func() { _ = client.Disconnect() }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	var buf1, buf2 bytes.Buffer
	progress1, err := client.Download(ctx, "testuser", "@@music/song.mp3", &buf1)
	if err != nil {
		t.Fatalf("First Download() error = %v", err)
	}

	// Second download for same file should fail
	_, err = client.Download(ctx, "testuser", "@@music/song.mp3", &buf2)
	if err == nil {
		t.Fatal("Download() should fail for duplicate transfer")
	}
	var dupErr *DuplicateTransferError
	if !errors.As(err, &dupErr) {
		t.Errorf("error type = %T, want *DuplicateTransferError", err)
	}

	for range progress1 { //nolint:revive // drain channel
	}
}

// TestDownload_InitializesTransferChannels verifies coordination channels are initialized.
func TestDownload_InitializesTransferChannels(t *testing.T) {
	client := newTestClient(t)
	client.loggedIn = true
	defer func() { _ = client.Disconnect() }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	var buf bytes.Buffer
	progress, err := client.Download(ctx, "testuser", "@@music/song.mp3", &buf)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	tr, ok := client.transfers.GetByFile("testuser", "@@music/song.mp3", peer.TransferDownload)
	if !ok {
		t.Fatal("Transfer should be registered")
	}

	if tr.TransferReadyCh() == nil {
		t.Error("TransferReadyCh should be initialized")
	}
	if tr.TransferConnCh() == nil {
		t.Error("TransferConnCh should be initialized")
	}

	for range progress { //nolint:revive // drain channel
	}
}

// TestDownload_ReturnsProgressChannel verifies progress channel is returned.
func TestDownload_ReturnsProgressChannel(t *testing.T) {
	client := newTestClient(t)
	client.loggedIn = true
	defer func() { _ = client.Disconnect() }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	var buf bytes.Buffer
	progress, err := client.Download(ctx, "testuser", "@@music/song.mp3", &buf)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	if progress == nil {
		t.Fatal("Progress channel should not be nil")
	}

	for range progress { //nolint:revive // drain channel
	}
}

// newTestClient creates a client for testing with mock connection.
func newTestClient(t *testing.T) *Client {
	t.Helper()

	// Create pipe for mock server connection
	serverEnd, clientEnd := net.Pipe()
	t.Cleanup(func() {
		serverEnd.Close()
		clientEnd.Close()
	})

	// Start a goroutine to drain messages from server end to prevent blocking
	go func() {
		buf := make([]byte, 4096)
		for {
			_, err := serverEnd.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	client := New(nil)
	client.conn = connection.NewConn(clientEnd)
	client.connected = true
	client.stopCh = make(chan struct{})
	client.doneCh = make(chan struct{})
	close(client.doneCh) // Already closed so Disconnect won't block
	client.disconnectedCh = make(chan struct{})
	client.running = false // Not running read loop

	return client
}

// TestDownloadOrchestrator_StateTransitions tests state machine transitions.
func TestDownloadOrchestrator_StateTransitions(t *testing.T) {
	client := newTestClient(t)
	client.loggedIn = true
	defer func() { _ = client.Disconnect() }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	var buf bytes.Buffer
	progress, err := client.Download(ctx, "testuser", "@@music/song.mp3", &buf)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	// Collect all states
	states := make([]TransferState, 0, 2) // Pre-allocate for expected state updates
	for p := range progress {
		states = append(states, p.State)
	}

	// Should have at least one state update (currently just Completed|Errored)
	if len(states) == 0 {
		t.Fatal("Expected at least one state update")
	}

	// Last state should be a Completed state
	lastState := states[len(states)-1]
	if !lastState.IsCompleted() {
		t.Errorf("Last state should be Completed, got %v", lastState)
	}
}

// TestHandleTransferRequestV2_SignalsTransfer tests that TransferRequest(Upload)
// signals the correct transfer via TransferRegistry.
func TestHandleTransferRequest_SignalsTransfer(t *testing.T) {
	client := newTestClient(t)
	client.loggedIn = true
	defer func() { _ = client.Disconnect() }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	var buf bytes.Buffer
	_, err := client.Download(ctx, "testpeer", "@@music/file.mp3", &buf)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	// Get the transfer
	tr, ok := client.transfers.GetByFile("testpeer", "@@music/file.mp3", peer.TransferDownload)
	if !ok {
		t.Fatal("Transfer should be registered")
	}

	// Simulate TransferRequest(Upload) by calling the handler
	// This is what happens when peer sends us TransferRequest with Upload direction
	reqPayload := encodeTransferRequest(peer.TransferUpload, 12345, "@@music/file.mp3", 1000)
	client.handleTransferRequest(reqPayload, "testpeer", nil)

	// Check that remote token was set
	select {
	case info := <-tr.TransferReadyCh():
		if info.RemoteToken != 12345 {
			t.Errorf("RemoteToken = %d, want 12345", info.RemoteToken)
		}
		if info.FileSize != 1000 {
			t.Errorf("FileSize = %d, want 1000", info.FileSize)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected to receive TransferReadyInfo signal")
	}
}

// encodeTransferRequest encodes a TransferRequest for testing.
func encodeTransferRequest(direction peer.TransferDirection, token uint32, filename string, fileSize int64) []byte {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	req := &peer.TransferRequest{
		Direction: direction,
		Token:     token,
		Filename:  filename,
		FileSize:  fileSize,
	}
	req.Encode(w)
	return buf.Bytes()
}

// TestDeliverTransferConnection_DeliversToTransfer tests that F-type connections
// are delivered to the correct transfer via TransferConnCh.
func TestDeliverTransferConnection_DeliversToTransfer(t *testing.T) {
	client := newTestClient(t)
	client.loggedIn = true
	defer func() { _ = client.Disconnect() }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	var buf bytes.Buffer
	_, err := client.Download(ctx, "testpeer", "@@music/file.mp3", &buf)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	// Get the transfer
	tr, ok := client.transfers.GetByFile("testpeer", "@@music/file.mp3", peer.TransferDownload)
	if !ok {
		t.Fatal("Transfer should be registered")
	}

	// Set remote token (normally done by handleTransferRequest)
	tr.mu.Lock()
	tr.RemoteToken = 54321
	tr.mu.Unlock()
	_ = client.transfers.SetRemoteToken(tr.Token, 54321)

	// Create a mock connection
	server, client2 := net.Pipe()
	t.Cleanup(func() {
		server.Close()
		client2.Close()
	})
	mockConn := connection.NewConn(client2)

	// Deliver the connection via the handler
	client.deliverTransferConnection("testpeer", 54321, mockConn)

	// Check that connection was delivered
	select {
	case conn := <-tr.TransferConnCh():
		if conn == nil {
			t.Error("Expected non-nil connection")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected connection to be delivered to TransferConnCh")
	}
}

// TestDeliverTransferConnection_NoMatchingTransfer tests graceful handling when
// no matching transfer is found.
func TestDeliverTransferConnection_NoMatchingTransfer(t *testing.T) {
	client := newTestClient(t)
	client.loggedIn = true
	defer func() { _ = client.Disconnect() }()

	// Create a mock connection (should be closed/ignored when no transfer found)
	server, client2 := net.Pipe()
	t.Cleanup(func() {
		server.Close()
		client2.Close()
	})
	mockConn := connection.NewConn(client2)

	// This should not panic and just log a message
	client.deliverTransferConnection("unknownpeer", 99999, mockConn)
	// Test passes if no panic occurs
}
