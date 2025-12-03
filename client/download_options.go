package client

// DownloadOption configures a download request.
type DownloadOption func(*downloadConfig)

// downloadConfig holds configuration for a download request.
type downloadConfig struct {
	startOffset int64
}

// WithStartOffset specifies a resume position in bytes.
// The download will start from this offset instead of the beginning.
func WithStartOffset(offset int64) DownloadOption {
	return func(c *downloadConfig) {
		c.startOffset = offset
	}
}
