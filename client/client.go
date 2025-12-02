package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/llehouerou/gosoulseek/connection"
	"github.com/llehouerou/gosoulseek/messages/server"
	"github.com/llehouerou/gosoulseek/protocol"
)

// Client represents a Soulseek client connection.
type Client struct {
	opts *Options
	conn *connection.Conn
	mu   sync.Mutex

	// State
	username    string
	ipAddress   net.IP
	isSupporter bool
	connected   bool
	loggedIn    bool
}

// New creates a new client with the given options.
// If opts is nil, DefaultOptions() is used.
func New(opts *Options) *Client {
	if opts == nil {
		opts = DefaultOptions()
	}
	return &Client{
		opts: opts,
	}
}

// Connect establishes a connection to the Soulseek server.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return errors.New("already connected")
	}

	// Apply timeout from options if context doesn't have a deadline
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.ConnectTimeout)
		defer cancel()
	}

	conn, err := connection.Dial(ctx, c.opts.ServerAddress)
	if err != nil {
		return fmt.Errorf("dial server: %w", err)
	}

	c.conn = conn
	c.connected = true
	return nil
}

// Login authenticates with the Soulseek server.
// Connect must be called first.
func (c *Client) Login(ctx context.Context, username, password string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return errors.New("not connected")
	}
	if c.loggedIn {
		return errors.New("already logged in")
	}

	// Set deadline from context or use default message timeout
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		deadline = time.Now().Add(c.opts.MessageTimeout)
	}
	if err := c.conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set deadline: %w", err)
	}
	// Clear deadline after login completes
	defer func() { _ = c.conn.SetDeadline(time.Time{}) }()

	// Build concatenated message: Login + SetListenPort
	// This prevents a race condition where peers see port 0.
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)

	loginReq := server.NewLoginRequest(username, password)
	loginReq.Encode(w)

	portReq := &server.SetListenPort{Port: c.opts.ListenPort}
	portReq.Encode(w)

	if err := w.Error(); err != nil {
		return fmt.Errorf("encode login: %w", err)
	}

	// Send as single write
	if err := c.conn.WriteMessage(buf.Bytes()); err != nil {
		return fmt.Errorf("send login: %w", err)
	}

	// Wait for response
	payload, err := c.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	resp, err := server.DecodeLoginResponse(protocol.NewReader(bytes.NewReader(payload)))
	if err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if !resp.Succeeded {
		return fmt.Errorf("login rejected: %s", resp.Message)
	}

	c.username = username
	c.ipAddress = resp.IPAddress
	c.isSupporter = resp.IsSupporter
	c.loggedIn = true

	// Send post-login configuration
	if err := c.sendPostLoginConfig(); err != nil {
		return fmt.Errorf("post-login config: %w", err)
	}

	return nil
}

// sendPostLoginConfig sends configuration messages after successful login.
func (c *Client) sendPostLoginConfig() error {
	var buf bytes.Buffer

	// Set online status
	w := protocol.NewWriter(&buf)
	(&server.SetOnlineStatus{Status: server.StatusOnline}).Encode(w)
	if err := w.Error(); err != nil {
		return fmt.Errorf("encode status: %w", err)
	}
	if err := c.conn.WriteMessage(buf.Bytes()); err != nil {
		return fmt.Errorf("send status: %w", err)
	}

	// Report shared files (0 for now - no share management yet)
	buf.Reset()
	w = protocol.NewWriter(&buf)
	(&server.SharedFoldersAndFiles{Directories: 0, Files: 0}).Encode(w)
	if err := w.Error(); err != nil {
		return fmt.Errorf("encode shares: %w", err)
	}
	if err := c.conn.WriteMessage(buf.Bytes()); err != nil {
		return fmt.Errorf("send shares: %w", err)
	}

	return nil
}

// Disconnect closes the connection to the server.
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil // Already disconnected
	}

	err := c.conn.Close()
	c.conn = nil
	c.connected = false
	c.loggedIn = false
	c.username = ""
	c.ipAddress = nil
	c.isSupporter = false

	return err
}

// Connected returns true if connected to the server.
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// LoggedIn returns true if authenticated with the server.
func (c *Client) LoggedIn() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loggedIn
}

// Username returns the logged-in username.
func (c *Client) Username() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.username
}

// IPAddress returns our public IP as seen by the server.
func (c *Client) IPAddress() net.IP {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ipAddress
}

// IsSupporter returns true if the user has purchased privileges.
func (c *Client) IsSupporter() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.isSupporter
}
