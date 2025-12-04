package client

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// ErrSlotManagerClosed is returned when acquiring slots on a closed manager.
var ErrSlotManagerClosed = errors.New("slot manager closed")

// SlotManager controls concurrent transfer limits using Go-idiomatic patterns.
// Download slots limit concurrent downloads across all users.
// Upload slots are two-tiered: per-user (protocol requirement) and global limits.
type SlotManager struct {
	downloadSlots chan struct{} // buffered to maxDownloads (nil if unlimited)
	uploadSlots   chan struct{} // buffered to maxUploads (nil if unlimited)
	userSlots     sync.Map      // username -> *userSlot

	maxDownloads int
	maxUploads   int

	// Track active counts for unlimited mode
	downloadsActive atomic.Int64
	uploadsActive   atomic.Int64

	closed   atomic.Bool
	closedCh chan struct{}
}

// userSlot manages per-user upload concurrency (Soulseek protocol: 1 upload per user).
type userSlot struct {
	sem      chan struct{} // 1-buffered channel
	lastUsed atomic.Value  // time.Time - for cleanup
}

// SlotStats contains current slot usage information.
type SlotStats struct {
	DownloadsActive int
	DownloadsMax    int
	UploadsActive   int
	UploadsMax      int
	UserSlotsCount  int // Number of cached per-user slots
}

// NewSlotManager creates a new slot manager with the given concurrency limits.
// A limit of 0 means unlimited.
func NewSlotManager(maxDownloads, maxUploads int) *SlotManager {
	sm := &SlotManager{
		maxDownloads: maxDownloads,
		maxUploads:   maxUploads,
		closedCh:     make(chan struct{}),
	}

	// Create buffered channels as semaphores (nil = unlimited)
	if maxDownloads > 0 {
		sm.downloadSlots = make(chan struct{}, maxDownloads)
	}
	if maxUploads > 0 {
		sm.uploadSlots = make(chan struct{}, maxUploads)
	}

	return sm
}

// AcquireDownloadSlot blocks until a download slot is available or ctx is cancelled.
// Returns nil on success, ctx.Err() on cancellation, ErrSlotManagerClosed if closed.
func (s *SlotManager) AcquireDownloadSlot(ctx context.Context) error {
	if s.closed.Load() {
		return ErrSlotManagerClosed
	}

	// Unlimited mode
	if s.downloadSlots == nil {
		s.downloadsActive.Add(1)
		return nil
	}

	// Acquire slot from buffered channel
	select {
	case s.downloadSlots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.closedCh:
		return ErrSlotManagerClosed
	}
}

// ReleaseDownloadSlot releases a previously acquired download slot.
func (s *SlotManager) ReleaseDownloadSlot() {
	// Unlimited mode
	if s.downloadSlots == nil {
		s.downloadsActive.Add(-1)
		return
	}

	// Release slot
	select {
	case <-s.downloadSlots:
		// Released
	default:
		// Already empty (idempotent)
	}
}

// AcquireUploadSlot acquires both a per-user slot and a global upload slot.
// Per-user slot ensures only 1 upload per user (Soulseek protocol requirement).
// Returns nil on success, ctx.Err() on cancellation, ErrSlotManagerClosed if closed.
func (s *SlotManager) AcquireUploadSlot(ctx context.Context, username string) error {
	if s.closed.Load() {
		return ErrSlotManagerClosed
	}

	// Stage 1: Per-user slot (protocol requirement)
	userSlot := s.getOrCreateUserSlot(username)
	if err := userSlot.acquire(ctx, s.closedCh); err != nil {
		return err
	}

	// Stage 2: Global upload slot
	if err := s.acquireGlobalUpload(ctx); err != nil {
		// Release per-user slot on failure
		userSlot.release()
		return err
	}

	return nil
}

// ReleaseUploadSlot releases both per-user and global upload slots.
func (s *SlotManager) ReleaseUploadSlot(username string) {
	// Release per-user slot
	if v, ok := s.userSlots.Load(username); ok {
		if slot, ok := v.(*userSlot); ok {
			slot.release()
		}
	}

	// Release global slot
	s.releaseGlobalUpload()
}

// Stats returns current slot usage statistics.
func (s *SlotManager) Stats() SlotStats {
	var downloadsActive, uploadsActive int

	if s.downloadSlots != nil {
		downloadsActive = len(s.downloadSlots)
	} else {
		downloadsActive = int(s.downloadsActive.Load())
	}

	if s.uploadSlots != nil {
		uploadsActive = len(s.uploadSlots)
	} else {
		uploadsActive = int(s.uploadsActive.Load())
	}

	// Count user slots
	var userSlotsCount int
	s.userSlots.Range(func(_, _ any) bool {
		userSlotsCount++
		return true
	})

	return SlotStats{
		DownloadsActive: downloadsActive,
		DownloadsMax:    s.maxDownloads,
		UploadsActive:   uploadsActive,
		UploadsMax:      s.maxUploads,
		UserSlotsCount:  userSlotsCount,
	}
}

// Close shuts down the slot manager, waking any blocked acquisitions.
func (s *SlotManager) Close() error {
	if s.closed.Swap(true) {
		return nil // Already closed
	}
	close(s.closedCh)
	return nil
}

// getOrCreateUserSlot returns the per-user slot for the given username, creating if needed.
func (s *SlotManager) getOrCreateUserSlot(username string) *userSlot {
	if v, ok := s.userSlots.Load(username); ok {
		if slot, ok := v.(*userSlot); ok {
			slot.lastUsed.Store(time.Now())
			return slot
		}
	}

	// Create new slot (1-buffered = 1 concurrent upload per user)
	slot := &userSlot{
		sem: make(chan struct{}, 1),
	}
	slot.lastUsed.Store(time.Now())

	// LoadOrStore handles race condition
	if existing, loaded := s.userSlots.LoadOrStore(username, slot); loaded {
		if existingSlot, ok := existing.(*userSlot); ok {
			return existingSlot
		}
	}
	return slot
}

// acquireGlobalUpload acquires a global upload slot.
func (s *SlotManager) acquireGlobalUpload(ctx context.Context) error {
	// Unlimited mode
	if s.uploadSlots == nil {
		s.uploadsActive.Add(1)
		return nil
	}

	select {
	case s.uploadSlots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.closedCh:
		return ErrSlotManagerClosed
	}
}

// releaseGlobalUpload releases a global upload slot.
func (s *SlotManager) releaseGlobalUpload() {
	// Unlimited mode
	if s.uploadSlots == nil {
		s.uploadsActive.Add(-1)
		return
	}

	select {
	case <-s.uploadSlots:
		// Released
	default:
		// Already empty (idempotent)
	}
}

// acquire blocks until the per-user slot is available.
func (u *userSlot) acquire(ctx context.Context, closedCh <-chan struct{}) error {
	u.lastUsed.Store(time.Now())

	select {
	case u.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-closedCh:
		return ErrSlotManagerClosed
	}
}

// release releases the per-user slot.
func (u *userSlot) release() {
	u.lastUsed.Store(time.Now())

	select {
	case <-u.sem:
		// Released
	default:
		// Already empty (idempotent)
	}
}

// isIdle returns true if the slot is not currently held.
func (u *userSlot) isIdle() bool {
	select {
	case u.sem <- struct{}{}:
		// Was idle, now we hold it
		<-u.sem // release immediately
		return true
	default:
		// In use
		return false
	}
}

// NewSlotManagerWithCleanup creates a slot manager with background cleanup.
// cleanupInterval controls how often idle user slots are cleaned up.
// idleThreshold is how long a slot must be idle before cleanup.
// Use cleanupInterval of 0 to disable automatic cleanup.
func NewSlotManagerWithCleanup(maxDownloads, maxUploads int, cleanupInterval, idleThreshold time.Duration) *SlotManager {
	sm := NewSlotManager(maxDownloads, maxUploads)

	if cleanupInterval > 0 {
		go sm.cleanupLoop(cleanupInterval, idleThreshold)
	}

	return sm
}

// cleanupLoop runs periodic cleanup of idle user slots.
func (s *SlotManager) cleanupLoop(interval, idleThreshold time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.CleanupIdleUserSlots(idleThreshold)
		case <-s.closedCh:
			return
		}
	}
}

// CleanupIdleUserSlots removes user slots that have been idle for longer than threshold.
// A slot is considered idle if it's not currently held and its lastUsed time is older than threshold.
// Use threshold of 0 to clean all idle slots regardless of time.
func (s *SlotManager) CleanupIdleUserSlots(threshold time.Duration) {
	cutoff := time.Now().Add(-threshold)

	s.userSlots.Range(func(key, value any) bool {
		slot, ok := value.(*userSlot)
		if !ok {
			return true
		}

		// Check time threshold
		lastUsedVal := slot.lastUsed.Load()
		if lastUsedVal == nil {
			return true
		}
		lastUsed, ok := lastUsedVal.(time.Time)
		if !ok {
			return true
		}
		if threshold > 0 && lastUsed.After(cutoff) {
			return true // Not old enough, skip
		}

		// Only delete if slot is idle (not currently held)
		if slot.isIdle() {
			s.userSlots.Delete(key)
		}

		return true
	})
}
