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

func TestGetPeerAddress_Encode(t *testing.T) {
	msg := &server.GetPeerAddress{
		Username: "testuser",
	}

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	msg.Encode(w)
	require.NoError(t, w.Error())

	r := protocol.NewReader(bytes.NewReader(buf.Bytes()))
	assert.Equal(t, uint32(3), r.ReadUint32()) // Code
	assert.Equal(t, "testuser", r.ReadString())
	require.NoError(t, r.Error())
}

func TestDecodeGetPeerAddress(t *testing.T) {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)

	// Code
	w.WriteUint32(uint32(protocol.ServerGetPeerAddress))
	// Username
	w.WriteString("remoteuser")
	// IP address (192.168.1.100) - sent in reverse byte order
	w.WriteUint8(100)
	w.WriteUint8(1)
	w.WriteUint8(168)
	w.WriteUint8(192)
	// Port
	w.WriteUint32(2234)

	require.NoError(t, w.Error())

	r := protocol.NewReader(bytes.NewReader(buf.Bytes()))
	resp, err := server.DecodeGetPeerAddress(r)
	require.NoError(t, err)

	assert.Equal(t, "remoteuser", resp.Username)
	assert.Equal(t, net.ParseIP("192.168.1.100").To4(), resp.IPAddress)
	assert.Equal(t, uint32(2234), resp.Port)
}

func TestDecodeGetPeerAddress_WrongCode(t *testing.T) {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	w.WriteUint32(999) // Wrong code

	r := protocol.NewReader(bytes.NewReader(buf.Bytes()))
	_, err := server.DecodeGetPeerAddress(r)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected code")
}

func TestGetPeerAddress_Code(t *testing.T) {
	msg := &server.GetPeerAddress{}
	assert.Equal(t, protocol.ServerGetPeerAddress, msg.Code())
}
