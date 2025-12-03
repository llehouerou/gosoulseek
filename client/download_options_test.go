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
	}

	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.startOffset != 2048 {
		t.Errorf("startOffset = %d, want 2048", cfg.startOffset)
	}
}
