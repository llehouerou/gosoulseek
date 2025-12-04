package client

import (
	"testing"
)

func TestDownloadOption_WithStartOffset(t *testing.T) {
	cfg := &downloadConfig{}

	opt := WithStartOffset(1024)
	opt(cfg)

	if cfg.startOffset != 1024 {
		t.Errorf("startOffset = %d, want 1024", cfg.startOffset)
	}
}

func TestDownloadConfig_DefaultValues(t *testing.T) {
	cfg := &downloadConfig{}

	if cfg.startOffset != 0 {
		t.Errorf("default startOffset = %d, want 0", cfg.startOffset)
	}
}

func TestDownloadOption_MultipleOptions(t *testing.T) {
	cfg := &downloadConfig{}

	opts := []DownloadOption{
		WithStartOffset(2048),
		WithExpectedSize(1024000),
	}

	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.startOffset != 2048 {
		t.Errorf("startOffset = %d, want 2048", cfg.startOffset)
	}
	if cfg.expectedSize != 1024000 {
		t.Errorf("expectedSize = %d, want 1024000", cfg.expectedSize)
	}
}

func TestDownloadOption_WithExpectedSize(t *testing.T) {
	cfg := &downloadConfig{}

	opt := WithExpectedSize(5000000)
	opt(cfg)

	if cfg.expectedSize != 5000000 {
		t.Errorf("expectedSize = %d, want 5000000", cfg.expectedSize)
	}
}

func TestDownloadConfig_ExpectedSizeDefaultZero(t *testing.T) {
	cfg := &downloadConfig{}

	// Default should be 0 (no size checking)
	if cfg.expectedSize != 0 {
		t.Errorf("default expectedSize = %d, want 0", cfg.expectedSize)
	}
}
