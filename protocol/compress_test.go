package protocol_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/llehouerou/gosoulseek/protocol"
)

func TestCompress_Roundtrip(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "empty",
			data: []byte{},
		},
		{
			name: "small",
			data: []byte("hello world"),
		},
		{
			name: "larger",
			data: []byte("The quick brown fox jumps over the lazy dog. " +
				"Pack my box with five dozen liquor jugs. " +
				"How vexingly quick daft zebras jump!"),
		},
		{
			name: "binary",
			data: []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compressed, err := protocol.Compress(tt.data)
			require.NoError(t, err)

			decompressed, err := protocol.Decompress(compressed)
			require.NoError(t, err)

			assert.Equal(t, tt.data, decompressed)
		})
	}
}

func TestDecompress_InvalidData(t *testing.T) {
	_, err := protocol.Decompress([]byte{0x00, 0x01, 0x02})
	assert.Error(t, err)
}

func TestCompress_ReducesSize(t *testing.T) {
	// Repetitive data should compress well
	data := make([]byte, 1000)
	for i := range data {
		data[i] = byte(i % 10)
	}

	compressed, err := protocol.Compress(data)
	require.NoError(t, err)

	assert.Less(t, len(compressed), len(data))
}
