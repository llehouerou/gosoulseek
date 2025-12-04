package client

import (
	"bytes"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/llehouerou/gosoulseek/connection"
	"github.com/llehouerou/gosoulseek/messages/peer"
	"github.com/llehouerou/gosoulseek/protocol"
)

const (
	testSharedFile   = `Music\artist\song.mp3`
	testUnsharedFile = `Music\unshared\track.mp3`
)

// testFileSharer creates a FileSharer with a single shared file.
func testFileSharer(soulseekPath string, size int64) *FileSharer {
	fs := NewFileSharer()
	fs.mu.Lock()
	fs.files[soulseekPath] = &SharedFile{
		LocalPath:    "/local" + soulseekPath,
		SoulseekPath: soulseekPath,
		Size:         size,
	}
	fs.mu.Unlock()
	return fs
}

// encodeQueueDownload creates a QueueDownload message payload.
func encodeQueueDownload(filename string) []byte { //nolint:unparam // test helper, may use various filenames
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	msg := &peer.QueueDownload{Filename: filename}
	msg.Encode(w)
	return buf.Bytes()
}

// readPeerMessage reads a message from the connection and returns the code and payload.
func readPeerMessage(t *testing.T, conn net.Conn) (code uint32, payload []byte) {
	t.Helper()

	// Set a short deadline
	if err := conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}

	// Read length (4 bytes)
	lenBuf := make([]byte, 4)
	if _, err := conn.Read(lenBuf); err != nil {
		t.Fatalf("read length: %v", err)
	}

	length := protocol.NewReader(bytes.NewReader(lenBuf)).ReadUint32()

	// Read payload
	payload = make([]byte, length)
	if _, err := conn.Read(payload); err != nil {
		t.Fatalf("read payload: %v", err)
	}

	if len(payload) < 4 {
		t.Fatal("payload too short")
	}

	code = protocol.NewReader(bytes.NewReader(payload[:4])).ReadUint32()
	return code, payload
}

func TestHandleQueueDownload_AcceptsValidFile(t *testing.T) {
	// Setup: client with FileSharer containing the requested file
	fs := testFileSharer(testSharedFile, 1024*1024)
	opts := DefaultOptions()
	opts.FileSharer = fs

	c := New(opts)

	// Create a pipe to simulate the peer connection
	clientConn, peerConn := net.Pipe()
	defer clientConn.Close()
	defer peerConn.Close()

	conn := connection.NewConn(clientConn)

	// Send QueueDownload in a goroutine (since handler will write response)
	payload := encodeQueueDownload(testSharedFile)

	go c.handleQueueDownload(payload, testUsername, conn)

	// Read the response from the peer side
	code, respPayload := readPeerMessage(t, peerConn)

	// Should receive PlaceInQueueResponse (code 44)
	if code != uint32(protocol.PeerPlaceInQueueResponse) {
		t.Errorf("expected PlaceInQueueResponse (44), got code %d", code)
	}

	resp, err := peer.DecodePlaceInQueueResponse(respPayload)
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Filename != testSharedFile {
		t.Errorf("filename: got %q, want %q", resp.Filename, testSharedFile)
	}

	if resp.Place != 1 {
		t.Errorf("queue position: got %d, want 1", resp.Place)
	}

	// Verify transfer was registered
	tr, ok := c.transfers.GetByFile(testUsername, testSharedFile, peer.TransferUpload)
	if !ok {
		t.Error("transfer not registered in registry")
	} else {
		if tr.Username != testUsername {
			t.Errorf("transfer username: got %q, want %q", tr.Username, testUsername)
		}
		if tr.Filename != testSharedFile {
			t.Errorf("transfer filename: got %q, want %q", tr.Filename, testSharedFile)
		}
		if tr.Size != 1024*1024 {
			t.Errorf("transfer size: got %d, want %d", tr.Size, 1024*1024)
		}
	}

	// Verify upload was enqueued
	pos := c.queueMgr.GetPosition(testUsername, testSharedFile)
	if pos != 1 {
		t.Errorf("queue position: got %d, want 1", pos)
	}
}

func TestHandleQueueDownload_RejectsUnsharedFile(t *testing.T) {
	// Setup: client with FileSharer that does NOT contain the requested file
	fs := testFileSharer(testUnsharedFile, 1024)
	opts := DefaultOptions()
	opts.FileSharer = fs

	c := New(opts)

	clientConn, peerConn := net.Pipe()
	defer clientConn.Close()
	defer peerConn.Close()

	conn := connection.NewConn(clientConn)

	payload := encodeQueueDownload(testSharedFile) // Request file not in FileSharer

	go c.handleQueueDownload(payload, testUsername, conn)

	// Read the response
	code, respPayload := readPeerMessage(t, peerConn)

	// Should receive UploadDenied (code 50)
	if code != uint32(protocol.PeerUploadDenied) {
		t.Errorf("expected UploadDenied (50), got code %d", code)
	}

	denied, err := peer.DecodeUploadDenied(respPayload)
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if denied.Filename != testSharedFile {
		t.Errorf("filename: got %q, want %q", denied.Filename, testSharedFile)
	}

	if denied.Reason != uploadDeniedFileNotShared {
		t.Errorf("reason: got %q, want %q", denied.Reason, uploadDeniedFileNotShared)
	}

	// Verify transfer was NOT registered
	_, ok := c.transfers.GetByFile(testUsername, testSharedFile, peer.TransferUpload)
	if ok {
		t.Error("transfer should not be registered for denied request")
	}
}

func TestHandleQueueDownload_RejectsByCustomValidator(t *testing.T) {
	// Setup: client with FileSharer AND a custom validator that rejects
	fs := testFileSharer(testSharedFile, 1024*1024)
	opts := DefaultOptions()
	opts.FileSharer = fs
	opts.UploadValidator = func(_, _ string) error {
		return errors.New("Downloads disabled")
	}

	c := New(opts)

	clientConn, peerConn := net.Pipe()
	defer clientConn.Close()
	defer peerConn.Close()

	conn := connection.NewConn(clientConn)

	payload := encodeQueueDownload(testSharedFile)

	go c.handleQueueDownload(payload, testUsername, conn)

	// Read the response
	code, respPayload := readPeerMessage(t, peerConn)

	// Should receive UploadDenied (code 50)
	if code != uint32(protocol.PeerUploadDenied) {
		t.Errorf("expected UploadDenied (50), got code %d", code)
	}

	denied, err := peer.DecodeUploadDenied(respPayload)
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if denied.Filename != testSharedFile {
		t.Errorf("filename: got %q, want %q", denied.Filename, testSharedFile)
	}

	if denied.Reason != "Downloads disabled" {
		t.Errorf("reason: got %q, want %q", denied.Reason, "Downloads disabled")
	}

	// Verify transfer was NOT registered
	_, ok := c.transfers.GetByFile(testUsername, testSharedFile, peer.TransferUpload)
	if ok {
		t.Error("transfer should not be registered for denied request")
	}
}

func TestHandleQueueDownload_DuplicateRequest(t *testing.T) {
	// Setup: client with FileSharer containing the requested file
	fs := testFileSharer(testSharedFile, 1024*1024)
	opts := DefaultOptions()
	opts.FileSharer = fs

	c := New(opts)

	clientConn, peerConn := net.Pipe()
	defer clientConn.Close()
	defer peerConn.Close()

	conn := connection.NewConn(clientConn)

	// First request
	payload := encodeQueueDownload(testSharedFile)

	go c.handleQueueDownload(payload, testUsername, conn)
	readPeerMessage(t, peerConn) // Consume first response

	// Get first transfer token
	firstTr, ok := c.transfers.GetByFile(testUsername, testSharedFile, peer.TransferUpload)
	if !ok {
		t.Fatal("first transfer not registered")
	}
	firstToken := firstTr.Token

	// Second request (duplicate)
	go c.handleQueueDownload(payload, testUsername, conn)
	code, respPayload := readPeerMessage(t, peerConn)

	// Should still receive PlaceInQueueResponse
	if code != uint32(protocol.PeerPlaceInQueueResponse) {
		t.Errorf("expected PlaceInQueueResponse (44), got code %d", code)
	}

	resp, err := peer.DecodePlaceInQueueResponse(respPayload)
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Place != 1 {
		t.Errorf("queue position: got %d, want 1 (not incremented)", resp.Place)
	}

	// Verify same transfer is used (not duplicated)
	tr, ok := c.transfers.GetByFile(testUsername, testSharedFile, peer.TransferUpload)
	if !ok {
		t.Fatal("transfer not found after duplicate request")
	}

	if tr.Token != firstToken {
		t.Errorf("transfer token changed: got %d, want %d", tr.Token, firstToken)
	}

	// Queue should still have only one entry
	if c.queueMgr.Len() != 1 {
		t.Errorf("queue length: got %d, want 1", c.queueMgr.Len())
	}
}

func TestHandleQueueDownload_NoFileSharerConfigured(t *testing.T) {
	// Setup: client with NO FileSharer
	opts := DefaultOptions()
	opts.FileSharer = nil

	c := New(opts)

	clientConn, peerConn := net.Pipe()
	defer clientConn.Close()
	defer peerConn.Close()

	conn := connection.NewConn(clientConn)

	payload := encodeQueueDownload(testSharedFile)

	go c.handleQueueDownload(payload, testUsername, conn)

	// Read the response
	code, respPayload := readPeerMessage(t, peerConn)

	// Should receive UploadDenied (code 50)
	if code != uint32(protocol.PeerUploadDenied) {
		t.Errorf("expected UploadDenied (50), got code %d", code)
	}

	denied, err := peer.DecodeUploadDenied(respPayload)
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if denied.Reason != uploadDeniedFileNotShared {
		t.Errorf("reason: got %q, want %q", denied.Reason, uploadDeniedFileNotShared)
	}
}

func TestHandleQueueDownload_ValidatorReceivesCorrectArgs(t *testing.T) {
	fs := testFileSharer(testSharedFile, 1024)
	opts := DefaultOptions()
	opts.FileSharer = fs

	var receivedUsername, receivedFilename string
	opts.UploadValidator = func(username, filename string) error {
		receivedUsername = username
		receivedFilename = filename
		return nil // Accept
	}

	c := New(opts)

	clientConn, peerConn := net.Pipe()
	defer clientConn.Close()
	defer peerConn.Close()

	conn := connection.NewConn(clientConn)

	payload := encodeQueueDownload(testSharedFile)

	go c.handleQueueDownload(payload, testUsername, conn)
	readPeerMessage(t, peerConn) // Consume response

	if receivedUsername != testUsername {
		t.Errorf("validator received username: got %q, want %q", receivedUsername, testUsername)
	}

	if receivedFilename != testSharedFile {
		t.Errorf("validator received filename: got %q, want %q", receivedFilename, testSharedFile)
	}
}
