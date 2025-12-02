// Package server provides encoding and decoding for Soulseek server messages.
package server

import (
	"github.com/llehouerou/gosoulseek/protocol"
)

// Message is the interface for all server messages.
type Message interface {
	// Code returns the server message code.
	Code() protocol.ServerCode
}

// Encoder can encode itself to a protocol writer.
type Encoder interface {
	Message
	// Encode writes the message to w.
	Encode(w *protocol.Writer)
}
