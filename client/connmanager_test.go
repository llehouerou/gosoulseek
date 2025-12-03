package client

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/llehouerou/gosoulseek/connection"
	"github.com/llehouerou/gosoulseek/messages/peer"
	"github.com/llehouerou/gosoulseek/protocol"
)

const testClientUsername = "testclient"

func TestPeerConnManager_GetCached_Miss(t *testing.T) {
	mgr := newPeerConnManager(nil)

	conn, ok := mgr.GetCached("someuser")

	if ok {
		t.Error("expected ok=false for uncached user")
	}
	if conn != nil {
		t.Error("expected nil connection for uncached user")
	}
}

func TestPeerConnManager_GetCached_Hit(t *testing.T) {
	mgr := newPeerConnManager(nil)

	// Create a mock connection using net.Pipe
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	mockConn := connection.NewConn(clientConn)

	// Add to cache
	mgr.Add("testuser", mockConn)

	// Should find it
	conn, ok := mgr.GetCached("testuser")

	if !ok {
		t.Error("expected ok=true for cached user")
	}
	if conn != mockConn {
		t.Error("expected same connection back")
	}
}

func TestPeerConnManager_Invalidate(t *testing.T) {
	mgr := newPeerConnManager(nil)

	// Create a mock connection
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	mockConn := connection.NewConn(clientConn)

	// Add to cache
	mgr.Add("testuser", mockConn)

	// Verify it's cached
	_, ok := mgr.GetCached("testuser")
	if !ok {
		t.Fatal("setup failed: connection not cached")
	}

	// Invalidate
	mgr.Invalidate("testuser")

	// Should no longer be cached
	conn, ok := mgr.GetCached("testuser")
	if ok {
		t.Error("expected ok=false after invalidation")
	}
	if conn != nil {
		t.Error("expected nil connection after invalidation")
	}
}

func TestPeerConnManager_Add_Supersedes(t *testing.T) {
	mgr := newPeerConnManager(nil)

	// Create first connection
	clientConn1, serverConn1 := net.Pipe()
	defer serverConn1.Close()
	conn1 := connection.NewConn(clientConn1)

	// Create second connection
	clientConn2, serverConn2 := net.Pipe()
	defer clientConn2.Close()
	defer serverConn2.Close()
	conn2 := connection.NewConn(clientConn2)

	// Add first connection
	superseded := mgr.Add("testuser", conn1)
	if superseded != nil {
		t.Error("first add should not supersede anything")
	}

	// Add second connection - should supersede first
	superseded = mgr.Add("testuser", conn2)
	if superseded != conn1 {
		t.Error("second add should return superseded connection")
	}

	// Cache should have the new connection
	cached, ok := mgr.GetCached("testuser")
	if !ok {
		t.Error("expected connection to be cached")
	}
	if cached != conn2 {
		t.Error("cache should contain the new connection")
	}

	// First connection should be closed by the manager
	// We can verify by trying to read from it - should get an error
	buf := make([]byte, 1)
	_, err := clientConn1.Read(buf)
	if err == nil {
		t.Error("superseded connection should be closed")
	}
}

func TestPeerConnManager_Close(t *testing.T) {
	mgr := newPeerConnManager(nil)

	// Create connections
	clientConn1, serverConn1 := net.Pipe()
	defer serverConn1.Close()
	conn1 := connection.NewConn(clientConn1)

	clientConn2, serverConn2 := net.Pipe()
	defer serverConn2.Close()
	conn2 := connection.NewConn(clientConn2)

	// Add to cache
	mgr.Add("user1", conn1)
	mgr.Add("user2", conn2)

	// Close the manager
	err := mgr.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}

	// Cache should be empty
	_, ok := mgr.GetCached("user1")
	if ok {
		t.Error("user1 should not be cached after Close")
	}
	_, ok = mgr.GetCached("user2")
	if ok {
		t.Error("user2 should not be cached after Close")
	}

	// Connections should be closed
	buf := make([]byte, 1)
	_, err = clientConn1.Read(buf)
	if err == nil {
		t.Error("conn1 should be closed after Close")
	}
	_, err = clientConn2.Read(buf)
	if err == nil {
		t.Error("conn2 should be closed after Close")
	}
}

// --- GetOrCreate tests ---

// mockDialer is a test dialer that uses net.Pipe for mock connections.
type mockDialer struct {
	mu         sync.Mutex
	dialCalls  int
	shouldFail bool
	delay      time.Duration
	conns      []struct{ client, server net.Conn }
}

func (d *mockDialer) Dial(ctx context.Context, _ string) (*connection.Conn, error) {
	d.mu.Lock()
	d.dialCalls++
	shouldFail := d.shouldFail
	delay := d.delay
	d.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if shouldFail {
		return nil, context.DeadlineExceeded
	}

	client, server := net.Pipe()
	d.mu.Lock()
	d.conns = append(d.conns, struct{ client, server net.Conn }{client, server})
	d.mu.Unlock()

	// Simulate server side accepting the PeerInit
	go d.handleServerSide(server)

	return connection.NewConn(client), nil
}

func (d *mockDialer) handleServerSide(server net.Conn) {
	conn := connection.NewConn(server)
	// Read the PeerInit message that the manager sends
	_, err := conn.ReadMessage()
	if err != nil {
		server.Close()
		return
	}
	// Connection stays open for the test
}

func (d *mockDialer) cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range d.conns {
		c.client.Close()
		c.server.Close()
	}
}

func TestPeerConnManager_GetOrCreate_ReturnsCached(t *testing.T) {
	mgr := newPeerConnManager(nil)

	// Pre-cache a connection
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	cachedConn := connection.NewConn(clientConn)
	mgr.Add("testuser", cachedConn)

	// GetOrCreate should return the cached connection without dialing
	ctx := context.Background()
	conn, err := mgr.GetOrCreate(ctx, "testuser", "1.2.3.4:5678")
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}
	if conn != cachedConn {
		t.Error("expected GetOrCreate to return cached connection")
	}
}

func TestPeerConnManager_GetOrCreate_DirectConnection(t *testing.T) {
	dialer := &mockDialer{}
	defer dialer.cleanup()

	mgr := newPeerConnManager(nil)
	mgr.dialer = dialer.Dial
	mgr.ourUsername = testClientUsername

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := mgr.GetOrCreate(ctx, "testuser", "1.2.3.4:5678")
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}
	if conn == nil {
		t.Fatal("expected non-nil connection")
	}

	// Should be cached now
	cached, ok := mgr.GetCached("testuser")
	if !ok {
		t.Error("connection should be cached after GetOrCreate")
	}
	if cached != conn {
		t.Error("cached connection should match returned connection")
	}

	// Dialer should have been called
	dialer.mu.Lock()
	if dialer.dialCalls != 1 {
		t.Errorf("expected 1 dial call, got %d", dialer.dialCalls)
	}
	dialer.mu.Unlock()
}

func TestPeerConnManager_GetOrCreate_Deduplication(t *testing.T) {
	dialer := &mockDialer{delay: 100 * time.Millisecond}
	defer dialer.cleanup()

	mgr := newPeerConnManager(nil)
	mgr.dialer = dialer.Dial
	mgr.ourUsername = testClientUsername

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Launch multiple concurrent GetOrCreate calls for the same user
	type result struct {
		conn *connection.Conn
		err  error
	}
	results := make(chan result, 3)

	for range 3 {
		go func() {
			conn, err := mgr.GetOrCreate(ctx, "testuser", "1.2.3.4:5678")
			results <- result{conn: conn, err: err}
		}()
	}

	// Collect all results
	conns := make([]*connection.Conn, 0, 3)
	for range 3 {
		r := <-results
		if r.err != nil {
			t.Errorf("GetOrCreate failed: %v", r.err)
		}
		conns = append(conns, r.conn)
	}

	// All should get the same connection
	if len(conns) != 3 || conns[0] != conns[1] || conns[1] != conns[2] {
		t.Error("concurrent GetOrCreate calls should return same connection")
	}

	// Dialer should have been called only once
	dialer.mu.Lock()
	if dialer.dialCalls != 1 {
		t.Errorf("expected 1 dial call (deduplication), got %d", dialer.dialCalls)
	}
	dialer.mu.Unlock()
}

func TestPeerConnManager_GetOrCreate_DirectFails(t *testing.T) {
	dialer := &mockDialer{shouldFail: true}
	defer dialer.cleanup()

	mgr := newPeerConnManager(nil)
	mgr.dialer = dialer.Dial
	mgr.ourUsername = testClientUsername

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Without indirect support, direct failure should fail the whole operation
	_, err := mgr.GetOrCreate(ctx, "testuser", "1.2.3.4:5678")
	if err == nil {
		t.Error("expected error when direct connection fails")
	}

	// Should not be cached
	_, ok := mgr.GetCached("testuser")
	if ok {
		t.Error("failed connection should not be cached")
	}
}

func TestPeerConnManager_GetOrCreate_SendsPeerInit(t *testing.T) {
	// Create a mock peer server
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	var dialCalled atomic.Bool
	dialer := func(_ context.Context, _ string) (*connection.Conn, error) {
		dialCalled.Store(true)
		return connection.NewConn(clientConn), nil
	}

	mgr := newPeerConnManager(nil)
	mgr.dialer = dialer
	mgr.ourUsername = "myclient"

	// Read the PeerInit on the server side
	peerInitReceived := make(chan *peer.Init, 1)
	go func() {
		conn := connection.NewConn(serverConn)
		data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		init, err := peer.DecodeInit(data)
		if err != nil {
			return
		}
		peerInitReceived <- init
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := mgr.GetOrCreate(ctx, "targetuser", "1.2.3.4:5678")
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}

	// Check the PeerInit was sent correctly
	select {
	case init := <-peerInitReceived:
		if init.Username != "myclient" {
			t.Errorf("expected Username='myclient', got '%s'", init.Username)
		}
		if init.Type != "P" {
			t.Errorf("expected Type='P', got '%s'", init.Type)
		}
		// Token should be non-zero (generated)
		if init.Token == 0 {
			t.Error("expected non-zero token")
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for PeerInit")
	}
}

func TestPeerConnManager_GetOrCreate_DirectWinsRace(t *testing.T) {
	// Set up a dialer that succeeds quickly
	dialer := &mockDialer{delay: 10 * time.Millisecond}
	defer dialer.cleanup()

	// Create a mock client with listener but make indirect slow
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	client := &Client{
		conn:          connection.NewConn(clientConn),
		peerSolicits:  newPendingPeerSolicits(),
		solicitations: newPendingSolicitations(),
		transfers:     NewTransferRegistry(),
	}
	client.listener = newListener(client)

	mgr := newPeerConnManager(client)
	mgr.dialer = dialer.Dial
	mgr.ourUsername = testClientUsername

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Direct should win because indirect requires listener which we haven't started
	conn, err := mgr.GetOrCreate(ctx, "testuser", "1.2.3.4:5678")
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}
	if conn == nil {
		t.Fatal("expected non-nil connection")
	}

	// Should be cached
	cached, ok := mgr.GetCached("testuser")
	if !ok {
		t.Error("connection should be cached")
	}
	if cached != conn {
		t.Error("cached connection should match")
	}
}

func TestPeerConnManager_GetOrCreate_IndirectWinsWhenDirectFails(t *testing.T) {
	// Set up a dialer that fails
	dialer := &mockDialer{shouldFail: true}
	defer dialer.cleanup()

	// Create a mock client with working listener
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()

	client := &Client{
		conn:          connection.NewConn(clientConn),
		connected:     true, // Mark as connected so WriteMessage works
		peerSolicits:  newPendingPeerSolicits(),
		solicitations: newPendingSolicitations(),
		transfers:     NewTransferRegistry(),
	}
	client.listener = newListener(client)

	// Start listener on a random port
	err := client.listener.Start(0)
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer func() { _ = client.listener.Stop() }()

	mgr := newPeerConnManager(client)
	mgr.dialer = dialer.Dial
	mgr.ourUsername = testClientUsername

	// Consume the server message (ConnectToPeerRequest)
	go func() {
		sConn := connection.NewConn(serverConn)
		// Read the ConnectToPeerRequest that the manager sends
		_, _ = sConn.ReadMessage()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Simulate a peer connecting back via listener
	go func() {
		time.Sleep(50 * time.Millisecond) // Let the request be sent first

		// Find the solicitation token - we need to get it from pending
		// For this test, we'll just connect directly to the listener
		// and simulate a PierceFirewall handshake
		peerConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", client.listener.Port()))
		if err != nil {
			return
		}

		// We need to know the token... In real code, the peer gets this from the server.
		// For testing, we'll access the solicitations map.
		// Wait a bit for solicitation to be registered
		time.Sleep(20 * time.Millisecond)

		client.solicitations.mu.Lock()
		var token uint32
		for t := range client.solicitations.pending {
			token = t
			break
		}
		client.solicitations.mu.Unlock()

		if token == 0 {
			peerConn.Close()
			return
		}

		// Send PierceFirewall with the token
		var buf bytes.Buffer
		w := protocol.NewWriter(&buf)
		pf := &peer.PierceFirewall{Token: token}
		pf.Encode(w)
		if w.Error() != nil {
			peerConn.Close()
			return
		}

		conn := connection.NewConn(peerConn)
		if err := conn.WriteMessage(buf.Bytes()); err != nil {
			peerConn.Close()
			return
		}
		// Keep connection open
	}()

	conn, err := mgr.GetOrCreate(ctx, "testuser", "1.2.3.4:5678")
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}
	if conn == nil {
		t.Fatal("expected non-nil connection from indirect path")
	}

	// Should be cached
	cached, ok := mgr.GetCached("testuser")
	if !ok {
		t.Error("connection should be cached")
	}
	if cached != conn {
		t.Error("cached connection should match")
	}
}
