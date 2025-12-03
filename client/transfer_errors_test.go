package client

import (
	"strings"
	"testing"

	"github.com/llehouerou/gosoulseek/messages/peer"
)

func TestDuplicateTransferError(t *testing.T) {
	tests := []struct {
		name     string
		err      *DuplicateTransferError
		wantPart string
	}{
		{
			name: "download",
			err: &DuplicateTransferError{
				Direction: peer.TransferDownload,
				Username:  "user1",
				Filename:  "file.mp3",
			},
			wantPart: "download",
		},
		{
			name: "upload",
			err: &DuplicateTransferError{
				Direction: peer.TransferUpload,
				Username:  "user2",
				Filename:  "doc.pdf",
			},
			wantPart: "upload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			if !strings.Contains(msg, tt.wantPart) {
				t.Errorf("Error() = %q, want to contain %q", msg, tt.wantPart)
			}
			if !strings.Contains(msg, tt.err.Username) {
				t.Errorf("Error() = %q, want to contain %q", msg, tt.err.Username)
			}
		})
	}
}

func TestTransferNotFoundError(t *testing.T) {
	err := &TransferNotFoundError{Token: 12345}
	msg := err.Error()

	if !strings.Contains(msg, "12345") {
		t.Errorf("Error() = %q, want to contain token", msg)
	}
}

func TestTransferRejectedError(t *testing.T) {
	err := &TransferRejectedError{Reason: "access denied"}
	msg := err.Error()

	if !strings.Contains(msg, "access denied") {
		t.Errorf("Error() = %q, want to contain reason", msg)
	}
}

func TestTransferSizeMismatchError(t *testing.T) {
	err := &TransferSizeMismatchError{
		LocalSize:  1000,
		RemoteSize: 2000,
	}
	msg := err.Error()

	if !strings.Contains(msg, "1000") || !strings.Contains(msg, "2000") {
		t.Errorf("Error() = %q, want to contain sizes", msg)
	}
}
