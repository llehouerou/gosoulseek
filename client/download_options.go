package client

// DownloadOption configures a download request.
type DownloadOption func(*downloadConfig)

// downloadConfig holds configuration for a download request.
type downloadConfig struct {
	startOffset  int64
	expectedSize int64 // Expected file size from search results (0 = don't check)
}

// WithStartOffset specifies a resume position in bytes.
// The download will start from this offset instead of the beginning.
func WithStartOffset(offset int64) DownloadOption {
	return func(c *downloadConfig) {
		c.startOffset = offset
	}
}

// WithExpectedSize specifies the expected file size from search results.
// If the peer reports a different size, the download will fail with
// TransferSizeMismatchError. This is recommended to detect file changes.
// Pass 0 to disable size checking (default).
func WithExpectedSize(size int64) DownloadOption {
	return func(c *downloadConfig) {
		c.expectedSize = size
	}
}
