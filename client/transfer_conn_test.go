package client

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/llehouerou/gosoulseek/connection"
	"github.com/llehouerou/gosoulseek/messages/peer"
	"github.com/llehouerou/gosoulseek/protocol"
)

func TestNewTransferConnectionManager(t *testing.T) {
	client := &Client{}
	mgr := NewTransferConnectionManager(client)

	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
	if mgr.client != client {
		t.Error("expected manager to hold client reference")
	}
}

func TestTransferConnectionManager_ConnectDirect(t *testing.T) {
	// Set up a mock peer that accepts connections
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	peerAddr := listener.Addr().String()

	// Channel to receive what peer got
	type peerReceived struct {
		init  *peer.Init
		token uint32
	}
	receivedCh := make(chan peerReceived, 1)

	// Mock peer accepts and reads handshake
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}

		c := connection.NewConn(conn)

		// Read PeerInit message
		data, err := c.ReadMessage()
		if err != nil {
			conn.Close()
			return
		}

		init, err := peer.DecodeInit(data)
		if err != nil {
			conn.Close()
			return
		}

		// Read 4-byte token
		var tokenBuf [4]byte
		if _, err := conn.Read(tokenBuf[:]); err != nil {
			conn.Close()
			return
		}
		token := binary.LittleEndian.Uint32(tokenBuf[:])

		receivedCh <- peerReceived{init: init, token: token}

		// Keep connection open until test completes
		time.Sleep(500 * time.Millisecond)
		conn.Close()
	}()

	// Create client with username
	client := &Client{
		username: "testuser",
	}
	mgr := NewTransferConnectionManager(client)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Call connectDirect
	conn, err := mgr.connectDirect(ctx, peerAddr, 12345)
	if err != nil {
		t.Fatalf("connectDirect failed: %v", err)
	}
	defer conn.Close()

	// Verify what the peer received
	select {
	case received := <-receivedCh:
		if received.init.Username != "testuser" {
			t.Errorf("expected username 'testuser', got %q", received.init.Username)
		}
		if received.init.Type != "F" {
			t.Errorf("expected type 'F', got %q", received.init.Type)
		}
		if received.init.Token != 12345 {
			t.Errorf("expected token in init 12345, got %d", received.init.Token)
		}
		if received.token != 12345 {
			t.Errorf("expected raw token 12345, got %d", received.token)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for peer to receive data")
	}
}

func TestTransferConnectionManager_ConnectIndirect(t *testing.T) {
	// Create a mock server connection using net.Pipe
	serverConn, clientConn := net.Pipe()

	// Create mock peer connection using net.Pipe
	peerLocal, peerRemote := net.Pipe()

	// Create client with solicitations and a fake listener port
	client := &Client{
		username:  "testuser",
		conn:      connection.NewConn(clientConn),
		opts:      &Options{ListenPort: 12345}, // Fake port to pass check
		connected: true,                        // Mark as connected so WriteMessage works
	}
	client.solicitations = &pendingSolicitations{
		pending: make(map[uint32]chan *connection.Conn),
	}
	client.listener = &Listener{port: 12345} // Fake listener with port set

	mgr := NewTransferConnectionManager(client)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Channels for coordination
	type result struct {
		conn *connection.Conn
		err  error
	}
	resultCh := make(chan result, 1)
	serverMsgCh := make(chan []byte, 1)
	serverErrCh := make(chan error, 1)

	// Start server reader goroutine FIRST
	go func() {
		srvConn := connection.NewConn(serverConn)
		msgData, err := srvConn.ReadMessage()
		if err != nil {
			serverErrCh <- err
			return
		}
		serverMsgCh <- msgData
	}()

	// Then start connectIndirect
	go func() {
		conn, err := mgr.connectIndirect(ctx, "remotepeer", 12345)
		resultCh <- result{conn, err}
	}()

	// Wait for server to receive message
	var msgData []byte
	select {
	case msgData = <-serverMsgCh:
	case err := <-serverErrCh:
		t.Fatalf("server read error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ConnectToPeerRequest")
	}

	// Verify message code is ConnectToPeer (18)
	if len(msgData) < 1 || msgData[0] != 18 {
		t.Fatalf("expected ConnectToPeer message (code 18), got code %d", msgData[0])
	}

	// Find the solicitation channel
	var connCh chan *connection.Conn
	for range 50 {
		client.solicitations.mu.Lock()
		for _, ch := range client.solicitations.pending {
			connCh = ch
			break
		}
		client.solicitations.mu.Unlock()
		if connCh != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if connCh == nil {
		t.Fatal("no solicitation channel found")
	}

	// Start a goroutine to send the token on the peer side
	go func() {
		var tokenBuf [4]byte
		binary.LittleEndian.PutUint32(tokenBuf[:], 12345)
		_, _ = peerRemote.Write(tokenBuf[:])
	}()

	// Deliver the connection to the solicitation channel (simulating listener behavior)
	connCh <- connection.NewConn(peerLocal)

	// Wait for result
	select {
	case r := <-resultCh:
		if r.err != nil {
			t.Fatalf("connectIndirect failed: %v", r.err)
		}
		if r.conn == nil {
			t.Fatal("expected non-nil connection")
		}
		r.conn.Close()
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for connectIndirect")
	}

	// Cleanup
	serverConn.Close()
	clientConn.Close()
	peerLocal.Close()
	peerRemote.Close()
}

func TestTransferConnectionManager_GetConnection_DirectWins(t *testing.T) {
	// Set up a mock peer that accepts direct connections
	peerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create peer listener: %v", err)
	}
	defer peerListener.Close()

	peerAddr := peerListener.Addr().String()

	// Mock peer accepts connection and keeps it open
	go func() {
		conn, err := peerListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		c := connection.NewConn(conn)

		// Read and discard the PeerInit and token
		_, _ = c.ReadMessage()
		var tokenBuf [4]byte
		_, _ = conn.Read(tokenBuf[:])

		// Keep connection open
		time.Sleep(500 * time.Millisecond)
	}()

	// Create mock server (for indirect path - which should lose)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	// Server just reads messages and doesn't respond (indirect will timeout/lose)
	go func() {
		srvConn := connection.NewConn(serverConn)
		for {
			if _, err := srvConn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// Create client
	client := &Client{
		username:  "testuser",
		conn:      connection.NewConn(clientConn),
		opts:      &Options{ListenPort: 12345},
		connected: true,
	}
	client.solicitations = &pendingSolicitations{
		pending: make(map[uint32]chan *connection.Conn),
	}
	client.listener = &Listener{port: 12345}

	mgr := NewTransferConnectionManager(client)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// GetConnection should succeed via direct path
	conn, err := mgr.GetConnection(ctx, "remotepeer", peerAddr, 12345)
	if err != nil {
		t.Fatalf("GetConnection failed: %v", err)
	}
	if conn == nil {
		t.Fatal("expected non-nil connection")
	}
	conn.Close()
}

func TestTransferConnectionManager_AwaitConnection_InboundSuccess(t *testing.T) {
	// Create mock peer connection using net.Pipe
	peerLocal, peerRemote := net.Pipe()
	defer peerLocal.Close()
	defer peerRemote.Close()

	// Create mock server connection (for indirect path)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	// Server just reads and ignores
	go func() {
		srvConn := connection.NewConn(serverConn)
		for {
			if _, err := srvConn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// Create client with needed fields for all strategies
	client := &Client{
		username:  "testuser",
		conn:      connection.NewConn(clientConn),
		opts:      &Options{ListenPort: 12345},
		connected: true,
	}
	client.solicitations = &pendingSolicitations{
		pending: make(map[uint32]chan *connection.Conn),
	}
	client.listener = &Listener{port: 12345}

	// Create a transfer with initialized channels
	transfer := &Transfer{
		Username:    "remotepeer",
		Filename:    "test.mp3",
		Token:       11111,
		RemoteToken: 22222,
	}
	transfer.InitDownloadChannels()

	mgr := NewTransferConnectionManager(client)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run AwaitConnection in a goroutine
	type result struct {
		conn *connection.Conn
		err  error
	}
	resultCh := make(chan result, 1)

	go func() {
		conn, err := mgr.AwaitConnection(ctx, transfer, "127.0.0.1:12345")
		resultCh <- result{conn, err}
	}()

	// Give strategies time to start
	time.Sleep(50 * time.Millisecond)

	// Simulate listener delivering connection via transfer's channel (inbound wins)
	transfer.TransferConnCh() <- connection.NewConn(peerLocal)

	// Wait for result
	select {
	case r := <-resultCh:
		if r.err != nil {
			t.Fatalf("AwaitConnection failed: %v", r.err)
		}
		if r.conn == nil {
			t.Fatal("expected non-nil connection")
		}
		r.conn.Close()
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for AwaitConnection")
	}
}

// Silence unused import warnings
var _ = protocol.NewWriter

// bytes is needed for the indirect test
var _ bytes.Buffer
