package client

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/llehouerou/gosoulseek/connection"
	"github.com/llehouerou/gosoulseek/messages/peer"
	"github.com/llehouerou/gosoulseek/messages/server"
	"github.com/llehouerou/gosoulseek/protocol"
)

// Client represents a Soulseek client connection.
type Client struct {
	opts          *Options
	conn          *connection.Conn
	router        *MessageRouter
	searches      *searchRegistry
	transfers     *TransferRegistry // Unified transfer tracking
	listener      *Listener
	peerConnMgr   *peerConnManager      // Manages P-type connections to peers
	solicitations *pendingSolicitations // Pending connections WE solicited
	peerSolicits  *pendingPeerSolicits  // Pending connections PEER solicited (from ConnectToPeer)
	mu            sync.Mutex

	// Read loop management
	stopCh  chan struct{}
	doneCh  chan struct{}
	running bool

	// State
	username    string
	ipAddress   net.IP
	isSupporter bool
	connected   bool
	loggedIn    bool

	// Disconnected channel - closed when connection is lost
	disconnectedCh chan struct{}
	disconnectErr  error
}

// New creates a new client with the given options.
// If opts is nil, DefaultOptions() is used.
func New(opts *Options) *Client {
	if opts == nil {
		opts = DefaultOptions()
	}
	c := &Client{
		opts:          opts,
		router:        NewMessageRouter(),
		searches:      newSearchRegistry(),
		transfers:     NewTransferRegistry(),
		solicitations: newPendingSolicitations(),
		peerSolicits:  newPendingPeerSolicits(),
	}
	c.peerConnMgr = newPeerConnManager(c)
	c.listener = newListener(c)
	return c
}

// Router returns the message router for registering custom handlers.
func (c *Client) Router() *MessageRouter {
	return c.router
}

// Transfers returns the transfer registry for tracking downloads and uploads.
func (c *Client) Transfers() *TransferRegistry {
	return c.transfers
}

// Disconnected returns a channel that is closed when the client disconnects.
// The error can be retrieved with DisconnectError().
func (c *Client) Disconnected() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.disconnectedCh
}

// DisconnectError returns the error that caused the disconnect, if any.
func (c *Client) DisconnectError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.disconnectErr
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
	c.stopCh = make(chan struct{})
	c.doneCh = make(chan struct{})
	c.disconnectedCh = make(chan struct{})
	c.disconnectErr = nil
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
	c.peerConnMgr.ourUsername = username

	// Send post-login configuration
	if err := c.sendPostLoginConfig(); err != nil {
		return fmt.Errorf("post-login config: %w", err)
	}

	// Register internal handlers
	c.registerInternalHandlers()

	// Start the read loop
	c.running = true
	go c.runReadLoop()

	return nil
}

// registerInternalHandlers sets up handlers for messages the client processes internally.
func (c *Client) registerInternalHandlers() {
	// Handle embedded messages (server code 93) - these contain peer messages
	c.router.Register(uint32(protocol.ServerEmbeddedMessage), c.handleEmbeddedMessage)

	// Handle ping (server code 32) - echo back
	c.router.Register(uint32(protocol.ServerPing), c.handlePing)

	// Handle ConnectToPeer (server code 18) - connect to peers who have search results
	c.router.Register(uint32(protocol.ServerConnectToPeer), c.handleConnectToPeer)
}

// handleEmbeddedMessage processes embedded peer messages from the server.
func (c *Client) handleEmbeddedMessage(_ uint32, payload []byte) {
	if len(payload) < 5 {
		return
	}

	// Skip 4-byte server code, read 1-byte distributed/peer code
	embeddedCode := payload[4]

	// The rest is the peer message (with its own 4-byte code prefix)
	embeddedPayload := payload[5:]

	// Check if it's a search response (peer code 9)
	if len(embeddedPayload) >= 4 {
		peerCode := binary.LittleEndian.Uint32(embeddedPayload[:4])
		if peerCode == uint32(protocol.PeerSearchResponse) {
			c.handleSearchResponse(embeddedPayload)
			return
		}
	}

	// For other embedded messages, dispatch with the embedded code
	c.router.Dispatch(uint32(embeddedCode), embeddedPayload)
}

// handleSearchResponse parses and delivers a search response to the appropriate channel.
func (c *Client) handleSearchResponse(payload []byte) {
	resp, err := peer.DecodeSearchResponse(payload)
	if err != nil {
		return
	}

	c.searches.deliver(resp)
}

// handlePing echoes ping messages back to the server.
func (c *Client) handlePing(_ uint32, _ []byte) {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	(&server.Ping{}).Encode(w)
	if w.Error() == nil {
		_ = c.conn.WriteMessage(buf.Bytes())
	}
}

// runReadLoop reads messages from the server and dispatches them.
func (c *Client) runReadLoop() {
	defer close(c.doneCh)

	for {
		select {
		case <-c.stopCh:
			return
		default:
		}

		payload, err := c.conn.ReadMessage()
		if err != nil {
			c.mu.Lock()
			c.connected = false
			c.loggedIn = false
			c.running = false
			c.disconnectErr = err
			close(c.disconnectedCh)
			c.mu.Unlock()

			// Close all active channels
			c.searches.closeAll()
			return
		}

		if len(payload) < 4 {
			continue
		}

		code := binary.LittleEndian.Uint32(payload[:4])
		c.router.Dispatch(code, payload)
	}
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

	if !c.connected {
		c.mu.Unlock()
		return nil // Already disconnected
	}

	// Signal read loop to stop
	if c.running {
		close(c.stopCh)
	}

	err := c.conn.Close()
	c.conn = nil
	c.connected = false
	c.loggedIn = false
	c.running = false
	c.username = ""
	c.ipAddress = nil
	c.isSupporter = false

	c.mu.Unlock()

	// Close all active channels
	c.searches.closeAll()

	// Close peer connection manager
	c.peerConnMgr.Close()

	// Wait for read loop to finish (outside lock to avoid deadlock)
	if c.doneCh != nil {
		<-c.doneCh
	}

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

// WriteMessage sends a raw message to the server.
// This is a low-level method; prefer using specific methods like Search.
func (c *Client) WriteMessage(payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return errors.New("not connected")
	}

	return c.conn.WriteMessage(payload)
}

// StartListener starts the TCP listener for incoming peer connections.
// This is required for downloads to work when peers need to connect to us.
func (c *Client) StartListener() error {
	if c.opts.ListenPort == 0 {
		return errors.New("listen port not configured")
	}
	return c.listener.Start(c.opts.ListenPort)
}

// StopListener stops the TCP listener.
func (c *Client) StopListener() error {
	return c.listener.Stop()
}

// ListenerPort returns the port the listener is bound to, or 0 if not running.
func (c *Client) ListenerPort() int {
	return c.listener.Port()
}

// pendingSolicitations tracks pending solicited peer connections.
// These are connections we requested via the server (e.g., for search results).
type pendingSolicitations struct {
	mu      sync.Mutex
	pending map[uint32]chan *connection.Conn // by token
}

func newPendingSolicitations() *pendingSolicitations {
	return &pendingSolicitations{
		pending: make(map[uint32]chan *connection.Conn),
	}
}

// pendingPeerSolicit represents a pending connection that a PEER solicited.
// The server sent us ConnectToPeer telling us to connect to the peer,
// but the peer might connect to us first via PierceFirewall.
type pendingPeerSolicit struct {
	username string
	connType server.ConnectionType // "P", "F", or "D"
}

// pendingPeerSolicits tracks pending peer-solicited connections.
// When we receive ConnectToPeer from the server, we store the token and type here.
// When we receive PierceFirewall with a matching token, we know what type of connection it is.
type pendingPeerSolicits struct {
	mu      sync.Mutex
	pending map[uint32]pendingPeerSolicit // by token
}

func newPendingPeerSolicits() *pendingPeerSolicits {
	return &pendingPeerSolicits{
		pending: make(map[uint32]pendingPeerSolicit),
	}
}

// add registers a pending peer-solicited connection.
func (p *pendingPeerSolicits) add(token uint32, username string, connType server.ConnectionType) {
	p.mu.Lock()
	p.pending[token] = pendingPeerSolicit{username: username, connType: connType}
	p.mu.Unlock()
}

// get retrieves and removes a pending peer-solicited connection.
func (p *pendingPeerSolicits) get(token uint32) (pendingPeerSolicit, bool) {
	p.mu.Lock()
	solicit, ok := p.pending[token]
	if ok {
		delete(p.pending, token)
	}
	p.mu.Unlock()
	return solicit, ok
}

// complete delivers a connection to a pending solicitation.
// Returns true if there was a pending solicitation for the token.
func (p *pendingSolicitations) complete(token uint32, conn *connection.Conn) bool {
	p.mu.Lock()
	ch, ok := p.pending[token]
	if ok {
		delete(p.pending, token)
	}
	p.mu.Unlock()

	if ok {
		select {
		case ch <- conn:
			return true
		default:
		}
	}
	return false
}

// handleIncomingPeerMessages reads and dispatches messages from an incoming peer connection.
func (c *Client) handleIncomingPeerMessages(conn *connection.Conn, username string) {
	for {
		// Set a read deadline for each message - 5 minutes to allow for queued transfers
		if err := conn.SetDeadline(time.Now().Add(5 * time.Minute)); err != nil {
			return
		}

		payload, err := conn.ReadMessage()
		if err != nil {
			return
		}

		if len(payload) < 4 {
			continue
		}

		code := binary.LittleEndian.Uint32(payload[:4])

		// Handle peer messages
		switch code {
		case uint32(protocol.PeerSearchResponse):
			c.handleSearchResponse(payload)
			// Don't return - keep connection open for potential transfer messages

		case uint32(protocol.PeerTransferRequest):
			c.handleTransferRequest(payload, username, conn)

		case uint32(protocol.PeerTransferResponse):
			// Handle transfer response from peer
			// This is handled by the download flow directly

		case uint32(protocol.PeerPlaceInQueueResponse):
			c.handlePlaceInQueueResponse(payload, username)

		case uint32(protocol.PeerUploadDenied):
			c.handleUploadDenied(payload, username)

		case uint32(protocol.PeerUploadFailed):
			c.handleUploadFailed(payload, username)
		}
	}
}

// handlePlaceInQueueResponse handles queue position updates.
func (c *Client) handlePlaceInQueueResponse(payload []byte, username string) {
	resp, err := peer.DecodePlaceInQueueResponse(payload)
	if err != nil {
		return
	}

	tr, ok := c.transfers.GetByFile(username, resp.Filename, peer.TransferDownload)
	if ok {
		tr.emitProgress()
	}
}

// handleUploadDenied handles upload denial messages.
func (c *Client) handleUploadDenied(payload []byte, username string) {
	denied, err := peer.DecodeUploadDenied(payload)
	if err != nil {
		return
	}

	tr, ok := c.transfers.GetByFile(username, denied.Filename, peer.TransferDownload)
	if ok {
		tr.mu.Lock()
		tr.Error = fmt.Errorf("upload denied: %s", denied.Reason)
		tr.mu.Unlock()
		tr.SetState(TransferStateCompleted | TransferStateErrored)
		tr.emitProgress()
	}
}

// handleUploadFailed handles upload failure messages.
func (c *Client) handleUploadFailed(payload []byte, username string) {
	failed, err := peer.DecodeUploadFailed(payload)
	if err != nil {
		return
	}

	tr, ok := c.transfers.GetByFile(username, failed.Filename, peer.TransferDownload)
	if ok {
		tr.mu.Lock()
		tr.Error = errors.New("upload failed")
		tr.mu.Unlock()
		tr.SetState(TransferStateCompleted | TransferStateErrored)
		tr.emitProgress()
	}
}
