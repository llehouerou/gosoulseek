package client

import (
	"bytes"
	"encoding/binary"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/llehouerou/gosoulseek/connection"
	"github.com/llehouerou/gosoulseek/messages/peer"
	"github.com/llehouerou/gosoulseek/messages/server"
	"github.com/llehouerou/gosoulseek/protocol"
)

// TestDecodePierceFirewall tests parsing of PierceFirewall handshake messages.
func TestDecodePierceFirewall(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		want    *peer.PierceFirewall
		wantErr bool
	}{
		{
			name: "valid message",
			data: []byte{0x00, 0x01, 0x02, 0x03, 0x04}, // code=0, token=0x04030201
			want: &peer.PierceFirewall{Token: 0x04030201},
		},
		{
			name:    "too short",
			data:    []byte{0x00, 0x01, 0x02},
			wantErr: true,
		},
		{
			name:    "wrong code",
			data:    []byte{0x01, 0x01, 0x02, 0x03, 0x04},
			wantErr: true,
		},
		{
			name:    "empty",
			data:    []byte{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := peer.DecodePierceFirewall(tt.data)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want.Token, got.Token)
		})
	}
}

// TestDecodeInit tests parsing of PeerInit handshake messages.
func TestDecodeInit(t *testing.T) {
	// Helper to build a valid PeerInit message
	buildInit := func(username, connType string, token uint32) []byte {
		var buf bytes.Buffer
		w := protocol.NewWriter(&buf)
		w.WriteUint8(uint8(protocol.InitPeerInit))
		w.WriteString(username)
		w.WriteString(connType)
		w.WriteUint32(token)
		return buf.Bytes()
	}

	tests := []struct {
		name    string
		data    []byte
		want    *peer.Init
		wantErr bool
	}{
		{
			name: "P-type connection",
			data: buildInit("testuser", "P", 12345),
			want: &peer.Init{Username: "testuser", Type: "P", Token: 12345},
		},
		{
			name: "F-type connection",
			data: buildInit("uploader", "F", 67890),
			want: &peer.Init{Username: "uploader", Type: "F", Token: 67890},
		},
		{
			name: "D-type connection",
			data: buildInit("distributed", "D", 11111),
			want: &peer.Init{Username: "distributed", Type: "D", Token: 11111},
		},
		{
			name:    "wrong code",
			data:    []byte{0x00, 0x01, 0x02, 0x03},
			wantErr: true,
		},
		{
			name:    "empty",
			data:    []byte{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := peer.DecodeInit(tt.data)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want.Username, got.Username)
			assert.Equal(t, tt.want.Type, got.Type)
			assert.Equal(t, tt.want.Token, got.Token)
		})
	}
}

// TestListenerStartStop tests basic listener lifecycle.
func TestListenerStartStop(t *testing.T) {
	client := &Client{
		peerSolicits:  newPendingPeerSolicits(),
		solicitations: newPendingSolicitations(),
		downloads:     newDownloadRegistry(),
	}
	listener := newListener(client)

	// Start on random port
	err := listener.Start(0)
	require.NoError(t, err)
	assert.Greater(t, listener.Port(), 0)

	// Should be running
	assert.NotZero(t, listener.Port())

	// Stop
	err = listener.Stop()
	require.NoError(t, err)
	assert.Equal(t, 0, listener.Port())
}

// TestListenerDoubleStart tests that starting twice fails.
func TestListenerDoubleStart(t *testing.T) {
	client := &Client{
		peerSolicits:  newPendingPeerSolicits(),
		solicitations: newPendingSolicitations(),
		downloads:     newDownloadRegistry(),
	}
	listener := newListener(client)

	err := listener.Start(0)
	require.NoError(t, err)
	defer func() { _ = listener.Stop() }()

	err = listener.Start(0)
	assert.Error(t, err)
}

// TestListenerAcceptsConnection tests that the listener accepts a connection.
func TestListenerAcceptsConnection(t *testing.T) {
	client := &Client{
		peerSolicits:  newPendingPeerSolicits(),
		solicitations: newPendingSolicitations(),
		downloads:     newDownloadRegistry(),
	}
	listener := newListener(client)

	err := listener.Start(0)
	require.NoError(t, err)
	defer func() { _ = listener.Stop() }()

	// Connect to the listener
	conn, err := net.Dial("tcp", listener.listener.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	// Send a valid handshake (PierceFirewall with unknown token)
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	(&peer.PierceFirewall{Token: 99999}).Encode(w)
	require.NoError(t, w.Error())

	// Write with length prefix
	err = connection.NewConn(conn).WriteMessage(buf.Bytes())
	require.NoError(t, err)

	// Give the listener time to process and close (unknown token)
	time.Sleep(50 * time.Millisecond)

	// Verify connection was closed by listener (unknown token)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(100*time.Millisecond)))
	_, err = conn.Read(make([]byte, 1))
	assert.Error(t, err) // Should be closed
}

// TestPierceFirewallRouting tests routing of PierceFirewall connections.
func TestPierceFirewallRouting(t *testing.T) {
	client := &Client{
		peerSolicits:  newPendingPeerSolicits(),
		solicitations: newPendingSolicitations(),
		downloads:     newDownloadRegistry(),
	}
	listener := newListener(client)

	err := listener.Start(0)
	require.NoError(t, err)
	defer func() { _ = listener.Stop() }()

	token := uint32(12345)

	// Register pending solicitation
	ch := make(chan *connection.Conn, 1)
	client.solicitations.mu.Lock()
	client.solicitations.pending[token] = ch
	client.solicitations.mu.Unlock()

	// Connect and send PierceFirewall
	conn, err := net.Dial("tcp", listener.listener.Addr().String())
	require.NoError(t, err)

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	(&peer.PierceFirewall{Token: token}).Encode(w)
	require.NoError(t, w.Error())

	err = connection.NewConn(conn).WriteMessage(buf.Bytes())
	require.NoError(t, err)

	// Should receive connection on channel
	select {
	case received := <-ch:
		require.NotNil(t, received)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for connection delivery")
	}
}

// TestPeerInitPTypeRouting tests routing of P-type PeerInit connections.
func TestPeerInitPTypeRouting(t *testing.T) {
	// Create a minimal client with required fields
	client := &Client{
		peerSolicits:  newPendingPeerSolicits(),
		solicitations: newPendingSolicitations(),
		downloads:     newDownloadRegistry(),
	}
	client.peerConnMgr = newPeerConnManager(client)
	listener := newListener(client)

	err := listener.Start(0)
	require.NoError(t, err)
	defer func() { _ = listener.Stop() }()

	// Connect and send PeerInit P-type
	conn, err := net.Dial("tcp", listener.listener.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	(&peer.Init{Username: "testpeer", Type: "P", Token: 12345}).Encode(w)
	require.NoError(t, w.Error())

	err = connection.NewConn(conn).WriteMessage(buf.Bytes())
	require.NoError(t, err)

	// Give the listener time to process
	time.Sleep(50 * time.Millisecond)

	// Verify the connection is cached via the manager
	cached, ok := client.peerConnMgr.GetCached("testpeer")

	assert.True(t, ok)
	assert.NotNil(t, cached)
}

// TestPeerInitFTypeRouting tests routing of F-type PeerInit connections.
func TestPeerInitFTypeRouting(t *testing.T) {
	client := &Client{
		peerSolicits:  newPendingPeerSolicits(),
		solicitations: newPendingSolicitations(),
		downloads:     newDownloadRegistry(),
	}
	listener := newListener(client)

	err := listener.Start(0)
	require.NoError(t, err)
	defer func() { _ = listener.Stop() }()

	remoteToken := uint32(67890)
	username := "uploader"

	// Create a pending download that expects this transfer
	dl := &activeDownload{
		transferConnCh: make(chan *connection.Conn, 1),
	}

	// Register using the same key format as getByRemoteToken (username/token)
	client.downloads.mu.Lock()
	key := username + "/" + strconv.FormatUint(uint64(remoteToken), 10)
	client.downloads.byRemoteToken[key] = dl
	client.downloads.mu.Unlock()

	// Connect and send PeerInit F-type + remote token
	conn, err := net.Dial("tcp", listener.listener.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	wrappedConn := connection.NewConn(conn)

	// Send PeerInit
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	(&peer.Init{Username: username, Type: "F", Token: 12345}).Encode(w)
	require.NoError(t, w.Error())

	err = wrappedConn.WriteMessage(buf.Bytes())
	require.NoError(t, err)

	// Send remote token (4 bytes raw)
	var tokenBuf [4]byte
	binary.LittleEndian.PutUint32(tokenBuf[:], remoteToken)
	_, err = wrappedConn.Write(tokenBuf[:])
	require.NoError(t, err)

	// Should receive connection on download channel
	select {
	case received := <-dl.transferConnCh:
		require.NotNil(t, received)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for transfer connection delivery")
	}
}

// TestUnknownTokenRejection tests that unknown tokens are rejected.
func TestUnknownTokenRejection(t *testing.T) {
	client := &Client{
		peerSolicits:  newPendingPeerSolicits(),
		solicitations: newPendingSolicitations(),
		downloads:     newDownloadRegistry(),
	}
	listener := newListener(client)

	err := listener.Start(0)
	require.NoError(t, err)
	defer func() { _ = listener.Stop() }()

	// Connect and send PierceFirewall with unknown token
	conn, err := net.Dial("tcp", listener.listener.Addr().String())
	require.NoError(t, err)

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	(&peer.PierceFirewall{Token: 99999}).Encode(w)
	require.NoError(t, w.Error())

	err = connection.NewConn(conn).WriteMessage(buf.Bytes())
	require.NoError(t, err)

	// Give the listener time to process
	time.Sleep(50 * time.Millisecond)

	// Verify connection was closed
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(100*time.Millisecond)))
	_, err = conn.Read(make([]byte, 1))
	assert.Error(t, err)
}

// TestConcurrentConnections tests handling multiple connections simultaneously.
func TestConcurrentConnections(t *testing.T) {
	client := &Client{
		peerSolicits:  newPendingPeerSolicits(),
		solicitations: newPendingSolicitations(),
		downloads:     newDownloadRegistry(),
	}
	listener := newListener(client)

	err := listener.Start(0)
	require.NoError(t, err)
	defer func() { _ = listener.Stop() }()

	// Register multiple pending solicitations
	const numConns = 5
	channels := make([]chan *connection.Conn, numConns)
	tokens := make([]uint32, numConns)

	for i := range numConns {
		token := uint32(10000 + i) //nolint:gosec // test code, no overflow risk
		tokens[i] = token
		channels[i] = make(chan *connection.Conn, 1)

		client.solicitations.mu.Lock()
		client.solicitations.pending[token] = channels[i]
		client.solicitations.mu.Unlock()
	}

	// Connect all concurrently
	var wg sync.WaitGroup
	for i := range numConns {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			conn, dialErr := net.Dial("tcp", listener.listener.Addr().String())
			if dialErr != nil {
				return
			}

			var buf bytes.Buffer
			w := protocol.NewWriter(&buf)
			(&peer.PierceFirewall{Token: tokens[idx]}).Encode(w)

			_ = connection.NewConn(conn).WriteMessage(buf.Bytes())
		}(i)
	}

	wg.Wait()

	// Verify all connections were delivered
	for i, ch := range channels {
		select {
		case conn := <-ch:
			assert.NotNil(t, conn, "connection %d should be delivered", i)
		case <-time.After(time.Second):
			t.Errorf("timeout waiting for connection %d", i)
		}
	}
}

// TestPeerSolicitRouting tests routing via peerSolicits (P-type via ConnectToPeer).
func TestPeerSolicitRouting(t *testing.T) {
	client := &Client{
		peerSolicits:  newPendingPeerSolicits(),
		solicitations: newPendingSolicitations(),
		downloads:     newDownloadRegistry(),
	}
	client.peerConnMgr = newPeerConnManager(client)
	listener := newListener(client)

	err := listener.Start(0)
	require.NoError(t, err)
	defer func() { _ = listener.Stop() }()

	token := uint32(54321)
	username := "remotepeer"

	// Register as peer-solicited (ConnectToPeer P-type)
	client.peerSolicits.add(token, username, server.ConnectionTypePeer)

	// Connect and send PierceFirewall
	conn, err := net.Dial("tcp", listener.listener.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	(&peer.PierceFirewall{Token: token}).Encode(w)
	require.NoError(t, w.Error())

	err = connection.NewConn(conn).WriteMessage(buf.Bytes())
	require.NoError(t, err)

	// Give the listener time to process
	time.Sleep(50 * time.Millisecond)

	// Verify the connection is cached via the manager
	cached, ok := client.peerConnMgr.GetCached(username)

	assert.True(t, ok)
	assert.NotNil(t, cached)
}

// TestDTypeRejection tests that D-type connections are closed.
func TestDTypeRejection(t *testing.T) {
	client := &Client{
		peerSolicits:  newPendingPeerSolicits(),
		solicitations: newPendingSolicitations(),
		downloads:     newDownloadRegistry(),
	}
	listener := newListener(client)

	err := listener.Start(0)
	require.NoError(t, err)
	defer func() { _ = listener.Stop() }()

	// Connect and send PeerInit D-type
	conn, err := net.Dial("tcp", listener.listener.Addr().String())
	require.NoError(t, err)

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	(&peer.Init{Username: "distributed", Type: "D", Token: 11111}).Encode(w)
	require.NoError(t, w.Error())

	err = connection.NewConn(conn).WriteMessage(buf.Bytes())
	require.NoError(t, err)

	// Give the listener time to process
	time.Sleep(50 * time.Millisecond)

	// Verify connection was closed
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(100*time.Millisecond)))
	_, err = conn.Read(make([]byte, 1))
	assert.Error(t, err)
}

// TestPeerConnCaching tests that P-type connections are cached correctly via the manager.
// Detailed caching tests are in connmanager_test.go; this tests integration.
func TestPeerConnCaching(t *testing.T) {
	client := &Client{
		peerSolicits:  newPendingPeerSolicits(),
		solicitations: newPendingSolicitations(),
		downloads:     newDownloadRegistry(),
	}
	client.peerConnMgr = newPeerConnManager(client)

	// Test caching behavior through the manager
	server1, client1 := net.Pipe()
	defer server1.Close()
	conn1 := connection.NewConn(client1)

	server2, client2 := net.Pipe()
	defer server2.Close()
	conn2 := connection.NewConn(client2)

	// Add first connection
	superseded := client.peerConnMgr.Add("testuser", conn1)
	assert.Nil(t, superseded)

	// Verify it's cached
	cached, ok := client.peerConnMgr.GetCached("testuser")
	assert.True(t, ok)
	assert.Equal(t, conn1, cached)

	// Add second connection - should supersede first
	superseded = client.peerConnMgr.Add("testuser", conn2)
	assert.Equal(t, conn1, superseded)

	// Verify new connection is cached
	cached, ok = client.peerConnMgr.GetCached("testuser")
	assert.True(t, ok)
	assert.Equal(t, conn2, cached)

	// Invalidate
	client.peerConnMgr.Invalidate("testuser")

	// Now should not be cached
	_, ok = client.peerConnMgr.GetCached("testuser")
	assert.False(t, ok)
}
