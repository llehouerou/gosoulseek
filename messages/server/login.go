package server

import (
	"crypto/md5" //nolint:gosec // MD5 required by Soulseek protocol
	"encoding/hex"
	"fmt"
	"net"

	"github.com/llehouerou/gosoulseek/protocol"
)

// LoginRequest authenticates with the Soulseek server.
type LoginRequest struct {
	Username     string
	Password     string
	Version      uint32 // Protocol version (default: 170)
	MinorVersion uint32 // Minor version (default: 100)
}

// NewLoginRequest creates a login request with default version numbers.
func NewLoginRequest(username, password string) *LoginRequest {
	return &LoginRequest{
		Username:     username,
		Password:     password,
		Version:      170,
		MinorVersion: 100,
	}
}

// Code returns the server message code for login.
func (r *LoginRequest) Code() protocol.ServerCode {
	return protocol.ServerLogin
}

// Encode writes the login request to w.
func (r *LoginRequest) Encode(w *protocol.Writer) {
	hash := computeMD5(r.Username + r.Password)
	w.WriteUint32(uint32(protocol.ServerLogin))
	w.WriteString(r.Username)
	w.WriteString(r.Password)
	w.WriteUint32(r.Version)
	w.WriteString(hash)
	w.WriteUint32(r.MinorVersion)
}

// LoginResponse is the server's reply to a login request.
type LoginResponse struct {
	Succeeded   bool
	Message     string // Greeting if succeeded, error reason if not
	IPAddress   net.IP // Client's public IP (only if succeeded)
	Hash        string // Echo of hash (only if succeeded)
	IsSupporter bool   // Has purchased privileges (only if succeeded)
}

// Code returns the server message code.
func (r *LoginResponse) Code() protocol.ServerCode {
	return protocol.ServerLogin
}

// DecodeLoginResponse parses a login response from r.
// The reader should be positioned at the start of the message payload
// (after the 4-byte length prefix has been consumed).
func DecodeLoginResponse(r *protocol.Reader) (*LoginResponse, error) {
	code := protocol.ServerCode(r.ReadUint32())
	if code != protocol.ServerLogin {
		return nil, fmt.Errorf("unexpected code %d, want %d", code, protocol.ServerLogin)
	}

	resp := &LoginResponse{
		Succeeded: r.ReadUint8() == 1,
		Message:   r.ReadString(),
	}

	if resp.Succeeded {
		// IP bytes are in network order, need to reverse for net.IP
		ipBytes := r.ReadBytes(4)
		if len(ipBytes) == 4 {
			resp.IPAddress = net.IPv4(ipBytes[3], ipBytes[2], ipBytes[1], ipBytes[0])
		}
		resp.Hash = r.ReadString()
		resp.IsSupporter = r.ReadUint8() == 1
	}

	if err := r.Error(); err != nil {
		return nil, fmt.Errorf("decode login response: %w", err)
	}
	return resp, nil
}

// computeMD5 returns the hex-encoded MD5 hash of s.
// Note: MD5 is required by the Soulseek protocol for authentication.
func computeMD5(s string) string {
	h := md5.Sum([]byte(s)) //nolint:gosec // MD5 required by Soulseek protocol
	return hex.EncodeToString(h[:])
}
