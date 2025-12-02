package peer_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/llehouerou/gosoulseek/messages/peer"
	"github.com/llehouerou/gosoulseek/protocol"
)

func TestTransferRequest_Download(t *testing.T) {
	// Test encoding
	msg := &peer.TransferRequest{
		Direction: peer.TransferDownload,
		Token:     12345,
		Filename:  "@@artist\\album\\song.mp3",
	}

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	msg.Encode(w)
	require.NoError(t, w.Error())

	// Verify encoding
	r := protocol.NewReader(bytes.NewReader(buf.Bytes()))
	assert.Equal(t, uint32(40), r.ReadUint32()) // Code
	assert.Equal(t, uint32(0), r.ReadUint32())  // direction: download
	assert.Equal(t, uint32(12345), r.ReadUint32())
	assert.Equal(t, "@@artist\\album\\song.mp3", r.ReadString())
	require.NoError(t, r.Error())

	// Test decoding
	decoded, err := peer.DecodeTransferRequest(buf.Bytes())
	require.NoError(t, err)
	assert.Equal(t, peer.TransferDownload, decoded.Direction)
	assert.Equal(t, uint32(12345), decoded.Token)
	assert.Equal(t, "@@artist\\album\\song.mp3", decoded.Filename)
	assert.Equal(t, int64(0), decoded.FileSize) // Not present for download
}

func TestTransferRequest_Upload(t *testing.T) {
	msg := &peer.TransferRequest{
		Direction: peer.TransferUpload,
		Token:     99999,
		Filename:  "@@music\\file.flac",
		FileSize:  1234567890,
	}

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	msg.Encode(w)
	require.NoError(t, w.Error())

	decoded, err := peer.DecodeTransferRequest(buf.Bytes())
	require.NoError(t, err)
	assert.Equal(t, peer.TransferUpload, decoded.Direction)
	assert.Equal(t, uint32(99999), decoded.Token)
	assert.Equal(t, "@@music\\file.flac", decoded.Filename)
	assert.Equal(t, int64(1234567890), decoded.FileSize)
}

func TestTransferRequest_WrongCode(t *testing.T) {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	w.WriteUint32(999) // Wrong code

	_, err := peer.DecodeTransferRequest(buf.Bytes())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected code")
}

func TestTransferResponse_Allowed(t *testing.T) {
	msg := &peer.TransferResponse{
		Token:    12345,
		Allowed:  true,
		FileSize: 9876543210,
	}

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	msg.Encode(w)
	require.NoError(t, w.Error())

	// Verify encoding
	r := protocol.NewReader(bytes.NewReader(buf.Bytes()))
	assert.Equal(t, uint32(41), r.ReadUint32()) // Code
	assert.Equal(t, uint32(12345), r.ReadUint32())
	assert.Equal(t, uint8(1), r.ReadUint8()) // Allowed
	assert.Equal(t, uint64(9876543210), r.ReadUint64())
	require.NoError(t, r.Error())

	// Test decoding
	decoded, err := peer.DecodeTransferResponse(buf.Bytes())
	require.NoError(t, err)
	assert.Equal(t, uint32(12345), decoded.Token)
	assert.True(t, decoded.Allowed)
	assert.Equal(t, int64(9876543210), decoded.FileSize)
	assert.Empty(t, decoded.Reason)
}

func TestTransferResponse_Denied(t *testing.T) {
	msg := &peer.TransferResponse{
		Token:   12345,
		Allowed: false,
		Reason:  "Queued",
	}

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	msg.Encode(w)
	require.NoError(t, w.Error())

	decoded, err := peer.DecodeTransferResponse(buf.Bytes())
	require.NoError(t, err)
	assert.Equal(t, uint32(12345), decoded.Token)
	assert.False(t, decoded.Allowed)
	assert.Equal(t, "Queued", decoded.Reason)
	assert.Equal(t, int64(0), decoded.FileSize)
}

func TestTransferResponse_WrongCode(t *testing.T) {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	w.WriteUint32(999) // Wrong code

	_, err := peer.DecodeTransferResponse(buf.Bytes())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected code")
}

func TestQueueDownload(t *testing.T) {
	msg := &peer.QueueDownload{
		Filename: "@@artist\\album\\track01.mp3",
	}

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	msg.Encode(w)
	require.NoError(t, w.Error())

	// Verify encoding
	r := protocol.NewReader(bytes.NewReader(buf.Bytes()))
	assert.Equal(t, uint32(43), r.ReadUint32()) // Code
	assert.Equal(t, "@@artist\\album\\track01.mp3", r.ReadString())
	require.NoError(t, r.Error())

	// Test decoding
	decoded, err := peer.DecodeQueueDownload(buf.Bytes())
	require.NoError(t, err)
	assert.Equal(t, "@@artist\\album\\track01.mp3", decoded.Filename)
}

func TestPlaceInQueueResponse(t *testing.T) {
	msg := &peer.PlaceInQueueResponse{
		Filename: "@@artist\\album\\track01.mp3",
		Place:    5,
	}

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	msg.Encode(w)
	require.NoError(t, w.Error())

	// Verify encoding
	r := protocol.NewReader(bytes.NewReader(buf.Bytes()))
	assert.Equal(t, uint32(44), r.ReadUint32()) // Code
	assert.Equal(t, "@@artist\\album\\track01.mp3", r.ReadString())
	assert.Equal(t, uint32(5), r.ReadUint32())
	require.NoError(t, r.Error())

	// Test decoding
	decoded, err := peer.DecodePlaceInQueueResponse(buf.Bytes())
	require.NoError(t, err)
	assert.Equal(t, "@@artist\\album\\track01.mp3", decoded.Filename)
	assert.Equal(t, uint32(5), decoded.Place)
}

func TestPlaceInQueueRequest(t *testing.T) {
	msg := &peer.PlaceInQueueRequest{
		Filename: "@@artist\\album\\track01.mp3",
	}

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	msg.Encode(w)
	require.NoError(t, w.Error())

	// Verify encoding
	r := protocol.NewReader(bytes.NewReader(buf.Bytes()))
	assert.Equal(t, uint32(51), r.ReadUint32()) // Code
	assert.Equal(t, "@@artist\\album\\track01.mp3", r.ReadString())
	require.NoError(t, r.Error())

	// Test decoding
	decoded, err := peer.DecodePlaceInQueueRequest(buf.Bytes())
	require.NoError(t, err)
	assert.Equal(t, "@@artist\\album\\track01.mp3", decoded.Filename)
}

func TestUploadFailed(t *testing.T) {
	msg := &peer.UploadFailed{
		Filename: "@@artist\\album\\track01.mp3",
	}

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	msg.Encode(w)
	require.NoError(t, w.Error())

	// Verify encoding
	r := protocol.NewReader(bytes.NewReader(buf.Bytes()))
	assert.Equal(t, uint32(46), r.ReadUint32()) // Code
	assert.Equal(t, "@@artist\\album\\track01.mp3", r.ReadString())
	require.NoError(t, r.Error())

	// Test decoding
	decoded, err := peer.DecodeUploadFailed(buf.Bytes())
	require.NoError(t, err)
	assert.Equal(t, "@@artist\\album\\track01.mp3", decoded.Filename)
}

func TestUploadDenied(t *testing.T) {
	msg := &peer.UploadDenied{
		Filename: "@@artist\\album\\track01.mp3",
		Reason:   "Banned",
	}

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	msg.Encode(w)
	require.NoError(t, w.Error())

	// Verify encoding
	r := protocol.NewReader(bytes.NewReader(buf.Bytes()))
	assert.Equal(t, uint32(50), r.ReadUint32()) // Code
	assert.Equal(t, "@@artist\\album\\track01.mp3", r.ReadString())
	assert.Equal(t, "Banned", r.ReadString())
	require.NoError(t, r.Error())

	// Test decoding
	decoded, err := peer.DecodeUploadDenied(buf.Bytes())
	require.NoError(t, err)
	assert.Equal(t, "@@artist\\album\\track01.mp3", decoded.Filename)
	assert.Equal(t, "Banned", decoded.Reason)
}

func TestTransferRequestCode(t *testing.T) {
	msg := &peer.TransferRequest{}
	assert.Equal(t, protocol.PeerTransferRequest, msg.Code())
}

func TestTransferResponseCode(t *testing.T) {
	msg := &peer.TransferResponse{}
	assert.Equal(t, protocol.PeerTransferResponse, msg.Code())
}

func TestQueueDownloadCode(t *testing.T) {
	msg := &peer.QueueDownload{}
	assert.Equal(t, protocol.PeerQueueDownload, msg.Code())
}

func TestPlaceInQueueResponseCode(t *testing.T) {
	msg := &peer.PlaceInQueueResponse{}
	assert.Equal(t, protocol.PeerPlaceInQueueResponse, msg.Code())
}

func TestPlaceInQueueRequestCode(t *testing.T) {
	msg := &peer.PlaceInQueueRequest{}
	assert.Equal(t, protocol.PeerPlaceInQueueRequest, msg.Code())
}

func TestUploadFailedCode(t *testing.T) {
	msg := &peer.UploadFailed{}
	assert.Equal(t, protocol.PeerUploadFailed, msg.Code())
}

func TestUploadDeniedCode(t *testing.T) {
	msg := &peer.UploadDenied{}
	assert.Equal(t, protocol.PeerUploadDenied, msg.Code())
}
