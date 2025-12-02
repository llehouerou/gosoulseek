package server_test

import (
	"bytes"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/llehouerou/gosoulseek/messages/server"
	"github.com/llehouerou/gosoulseek/protocol"
)

func TestNewLoginRequest(t *testing.T) {
	req := server.NewLoginRequest("user", "pass")

	assert.Equal(t, "user", req.Username)
	assert.Equal(t, "pass", req.Password)
	assert.Equal(t, uint32(170), req.Version)
	assert.Equal(t, uint32(100), req.MinorVersion)
}

func TestLoginRequest_Code(t *testing.T) {
	req := server.NewLoginRequest("user", "pass")
	assert.Equal(t, protocol.ServerLogin, req.Code())
}

func TestLoginRequest_Encode(t *testing.T) {
	req := &server.LoginRequest{
		Username:     "testuser",
		Password:     "testpass",
		Version:      170,
		MinorVersion: 100,
	}

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	req.Encode(w)
	require.NoError(t, w.Error())

	// Verify by decoding the raw bytes
	r := protocol.NewReader(bytes.NewReader(buf.Bytes()))

	// Code
	assert.Equal(t, uint32(1), r.ReadUint32())
	// Username
	assert.Equal(t, "testuser", r.ReadString())
	// Password
	assert.Equal(t, "testpass", r.ReadString())
	// Version
	assert.Equal(t, uint32(170), r.ReadUint32())
	// Hash (MD5 of "testusertestpass")
	hash := r.ReadString()
	assert.Len(t, hash, 32) // MD5 hex is 32 chars
	// MinorVersion
	assert.Equal(t, uint32(100), r.ReadUint32())

	require.NoError(t, r.Error())
}

func TestDecodeLoginResponse_Success(t *testing.T) {
	// Build a successful login response
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	w.WriteUint32(1)                       // code: Login
	w.WriteUint8(1)                        // succeeded: true
	w.WriteString("Welcome to Soulseek!")  // greeting
	w.WriteBytes([]byte{192, 168, 1, 100}) // IP bytes (network order)
	w.WriteString("abc123hash")            // hash echo
	w.WriteUint8(1)                        // is supporter
	require.NoError(t, w.Error())

	resp, err := server.DecodeLoginResponse(protocol.NewReader(bytes.NewReader(buf.Bytes())))

	require.NoError(t, err)
	assert.True(t, resp.Succeeded)
	assert.Equal(t, "Welcome to Soulseek!", resp.Message)
	// IP should be reversed: 100.1.168.192
	assert.Equal(t, net.IPv4(100, 1, 168, 192).To4(), resp.IPAddress.To4())
	assert.Equal(t, "abc123hash", resp.Hash)
	assert.True(t, resp.IsSupporter)
}

func TestDecodeLoginResponse_Failure(t *testing.T) {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	w.WriteUint32(1)                  // code: Login
	w.WriteUint8(0)                   // succeeded: false
	w.WriteString("Invalid password") // error reason
	require.NoError(t, w.Error())

	resp, err := server.DecodeLoginResponse(protocol.NewReader(bytes.NewReader(buf.Bytes())))

	require.NoError(t, err)
	assert.False(t, resp.Succeeded)
	assert.Equal(t, "Invalid password", resp.Message)
	assert.Nil(t, resp.IPAddress)
	assert.Empty(t, resp.Hash)
	assert.False(t, resp.IsSupporter)
}

func TestDecodeLoginResponse_WrongCode(t *testing.T) {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	w.WriteUint32(99) // wrong code
	require.NoError(t, w.Error())

	_, err := server.DecodeLoginResponse(protocol.NewReader(bytes.NewReader(buf.Bytes())))

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected code")
}

func TestDecodeLoginResponse_TruncatedSuccess(t *testing.T) {
	// Success response but missing IP and other fields
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	w.WriteUint32(1) // code
	w.WriteUint8(1)  // succeeded
	w.WriteString("Welcome")
	// Missing: IP, hash, isSupporter
	require.NoError(t, w.Error())

	_, err := server.DecodeLoginResponse(protocol.NewReader(bytes.NewReader(buf.Bytes())))

	assert.Error(t, err)
}

func TestLoginRequest_Roundtrip(t *testing.T) {
	// This test verifies the encode format matches what DecodeLoginResponse expects
	// by building a mock response that echoes back the hash

	req := server.NewLoginRequest("alice", "secret123")

	// Encode the request to get the hash
	var reqBuf bytes.Buffer
	reqWriter := protocol.NewWriter(&reqBuf)
	req.Encode(reqWriter)
	require.NoError(t, reqWriter.Error())

	// Parse it back to extract the hash
	reqReader := protocol.NewReader(bytes.NewReader(reqBuf.Bytes()))
	_ = reqReader.ReadUint32() // code
	_ = reqReader.ReadString() // username
	_ = reqReader.ReadString() // password
	_ = reqReader.ReadUint32() // version
	sentHash := reqReader.ReadString()
	require.NoError(t, reqReader.Error())

	// Build a response that echoes back the hash
	var respBuf bytes.Buffer
	respWriter := protocol.NewWriter(&respBuf)
	respWriter.WriteUint32(1)
	respWriter.WriteUint8(1)
	respWriter.WriteString("Logged in")
	respWriter.WriteBytes([]byte{8, 8, 8, 8})
	respWriter.WriteString(sentHash)
	respWriter.WriteUint8(0)
	require.NoError(t, respWriter.Error())

	resp, err := server.DecodeLoginResponse(protocol.NewReader(bytes.NewReader(respBuf.Bytes())))
	require.NoError(t, err)

	assert.True(t, resp.Succeeded)
	assert.Equal(t, sentHash, resp.Hash)
}
