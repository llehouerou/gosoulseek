package protocol_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/llehouerou/gosoulseek/protocol"
)

func TestReader_ReadUint8(t *testing.T) {
	r := protocol.NewReader(bytes.NewReader([]byte{0x42}))

	got := r.ReadUint8()

	require.NoError(t, r.Error())
	assert.Equal(t, uint8(0x42), got)
}

func TestReader_ReadUint32(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  uint32
	}{
		{"zero", []byte{0x00, 0x00, 0x00, 0x00}, 0},
		{"one", []byte{0x01, 0x00, 0x00, 0x00}, 1},
		{"max", []byte{0xFF, 0xFF, 0xFF, 0xFF}, 0xFFFFFFFF},
		{"mixed", []byte{0x78, 0x56, 0x34, 0x12}, 0x12345678}, // little-endian
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := protocol.NewReader(bytes.NewReader(tt.input))

			got := r.ReadUint32()

			require.NoError(t, r.Error())
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestReader_ReadUint64(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  uint64
	}{
		{"zero", []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, 0},
		{"one", []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, 1},
		{"large", []byte{0xF0, 0xDE, 0xBC, 0x9A, 0x78, 0x56, 0x34, 0x12}, 0x123456789ABCDEF0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := protocol.NewReader(bytes.NewReader(tt.input))

			got := r.ReadUint64()

			require.NoError(t, r.Error())
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestReader_ReadString(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{
			name:  "empty",
			input: []byte{0x00, 0x00, 0x00, 0x00},
			want:  "",
		},
		{
			name:  "hello",
			input: []byte{0x05, 0x00, 0x00, 0x00, 'h', 'e', 'l', 'l', 'o'},
			want:  "hello",
		},
		{
			name:  "unicode",
			input: []byte{0x05, 0x00, 0x00, 0x00, 'c', 'a', 'f', 0xC3, 0xA9},
			want:  "café",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := protocol.NewReader(bytes.NewReader(tt.input))

			got := r.ReadString()

			require.NoError(t, r.Error())
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestReader_ReadBytes(t *testing.T) {
	r := protocol.NewReader(bytes.NewReader([]byte{0x01, 0x02, 0x03, 0x04}))

	got := r.ReadBytes(3)

	require.NoError(t, r.Error())
	assert.Equal(t, []byte{0x01, 0x02, 0x03}, got)
}

func TestReader_MultipleReads(t *testing.T) {
	input := []byte{
		// code: 1
		0x01, 0x00, 0x00, 0x00,
		// username: "testuser" (len=8)
		0x08, 0x00, 0x00, 0x00, 't', 'e', 's', 't', 'u', 's', 'e', 'r',
		// password: "testpass" (len=8)
		0x08, 0x00, 0x00, 0x00, 't', 'e', 's', 't', 'p', 'a', 's', 's',
		// version: 170
		0xAA, 0x00, 0x00, 0x00,
	}
	r := protocol.NewReader(bytes.NewReader(input))

	assert.Equal(t, uint32(1), r.ReadUint32())
	assert.Equal(t, "testuser", r.ReadString())
	assert.Equal(t, "testpass", r.ReadString())
	assert.Equal(t, uint32(170), r.ReadUint32())
	require.NoError(t, r.Error())
}

func TestReader_StickyError(t *testing.T) {
	// Only 2 bytes, but we try to read a uint32 (4 bytes)
	r := protocol.NewReader(bytes.NewReader([]byte{0x01, 0x02}))

	// First read fails (EOF)
	_ = r.ReadUint32()
	assert.Error(t, r.Error())

	// Subsequent reads return zero values
	assert.Equal(t, uint32(0), r.ReadUint32())
	assert.Equal(t, "", r.ReadString())
	assert.Equal(t, uint8(0), r.ReadUint8())
	assert.Nil(t, r.ReadBytes(1))

	// Error is preserved
	assert.ErrorIs(t, r.Error(), io.ErrUnexpectedEOF)
}

func TestReader_ReadBytes_Negative(t *testing.T) {
	r := protocol.NewReader(bytes.NewReader([]byte{0x01, 0x02}))

	got := r.ReadBytes(-1)

	assert.Nil(t, got)
	assert.Error(t, r.Error())
}

func TestReader_ReadString_TooLarge(t *testing.T) {
	// Claim a string is 20MB
	input := []byte{0x00, 0x00, 0x40, 0x01} // 20971520 in little-endian
	r := protocol.NewReader(bytes.NewReader(input))

	got := r.ReadString()

	assert.Equal(t, "", got)
	assert.Error(t, r.Error())
	assert.Contains(t, r.Error().Error(), "exceeds maximum")
}

func TestWriterReader_Roundtrip(t *testing.T) {
	// Write data
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	w.WriteUint32(42)
	w.WriteString("hello world")
	w.WriteUint8(1)
	w.WriteUint64(12345678901234)
	require.NoError(t, w.Error())

	// Read it back
	r := protocol.NewReader(bytes.NewReader(buf.Bytes()))
	assert.Equal(t, uint32(42), r.ReadUint32())
	assert.Equal(t, "hello world", r.ReadString())
	assert.Equal(t, uint8(1), r.ReadUint8())
	assert.Equal(t, uint64(12345678901234), r.ReadUint64())
	require.NoError(t, r.Error())
}
