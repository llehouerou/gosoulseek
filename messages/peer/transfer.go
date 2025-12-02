package peer

import (
	"bytes"
	"fmt"

	"github.com/llehouerou/gosoulseek/protocol"
)

// TransferDirection indicates whether a transfer is a download or upload.
type TransferDirection uint32

const (
	// TransferDownload indicates the sender is requesting to download.
	TransferDownload TransferDirection = 0
	// TransferUpload indicates the sender is offering to upload.
	TransferUpload TransferDirection = 1
)

// TransferRequest is sent to request or initiate a file transfer.
// Code 40.
//
// When Direction is Download, the sender requests to download a file.
// When Direction is Upload, the sender offers to upload a file (includes FileSize).
type TransferRequest struct {
	Direction TransferDirection
	Token     uint32
	Filename  string
	FileSize  int64 // Only present when Direction == Upload
}

// Code returns the peer message code.
func (m *TransferRequest) Code() protocol.PeerCode {
	return protocol.PeerTransferRequest
}

// Encode writes the TransferRequest message.
func (m *TransferRequest) Encode(w *protocol.Writer) {
	w.WriteUint32(uint32(protocol.PeerTransferRequest))
	w.WriteUint32(uint32(m.Direction))
	w.WriteUint32(m.Token)
	w.WriteString(m.Filename)
	if m.Direction == TransferUpload {
		w.WriteUint64(uint64(m.FileSize)) //nolint:gosec // file sizes are always positive
	}
}

// DecodeTransferRequest reads a TransferRequest from the payload.
func DecodeTransferRequest(payload []byte) (*TransferRequest, error) {
	r := protocol.NewReader(bytes.NewReader(payload))

	code := r.ReadUint32()
	if code != uint32(protocol.PeerTransferRequest) {
		return nil, fmt.Errorf("unexpected code %d, expected %d", code, protocol.PeerTransferRequest)
	}

	msg := &TransferRequest{
		Direction: TransferDirection(r.ReadUint32()),
		Token:     r.ReadUint32(),
		Filename:  r.ReadString(),
	}

	// FileSize is only present for upload direction
	if msg.Direction == TransferUpload {
		msg.FileSize = int64(r.ReadUint64()) //nolint:gosec // file sizes are always positive
	}

	if err := r.Error(); err != nil {
		return nil, fmt.Errorf("decode transfer request: %w", err)
	}

	return msg, nil
}

// TransferResponse is sent in response to a TransferRequest.
// Code 41.
type TransferResponse struct {
	Token    uint32
	Allowed  bool
	FileSize int64  // Only present when Allowed == true
	Reason   string // Only present when Allowed == false
}

// Code returns the peer message code.
func (m *TransferResponse) Code() protocol.PeerCode {
	return protocol.PeerTransferResponse
}

// Encode writes the TransferResponse message.
func (m *TransferResponse) Encode(w *protocol.Writer) {
	w.WriteUint32(uint32(protocol.PeerTransferResponse))
	w.WriteUint32(m.Token)
	if m.Allowed {
		w.WriteUint8(1)
		w.WriteUint64(uint64(m.FileSize)) //nolint:gosec // file sizes are always positive
	} else {
		w.WriteUint8(0)
		w.WriteString(m.Reason)
	}
}

// DecodeTransferResponse reads a TransferResponse from the payload.
func DecodeTransferResponse(payload []byte) (*TransferResponse, error) {
	r := protocol.NewReader(bytes.NewReader(payload))

	code := r.ReadUint32()
	if code != uint32(protocol.PeerTransferResponse) {
		return nil, fmt.Errorf("unexpected code %d, expected %d", code, protocol.PeerTransferResponse)
	}

	msg := &TransferResponse{
		Token:   r.ReadUint32(),
		Allowed: r.ReadUint8() == 1,
	}

	if msg.Allowed {
		msg.FileSize = int64(r.ReadUint64()) //nolint:gosec // file sizes are always positive
	} else {
		msg.Reason = r.ReadString()
	}

	if err := r.Error(); err != nil {
		return nil, fmt.Errorf("decode transfer response: %w", err)
	}

	return msg, nil
}

// QueueDownload requests to add a file to the peer's upload queue.
// Code 43.
type QueueDownload struct {
	Filename string
}

// Code returns the peer message code.
func (m *QueueDownload) Code() protocol.PeerCode {
	return protocol.PeerQueueDownload
}

// Encode writes the QueueDownload message.
func (m *QueueDownload) Encode(w *protocol.Writer) {
	w.WriteUint32(uint32(protocol.PeerQueueDownload))
	w.WriteString(m.Filename)
}

// DecodeQueueDownload reads a QueueDownload from the payload.
func DecodeQueueDownload(payload []byte) (*QueueDownload, error) {
	r := protocol.NewReader(bytes.NewReader(payload))

	code := r.ReadUint32()
	if code != uint32(protocol.PeerQueueDownload) {
		return nil, fmt.Errorf("unexpected code %d, expected %d", code, protocol.PeerQueueDownload)
	}

	msg := &QueueDownload{
		Filename: r.ReadString(),
	}

	if err := r.Error(); err != nil {
		return nil, fmt.Errorf("decode queue download: %w", err)
	}

	return msg, nil
}

// PlaceInQueueResponse reports the current position in a peer's upload queue.
// Code 44.
type PlaceInQueueResponse struct {
	Filename string
	Place    uint32 // 1-indexed position in queue
}

// Code returns the peer message code.
func (m *PlaceInQueueResponse) Code() protocol.PeerCode {
	return protocol.PeerPlaceInQueueResponse
}

// Encode writes the PlaceInQueueResponse message.
func (m *PlaceInQueueResponse) Encode(w *protocol.Writer) {
	w.WriteUint32(uint32(protocol.PeerPlaceInQueueResponse))
	w.WriteString(m.Filename)
	w.WriteUint32(m.Place)
}

// DecodePlaceInQueueResponse reads a PlaceInQueueResponse from the payload.
func DecodePlaceInQueueResponse(payload []byte) (*PlaceInQueueResponse, error) {
	r := protocol.NewReader(bytes.NewReader(payload))

	code := r.ReadUint32()
	if code != uint32(protocol.PeerPlaceInQueueResponse) {
		return nil, fmt.Errorf("unexpected code %d, expected %d", code, protocol.PeerPlaceInQueueResponse)
	}

	msg := &PlaceInQueueResponse{
		Filename: r.ReadString(),
		Place:    r.ReadUint32(),
	}

	if err := r.Error(); err != nil {
		return nil, fmt.Errorf("decode place in queue response: %w", err)
	}

	return msg, nil
}

// PlaceInQueueRequest requests the current queue position for a file.
// Code 51.
type PlaceInQueueRequest struct {
	Filename string
}

// Code returns the peer message code.
func (m *PlaceInQueueRequest) Code() protocol.PeerCode {
	return protocol.PeerPlaceInQueueRequest
}

// Encode writes the PlaceInQueueRequest message.
func (m *PlaceInQueueRequest) Encode(w *protocol.Writer) {
	w.WriteUint32(uint32(protocol.PeerPlaceInQueueRequest))
	w.WriteString(m.Filename)
}

// DecodePlaceInQueueRequest reads a PlaceInQueueRequest from the payload.
func DecodePlaceInQueueRequest(payload []byte) (*PlaceInQueueRequest, error) {
	r := protocol.NewReader(bytes.NewReader(payload))

	code := r.ReadUint32()
	if code != uint32(protocol.PeerPlaceInQueueRequest) {
		return nil, fmt.Errorf("unexpected code %d, expected %d", code, protocol.PeerPlaceInQueueRequest)
	}

	msg := &PlaceInQueueRequest{
		Filename: r.ReadString(),
	}

	if err := r.Error(); err != nil {
		return nil, fmt.Errorf("decode place in queue request: %w", err)
	}

	return msg, nil
}

// UploadFailed indicates a transfer failed mid-stream.
// Code 46.
type UploadFailed struct {
	Filename string
}

// Code returns the peer message code.
func (m *UploadFailed) Code() protocol.PeerCode {
	return protocol.PeerUploadFailed
}

// Encode writes the UploadFailed message.
func (m *UploadFailed) Encode(w *protocol.Writer) {
	w.WriteUint32(uint32(protocol.PeerUploadFailed))
	w.WriteString(m.Filename)
}

// DecodeUploadFailed reads an UploadFailed from the payload.
func DecodeUploadFailed(payload []byte) (*UploadFailed, error) {
	r := protocol.NewReader(bytes.NewReader(payload))

	code := r.ReadUint32()
	if code != uint32(protocol.PeerUploadFailed) {
		return nil, fmt.Errorf("unexpected code %d, expected %d", code, protocol.PeerUploadFailed)
	}

	msg := &UploadFailed{
		Filename: r.ReadString(),
	}

	if err := r.Error(); err != nil {
		return nil, fmt.Errorf("decode upload failed: %w", err)
	}

	return msg, nil
}

// UploadDenied indicates a transfer was explicitly denied.
// Code 50.
type UploadDenied struct {
	Filename string
	Reason   string
}

// Code returns the peer message code.
func (m *UploadDenied) Code() protocol.PeerCode {
	return protocol.PeerUploadDenied
}

// Encode writes the UploadDenied message.
func (m *UploadDenied) Encode(w *protocol.Writer) {
	w.WriteUint32(uint32(protocol.PeerUploadDenied))
	w.WriteString(m.Filename)
	w.WriteString(m.Reason)
}

// DecodeUploadDenied reads an UploadDenied from the payload.
func DecodeUploadDenied(payload []byte) (*UploadDenied, error) {
	r := protocol.NewReader(bytes.NewReader(payload))

	code := r.ReadUint32()
	if code != uint32(protocol.PeerUploadDenied) {
		return nil, fmt.Errorf("unexpected code %d, expected %d", code, protocol.PeerUploadDenied)
	}

	msg := &UploadDenied{
		Filename: r.ReadString(),
		Reason:   r.ReadString(),
	}

	if err := r.Error(); err != nil {
		return nil, fmt.Errorf("decode upload denied: %w", err)
	}

	return msg, nil
}
