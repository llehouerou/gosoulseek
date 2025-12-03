package client

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/llehouerou/gosoulseek/connection"
	"github.com/llehouerou/gosoulseek/messages/peer"
	"github.com/llehouerou/gosoulseek/messages/server"
	"github.com/llehouerou/gosoulseek/protocol"
)

const (
	transferConnTimeout   = 30 * time.Second
	transferTokenTimeout  = 10 * time.Second
	transferRemoteTimeout = 30 * time.Second
)

// TransferConnectionManager handles F-type transfer connection establishment.
// It supports two roles:
// - Initiator (uploader): actively connects to peer via GetConnection
// - Receiver (downloader): waits for peer to connect via AwaitConnection
type TransferConnectionManager struct {
	client *Client
}

// NewTransferConnectionManager creates a new transfer connection manager.
func NewTransferConnectionManager(client *Client) *TransferConnectionManager {
	return &TransferConnectionManager{
		client: client,
	}
}

// connectDirect establishes a direct F-type connection to the peer.
// It sends PeerInit with type "F" and the token, then sends the raw token.
func (m *TransferConnectionManager) connectDirect(ctx context.Context, peerAddr string, token uint32) (*connection.Conn, error) {
	conn, err := connection.Dial(ctx, peerAddr)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}

	// Get username
	m.client.mu.Lock()
	username := m.client.username
	m.client.mu.Unlock()

	// Send PeerInit message
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	init := &peer.Init{
		Username: username,
		Type:     "F",
		Token:    token,
	}
	init.Encode(w)
	if err := w.Error(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("encode init: %w", err)
	}

	if err := conn.WriteMessage(buf.Bytes()); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send init: %w", err)
	}

	// Send raw token (4 bytes little-endian)
	var tokenBuf [4]byte
	binary.LittleEndian.PutUint32(tokenBuf[:], token)
	if _, err := conn.Write(tokenBuf[:]); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send token: %w", err)
	}

	return conn, nil
}

// Token counter for solicitation tokens
var transferSolicitToken uint32

// connectIndirect establishes an F-type connection via server relay.
// It sends ConnectToPeerRequest to the server, which tells the peer to connect to us.
// The peer then connects to our listener and we receive the connection.
func (m *TransferConnectionManager) connectIndirect(ctx context.Context, username string, expectedToken uint32) (*connection.Conn, error) {
	if m.client.ListenerPort() == 0 {
		return nil, errors.New("no listener running")
	}

	// Generate unique solicitation token
	solicitToken := atomic.AddUint32(&transferSolicitToken, 1)

	// Create channel to receive connection
	connCh := make(chan *connection.Conn, 1)

	// Register solicitation
	m.client.solicitations.mu.Lock()
	m.client.solicitations.pending[solicitToken] = connCh
	m.client.solicitations.mu.Unlock()

	// Ensure cleanup
	defer func() {
		m.client.solicitations.mu.Lock()
		delete(m.client.solicitations.pending, solicitToken)
		m.client.solicitations.mu.Unlock()
	}()

	// Send ConnectToPeerRequest to server
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	req := &server.ConnectToPeerRequest{
		Token:    solicitToken,
		Username: username,
		Type:     server.ConnectionTypeTransfer,
	}
	req.Encode(w)
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	if err := m.client.WriteMessage(buf.Bytes()); err != nil {
		return nil, fmt.Errorf("send connect request: %w", err)
	}

	// Wait for connection
	var conn *connection.Conn
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case conn = <-connCh:
		if conn == nil {
			return nil, errors.New("connection closed")
		}
	case <-time.After(transferConnTimeout):
		return nil, errors.New("timeout waiting for indirect connection")
	}

	// Read remote token from peer
	var tokenBuf [4]byte
	if err := conn.SetReadDeadline(time.Now().Add(transferTokenTimeout)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set deadline: %w", err)
	}
	if _, err := io.ReadFull(conn, tokenBuf[:]); err != nil {
		conn.Close()
		return nil, fmt.Errorf("read token: %w", err)
	}
	remoteToken := binary.LittleEndian.Uint32(tokenBuf[:])

	// Validate token if expected
	if expectedToken != 0 && remoteToken != expectedToken {
		conn.Close()
		return nil, fmt.Errorf("token mismatch: got %d, want %d", remoteToken, expectedToken)
	}

	// Clear deadline
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("clear deadline: %w", err)
	}

	return conn, nil
}

// GetConnection initiates a transfer connection to the peer (initiator/uploader role).
// Uses concurrent direct + indirect strategy, first successful connection wins.
// The token is sent to the peer to identify the transfer.
func (m *TransferConnectionManager) GetConnection(ctx context.Context, username, peerAddr string, token uint32) (*connection.Conn, error) {
	type result struct {
		conn   *connection.Conn
		method string
		err    error
	}
	resultCh := make(chan result, 2)

	// Create cancellable contexts for each strategy
	ctx1, cancel1 := context.WithCancel(ctx)
	ctx2, cancel2 := context.WithCancel(ctx)
	defer cancel1()
	defer cancel2()

	// Strategy 1: Direct connection
	go func() {
		conn, err := m.connectDirect(ctx1, peerAddr, token)
		select {
		case resultCh <- result{conn, "direct", err}:
		case <-ctx1.Done():
			if conn != nil {
				conn.Close()
			}
		}
	}()

	// Strategy 2: Indirect connection via server
	go func() {
		conn, err := m.connectIndirect(ctx2, username, token)
		select {
		case resultCh <- result{conn, "indirect", err}:
		case <-ctx2.Done():
			if conn != nil {
				conn.Close()
			}
		}
	}()

	// Wait for first success or all failures
	var errs []error
	for range 2 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case r := <-resultCh:
			if r.err == nil && r.conn != nil {
				// Success! Cancel other strategy and return
				cancel1()
				cancel2()
				return r.conn, nil
			}
			errs = append(errs, fmt.Errorf("%s: %w", r.method, r.err))
		}
	}

	return nil, errors.Join(errs...)
}

// AwaitConnection waits for a transfer connection from the peer (receiver/downloader role).
// Uses triple strategy: inbound (wait on listener), direct, and indirect - racing all three.
// The transfer must have RemoteToken set and TransferConnCh initialized.
func (m *TransferConnectionManager) AwaitConnection(ctx context.Context, transfer *Transfer, peerAddr string) (*connection.Conn, error) {
	type result struct {
		conn   *connection.Conn
		method string
		err    error
	}
	resultCh := make(chan result, 3)

	// Create cancellable contexts for each strategy
	ctx1, cancel1 := context.WithCancel(ctx)
	ctx2, cancel2 := context.WithCancel(ctx)
	ctx3, cancel3 := context.WithCancel(ctx)
	defer cancel1()
	defer cancel2()
	defer cancel3()

	// Strategy 1: Wait for inbound connection via listener
	go func() {
		conn, err := m.waitInbound(ctx1, transfer)
		select {
		case resultCh <- result{conn, "inbound", err}:
		case <-ctx1.Done():
			if conn != nil {
				conn.Close()
			}
		}
	}()

	// Strategy 2: Direct connection to peer
	go func() {
		conn, err := m.awaitDirect(ctx2, transfer, peerAddr)
		select {
		case resultCh <- result{conn, "direct", err}:
		case <-ctx2.Done():
			if conn != nil {
				conn.Close()
			}
		}
	}()

	// Strategy 3: Indirect connection via server
	go func() {
		conn, err := m.connectIndirect(ctx3, transfer.Username, transfer.RemoteToken)
		select {
		case resultCh <- result{conn, "indirect", err}:
		case <-ctx3.Done():
			if conn != nil {
				conn.Close()
			}
		}
	}()

	// Wait for first success or all failures
	var errs []error
	for range 3 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case r := <-resultCh:
			if r.err == nil && r.conn != nil {
				// Success! Cancel other strategies and return
				cancel1()
				cancel2()
				cancel3()
				return r.conn, nil
			}
			errs = append(errs, fmt.Errorf("%s: %w", r.method, r.err))
		}
	}

	return nil, errors.Join(errs...)
}

// waitInbound waits for the peer to connect to our listener.
// The connection will be delivered via the transfer's TransferConnCh.
func (m *TransferConnectionManager) waitInbound(ctx context.Context, transfer *Transfer) (*connection.Conn, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case conn := <-transfer.TransferConnCh():
		if conn == nil {
			return nil, errors.New("connection channel closed")
		}
		return conn, nil
	}
}

// awaitDirect connects directly to the peer using RemoteToken.
// It waits for RemoteToken to be set if not already available.
func (m *TransferConnectionManager) awaitDirect(ctx context.Context, transfer *Transfer, peerAddr string) (*connection.Conn, error) {
	// Wait for RemoteToken to be available
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(transferRemoteTimeout)
	for {
		transfer.mu.RLock()
		remoteToken := transfer.RemoteToken
		transfer.mu.RUnlock()

		if remoteToken != 0 {
			return m.connectDirect(ctx, peerAddr, remoteToken)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return nil, errors.New("timeout waiting for remote token")
		case <-ticker.C:
		}
	}
}
