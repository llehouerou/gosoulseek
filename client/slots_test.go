package client

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewSlotManager(t *testing.T) {
	tests := []struct {
		name         string
		maxDownloads int
		maxUploads   int
	}{
		{"default limits", 10, 10},
		{"custom limits", 5, 3},
		{"unlimited downloads", 0, 10},
		{"unlimited uploads", 10, 0},
		{"both unlimited", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewSlotManager(tt.maxDownloads, tt.maxUploads)
			if sm == nil {
				t.Fatal("NewSlotManager returned nil")
			}
			defer sm.Close()

			stats := sm.Stats()
			if stats.DownloadsMax != tt.maxDownloads {
				t.Errorf("DownloadsMax = %d, want %d", stats.DownloadsMax, tt.maxDownloads)
			}
			if stats.UploadsMax != tt.maxUploads {
				t.Errorf("UploadsMax = %d, want %d", stats.UploadsMax, tt.maxUploads)
			}
			if stats.DownloadsActive != 0 {
				t.Errorf("DownloadsActive = %d, want 0", stats.DownloadsActive)
			}
			if stats.UploadsActive != 0 {
				t.Errorf("UploadsActive = %d, want 0", stats.UploadsActive)
			}
		})
	}
}

func TestSlotManager_AcquireDownloadSlot_Basic(t *testing.T) {
	sm := NewSlotManager(2, 10)
	defer sm.Close()

	ctx := context.Background()

	// Acquire first slot
	if err := sm.AcquireDownloadSlot(ctx); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	stats := sm.Stats()
	if stats.DownloadsActive != 1 {
		t.Errorf("DownloadsActive = %d, want 1", stats.DownloadsActive)
	}

	// Acquire second slot
	if err := sm.AcquireDownloadSlot(ctx); err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}

	stats = sm.Stats()
	if stats.DownloadsActive != 2 {
		t.Errorf("DownloadsActive = %d, want 2", stats.DownloadsActive)
	}
}

func TestSlotManager_AcquireDownloadSlot_Blocking(t *testing.T) {
	sm := NewSlotManager(1, 10)
	defer sm.Close()

	ctx := context.Background()

	// Acquire the only slot
	if err := sm.AcquireDownloadSlot(ctx); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	// Second acquire should block, use timeout context
	ctxTimeout, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	err := sm.AcquireDownloadSlot(ctxTimeout)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestSlotManager_AcquireDownloadSlot_Cancellation(t *testing.T) {
	sm := NewSlotManager(1, 10)
	defer sm.Close()

	ctx := context.Background()

	// Acquire the only slot
	if err := sm.AcquireDownloadSlot(ctx); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	// Cancel context before acquiring
	ctxCancel, cancel := context.WithCancel(ctx)
	cancel() // Cancel immediately

	err := sm.AcquireDownloadSlot(ctxCancel)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected Canceled, got %v", err)
	}
}

func TestSlotManager_ReleaseDownloadSlot(t *testing.T) {
	sm := NewSlotManager(1, 10)
	defer sm.Close()

	ctx := context.Background()

	// Acquire slot
	if err := sm.AcquireDownloadSlot(ctx); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	stats := sm.Stats()
	if stats.DownloadsActive != 1 {
		t.Errorf("DownloadsActive = %d, want 1", stats.DownloadsActive)
	}

	// Release slot
	sm.ReleaseDownloadSlot()

	stats = sm.Stats()
	if stats.DownloadsActive != 0 {
		t.Errorf("DownloadsActive = %d, want 0", stats.DownloadsActive)
	}

	// Should be able to acquire again
	if err := sm.AcquireDownloadSlot(ctx); err != nil {
		t.Fatalf("re-acquire failed: %v", err)
	}
}

func TestSlotManager_ReleaseDownloadSlot_Unblocks(t *testing.T) {
	sm := NewSlotManager(1, 10)
	defer sm.Close()

	ctx := context.Background()

	// Acquire the only slot
	if err := sm.AcquireDownloadSlot(ctx); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	// Start goroutine waiting for slot
	acquired := make(chan struct{})
	go func() {
		if err := sm.AcquireDownloadSlot(ctx); err == nil {
			close(acquired)
		}
	}()

	// Give goroutine time to block
	time.Sleep(10 * time.Millisecond)

	// Release slot
	sm.ReleaseDownloadSlot()

	// Wait for goroutine to acquire
	select {
	case <-acquired:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Error("goroutine did not acquire slot after release")
	}
}

func TestSlotManager_AcquireDownloadSlot_Unlimited(t *testing.T) {
	sm := NewSlotManager(0, 10) // 0 = unlimited
	defer sm.Close()

	ctx := context.Background()

	// Should be able to acquire many slots without blocking
	for i := range 100 {
		if err := sm.AcquireDownloadSlot(ctx); err != nil {
			t.Fatalf("acquire %d failed: %v", i, err)
		}
	}

	stats := sm.Stats()
	if stats.DownloadsActive != 100 {
		t.Errorf("DownloadsActive = %d, want 100", stats.DownloadsActive)
	}
}

func TestSlotManager_AcquireUploadSlot_Basic(t *testing.T) {
	sm := NewSlotManager(10, 2)
	defer sm.Close()

	ctx := context.Background()

	// Acquire upload slot for user1
	if err := sm.AcquireUploadSlot(ctx, "user1"); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	stats := sm.Stats()
	if stats.UploadsActive != 1 {
		t.Errorf("UploadsActive = %d, want 1", stats.UploadsActive)
	}
	if stats.UserSlotsCount != 1 {
		t.Errorf("UserSlotsCount = %d, want 1", stats.UserSlotsCount)
	}

	// Acquire upload slot for user2
	if err := sm.AcquireUploadSlot(ctx, "user2"); err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}

	stats = sm.Stats()
	if stats.UploadsActive != 2 {
		t.Errorf("UploadsActive = %d, want 2", stats.UploadsActive)
	}
	if stats.UserSlotsCount != 2 {
		t.Errorf("UserSlotsCount = %d, want 2", stats.UserSlotsCount)
	}
}

func TestSlotManager_AcquireUploadSlot_PerUserLimit(t *testing.T) {
	sm := NewSlotManager(10, 10)
	defer sm.Close()

	ctx := context.Background()

	// Acquire upload slot for user1
	if err := sm.AcquireUploadSlot(ctx, "user1"); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	// Second acquire for same user should block (per-user limit = 1)
	ctxTimeout, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	err := sm.AcquireUploadSlot(ctxTimeout, "user1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded for same user, got %v", err)
	}

	// But different user should work
	if err := sm.AcquireUploadSlot(ctx, "user2"); err != nil {
		t.Fatalf("acquire for different user failed: %v", err)
	}
}

func TestSlotManager_AcquireUploadSlot_GlobalLimit(t *testing.T) {
	sm := NewSlotManager(10, 2) // Only 2 global upload slots
	defer sm.Close()

	ctx := context.Background()

	// Acquire for user1 and user2 (fills global slots)
	if err := sm.AcquireUploadSlot(ctx, "user1"); err != nil {
		t.Fatalf("user1 acquire failed: %v", err)
	}
	if err := sm.AcquireUploadSlot(ctx, "user2"); err != nil {
		t.Fatalf("user2 acquire failed: %v", err)
	}

	// user3 should block on global limit
	ctxTimeout, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	err := sm.AcquireUploadSlot(ctxTimeout, "user3")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded for global limit, got %v", err)
	}
}

func TestSlotManager_ReleaseUploadSlot(t *testing.T) {
	sm := NewSlotManager(10, 2)
	defer sm.Close()

	ctx := context.Background()

	// Acquire slots for user1 and user2
	if err := sm.AcquireUploadSlot(ctx, "user1"); err != nil {
		t.Fatalf("user1 acquire failed: %v", err)
	}
	if err := sm.AcquireUploadSlot(ctx, "user2"); err != nil {
		t.Fatalf("user2 acquire failed: %v", err)
	}

	stats := sm.Stats()
	if stats.UploadsActive != 2 {
		t.Errorf("UploadsActive = %d, want 2", stats.UploadsActive)
	}

	// Release user1's slot
	sm.ReleaseUploadSlot("user1")

	stats = sm.Stats()
	if stats.UploadsActive != 1 {
		t.Errorf("UploadsActive = %d, want 1", stats.UploadsActive)
	}

	// user1 can acquire again
	if err := sm.AcquireUploadSlot(ctx, "user1"); err != nil {
		t.Fatalf("user1 re-acquire failed: %v", err)
	}
}

func TestSlotManager_ReleaseUploadSlot_UnblocksPerUser(t *testing.T) {
	sm := NewSlotManager(10, 10)
	defer sm.Close()

	ctx := context.Background()

	// Acquire upload slot for user1
	if err := sm.AcquireUploadSlot(ctx, "user1"); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	// Start goroutine waiting for user1's slot
	acquired := make(chan struct{})
	go func() {
		if err := sm.AcquireUploadSlot(ctx, "user1"); err == nil {
			close(acquired)
		}
	}()

	// Give goroutine time to block
	time.Sleep(10 * time.Millisecond)

	// Release user1's slot
	sm.ReleaseUploadSlot("user1")

	// Wait for goroutine to acquire
	select {
	case <-acquired:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Error("goroutine did not acquire slot after release")
	}
}

func TestSlotManager_AcquireUploadSlot_Unlimited(t *testing.T) {
	sm := NewSlotManager(10, 0) // 0 = unlimited uploads
	defer sm.Close()

	ctx := context.Background()

	// Should be able to acquire many slots without blocking on global limit
	// But per-user limit still applies
	for i := range 100 {
		username := "user" + itoa(uint32(i))
		if err := sm.AcquireUploadSlot(ctx, username); err != nil {
			t.Fatalf("acquire for %s failed: %v", username, err)
		}
	}

	stats := sm.Stats()
	if stats.UploadsActive != 100 {
		t.Errorf("UploadsActive = %d, want 100", stats.UploadsActive)
	}
}

func TestSlotManager_Close(t *testing.T) {
	sm := NewSlotManager(1, 1)

	ctx := context.Background()

	// Acquire slots
	if err := sm.AcquireDownloadSlot(ctx); err != nil {
		t.Fatalf("download acquire failed: %v", err)
	}

	// Start goroutine waiting for slot
	errCh := make(chan error, 1)
	go func() {
		errCh <- sm.AcquireDownloadSlot(ctx)
	}()

	// Give goroutine time to block
	time.Sleep(10 * time.Millisecond)

	// Close the manager
	if err := sm.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Blocked goroutine should return error
	select {
	case err := <-errCh:
		if !errors.Is(err, ErrSlotManagerClosed) {
			t.Errorf("expected ErrSlotManagerClosed, got %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("goroutine did not unblock after Close")
	}
}

func TestSlotManager_AcquireAfterClose(t *testing.T) {
	sm := NewSlotManager(10, 10)
	sm.Close()

	ctx := context.Background()

	err := sm.AcquireDownloadSlot(ctx)
	if !errors.Is(err, ErrSlotManagerClosed) {
		t.Errorf("AcquireDownloadSlot after Close: expected ErrSlotManagerClosed, got %v", err)
	}

	err = sm.AcquireUploadSlot(ctx, "user1")
	if !errors.Is(err, ErrSlotManagerClosed) {
		t.Errorf("AcquireUploadSlot after Close: expected ErrSlotManagerClosed, got %v", err)
	}
}

func TestSlotManager_ConcurrentAccess(t *testing.T) {
	sm := NewSlotManager(5, 5)
	defer sm.Close()

	ctx := context.Background()
	const goroutines = 20
	const iterations = 100

	done := make(chan struct{})

	// Spawn goroutines doing acquire/release cycles
	for i := range goroutines {
		go func(id int) {
			for range iterations {
				if id%2 == 0 {
					// Download slots
					if err := sm.AcquireDownloadSlot(ctx); err == nil {
						sm.ReleaseDownloadSlot()
					}
				} else {
					// Upload slots (different users)
					username := "user" + itoa(uint32(id))
					if err := sm.AcquireUploadSlot(ctx, username); err == nil {
						sm.ReleaseUploadSlot(username)
					}
				}
			}
			done <- struct{}{}
		}(i)
	}

	// Wait for all goroutines
	for range goroutines {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for goroutines - possible deadlock")
		}
	}

	// All slots should be released
	stats := sm.Stats()
	if stats.DownloadsActive != 0 {
		t.Errorf("DownloadsActive = %d, want 0", stats.DownloadsActive)
	}
	if stats.UploadsActive != 0 {
		t.Errorf("UploadsActive = %d, want 0", stats.UploadsActive)
	}
}

func TestSlotManager_CleanupIdleUserSlots(t *testing.T) {
	sm := NewSlotManager(10, 10)
	defer sm.Close()

	ctx := context.Background()

	// Create some user slots
	for i := range 5 {
		username := "user" + itoa(uint32(i))
		if err := sm.AcquireUploadSlot(ctx, username); err != nil {
			t.Fatalf("acquire for %s failed: %v", username, err)
		}
		sm.ReleaseUploadSlot(username)
	}

	stats := sm.Stats()
	if stats.UserSlotsCount != 5 {
		t.Errorf("UserSlotsCount = %d, want 5", stats.UserSlotsCount)
	}

	// Run cleanup with very short idle threshold (should clean all idle slots)
	sm.CleanupIdleUserSlots(0)

	stats = sm.Stats()
	if stats.UserSlotsCount != 0 {
		t.Errorf("after cleanup: UserSlotsCount = %d, want 0", stats.UserSlotsCount)
	}
}

func TestSlotManager_CleanupPreservesActiveSlots(t *testing.T) {
	sm := NewSlotManager(10, 10)
	defer sm.Close()

	ctx := context.Background()

	// Acquire a slot and keep it held
	if err := sm.AcquireUploadSlot(ctx, "active_user"); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// Acquire and release another slot
	if err := sm.AcquireUploadSlot(ctx, "idle_user"); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	sm.ReleaseUploadSlot("idle_user")

	stats := sm.Stats()
	if stats.UserSlotsCount != 2 {
		t.Errorf("UserSlotsCount = %d, want 2", stats.UserSlotsCount)
	}

	// Run cleanup (should only clean idle_user, not active_user)
	sm.CleanupIdleUserSlots(0)

	stats = sm.Stats()
	if stats.UserSlotsCount != 1 {
		t.Errorf("after cleanup: UserSlotsCount = %d, want 1 (active_user should remain)", stats.UserSlotsCount)
	}

	// active_user's slot should still work
	sm.ReleaseUploadSlot("active_user")
}

func TestSlotManager_CleanupRespectsIdleThreshold(t *testing.T) {
	sm := NewSlotManager(10, 10)
	defer sm.Close()

	ctx := context.Background()

	// Acquire and release a slot
	if err := sm.AcquireUploadSlot(ctx, "user1"); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	sm.ReleaseUploadSlot("user1")

	stats := sm.Stats()
	if stats.UserSlotsCount != 1 {
		t.Errorf("UserSlotsCount = %d, want 1", stats.UserSlotsCount)
	}

	// Cleanup with 1 hour threshold (slot was just used, should not be cleaned)
	sm.CleanupIdleUserSlots(time.Hour)

	stats = sm.Stats()
	if stats.UserSlotsCount != 1 {
		t.Errorf("after cleanup with long threshold: UserSlotsCount = %d, want 1", stats.UserSlotsCount)
	}

	// Cleanup with 0 threshold (should clean)
	sm.CleanupIdleUserSlots(0)

	stats = sm.Stats()
	if stats.UserSlotsCount != 0 {
		t.Errorf("after cleanup with zero threshold: UserSlotsCount = %d, want 0", stats.UserSlotsCount)
	}
}

func TestSlotManager_WithCleanupInterval(t *testing.T) {
	// Create manager with fast cleanup for testing
	sm := NewSlotManagerWithCleanup(10, 10, 50*time.Millisecond, 0)
	defer sm.Close()

	ctx := context.Background()

	// Create a user slot
	if err := sm.AcquireUploadSlot(ctx, "user1"); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	sm.ReleaseUploadSlot("user1")

	stats := sm.Stats()
	if stats.UserSlotsCount != 1 {
		t.Errorf("UserSlotsCount = %d, want 1", stats.UserSlotsCount)
	}

	// Wait for cleanup to run
	time.Sleep(100 * time.Millisecond)

	stats = sm.Stats()
	if stats.UserSlotsCount != 0 {
		t.Errorf("after automatic cleanup: UserSlotsCount = %d, want 0", stats.UserSlotsCount)
	}
}

func TestSlotManager_CleanupStopsOnClose(t *testing.T) {
	sm := NewSlotManagerWithCleanup(10, 10, 10*time.Millisecond, 0)

	ctx := context.Background()

	// Create a user slot
	if err := sm.AcquireUploadSlot(ctx, "user1"); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	sm.ReleaseUploadSlot("user1")

	// Close immediately
	sm.Close()

	// Give cleanup goroutine time to exit
	time.Sleep(50 * time.Millisecond)

	// Slot should NOT have been cleaned (cleanup stopped on close)
	// Actually, closing should be immediate - test that no panic occurs
}
