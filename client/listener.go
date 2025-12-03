package client

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/llehouerou/gosoulseek/connection"
	"github.com/llehouerou/gosoulseek/messages/peer"
	"github.com/llehouerou/gosoulseek/messages/server"
	"github.com/llehouerou/gosoulseek/protocol"
)

const (
	handshakeTimeout = 10 * time.Second
)

// Listener handles incoming peer connections.
// It accepts TCP connections, parses handshakes, and routes them appropriately.
type Listener struct {
	client   *Client
	listener net.Listener
	port     int
	running  bool
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc

	// P-type connection cache (one per username)
	peerConns   map[string]*connection.Conn
	peerConnsMu sync.RWMutex
}

// newListener creates a new listener for the client.
func newListener(client *Client) *Listener {
	return &Listener{
		client:    client,
		peerConns: make(map[string]*connection.Conn),
	}
}

// Start begins accepting incoming connections on the specified port.
func (l *Listener) Start(port uint32) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.running {
		return errors.New("listener already running")
	}

	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	// Get the actual port (useful if port was 0)
	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		listener.Close()
		return errors.New("listener address is not TCP")
	}
	l.port = tcpAddr.Port
	l.listener = listener
	l.running = true

	l.ctx, l.cancel = context.WithCancel(context.Background())

	go l.acceptLoop()

	return nil
}

// Stop stops accepting connections and closes the listener.
func (l *Listener) Stop() error {
	l.mu.Lock()

	if !l.running {
		l.mu.Unlock()
		return nil
	}

	l.running = false
	l.cancel()

	err := l.listener.Close()
	l.listener = nil
	l.port = 0

	l.mu.Unlock()

	// Close all cached peer connections
	l.peerConnsMu.Lock()
	for username, conn := range l.peerConns {
		conn.Close()
		delete(l.peerConns, username)
	}
	l.peerConnsMu.Unlock()

	return err
}

// Port returns the port the listener is bound to, or 0 if not running.
func (l *Listener) Port() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.port
}

// acceptLoop continuously accepts incoming connections.
func (l *Listener) acceptLoop() {
	for {
		select {
		case <-l.ctx.Done():
			return
		default:
		}

		conn, err := l.listener.Accept()
		if err != nil {
			// Check if we're shutting down
			select {
			case <-l.ctx.Done():
				return
			default:
				// Accept failed but we're not shutting down
				continue
			}
		}

		go l.handleConnection(conn)
	}
}

// handleConnection processes a single incoming connection.
// It reads the handshake and routes the connection appropriately.
func (l *Listener) handleConnection(netConn net.Conn) {
	// Set deadline for handshake
	if err := netConn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		netConn.Close()
		return
	}

	conn := connection.NewConn(netConn)

	// Read the handshake message
	data, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return
	}

	// Clear deadline after handshake
	if err := netConn.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		return
	}

	if len(data) < 1 {
		conn.Close()
		return
	}

	// Route based on init code
	code := data[0]
	switch code {
	case uint8(protocol.InitPierceFirewall):
		pf, err := peer.DecodePierceFirewall(data)
		if err != nil {
			conn.Close()
			return
		}
		l.handlePierceFirewall(conn, pf.Token)

	case uint8(protocol.InitPeerInit):
		init, err := peer.DecodeInit(data)
		if err != nil {
			conn.Close()
			return
		}
		l.handlePeerInit(conn, init)

	default:
		conn.Close()
	}
}

// handlePierceFirewall routes a PierceFirewall connection.
// This is a solicited connection - the peer is responding to our ConnectToPeer request.
func (l *Listener) handlePierceFirewall(conn *connection.Conn, token uint32) {
	// 1. Check peerSolicits for this token (peer-initiated via server)
	solicit, ok := l.client.peerSolicits.get(token)
	if ok {
		switch solicit.connType {
		case server.ConnectionTypePeer:
			// P-type: cache and handle messages
			cached := l.getOrCachePeerConn(solicit.username, conn)
			if cached == conn {
				go l.client.handleIncomingPeerMessages(conn, solicit.username)
			}
		case server.ConnectionTypeTransfer:
			// F-type: hand off to download flow
			l.handleTransferConnection(conn, solicit.username)
		case server.ConnectionTypeDistributed:
			// D-type: not implemented
			conn.Close()
		}
		return
	}

	// 2. Check solicitations (our outbound requests)
	if l.client.solicitations.complete(token, conn) {
		return
	}

	// Unknown token - close
	conn.Close()
}

// handlePeerInit routes a PeerInit connection.
// This is an unsolicited connection - the peer is initiating directly.
func (l *Listener) handlePeerInit(conn *connection.Conn, init *peer.Init) {
	switch init.Type {
	case "P":
		// P-type: cache and handle messages
		cached := l.getOrCachePeerConn(init.Username, conn)
		if cached == conn {
			go l.client.handleIncomingPeerMessages(conn, init.Username)
		}
	case "F":
		// F-type: hand off to download flow
		l.handleTransferConnection(conn, init.Username)
	default:
		// D-type or unknown: close
		conn.Close()
	}
}

// handleTransferConnection hands off a transfer connection to the download flow.
func (l *Listener) handleTransferConnection(conn *connection.Conn, username string) {
	// For F-type connections, the peer sends their remote token next (4 bytes raw).
	// This token identifies the specific transfer.

	if err := conn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		conn.Close()
		return
	}

	var tokenBuf [4]byte
	if _, err := conn.Read(tokenBuf[:]); err != nil {
		conn.Close()
		return
	}

	if err := conn.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		return
	}

	remoteToken := binary.LittleEndian.Uint32(tokenBuf[:])

	// Find the matching download
	dl := l.client.downloads.getByRemoteToken(username, remoteToken)
	if dl == nil {
		conn.Close()
		return
	}

	// Deliver connection to the download
	select {
	case dl.transferConnCh <- conn:
		// Connection handed off successfully
	default:
		// Channel full or closed
		conn.Close()
	}
}

// getOrCachePeerConn caches a P-type connection for the given username.
// If a connection already exists, the new one is closed and the existing one is returned.
func (l *Listener) getOrCachePeerConn(username string, conn *connection.Conn) *connection.Conn {
	l.peerConnsMu.Lock()
	defer l.peerConnsMu.Unlock()

	if existing, ok := l.peerConns[username]; ok {
		// Already have a connection - close the new one
		conn.Close()
		return existing
	}

	l.peerConns[username] = conn
	return conn
}

// invalidatePeerConn removes a cached peer connection.
// Called when a connection is closed or fails.
func (l *Listener) invalidatePeerConn(username string) {
	l.peerConnsMu.Lock()
	delete(l.peerConns, username)
	l.peerConnsMu.Unlock()
}
