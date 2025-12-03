package client

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/llehouerou/gosoulseek/connection"
	"github.com/llehouerou/gosoulseek/messages/peer"
	"github.com/llehouerou/gosoulseek/messages/server"
	"github.com/llehouerou/gosoulseek/protocol"
)

// handleConnectToPeer processes ConnectToPeer messages from the server.
// These messages tell us to connect to a peer who has something for us.
// The peer solicited this connection - they asked the server to tell us to connect.
// But the peer might also connect to us directly via PierceFirewall.
func (c *Client) handleConnectToPeer(_ uint32, payload []byte) {
	r := protocol.NewReader(bytes.NewReader(payload))
	msg, err := server.DecodeConnectToPeer(r)
	if err != nil {
		return
	}

	fmt.Printf("[DEBUG] handleConnectToPeer: type=%s, username=%s, token=%d, ip=%s, port=%d\n",
		msg.Type, msg.Username, msg.Token, msg.IPAddress, msg.Port)

	// Register this as a pending peer-solicited connection.
	// If the peer connects to us via PierceFirewall before we connect to them,
	// we'll know what type of connection it is.
	c.peerSolicits.add(msg.Token, msg.Username, msg.Type)

	switch msg.Type {
	case server.ConnectionTypePeer:
		// Peer message connection (search results, etc.)
		go c.connectToPeer(msg)
	case server.ConnectionTypeTransfer:
		// Transfer connection - peer wants to send us a file
		fmt.Printf("[DEBUG] handleConnectToPeer: starting connectToPeerForTransfer\n")
		go c.connectToPeerForTransfer(msg)
	case server.ConnectionTypeDistributed:
		// Distributed network - not implemented yet
	}
}

// connectToPeerForTransfer handles indirect transfer connections.
// This is called when the server sends ConnectToPeer with type "F" (transfer).
// The peer couldn't connect to us directly, so we connect to them instead.
func (c *Client) connectToPeerForTransfer(msg *server.ConnectToPeer) {
	fmt.Printf("[DEBUG] connectToPeerForTransfer: connecting to %s:%d for user %s with token %d\n",
		msg.IPAddress, msg.Port, msg.Username, msg.Token)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	addr := fmt.Sprintf("%s:%d", msg.IPAddress, msg.Port)

	conn, err := connection.Dial(ctx, addr)
	if err != nil {
		fmt.Printf("[DEBUG] connectToPeerForTransfer: dial failed: %v\n", err)
		return
	}
	fmt.Printf("[DEBUG] connectToPeerForTransfer: connected\n")

	// Send PierceFirewall message with the token
	// This tells the peer which transfer this connection is for
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	pf := &peer.PierceFirewall{Token: msg.Token}
	pf.Encode(w)
	if err := w.Error(); err != nil {
		conn.Close()
		return
	}

	if err := conn.WriteMessage(buf.Bytes()); err != nil {
		conn.Close()
		return
	}

	// After PierceFirewall, peer sends their remote token as 4 bytes
	var tokenBuf [4]byte
	if _, err := conn.Read(tokenBuf[:]); err != nil {
		fmt.Printf("[DEBUG] connectToPeerForTransfer: read token failed: %v\n", err)
		conn.Close()
		return
	}
	remoteToken := binary.LittleEndian.Uint32(tokenBuf[:])
	fmt.Printf("[DEBUG] connectToPeerForTransfer: received remoteToken=%d\n", remoteToken)

	// Find the matching download using the remote token
	dl := c.downloads.getByRemoteToken(msg.Username, remoteToken)
	if dl == nil {
		fmt.Printf("[DEBUG] connectToPeerForTransfer: no matching download for username=%s, remoteToken=%d\n", msg.Username, remoteToken)
		conn.Close()
		return
	}
	fmt.Printf("[DEBUG] connectToPeerForTransfer: found matching download, handing off connection\n")

	// NOTE: Do NOT send token back! The protocol does not expect a token echo.
	// The next data the uploader expects is the 8-byte StartOffset from the downloader.

	// Deliver connection to the download
	select {
	case dl.transferConnCh <- conn:
		// Connection handed off successfully - don't close it
		fmt.Printf("[DEBUG] connectToPeerForTransfer: connection handed off successfully\n")
	default:
		// Channel full or closed - download may have been cancelled
		fmt.Printf("[DEBUG] connectToPeerForTransfer: channel full or closed\n")
		conn.Close()
	}
}

// connectToPeer establishes a connection to a peer and receives messages.
func (c *Client) connectToPeer(msg *server.ConnectToPeer) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	addr := fmt.Sprintf("%s:%d", msg.IPAddress, msg.Port)

	conn, err := connection.Dial(ctx, addr)
	if err != nil {
		// Connection failed - peer might be behind NAT or firewall
		return
	}
	defer conn.Close()

	// Send PeerInit message
	c.mu.Lock()
	username := c.username
	c.mu.Unlock()

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	init := &peer.Init{
		Username: username,
		Type:     string(msg.Type),
		Token:    msg.Token,
	}
	init.Encode(w)
	if err := w.Error(); err != nil {
		return
	}

	if err := conn.WriteMessage(buf.Bytes()); err != nil {
		return
	}

	// Read messages from peer
	// Set a read deadline to avoid hanging
	if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return
	}

	for {
		payload, err := conn.ReadMessage()
		if err != nil {
			return
		}

		if len(payload) < 4 {
			continue
		}

		code := binary.LittleEndian.Uint32(payload[:4])

		// Handle search response
		if code == uint32(protocol.PeerSearchResponse) {
			c.handleSearchResponse(payload)
			return // Search response received, close connection
		}

		// Other peer messages can be handled here in the future
	}
}
