package peer

import (
	"github.com/llehouerou/gosoulseek/protocol"
)

// Code identifies a peer message type.
type Code uint32

const (
	// CodeSearchResponse is the code for search response messages.
	CodeSearchResponse Code = 9
)

// Message is the interface for all peer messages.
type Message interface {
	// Code returns the peer message code.
	Code() protocol.PeerCode
}

// Encoder can encode itself to a protocol writer.
type Encoder interface {
	Message
	// Encode writes the message to w.
	Encode(w *protocol.Writer)
}
