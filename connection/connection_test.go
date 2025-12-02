package connection_test

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/llehouerou/gosoulseek/connection"
)

func TestConn_WriteReadMessage(t *testing.T) {
	// Create connected pipe for testing
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	clientConn := connection.NewConn(client)
	serverConn := connection.NewConn(server)

	payload := []byte{0x01, 0x00, 0x00, 0x00, 'h', 'e', 'l', 'l', 'o'}

	// Write in goroutine to prevent blocking
	errCh := make(chan error, 1)
	go func() {
		errCh <- clientConn.WriteMessage(payload)
	}()

	// Read from server side
	got, err := serverConn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, payload, got)

	// Check write completed without error
	require.NoError(t, <-errCh)
}

func TestConn_WriteMessage_Format(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	clientConn := connection.NewConn(client)

	payload := []byte("hello")

	// Write in goroutine
	go func() {
		_ = clientConn.WriteMessage(payload)
	}()

	// Read raw bytes to verify format
	buf := make([]byte, 9) // 4 bytes length + 5 bytes payload
	_, err := server.Read(buf)
	require.NoError(t, err)

	// Verify length prefix (little-endian)
	assert.Equal(t, []byte{0x05, 0x00, 0x00, 0x00}, buf[:4])
	// Verify payload
	assert.Equal(t, []byte("hello"), buf[4:])
}

func TestConn_ReadMessage_TooLarge(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	clientConn := connection.NewConn(client)

	// Write a message claiming to be 200MB
	go func() {
		var length uint32 = 200 * 1024 * 1024
		_ = binary.Write(server, binary.LittleEndian, length)
	}()

	_, err := clientConn.ReadMessage()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

func TestConn_ReadMessage_Empty(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	clientConn := connection.NewConn(client)

	// Write a zero-length message
	go func() {
		var length uint32
		_ = binary.Write(server, binary.LittleEndian, length)
	}()

	got, err := clientConn.ReadMessage()
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestConn_MultipleMessages(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	clientConn := connection.NewConn(client)
	serverConn := connection.NewConn(server)

	messages := [][]byte{
		{1, 2, 3},
		{4, 5, 6, 7, 8},
		{9},
	}

	// Write all messages
	go func() {
		for _, msg := range messages {
			_ = clientConn.WriteMessage(msg)
		}
	}()

	// Read all messages
	for _, want := range messages {
		got, err := serverConn.ReadMessage()
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

func TestConn_Close(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()

	clientConn := connection.NewConn(client)

	err := clientConn.Close()
	require.NoError(t, err)

	// Further operations should fail
	_, err = clientConn.ReadMessage()
	assert.Error(t, err)
}

func TestConn_SetDeadline(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	clientConn := connection.NewConn(client)

	// Set a very short deadline
	err := clientConn.SetDeadline(time.Now().Add(1 * time.Millisecond))
	require.NoError(t, err)

	// Wait for deadline to pass
	time.Sleep(10 * time.Millisecond)

	// Read should timeout
	_, err = clientConn.ReadMessage()
	assert.Error(t, err)
}

func TestConn_Addresses(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	clientConn := connection.NewConn(client)

	// net.Pipe returns pipe addresses
	assert.NotNil(t, clientConn.LocalAddr())
	assert.NotNil(t, clientConn.RemoteAddr())
}

// TestProtocolRoundtrip tests a realistic protocol exchange
func TestProtocolRoundtrip(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	clientConn := connection.NewConn(client)
	serverConn := connection.NewConn(server)

	// Simulate login request (code=1, username, password)
	var loginRequest bytes.Buffer
	_ = binary.Write(&loginRequest, binary.LittleEndian, uint32(1)) // code
	writeString(&loginRequest, "testuser")
	writeString(&loginRequest, "testpass")

	// Simulate login response (code=1, success=1, greeting)
	var loginResponse bytes.Buffer
	_ = binary.Write(&loginResponse, binary.LittleEndian, uint32(1)) // code
	loginResponse.WriteByte(1)                                       // success
	writeString(&loginResponse, "Welcome!")
	loginResponse.Write([]byte{192, 168, 1, 1}) // IP
	writeString(&loginResponse, "hash123")
	loginResponse.WriteByte(0) // not supporter

	// Client sends request, server sends response
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Read request on server
		req, err := serverConn.ReadMessage()
		require.NoError(t, err)
		assert.Equal(t, loginRequest.Bytes(), req)

		// Send response from server
		err = serverConn.WriteMessage(loginResponse.Bytes())
		require.NoError(t, err)
	}()

	// Send request from client
	err := clientConn.WriteMessage(loginRequest.Bytes())
	require.NoError(t, err)

	// Read response on client
	resp, err := clientConn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, loginResponse.Bytes(), resp)

	<-done
}

// writeString writes a length-prefixed string to w
func writeString(w *bytes.Buffer, s string) {
	//nolint:gosec // Test helper, string lengths are small
	_ = binary.Write(w, binary.LittleEndian, uint32(len(s)))
	w.WriteString(s)
}
