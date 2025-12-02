// Package connection provides TCP connection management for the Soulseek protocol.
package connection

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

// maxMessageSize is the maximum allowed message size to prevent OOM attacks.
const maxMessageSize = 100 * 1024 * 1024 // 100MB

// Conn handles framed message I/O over a network connection.
// Messages are framed as [4-byte length][payload].
type Conn struct {
	conn net.Conn
	r    *bufio.Reader
	w    *bufio.Writer
}

// Dial connects to a Soulseek server.
func Dial(ctx context.Context, address string) (*Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", address, err)
	}
	return NewConn(conn), nil
}

// NewConn wraps an existing net.Conn.
// This is useful for testing with mock connections.
func NewConn(conn net.Conn) *Conn {
	return &Conn{
		conn: conn,
		r:    bufio.NewReader(conn),
		w:    bufio.NewWriter(conn),
	}
}

// ReadMessage reads the next framed message.
// Returns the payload (without the length prefix).
func (c *Conn) ReadMessage() ([]byte, error) {
	var length uint32
	if err := binary.Read(c.r, binary.LittleEndian, &length); err != nil {
		return nil, fmt.Errorf("read message length: %w", err)
	}

	if length > maxMessageSize {
		return nil, fmt.Errorf("message too large: %d bytes (max %d)", length, maxMessageSize)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(c.r, payload); err != nil {
		return nil, fmt.Errorf("read message payload: %w", err)
	}

	return payload, nil
}

// WriteMessage writes a framed message.
// Automatically prepends the 4-byte length prefix.
func (c *Conn) WriteMessage(payload []byte) error {
	//nolint:gosec // Message payloads are always < 4GB
	length := uint32(len(payload))
	if err := binary.Write(c.w, binary.LittleEndian, length); err != nil {
		return fmt.Errorf("write message length: %w", err)
	}
	if _, err := c.w.Write(payload); err != nil {
		return fmt.Errorf("write message payload: %w", err)
	}
	return c.w.Flush()
}

// Close closes the underlying connection.
func (c *Conn) Close() error {
	return c.conn.Close()
}

// SetDeadline sets read and write deadlines on the connection.
func (c *Conn) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}

// SetReadDeadline sets the read deadline on the connection.
func (c *Conn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline on the connection.
func (c *Conn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

// LocalAddr returns the local network address.
func (c *Conn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

// RemoteAddr returns the remote network address.
func (c *Conn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}
