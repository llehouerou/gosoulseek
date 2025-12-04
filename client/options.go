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

	// DefaultMaxConcurrentDownloads is the default limit for concurrent downloads.
	// 0 means unlimited.
	DefaultMaxConcurrentDownloads = 10

	// DefaultMaxConcurrentUploads is the default limit for concurrent uploads.
	// 0 means unlimited.
	DefaultMaxConcurrentUploads = 10

	// DefaultSlotCleanupInterval is how often idle per-user upload slots are cleaned up.
	DefaultSlotCleanupInterval = 15 * time.Minute

	// DefaultSlotIdleThreshold is how long a per-user slot must be idle before cleanup.
	DefaultSlotIdleThreshold = 15 * time.Minute
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

	// MaxConcurrentDownloads limits concurrent downloads (0 = unlimited).
	// Default: 10
	MaxConcurrentDownloads int

	// MaxConcurrentUploads limits concurrent uploads (0 = unlimited).
	// Default: 10
	MaxConcurrentUploads int

	// SlotCleanupInterval controls how often idle per-user upload slots are cleaned up.
	// Set to 0 to disable automatic cleanup.
	// Default: 15 minutes
	SlotCleanupInterval time.Duration

	// SlotIdleThreshold is how long a per-user slot must be idle before cleanup.
	// Default: 15 minutes
	SlotIdleThreshold time.Duration

	// FileSharer is the shared file index for uploads.
	// If nil, no files are shared.
	FileSharer *FileSharer
}

// DefaultOptions returns Options with sensible defaults.
func DefaultOptions() *Options {
	return &Options{
		ListenPort:             0,
		ServerAddress:          DefaultServerAddress,
		ConnectTimeout:         DefaultConnectTimeout,
		MessageTimeout:         DefaultMessageTimeout,
		MaxConcurrentDownloads: DefaultMaxConcurrentDownloads,
		MaxConcurrentUploads:   DefaultMaxConcurrentUploads,
		SlotCleanupInterval:    DefaultSlotCleanupInterval,
		SlotIdleThreshold:      DefaultSlotIdleThreshold,
	}
}
