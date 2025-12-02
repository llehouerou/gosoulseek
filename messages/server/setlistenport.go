package server

import (
	"github.com/llehouerou/gosoulseek/protocol"
)

// SetListenPort reports the port we're listening on for incoming peer connections.
// This message should be sent immediately after login to prevent a race condition
// where peers see port 0.
type SetListenPort struct {
	Port uint32
}

// Code returns the server message code.
func (m *SetListenPort) Code() protocol.ServerCode {
	return protocol.ServerSetListenPort
}

// Encode writes the SetListenPort message.
func (m *SetListenPort) Encode(w *protocol.Writer) {
	w.WriteUint32(uint32(protocol.ServerSetListenPort))
	w.WriteUint32(m.Port)
}
