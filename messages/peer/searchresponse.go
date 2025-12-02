package peer

import (
	"bytes"
	"fmt"

	"github.com/llehouerou/gosoulseek/protocol"
)

// SearchResponse contains search results from a single peer.
type SearchResponse struct {
	Username    string // Peer's username
	Token       uint32 // Search token (matches the search request)
	Files       []File // Available files matching the search
	LockedFiles []File // Files the user won't share (premium users only)
	HasFreeSlot bool   // Peer has a free upload slot
	UploadSpeed uint32 // Peer's upload speed in bytes/second
	QueueLength uint32 // Number of files queued for upload
}

// Code returns the peer message code.
func (r *SearchResponse) Code() protocol.PeerCode {
	return protocol.PeerSearchResponse
}

// DecodeSearchResponse parses a compressed search response.
// The data should include the 4-byte message code prefix.
func DecodeSearchResponse(data []byte) (*SearchResponse, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("data too short: %d bytes", len(data))
	}

	// Skip 4-byte peer code
	compressed := data[4:]

	decompressed, err := protocol.Decompress(compressed)
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}

	return decodeSearchResponsePayload(decompressed)
}

// decodeSearchResponsePayload parses the decompressed search response.
func decodeSearchResponsePayload(data []byte) (*SearchResponse, error) {
	r := protocol.NewReader(bytes.NewReader(data))

	resp := &SearchResponse{
		Username: r.ReadString(),
		Token:    r.ReadUint32(),
	}

	// Read files
	fileCount := r.ReadUint32()
	resp.Files = make([]File, 0, fileCount)
	for range fileCount {
		resp.Files = append(resp.Files, DecodeFile(r))
	}

	// Read metadata
	resp.HasFreeSlot = r.ReadUint8() == 1
	resp.UploadSpeed = r.ReadUint32()
	resp.QueueLength = r.ReadUint32()

	// Skip unknown field for compatibility (some clients send extra data)
	if r.Error() == nil {
		_ = r.ReadUint32() // unknown/reserved
	}

	// Read locked files (if present)
	if r.Error() == nil {
		lockedCount := r.ReadUint32()
		resp.LockedFiles = make([]File, 0, lockedCount)
		for range lockedCount {
			resp.LockedFiles = append(resp.LockedFiles, DecodeFile(r))
		}
	}

	if err := r.Error(); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}

	return resp, nil
}
