package server

import (
	"fmt"
	"net"

	"github.com/llehouerou/gosoulseek/protocol"
)

// ConnectionType represents the type of peer connection.
type ConnectionType string

// Connection types.
const (
	ConnectionTypePeer        ConnectionType = "P" // Message/peer connection
	ConnectionTypeTransfer    ConnectionType = "F" // File transfer connection
	ConnectionTypeDistributed ConnectionType = "D" // Distributed network connection
)

// ConnectToPeer is sent by the server to instruct us to connect to a peer.
// This happens when a peer has search results for us or wants to send us a message.
type ConnectToPeer struct {
	Username     string
	Type         ConnectionType
	IPAddress    net.IP
	Port         uint32
	Token        uint32
	IsPrivileged bool
}

// DecodeConnectToPeer parses a ConnectToPeer message from the server.
func DecodeConnectToPeer(r *protocol.Reader) (*ConnectToPeer, error) {
	code := r.ReadUint32()
	if protocol.ServerCode(code) != protocol.ServerConnectToPeer {
		return nil, fmt.Errorf("unexpected code: %d", code)
	}

	username := r.ReadString()
	connType := r.ReadString()

	// IP address is in big-endian (network byte order)
	ipBytes := make([]byte, 4)
	for i := 3; i >= 0; i-- {
		ipBytes[i] = r.ReadUint8()
	}
	ip := net.IP(ipBytes)

	port := r.ReadUint32()
	token := r.ReadUint32()
	isPrivileged := r.ReadUint8() > 0

	if err := r.Error(); err != nil {
		return nil, fmt.Errorf("decode connect to peer: %w", err)
	}

	return &ConnectToPeer{
		Username:     username,
		Type:         ConnectionType(connType),
		IPAddress:    ip,
		Port:         port,
		Token:        token,
		IsPrivileged: isPrivileged,
	}, nil
}
