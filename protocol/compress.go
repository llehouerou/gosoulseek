package protocol

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
)

// Decompress decompresses zlib-compressed data.
func Decompress(data []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create zlib reader: %w", err)
	}
	defer r.Close()

	result, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}

	return result, nil
}

// Compress compresses data using zlib.
func Compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)

	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("compress write: %w", err)
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("compress close: %w", err)
	}

	return buf.Bytes(), nil
}
