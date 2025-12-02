package server

import (
	"github.com/llehouerou/gosoulseek/protocol"
)

// UserPresence represents user online status.
type UserPresence uint32

const (
	// StatusOffline indicates the user is offline.
	StatusOffline UserPresence = 0
	// StatusAway indicates the user is away.
	StatusAway UserPresence = 1
	// StatusOnline indicates the user is online and available.
	StatusOnline UserPresence = 2
)

// String returns the string representation of the presence status.
func (p UserPresence) String() string {
	switch p {
	case StatusOffline:
		return "Offline"
	case StatusAway:
		return "Away"
	case StatusOnline:
		return "Online"
	default:
		return "Unknown"
	}
}

// SetOnlineStatus sets the user's presence status.
type SetOnlineStatus struct {
	Status UserPresence
}

// Code returns the server message code.
func (m *SetOnlineStatus) Code() protocol.ServerCode {
	return protocol.ServerSetOnlineStatus
}

// Encode writes the SetOnlineStatus message.
func (m *SetOnlineStatus) Encode(w *protocol.Writer) {
	w.WriteUint32(uint32(protocol.ServerSetOnlineStatus))
	w.WriteUint32(uint32(m.Status))
}
