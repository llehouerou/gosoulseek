package protocol_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/llehouerou/gosoulseek/protocol"
)

func TestWriter_WriteUint8(t *testing.T) {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)

	w.WriteUint8(0x42)

	require.NoError(t, w.Error())
	assert.Equal(t, []byte{0x42}, buf.Bytes())
}

func TestWriter_WriteUint32(t *testing.T) {
	tests := []struct {
		name  string
		value uint32
		want  []byte
	}{
		{"zero", 0, []byte{0x00, 0x00, 0x00, 0x00}},
		{"one", 1, []byte{0x01, 0x00, 0x00, 0x00}},
		{"max", 0xFFFFFFFF, []byte{0xFF, 0xFF, 0xFF, 0xFF}},
		{"mixed", 0x12345678, []byte{0x78, 0x56, 0x34, 0x12}}, // little-endian
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := protocol.NewWriter(&buf)

			w.WriteUint32(tt.value)

			require.NoError(t, w.Error())
			assert.Equal(t, tt.want, buf.Bytes())
		})
	}
}

func TestWriter_WriteUint64(t *testing.T) {
	tests := []struct {
		name  string
		value uint64
		want  []byte
	}{
		{"zero", 0, []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}},
		{"one", 1, []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}},
		{"large", 0x123456789ABCDEF0, []byte{0xF0, 0xDE, 0xBC, 0x9A, 0x78, 0x56, 0x34, 0x12}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := protocol.NewWriter(&buf)

			w.WriteUint64(tt.value)

			require.NoError(t, w.Error())
			assert.Equal(t, tt.want, buf.Bytes())
		})
	}
}

func TestWriter_WriteString(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []byte
	}{
		{
			name:  "empty",
			value: "",
			want:  []byte{0x00, 0x00, 0x00, 0x00},
		},
		{
			name:  "hello",
			value: "hello",
			want:  []byte{0x05, 0x00, 0x00, 0x00, 'h', 'e', 'l', 'l', 'o'},
		},
		{
			name:  "unicode",
			value: "café",
			// "café" is 5 bytes in UTF-8: c a f 0xC3 0xA9
			want: []byte{0x05, 0x00, 0x00, 0x00, 'c', 'a', 'f', 0xC3, 0xA9},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := protocol.NewWriter(&buf)

			w.WriteString(tt.value)

			require.NoError(t, w.Error())
			assert.Equal(t, tt.want, buf.Bytes())
		})
	}
}

func TestWriter_WriteBytes(t *testing.T) {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)

	w.WriteBytes([]byte{0x01, 0x02, 0x03})

	require.NoError(t, w.Error())
	assert.Equal(t, []byte{0x01, 0x02, 0x03}, buf.Bytes())
}

func TestWriter_MultipleWrites(t *testing.T) {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)

	w.WriteUint32(1)          // code
	w.WriteString("testuser") // username
	w.WriteString("testpass") // password
	w.WriteUint32(170)        // version

	require.NoError(t, w.Error())

	// Verify structure
	expected := []byte{
		// code: 1
		0x01, 0x00, 0x00, 0x00,
		// username: "testuser" (len=8)
		0x08, 0x00, 0x00, 0x00, 't', 'e', 's', 't', 'u', 's', 'e', 'r',
		// password: "testpass" (len=8)
		0x08, 0x00, 0x00, 0x00, 't', 'e', 's', 't', 'p', 'a', 's', 's',
		// version: 170
		0xAA, 0x00, 0x00, 0x00,
	}
	assert.Equal(t, expected, buf.Bytes())
}

// errWriter is an io.Writer that always returns an error.
type errWriter struct {
	err error
}

func (e errWriter) Write(_ []byte) (int, error) {
	return 0, e.err
}

func TestWriter_StickyError(t *testing.T) {
	testErr := errors.New("write error")
	w := protocol.NewWriter(errWriter{err: testErr})

	// First write fails
	w.WriteUint32(1)
	assert.ErrorIs(t, w.Error(), testErr)

	// Subsequent writes are no-ops
	w.WriteString("hello")
	w.WriteUint64(123)
	w.WriteBytes([]byte{1, 2, 3})

	// Error is preserved
	assert.ErrorIs(t, w.Error(), testErr)
}
