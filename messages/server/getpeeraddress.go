package server

import (
	"fmt"
	"net"

	"github.com/llehouerou/gosoulseek/protocol"
)

// GetPeerAddress is a request to get a peer's IP address and port.
// Code 3.
type GetPeerAddress struct {
	Username string
}

// Code returns the server message code.
func (m *GetPeerAddress) Code() protocol.ServerCode {
	return protocol.ServerGetPeerAddress
}

// Encode writes the GetPeerAddress request.
func (m *GetPeerAddress) Encode(w *protocol.Writer) {
	w.WriteUint32(uint32(protocol.ServerGetPeerAddress))
	w.WriteString(m.Username)
}

// GetPeerAddressResponse is the server's response with a peer's address.
type GetPeerAddressResponse struct {
	Username  string
	IPAddress net.IP
	Port      uint32
}

// DecodeGetPeerAddress reads a GetPeerAddressResponse from the reader.
func DecodeGetPeerAddress(r *protocol.Reader) (*GetPeerAddressResponse, error) {
	code := r.ReadUint32()
	if code != uint32(protocol.ServerGetPeerAddress) {
		return nil, fmt.Errorf("unexpected code %d, expected %d", code, protocol.ServerGetPeerAddress)
	}

	resp := &GetPeerAddressResponse{
		Username: r.ReadString(),
	}

	// IP address is sent as 4 bytes in reverse order (little-endian)
	ipBytes := make([]byte, 4)
	ipBytes[3] = r.ReadUint8()
	ipBytes[2] = r.ReadUint8()
	ipBytes[1] = r.ReadUint8()
	ipBytes[0] = r.ReadUint8()
	resp.IPAddress = net.IP(ipBytes)

	resp.Port = r.ReadUint32()

	if err := r.Error(); err != nil {
		return nil, fmt.Errorf("decode get peer address: %w", err)
	}

	return resp, nil
}
