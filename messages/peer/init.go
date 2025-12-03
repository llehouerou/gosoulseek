package peer

import (
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
