package client

import (
	"errors"
	"sync"
	"testing"

	"github.com/llehouerou/gosoulseek/messages/peer"
)

func TestTransferRegistry_RegisterDownload(t *testing.T) {
	reg := NewTransferRegistry()

	tr, err := reg.RegisterDownload("user1", "file.mp3", 12345)
	if err != nil {
		t.Fatalf("RegisterDownload failed: %v", err)
	}

	if tr.Direction != peer.TransferDownload {
		t.Errorf("Direction = %v, want %v", tr.Direction, peer.TransferDownload)
	}
	if tr.Username != "user1" {
		t.Errorf("Username = %q, want %q", tr.Username, "user1")
	}
	if tr.Filename != "file.mp3" {
		t.Errorf("Filename = %q, want %q", tr.Filename, "file.mp3")
	}
	if tr.Token != 12345 {
		t.Errorf("Token = %d, want %d", tr.Token, 12345)
	}
}

func TestTransferRegistry_RegisterUpload(t *testing.T) {
	reg := NewTransferRegistry()

	tr, err := reg.RegisterUpload("user2", "doc.pdf", 54321, 1024)
	if err != nil {
		t.Fatalf("RegisterUpload failed: %v", err)
	}

	if tr.Direction != peer.TransferUpload {
		t.Errorf("Direction = %v, want %v", tr.Direction, peer.TransferUpload)
	}
	if tr.Size != 1024 {
		t.Errorf("Size = %d, want %d", tr.Size, 1024)
	}
}

func TestTransferRegistry_DuplicateDownload(t *testing.T) {
	reg := NewTransferRegistry()

	_, err := reg.RegisterDownload("user1", "file.mp3", 1)
	if err != nil {
		t.Fatalf("First RegisterDownload failed: %v", err)
	}

	// Try to register same file again (different token)
	_, err = reg.RegisterDownload("user1", "file.mp3", 2)
	if err == nil {
		t.Error("Expected error for duplicate download, got nil")
	}

	// Should be a DuplicateTransferError
	var dupErr *DuplicateTransferError
	if !errors.As(err, &dupErr) {
		t.Errorf("Expected DuplicateTransferError, got %T", err)
	}
}

func TestTransferRegistry_DuplicateUpload(t *testing.T) {
	reg := NewTransferRegistry()

	_, err := reg.RegisterUpload("user1", "file.mp3", 1, 1000)
	if err != nil {
		t.Fatalf("First RegisterUpload failed: %v", err)
	}

	// Try to register same file again
	_, err = reg.RegisterUpload("user1", "file.mp3", 2, 1000)
	if err == nil {
		t.Error("Expected error for duplicate upload, got nil")
	}
}

func TestTransferRegistry_SameFileDifferentDirection(t *testing.T) {
	reg := NewTransferRegistry()

	// Download and upload of same file should be allowed
	_, err := reg.RegisterDownload("user1", "file.mp3", 1)
	if err != nil {
		t.Fatalf("RegisterDownload failed: %v", err)
	}

	_, err = reg.RegisterUpload("user1", "file.mp3", 2, 1000)
	if err != nil {
		t.Fatalf("RegisterUpload should succeed for different direction: %v", err)
	}
}

func TestTransferRegistry_GetByToken(t *testing.T) {
	reg := NewTransferRegistry()

	tr, _ := reg.RegisterDownload("user1", "file.mp3", 12345)

	found, ok := reg.GetByToken(12345)
	if !ok {
		t.Fatal("GetByToken should find registered transfer")
	}
	if found != tr {
		t.Error("GetByToken returned different transfer")
	}

	// Non-existent token
	_, ok = reg.GetByToken(99999)
	if ok {
		t.Error("GetByToken should not find non-existent token")
	}
}

func TestTransferRegistry_GetByFile(t *testing.T) {
	reg := NewTransferRegistry()

	tr, _ := reg.RegisterDownload("user1", "file.mp3", 12345)

	found, ok := reg.GetByFile("user1", "file.mp3", peer.TransferDownload)
	if !ok {
		t.Fatal("GetByFile should find registered transfer")
	}
	if found != tr {
		t.Error("GetByFile returned different transfer")
	}

	// Wrong direction
	_, ok = reg.GetByFile("user1", "file.mp3", peer.TransferUpload)
	if ok {
		t.Error("GetByFile should not find with wrong direction")
	}

	// Wrong user
	_, ok = reg.GetByFile("user2", "file.mp3", peer.TransferDownload)
	if ok {
		t.Error("GetByFile should not find with wrong user")
	}
}

func TestTransferRegistry_SetRemoteToken(t *testing.T) {
	reg := NewTransferRegistry()

	tr, _ := reg.RegisterDownload("user1", "file.mp3", 12345)

	err := reg.SetRemoteToken(12345, 9999)
	if err != nil {
		t.Fatalf("SetRemoteToken failed: %v", err)
	}

	if tr.RemoteToken != 9999 {
		t.Errorf("RemoteToken = %d, want %d", tr.RemoteToken, 9999)
	}

	// Should now be findable by remote token
	found, ok := reg.GetByRemoteToken("user1", 9999)
	if !ok {
		t.Fatal("GetByRemoteToken should find transfer after SetRemoteToken")
	}
	if found != tr {
		t.Error("GetByRemoteToken returned different transfer")
	}
}

func TestTransferRegistry_SetRemoteToken_NotFound(t *testing.T) {
	reg := NewTransferRegistry()

	err := reg.SetRemoteToken(99999, 1234)
	if err == nil {
		t.Error("SetRemoteToken should fail for non-existent token")
	}
}

func TestTransferRegistry_Remove(t *testing.T) {
	reg := NewTransferRegistry()

	tr, _ := reg.RegisterDownload("user1", "file.mp3", 12345)
	_ = reg.SetRemoteToken(12345, 9999)

	// Remove it
	reg.Remove(12345)

	// Should not be findable by any index
	if _, ok := reg.GetByToken(12345); ok {
		t.Error("Should not find by token after Remove")
	}
	if _, ok := reg.GetByFile("user1", "file.mp3", peer.TransferDownload); ok {
		t.Error("Should not find by file after Remove")
	}
	if _, ok := reg.GetByRemoteToken("user1", 9999); ok {
		t.Error("Should not find by remote token after Remove")
	}

	// Should be able to register same file again
	_, err := reg.RegisterDownload("user1", "file.mp3", 54321)
	if err != nil {
		t.Errorf("Should be able to re-register after Remove: %v", err)
	}

	// Verify channel was closed
	select {
	case _, ok := <-tr.Progress():
		if ok {
			t.Error("Progress channel should be closed")
		}
	default:
		// This is expected if channel is already drained
	}
}

func TestTransferRegistry_All(t *testing.T) {
	reg := NewTransferRegistry()

	_, _ = reg.RegisterDownload("user1", "file1.mp3", 1)
	_, _ = reg.RegisterDownload("user2", "file2.mp3", 2)
	_, _ = reg.RegisterUpload("user3", "file3.mp3", 3, 1000)

	all := reg.All()
	if len(all) != 3 {
		t.Errorf("All() returned %d transfers, want 3", len(all))
	}
}

func TestTransferRegistry_Downloads(t *testing.T) {
	reg := NewTransferRegistry()

	_, _ = reg.RegisterDownload("user1", "file1.mp3", 1)
	_, _ = reg.RegisterDownload("user2", "file2.mp3", 2)
	_, _ = reg.RegisterUpload("user3", "file3.mp3", 3, 1000)

	downloads := reg.Downloads()
	if len(downloads) != 2 {
		t.Errorf("Downloads() returned %d transfers, want 2", len(downloads))
	}
}

func TestTransferRegistry_Uploads(t *testing.T) {
	reg := NewTransferRegistry()

	_, _ = reg.RegisterDownload("user1", "file1.mp3", 1)
	_, _ = reg.RegisterUpload("user2", "file2.mp3", 2, 1000)
	_, _ = reg.RegisterUpload("user3", "file3.mp3", 3, 2000)

	uploads := reg.Uploads()
	if len(uploads) != 2 {
		t.Errorf("Uploads() returned %d transfers, want 2", len(uploads))
	}
}

func TestTransferRegistry_ConcurrentAccess(t *testing.T) {
	reg := NewTransferRegistry()

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	// Concurrent registrations
	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := reg.RegisterDownload("user", "file"+string(rune('A'+n)), uint32(n)) //nolint:gosec // test code
			if err != nil {
				errCh <- err
			}
		}(i)
	}

	// Concurrent lookups
	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			reg.GetByToken(uint32(n)) //nolint:gosec // test code
			reg.All()
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("Concurrent operation failed: %v", err)
	}
}

func TestTransferRegistry_SetState(t *testing.T) {
	reg := NewTransferRegistry()

	tr, _ := reg.RegisterDownload("user1", "file.mp3", 12345)

	err := reg.SetState(12345, TransferStateInProgress)
	if err != nil {
		t.Fatalf("SetState failed: %v", err)
	}

	if tr.State() != TransferStateInProgress {
		t.Errorf("State = %v, want %v", tr.State(), TransferStateInProgress)
	}
}

func TestTransferRegistry_SetState_NotFound(t *testing.T) {
	reg := NewTransferRegistry()

	err := reg.SetState(99999, TransferStateInProgress)
	if err == nil {
		t.Error("SetState should fail for non-existent token")
	}
}

func TestTransferRegistry_UpdateProgress(t *testing.T) {
	reg := NewTransferRegistry()

	tr, _ := reg.RegisterDownload("user1", "file.mp3", 12345)
	tr.Size = 1000

	err := reg.UpdateProgress(12345, 500)
	if err != nil {
		t.Fatalf("UpdateProgress failed: %v", err)
	}

	if tr.Transferred != 500 {
		t.Errorf("Transferred = %d, want %d", tr.Transferred, 500)
	}
}

func TestTransferRegistry_SetSize(t *testing.T) {
	reg := NewTransferRegistry()

	tr, _ := reg.RegisterDownload("user1", "file.mp3", 12345)

	err := reg.SetSize(12345, 5000)
	if err != nil {
		t.Fatalf("SetSize failed: %v", err)
	}

	if tr.Size != 5000 {
		t.Errorf("Size = %d, want %d", tr.Size, 5000)
	}
}

func TestTransferRegistry_Complete(t *testing.T) {
	reg := NewTransferRegistry()

	tr, _ := reg.RegisterDownload("user1", "file.mp3", 12345)

	err := reg.Complete(12345, TransferStateCompleted|TransferStateSucceeded, nil)
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	if !tr.State().IsCompleted() {
		t.Error("State should be completed")
	}
	if tr.State()&TransferStateSucceeded == 0 {
		t.Error("State should include Succeeded")
	}
}

func TestTransferRegistry_Complete_WithError(t *testing.T) {
	reg := NewTransferRegistry()

	tr, _ := reg.RegisterDownload("user1", "file.mp3", 12345)

	testErr := &TransferRejectedError{Reason: "access denied"}
	err := reg.Complete(12345, TransferStateCompleted|TransferStateRejected, testErr)
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	if !errors.Is(tr.Error, testErr) {
		t.Error("Transfer error should be set")
	}
}
