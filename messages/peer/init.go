package peer

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/llehouerou/gosoulseek/protocol"
)

// Init is sent as the first message when connecting to a peer.
// It identifies us and the type of connection.
type Init struct {
	Username string // Our username
	Type     string // Connection type: "P" for peer, "F" for transfer, "D" for distributed
	Token    uint32 // Token from ConnectToPeer or our own solicitation token
}

// Encode writes the Init message.
func (m *Init) Encode(w *protocol.Writer) {
	w.WriteUint8(uint8(protocol.InitPeerInit)) // 1-byte init code
	w.WriteString(m.Username)
	w.WriteString(m.Type)
	w.WriteUint32(m.Token)
}

// DecodeInit parses a PeerInit handshake message.
// Format: [1-byte code=1][string username][string type][4-byte token]
// The message should NOT include the 4-byte length prefix.
func DecodeInit(data []byte) (*Init, error) {
	if len(data) < 1 {
		return nil, errors.New("empty init message")
	}

	code := data[0]
	if code != uint8(protocol.InitPeerInit) {
		return nil, fmt.Errorf("unexpected init code: %d (expected %d)", code, protocol.InitPeerInit)
	}

	r := protocol.NewReader(bytes.NewReader(data[1:]))
	username := r.ReadString()
	connType := r.ReadString()
	token := r.ReadUint32()

	if err := r.Error(); err != nil {
		return nil, fmt.Errorf("decode init: %w", err)
	}

	return &Init{
		Username: username,
		Type:     connType,
		Token:    token,
	}, nil
}

// PierceFirewall is sent when connecting to a peer via the server's ConnectToPeer instruction.
// This is used for NAT traversal when direct connections fail.
type PierceFirewall struct {
	Token uint32 // Token from ConnectToPeer message
}

// Encode writes the PierceFirewall message.
func (m *PierceFirewall) Encode(w *protocol.Writer) {
	w.WriteUint8(uint8(protocol.InitPierceFirewall)) // 1-byte init code (0)
	w.WriteUint32(m.Token)
}

// DecodePierceFirewall parses a PierceFirewall handshake message.
// Format: [1-byte code=0][4-byte token]
// The message should NOT include the 4-byte length prefix.
func DecodePierceFirewall(data []byte) (*PierceFirewall, error) {
	if len(data) < 5 {
		return nil, fmt.Errorf("pierce firewall message too short: %d bytes (expected 5)", len(data))
	}

	code := data[0]
	if code != uint8(protocol.InitPierceFirewall) {
		return nil, fmt.Errorf("unexpected init code: %d (expected %d)", code, protocol.InitPierceFirewall)
	}

	r := protocol.NewReader(bytes.NewReader(data[1:]))
	token := r.ReadUint32()

	if err := r.Error(); err != nil {
		return nil, fmt.Errorf("decode pierce firewall: %w", err)
	}

	return &PierceFirewall{
		Token: token,
	}, nil
}
