// Package client provides a high-level Soulseek client.
package client

import "time"

const (
	// DefaultServerAddress is the official Soulseek server.
	DefaultServerAddress = "vps.slsknet.org:2271"

	// DefaultConnectTimeout is the default timeout for establishing a connection.
	DefaultConnectTimeout = 10 * time.Second

	// DefaultMessageTimeout is the default timeout for waiting on message responses.
	DefaultMessageTimeout = 5 * time.Second
)

// Options configures the Soulseek client.
type Options struct {
	// ListenPort is the port for incoming peer connections (1024-65535).
	// Set to 0 if not accepting incoming connections.
	// Default: 0
	ListenPort uint32

	// ServerAddress is the Soulseek server address.
	// Default: "vps.slsknet.org:2271"
	ServerAddress string

	// ConnectTimeout is the timeout for establishing connection.
	// Default: 10s
	ConnectTimeout time.Duration

	// MessageTimeout is the timeout for waiting on message responses.
	// Default: 5s
	MessageTimeout time.Duration
}

// DefaultOptions returns Options with sensible defaults.
func DefaultOptions() *Options {
	return &Options{
		ListenPort:     0,
		ServerAddress:  DefaultServerAddress,
		ConnectTimeout: DefaultConnectTimeout,
		MessageTimeout: DefaultMessageTimeout,
	}
}
