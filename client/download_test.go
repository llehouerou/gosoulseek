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

const testUsername = "testUsername"

// TestDownload_RegistersWithTransferRegistry verifies that Download
// registers the transfer with the TransferRegistry.
func TestDownload_RegistersWithTransferRegistry(t *testing.T) {
	client := newTestClient(t)
	client.loggedIn = true
	defer func() { _ = client.Disconnect() }()

	var buf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	progress, err := client.Download(ctx, testUsername, "@@music/song.mp3", &buf)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	// Verify transfer is registered (check immediately, before orchestrator might remove it)
	tr, ok := client.transfers.GetByFile(testUsername, "@@music/song.mp3", peer.TransferDownload)
	if !ok {
		t.Fatal("Transfer should be registered in TransferRegistry")
	}
	if tr.Username != testUsername {
		t.Errorf("Username = %q, want %q", tr.Username, testUsername)
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

	progress, err := client.Download(ctx, "testUsername", "@@music/song.mp3", &buf, WithStartOffset(1024))
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	tr, ok := client.transfers.GetByFile("testUsername", "@@music/song.mp3", peer.TransferDownload)
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
	_, err := client.Download(context.Background(), "testUsername", "@@music/song.mp3", &buf)
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
	progress1, err := client.Download(ctx, "testUsername", "@@music/song.mp3", &buf1)
	if err != nil {
		t.Fatalf("First Download() error = %v", err)
	}

	// Second download for same file should fail
	_, err = client.Download(ctx, "testUsername", "@@music/song.mp3", &buf2)
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
	progress, err := client.Download(ctx, "testUsername", "@@music/song.mp3", &buf)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	tr, ok := client.transfers.GetByFile("testUsername", "@@music/song.mp3", peer.TransferDownload)
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
	progress, err := client.Download(ctx, "testUsername", "@@music/song.mp3", &buf)
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
	progress, err := client.Download(ctx, "testUsername", "@@music/song.mp3", &buf)
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

// TestGetDownloadPlaceInQueue_NotLoggedIn verifies error when not logged in.
func TestGetDownloadPlaceInQueue_NotLoggedIn(t *testing.T) {
	client := newTestClient(t)
	defer func() { _ = client.Disconnect() }()

	_, err := client.GetDownloadPlaceInQueue(context.Background(), "testUsername", "@@music/song.mp3")
	if err == nil {
		t.Fatal("GetDownloadPlaceInQueue() should fail when not logged in")
	}
}

// TestGetDownloadPlaceInQueue_NoSuchDownload verifies error when no download exists.
func TestGetDownloadPlaceInQueue_NoSuchDownload(t *testing.T) {
	client := newTestClient(t)
	client.loggedIn = true
	defer func() { _ = client.Disconnect() }()

	_, err := client.GetDownloadPlaceInQueue(context.Background(), "testUsername", "@@music/song.mp3")
	if err == nil {
		t.Fatal("GetDownloadPlaceInQueue() should fail when no download exists")
	}
}

// TestGetDownloadPlaceInQueue_UpdatesTransferQueuePosition tests that receiving
// PlaceInQueueResponse updates the transfer's queue position.
func TestGetDownloadPlaceInQueue_UpdatesTransferQueuePosition(t *testing.T) {
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

	// Simulate receiving PlaceInQueueResponse
	responsePayload := encodePlaceInQueueResponse("@@music/file.mp3", 5)
	client.handlePlaceInQueueResponse(responsePayload, "testpeer")

	// Verify queue position was updated
	if tr.QueuePosition() != 5 {
		t.Errorf("QueuePosition = %d, want 5", tr.QueuePosition())
	}
}

// encodePlaceInQueueResponse encodes a PlaceInQueueResponse for testing.
func encodePlaceInQueueResponse(filename string, place uint32) []byte {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	resp := &peer.PlaceInQueueResponse{
		Filename: filename,
		Place:    place,
	}
	resp.Encode(w)
	return buf.Bytes()
}

// encodePlaceInQueueRequest encodes a PlaceInQueueRequest for testing.
func encodePlaceInQueueRequest(filename string) []byte {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	req := &peer.PlaceInQueueRequest{
		Filename: filename,
	}
	req.Encode(w)
	return buf.Bytes()
}

// TestHandlePlaceInQueueRequest_SendsResponse tests that receiving a PlaceInQueueRequest
// results in a PlaceInQueueResponse being sent.
func TestHandlePlaceInQueueRequest_SendsResponse(t *testing.T) {
	client := newTestClientWithQueueManager(t)
	client.loggedIn = true
	defer func() { _ = client.Disconnect() }()

	// Enqueue an upload in the queue manager
	client.queueMgr.EnqueueUpload("requester", "@@music/file.mp3", 100)

	// Create a mock peer connection to capture the response
	peerServer, peerClient := net.Pipe()
	t.Cleanup(func() {
		peerServer.Close()
		peerClient.Close()
	})
	mockConn := connection.NewConn(peerClient)

	// Simulate receiving PlaceInQueueRequest
	requestPayload := encodePlaceInQueueRequest("@@music/file.mp3")

	// Start goroutine to read the response
	responseCh := make(chan *peer.PlaceInQueueResponse, 1)
	go func() {
		// Read the response from peerServer
		lenBuf := make([]byte, 4)
		_, err := peerServer.Read(lenBuf)
		if err != nil {
			return
		}
		msgLen := int(lenBuf[0]) | int(lenBuf[1])<<8 | int(lenBuf[2])<<16 | int(lenBuf[3])<<24
		payload := make([]byte, msgLen)
		_, err = peerServer.Read(payload)
		if err != nil {
			return
		}
		resp, err := peer.DecodePlaceInQueueResponse(payload)
		if err != nil {
			return
		}
		responseCh <- resp
	}()

	// Call the handler
	client.handlePlaceInQueueRequest(requestPayload, "requester", mockConn)

	// Verify response was sent
	select {
	case resp := <-responseCh:
		if resp.Filename != "@@music/file.mp3" {
			t.Errorf("Filename = %q, want %q", resp.Filename, "@@music/file.mp3")
		}
		if resp.Place != 1 {
			t.Errorf("Place = %d, want 1", resp.Place)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected PlaceInQueueResponse to be sent")
	}
}

// TestHandlePlaceInQueueRequest_FileNotInQueue tests that no response is sent
// when the file is not in our queue.
func TestHandlePlaceInQueueRequest_FileNotInQueue(t *testing.T) {
	client := newTestClientWithQueueManager(t)
	client.loggedIn = true
	defer func() { _ = client.Disconnect() }()

	// Create a mock peer connection
	peerServer, peerClient := net.Pipe()
	t.Cleanup(func() {
		peerServer.Close()
		peerClient.Close()
	})
	mockConn := connection.NewConn(peerClient)

	// Set a short deadline for the pipe read
	_ = peerServer.SetReadDeadline(time.Now().Add(50 * time.Millisecond))

	// Simulate receiving PlaceInQueueRequest for unknown file
	requestPayload := encodePlaceInQueueRequest("@@music/unknown.mp3")

	// Call the handler
	client.handlePlaceInQueueRequest(requestPayload, "requester", mockConn)

	// Verify NO response was sent (read should timeout)
	buf := make([]byte, 4)
	_, err := peerServer.Read(buf)
	if err == nil {
		t.Error("Should not receive response for file not in queue")
	}
}

// TestHandlePlaceInQueueRequest_CustomResolver tests that a custom resolver
// can override the queue position.
func TestHandlePlaceInQueueRequest_CustomResolver(t *testing.T) {
	client := newTestClientWithQueueManager(t)
	client.loggedIn = true
	defer func() { _ = client.Disconnect() }()

	// Set custom resolver that always returns position 42
	client.queueMgr.SetResolver(func(_, _ string) (uint32, error) {
		return 42, nil
	})

	// Enqueue an upload (position would be 1 without custom resolver)
	client.queueMgr.EnqueueUpload("requester", "@@music/file.mp3", 100)

	// Create a mock peer connection
	peerServer, peerClient := net.Pipe()
	t.Cleanup(func() {
		peerServer.Close()
		peerClient.Close()
	})
	mockConn := connection.NewConn(peerClient)

	// Start goroutine to read the response
	responseCh := make(chan *peer.PlaceInQueueResponse, 1)
	go func() {
		lenBuf := make([]byte, 4)
		_, err := peerServer.Read(lenBuf)
		if err != nil {
			return
		}
		msgLen := int(lenBuf[0]) | int(lenBuf[1])<<8 | int(lenBuf[2])<<16 | int(lenBuf[3])<<24
		payload := make([]byte, msgLen)
		_, err = peerServer.Read(payload)
		if err != nil {
			return
		}
		resp, err := peer.DecodePlaceInQueueResponse(payload)
		if err != nil {
			return
		}
		responseCh <- resp
	}()

	// Call the handler
	requestPayload := encodePlaceInQueueRequest("@@music/file.mp3")
	client.handlePlaceInQueueRequest(requestPayload, "requester", mockConn)

	// Verify response has custom position
	select {
	case resp := <-responseCh:
		if resp.Place != 42 {
			t.Errorf("Place = %d, want 42 (from custom resolver)", resp.Place)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected PlaceInQueueResponse to be sent")
	}
}

// newTestClientWithQueueManager creates a test client with a QueueManager.
func newTestClientWithQueueManager(t *testing.T) *Client {
	t.Helper()
	client := newTestClient(t)
	client.queueMgr = NewQueueManager()
	return client
}

// TestHandleUploadDenied_SetsRejectedState verifies UploadDenied handler
// sets the correct state (Rejected, not Errored).
func TestHandleUploadDenied_SetsRejectedState(t *testing.T) {
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

	// Simulate UploadDenied message
	deniedPayload := encodeUploadDenied("@@music/file.mp3", "access denied")
	client.handleUploadDenied(deniedPayload, "testpeer")

	// Verify state is Completed|Rejected (not Errored)
	state := tr.State()
	if !state.IsCompleted() {
		t.Error("Transfer should be completed")
	}
	if state&TransferStateRejected == 0 {
		t.Errorf("State = %v, want Rejected flag set", state)
	}
	if state&TransferStateErrored != 0 {
		t.Errorf("State = %v, should NOT have Errored flag", state)
	}

	// Verify error type
	tr.mu.RLock()
	transferErr := tr.Error
	tr.mu.RUnlock()
	var rejected *TransferRejectedError
	if !errors.As(transferErr, &rejected) {
		t.Errorf("Error type = %T, want *TransferRejectedError", transferErr)
	}
}

// TestHandleUploadFailed_SetsErroredState verifies UploadFailed handler
// sets the correct state and uses TransferFailedError.
func TestHandleUploadFailed_SetsErroredState(t *testing.T) {
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

	// Simulate UploadFailed message
	failedPayload := encodeUploadFailed("@@music/file.mp3")
	client.handleUploadFailed(failedPayload, "testpeer")

	// Verify state is Completed|Errored
	state := tr.State()
	if !state.IsCompleted() {
		t.Error("Transfer should be completed")
	}
	if state&TransferStateErrored == 0 {
		t.Errorf("State = %v, want Errored flag set", state)
	}

	// Verify error type is TransferFailedError
	tr.mu.RLock()
	transferErr := tr.Error
	tr.mu.RUnlock()
	var failed *TransferFailedError
	if !errors.As(transferErr, &failed) {
		t.Errorf("Error type = %T, want *TransferFailedError", transferErr)
	}
	if failed.Username != "testpeer" {
		t.Errorf("Username = %q, want %q", failed.Username, "testpeer")
	}
}

// encodeUploadDenied encodes an UploadDenied message for testing.
func encodeUploadDenied(filename, reason string) []byte {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	msg := &peer.UploadDenied{
		Filename: filename,
		Reason:   reason,
	}
	msg.Encode(w)
	return buf.Bytes()
}

// encodeUploadFailed encodes an UploadFailed message for testing.
func encodeUploadFailed(filename string) []byte {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	msg := &peer.UploadFailed{
		Filename: filename,
	}
	msg.Encode(w)
	return buf.Bytes()
}

// TestDownloadOrchestrator_CheckSizeMismatch tests size mismatch detection.
func TestDownloadOrchestrator_CheckSizeMismatch(t *testing.T) {
	tests := []struct {
		name         string
		expectedSize int64
		remoteSize   int64
		wantErr      bool
	}{
		{
			name:         "no expected size (disabled)",
			expectedSize: 0,
			remoteSize:   1000,
			wantErr:      false,
		},
		{
			name:         "sizes match",
			expectedSize: 1000,
			remoteSize:   1000,
			wantErr:      false,
		},
		{
			name:         "sizes mismatch",
			expectedSize: 1000,
			remoteSize:   2000,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orch := &downloadOrchestrator{
				expectedSize: tt.expectedSize,
			}
			err := orch.checkSizeMismatch(tt.remoteSize)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkSizeMismatch() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				var mismatch *TransferSizeMismatchError
				if !errors.As(err, &mismatch) {
					t.Errorf("error type = %T, want *TransferSizeMismatchError", err)
				}
				if mismatch.LocalSize != tt.expectedSize {
					t.Errorf("LocalSize = %d, want %d", mismatch.LocalSize, tt.expectedSize)
				}
				if mismatch.RemoteSize != tt.remoteSize {
					t.Errorf("RemoteSize = %d, want %d", mismatch.RemoteSize, tt.remoteSize)
				}
			}
		})
	}
}
