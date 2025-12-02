package server

import (
	"fmt"

	"github.com/llehouerou/gosoulseek/protocol"
)

// Ping is a keep-alive message. The server echoes pings back.
type Ping struct{}

// Code returns the server message code.
func (p *Ping) Code() protocol.ServerCode {
	return protocol.ServerPing
}

// Encode writes the ping message (just the code, no payload).
func (p *Ping) Encode(w *protocol.Writer) {
	w.WriteUint32(uint32(protocol.ServerPing))
}

// DecodePing verifies a ping response.
func DecodePing(r *protocol.Reader) error {
	code := protocol.ServerCode(r.ReadUint32())
	if code != protocol.ServerPing {
		return fmt.Errorf("unexpected code %d, want %d", code, protocol.ServerPing)
	}
	return r.Error()
}
