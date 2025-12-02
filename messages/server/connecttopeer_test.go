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

func TestDecodeConnectToPeer(t *testing.T) {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)

	// Code
	w.WriteUint32(uint32(protocol.ServerConnectToPeer))
	// Username
	w.WriteString("testuser")
	// Type
	w.WriteString("P")
	// IP address (192.168.1.100) - sent in reverse byte order (little-endian)
	w.WriteUint8(100)
	w.WriteUint8(1)
	w.WriteUint8(168)
	w.WriteUint8(192)
	// Port
	w.WriteUint32(2234)
	// Token
	w.WriteUint32(12345)
	// Is privileged
	w.WriteUint8(1)

	require.NoError(t, w.Error())

	r := protocol.NewReader(bytes.NewReader(buf.Bytes()))
	msg, err := server.DecodeConnectToPeer(r)
	require.NoError(t, err)

	assert.Equal(t, "testuser", msg.Username)
	assert.Equal(t, server.ConnectionTypePeer, msg.Type)
	assert.Equal(t, net.ParseIP("192.168.1.100").To4(), msg.IPAddress)
	assert.Equal(t, uint32(2234), msg.Port)
	assert.Equal(t, uint32(12345), msg.Token)
	assert.True(t, msg.IsPrivileged)
}

func TestDecodeConnectToPeer_Transfer(t *testing.T) {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)

	w.WriteUint32(uint32(protocol.ServerConnectToPeer))
	w.WriteString("uploader")
	w.WriteString("F")
	// IP 10.0.0.1 in reverse order
	w.WriteUint8(1)
	w.WriteUint8(0)
	w.WriteUint8(0)
	w.WriteUint8(10)
	w.WriteUint32(6000)
	w.WriteUint32(99999)
	w.WriteUint8(0)

	require.NoError(t, w.Error())

	r := protocol.NewReader(bytes.NewReader(buf.Bytes()))
	msg, err := server.DecodeConnectToPeer(r)
	require.NoError(t, err)

	assert.Equal(t, "uploader", msg.Username)
	assert.Equal(t, server.ConnectionTypeTransfer, msg.Type)
	assert.Equal(t, net.ParseIP("10.0.0.1").To4(), msg.IPAddress)
	assert.Equal(t, uint32(6000), msg.Port)
	assert.Equal(t, uint32(99999), msg.Token)
	assert.False(t, msg.IsPrivileged)
}

func TestDecodeConnectToPeer_Distributed(t *testing.T) {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)

	w.WriteUint32(uint32(protocol.ServerConnectToPeer))
	w.WriteString("distributed_peer")
	w.WriteString("D")
	// IP 172.16.0.50 in reverse order
	w.WriteUint8(50)
	w.WriteUint8(0)
	w.WriteUint8(16)
	w.WriteUint8(172)
	w.WriteUint32(5555)
	w.WriteUint32(777)
	w.WriteUint8(1)

	require.NoError(t, w.Error())

	r := protocol.NewReader(bytes.NewReader(buf.Bytes()))
	msg, err := server.DecodeConnectToPeer(r)
	require.NoError(t, err)

	assert.Equal(t, "distributed_peer", msg.Username)
	assert.Equal(t, server.ConnectionTypeDistributed, msg.Type)
	assert.Equal(t, net.ParseIP("172.16.0.50").To4(), msg.IPAddress)
}

func TestDecodeConnectToPeer_WrongCode(t *testing.T) {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	w.WriteUint32(999) // Wrong code

	r := protocol.NewReader(bytes.NewReader(buf.Bytes()))
	_, err := server.DecodeConnectToPeer(r)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected code")
}
