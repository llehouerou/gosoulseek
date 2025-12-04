package client

import (
	"fmt"

	"github.com/llehouerou/gosoulseek/messages/peer"
)

// DuplicateTransferError is returned when attempting to register a transfer
// that already exists for the same user, file, and direction.
type DuplicateTransferError struct {
	Direction peer.TransferDirection
	Username  string
	Filename  string
}

func (e *DuplicateTransferError) Error() string {
	dir := "download"
	if e.Direction == peer.TransferUpload {
		dir = "upload"
	}
	return fmt.Sprintf("duplicate %s: %s/%s", dir, e.Username, e.Filename)
}

// TransferNotFoundError is returned when a transfer lookup fails.
type TransferNotFoundError struct {
	Token uint32
}

func (e *TransferNotFoundError) Error() string {
	return fmt.Sprintf("transfer not found: token %d", e.Token)
}

// TransferRejectedError is returned when a peer rejects a transfer request.
type TransferRejectedError struct {
	Reason string
}

func (e *TransferRejectedError) Error() string {
	return "transfer rejected: " + e.Reason
}

// TransferSizeMismatchError is returned when the local and remote file sizes don't match.
type TransferSizeMismatchError struct {
	LocalSize  int64
	RemoteSize int64
}

func (e *TransferSizeMismatchError) Error() string {
	return fmt.Sprintf("size mismatch: local %d, remote %d", e.LocalSize, e.RemoteSize)
}

// TransferFailedError is returned when the remote peer reports the transfer failed.
// This is distinct from rejection - the peer accepted the transfer but couldn't complete it.
type TransferFailedError struct {
	Username string
	Filename string
}

func (e *TransferFailedError) Error() string {
	return fmt.Sprintf("transfer failed: peer %s reported failure for %s", e.Username, e.Filename)
}

// ConnectionError wraps connection failures during transfers with additional context.
type ConnectionError struct {
	Operation  string // "read" or "write"
	BytesSoFar int64
	TotalBytes int64
	Underlying error
}

func (e *ConnectionError) Error() string {
	return fmt.Sprintf("connection %s error at byte %d/%d: %v", e.Operation, e.BytesSoFar, e.TotalBytes, e.Underlying)
}

func (e *ConnectionError) Unwrap() error {
	return e.Underlying
}
