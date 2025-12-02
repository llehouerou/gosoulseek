package peer_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/llehouerou/gosoulseek/messages/peer"
	"github.com/llehouerou/gosoulseek/protocol"
)

func TestDecodeSearchResponse(t *testing.T) {
	// Build a search response payload (uncompressed)
	var payload bytes.Buffer
	w := protocol.NewWriter(&payload)

	// Username
	w.WriteString("testuser")
	// Token
	w.WriteUint32(12345)
	// File count
	w.WriteUint32(2)

	// File 1
	w.WriteUint8(1)                                  // code
	w.WriteString("Music\\Artist\\Album\\song1.mp3") // filename
	w.WriteUint64(5000000)                           // size (5MB)
	w.WriteString("mp3")                             // extension
	w.WriteUint32(2)                                 // attribute count
	w.WriteUint32(0)                                 // BitRate type
	w.WriteUint32(320)                               // 320 kbps
	w.WriteUint32(1)                                 // Length type
	w.WriteUint32(240)                               // 4 minutes

	// File 2
	w.WriteUint8(1)                                   // code
	w.WriteString("Music\\Artist\\Album\\song2.flac") // filename
	w.WriteUint64(30000000)                           // size (30MB)
	w.WriteString("flac")                             // extension
	w.WriteUint32(1)                                  // attribute count
	w.WriteUint32(1)                                  // Length type
	w.WriteUint32(300)                                // 5 minutes

	// Metadata
	w.WriteUint8(1)       // has free slot
	w.WriteUint32(100000) // upload speed
	w.WriteUint32(5)      // queue length
	w.WriteUint32(0)      // unknown/reserved

	// Locked files (none)
	w.WriteUint32(0)

	require.NoError(t, w.Error())

	// Compress the payload
	compressed, err := protocol.Compress(payload.Bytes())
	require.NoError(t, err)

	// Prepend the message code
	var message bytes.Buffer
	codeWriter := protocol.NewWriter(&message)
	codeWriter.WriteUint32(uint32(protocol.PeerSearchResponse))
	require.NoError(t, codeWriter.Error())
	message.Write(compressed)

	// Decode
	resp, err := peer.DecodeSearchResponse(message.Bytes())
	require.NoError(t, err)

	// Verify
	assert.Equal(t, "testuser", resp.Username)
	assert.Equal(t, uint32(12345), resp.Token)
	assert.True(t, resp.HasFreeSlot)
	assert.Equal(t, uint32(100000), resp.UploadSpeed)
	assert.Equal(t, uint32(5), resp.QueueLength)
	assert.Len(t, resp.Files, 2)
	assert.Empty(t, resp.LockedFiles)

	// Verify file 1
	f1 := resp.Files[0]
	assert.Equal(t, "Music\\Artist\\Album\\song1.mp3", f1.Filename)
	assert.Equal(t, int64(5000000), f1.Size)
	assert.Equal(t, "mp3", f1.Extension)
	assert.Equal(t, uint32(320), f1.BitRate)
	assert.Equal(t, uint32(240), f1.Duration)

	// Verify file 2
	f2 := resp.Files[1]
	assert.Equal(t, "Music\\Artist\\Album\\song2.flac", f2.Filename)
	assert.Equal(t, int64(30000000), f2.Size)
	assert.Equal(t, "flac", f2.Extension)
	assert.Equal(t, uint32(300), f2.Duration)
}

func TestDecodeSearchResponse_TooShort(t *testing.T) {
	_, err := peer.DecodeSearchResponse([]byte{0x01, 0x02})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too short")
}

func TestDecodeSearchResponse_InvalidCompression(t *testing.T) {
	// Valid code but invalid zlib data
	data := []byte{
		0x09, 0x00, 0x00, 0x00, // code 9
		0xFF, 0xFF, 0xFF, 0xFF, // invalid zlib
	}
	_, err := peer.DecodeSearchResponse(data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "decompress")
}

func TestDecodeFile(t *testing.T) {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)

	w.WriteUint8(1)
	w.WriteString("path/to/file.mp3")
	w.WriteUint64(1234567)
	w.WriteString("mp3")
	w.WriteUint32(3)         // 3 attributes
	w.WriteUint32(0)         // BitRate
	w.WriteUint32(256)       // 256 kbps
	w.WriteUint32(1)         // Length
	w.WriteUint32(180)       // 3 minutes
	w.WriteUint32(uint32(5)) // VBR
	w.WriteUint32(1)         // is VBR

	require.NoError(t, w.Error())

	r := protocol.NewReader(bytes.NewReader(buf.Bytes()))
	f := peer.DecodeFile(r)

	require.NoError(t, r.Error())
	assert.Equal(t, uint8(1), f.Code)
	assert.Equal(t, "path/to/file.mp3", f.Filename)
	assert.Equal(t, int64(1234567), f.Size)
	assert.Equal(t, "mp3", f.Extension)
	assert.Len(t, f.Attributes, 3)
	assert.Equal(t, uint32(256), f.BitRate)
	assert.Equal(t, uint32(180), f.Duration)
	assert.True(t, f.IsVBR)
}
