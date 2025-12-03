package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/llehouerou/gosoulseek/connection"
	"github.com/llehouerou/gosoulseek/messages/peer"
	"github.com/llehouerou/gosoulseek/messages/server"
	"github.com/llehouerou/gosoulseek/protocol"
)

// dialerFunc is the type for connection dialer functions.
type dialerFunc func(ctx context.Context, addr string) (*connection.Conn, error)

// pendingConn tracks an in-progress connection attempt.
// Multiple concurrent callers can wait on the same pending connection.
type pendingConn struct {
	ready chan struct{}    // Closed when connection is ready or failed
	conn  *connection.Conn // Set before closing ready
	err   error            // Set before closing ready
}

// peerConnManager manages P-type (message) connections to peers.
// It caches connections per username and handles connection lifecycle.
type peerConnManager struct {
	client      *Client
	mu          sync.RWMutex
	conns       map[string]*connection.Conn
	pending     map[string]*pendingConn
	dialer      dialerFunc // Configurable for testing
	ourUsername string     // Our username for PeerInit messages
}

// Token counter for PeerInit messages
var peerConnToken uint32

// newPeerConnManager creates a new peer connection manager.
func newPeerConnManager(client *Client) *peerConnManager {
	return &peerConnManager{
		client:  client,
		conns:   make(map[string]*connection.Conn),
		pending: make(map[string]*pendingConn),
		dialer:  connection.Dial, // Default to real dialer
	}
}

// GetCached returns a cached connection if one exists.
// Does not create new connections.
func (m *peerConnManager) GetCached(username string) (*connection.Conn, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	conn, ok := m.conns[username]
	return conn, ok
}

// Add caches a connection for the given username.
// If a connection already exists for this user, it is superseded (closed and replaced).
// Returns the superseded connection, or nil if none was superseded.
func (m *peerConnManager) Add(username string, conn *connection.Conn) *connection.Conn {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing := m.conns[username]
	if existing != nil {
		existing.Close()
	}

	m.conns[username] = conn
	return existing
}

// Invalidate removes a connection from the cache.
// Called when a connection is closed or fails.
func (m *peerConnManager) Invalidate(username string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.conns, username)
}

// Close closes all cached connections and clears the cache.
func (m *peerConnManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for username, conn := range m.conns {
		conn.Close()
		delete(m.conns, username)
	}

	return nil
}

// GetOrCreate returns an existing cached connection or creates a new one.
// If multiple goroutines call GetOrCreate for the same username concurrently,
// only one connection attempt is made and all callers receive the same result.
func (m *peerConnManager) GetOrCreate(ctx context.Context, username, addr string) (*connection.Conn, error) {
	conn, _, err := m.GetOrCreateEx(ctx, username, addr)
	return conn, err
}

// GetOrCreateEx is like GetOrCreate but also returns whether this is a newly created connection.
// isNew is true only for the caller that initiated the connection creation.
// Concurrent callers waiting on the same pending connection will receive isNew=false.
func (m *peerConnManager) GetOrCreateEx(ctx context.Context, username, addr string) (conn *connection.Conn, isNew bool, err error) {
	// Fast path: check cache first
	m.mu.RLock()
	if conn, ok := m.conns[username]; ok {
		m.mu.RUnlock()
		return conn, false, nil
	}
	m.mu.RUnlock()

	// Slow path: need to create connection
	return m.getOrCreateSlow(ctx, username, addr)
}

// getOrCreateSlow handles the slow path of connection creation with deduplication.
// Returns isNew=true only for the caller that initiated the connection creation.
func (m *peerConnManager) getOrCreateSlow(ctx context.Context, username, addr string) (*connection.Conn, bool, error) {
	m.mu.Lock()

	// Double-check cache under lock
	if conn, ok := m.conns[username]; ok {
		m.mu.Unlock()
		return conn, false, nil
	}

	// Check if there's already a pending connection attempt
	if p, ok := m.pending[username]; ok {
		m.mu.Unlock()
		// Wait for the pending connection - we're not the initiator
		conn, err := m.waitForPending(ctx, p)
		return conn, false, err
	}

	// We're the first - create a pending entry
	p := &pendingConn{
		ready: make(chan struct{}),
	}
	m.pending[username] = p
	m.mu.Unlock()

	// Do the connection attempt using parallel direct+indirect strategy
	conn, err := m.connect(ctx, username, addr)

	// Store results and signal completion
	m.mu.Lock()
	p.conn = conn
	p.err = err

	if err == nil && conn != nil {
		// Cache the successful connection
		m.conns[username] = conn
	}

	// Clean up pending entry
	delete(m.pending, username)
	m.mu.Unlock()

	// Signal all waiters
	close(p.ready)

	// We're the initiator - return isNew=true if successful
	return conn, err == nil && conn != nil, err
}

// connect attempts to establish a connection using parallel direct+indirect strategy.
// Both direct and indirect connections are attempted simultaneously.
// The first to succeed wins, and the other is cancelled.
func (m *peerConnManager) connect(ctx context.Context, username, addr string) (*connection.Conn, error) {
	type connResult struct {
		conn     *connection.Conn
		isDirect bool
		err      error
	}
	resultCh := make(chan connResult, 2)

	// Create cancellation contexts for each method
	directCtx, cancelDirect := context.WithCancel(ctx)
	defer cancelDirect()
	indirectCtx, cancelIndirect := context.WithCancel(ctx)
	defer cancelIndirect()

	// Attempt direct connection
	go func() {
		conn, err := m.connectDirect(directCtx, username, addr)
		select {
		case resultCh <- connResult{conn: conn, isDirect: true, err: err}:
		case <-directCtx.Done():
			if conn != nil {
				conn.Close()
			}
		}
	}()

	// Attempt indirect connection (only if we have a client with listener)
	go func() {
		conn, err := m.connectIndirect(indirectCtx, username)
		select {
		case resultCh <- connResult{conn: conn, isDirect: false, err: err}:
		case <-indirectCtx.Done():
			if conn != nil {
				conn.Close()
			}
		}
	}()

	// Wait for first successful connection
	var directErr, indirectErr error
	for range 2 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case result := <-resultCh:
			if result.err == nil && result.conn != nil {
				// Success! Cancel the other method
				if result.isDirect {
					cancelIndirect()
				} else {
					cancelDirect()
				}
				return result.conn, nil
			}
			// Track errors
			if result.isDirect {
				directErr = result.err
			} else {
				indirectErr = result.err
			}
		}
	}

	// Both failed
	if directErr != nil && indirectErr != nil {
		return nil, errors.Join(
			fmt.Errorf("direct: %w", directErr),
			fmt.Errorf("indirect: %w", indirectErr),
		)
	}
	if directErr != nil {
		return nil, fmt.Errorf("direct: %w", directErr)
	}
	return nil, fmt.Errorf("indirect: %w", indirectErr)
}

// waitForPending waits for a pending connection attempt to complete.
func (m *peerConnManager) waitForPending(ctx context.Context, p *pendingConn) (*connection.Conn, error) {
	select {
	case <-p.ready:
		return p.conn, p.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// connectDirect attempts a direct TCP connection to the peer.
func (m *peerConnManager) connectDirect(ctx context.Context, username, addr string) (*connection.Conn, error) {
	conn, err := m.dialer(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("dial peer %s at %s: %w", username, addr, err)
	}

	// Send PeerInit message
	if err := m.sendPeerInit(conn, username); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send PeerInit to %s: %w", username, err)
	}

	return conn, nil
}

// sendPeerInit sends a PeerInit message to establish the P-type connection.
func (m *peerConnManager) sendPeerInit(conn *connection.Conn, _ string) error {
	token := atomic.AddUint32(&peerConnToken, 1)

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	init := &peer.Init{
		Username: m.ourUsername,
		Type:     "P",
		Token:    token,
	}
	init.Encode(w)
	if err := w.Error(); err != nil {
		return err
	}

	return conn.WriteMessage(buf.Bytes())
}

// connectIndirect solicits a connection via the server.
// The server tells the peer to connect to us, and we receive the connection via our listener.
func (m *peerConnManager) connectIndirect(ctx context.Context, username string) (*connection.Conn, error) {
	// Need a client with a working listener for indirect connections
	if m.client == nil {
		return nil, errors.New("no client configured for indirect connections")
	}

	// Check if we have a listener running
	if m.client.ListenerPort() == 0 {
		return nil, errors.New("no listener running for indirect connections")
	}

	// Generate a solicitation token
	solicitToken := atomic.AddUint32(&peerConnToken, 1)

	// Create a channel to receive the incoming connection
	connCh := make(chan *connection.Conn, 1)

	// Register the pending solicitation
	m.client.solicitations.mu.Lock()
	m.client.solicitations.pending[solicitToken] = connCh
	m.client.solicitations.mu.Unlock()

	// Clean up on exit
	defer func() {
		m.client.solicitations.mu.Lock()
		delete(m.client.solicitations.pending, solicitToken)
		m.client.solicitations.mu.Unlock()
	}()

	// Send ConnectToPeerRequest to the server
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	req := &server.ConnectToPeerRequest{
		Token:    solicitToken,
		Username: username,
		Type:     server.ConnectionTypePeer,
	}
	req.Encode(w)
	if err := w.Error(); err != nil {
		return nil, err
	}

	if err := m.client.WriteMessage(buf.Bytes()); err != nil {
		return nil, fmt.Errorf("send connect request: %w", err)
	}

	// Wait for the peer to connect to us
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case conn := <-connCh:
		if conn == nil {
			return nil, errors.New("connection closed")
		}
		return conn, nil
	case <-time.After(30 * time.Second):
		return nil, errors.New("indirect connection timeout")
	}
}
