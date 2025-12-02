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
// These messages tell us to connect to a peer who has something for us (e.g., search results).
func (c *Client) handleConnectToPeer(_ uint32, payload []byte) {
	r := protocol.NewReader(bytes.NewReader(payload))
	msg, err := server.DecodeConnectToPeer(r)
	if err != nil {
		return
	}

	// Only handle peer connections (type "P") for now
	// Type "F" is for transfers, "D" is for distributed network
	if msg.Type != server.ConnectionTypePeer {
		return
	}

	// Connect to the peer in a goroutine to avoid blocking the message loop
	go c.connectToPeer(msg)
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
