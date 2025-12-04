# Task 3.1: Semaphore-Based Slot Management

## Overview

Implement slot management for concurrent transfer limits using Go-idiomatic patterns. This provides concurrency control that prevents overwhelming local resources or violating Soulseek protocol constraints.

## Analysis of .NET Reference

The Soulseek.NET implementation uses a **three-tier semaphore strategy**:

1. **Global Download Semaphore**: Limits concurrent downloads across all users
2. **Global Upload Semaphore**: Limits concurrent uploads across all users
3. **Per-User Upload Semaphore**: Protocol requirement - maximum 1 upload per user at a time

Key patterns from .NET:
- `SemaphoreSlim` for all slot management
- All acquisitions accept `CancellationToken`
- Per-user semaphores are lazy-initialized and periodically cleaned up (every 15 min)
- Release happens in `finally` blocks for guaranteed cleanup
- Release order is same as acquisition order (not reversed)

## Go-Idiomatic Design

### Core Principles

1. **Buffered channels as semaphores** - Natural Go pattern for counting semaphores
2. **Context for cancellation** - All blocking operations accept `context.Context`
3. **sync.Map for per-user semaphores** - Thread-safe lazy initialization
4. **Deferred cleanup** - Go's `defer` for guaranteed release

### API Design

```go
// SlotManager controls concurrent transfer limits using Go-idiomatic patterns.
// Download slots limit concurrent downloads across all users.
// Upload slots are two-tiered: per-user (protocol requirement) and global limits.
type SlotManager struct {
    downloadSlots chan struct{} // buffered to maxDownloads
    uploadSlots   chan struct{} // buffered to maxUploads
    userSlots     sync.Map      // username -> *userSlot

    maxDownloads int
    maxUploads   int

    closed   atomic.Bool
    closedCh chan struct{}
}

// userSlot manages per-user upload concurrency (Soulseek protocol: 1 upload per user)
type userSlot struct {
    sem      chan struct{}  // 1-buffered channel
    lastUsed atomic.Value   // time.Time - for cleanup
}

// NewSlotManager creates a new slot manager with the given concurrency limits.
func NewSlotManager(maxDownloads, maxUploads int) *SlotManager

// AcquireDownloadSlot blocks until a download slot is available or ctx is cancelled.
// Returns nil on success, ctx.Err() on cancellation.
func (s *SlotManager) AcquireDownloadSlot(ctx context.Context) error

// ReleaseDownloadSlot releases a previously acquired download slot.
// Safe to call multiple times (idempotent via channel receive).
func (s *SlotManager) ReleaseDownloadSlot()

// AcquireUploadSlot acquires both a per-user slot and a global upload slot.
// Per-user slot ensures only 1 upload per user (Soulseek protocol requirement).
// Returns nil on success, ctx.Err() on cancellation.
func (s *SlotManager) AcquireUploadSlot(ctx context.Context, username string) error

// ReleaseUploadSlot releases both per-user and global upload slots.
func (s *SlotManager) ReleaseUploadSlot(username string)

// Stats returns current slot usage statistics.
func (s *SlotManager) Stats() SlotStats

// Close shuts down the slot manager, waking any blocked acquisitions.
func (s *SlotManager) Close() error
```

### Slot Statistics

```go
// SlotStats contains current slot usage information.
type SlotStats struct {
    DownloadsActive int
    DownloadsMax    int
    UploadsActive   int
    UploadsMax      int
    UserSlotsCount  int // Number of cached per-user slots
}
```

## Implementation Details

### Buffered Channel Semaphore Pattern

```go
// Acquire slot (blocking with context)
select {
case slots <- struct{}{}:
    // acquired
    return nil
case <-ctx.Done():
    return ctx.Err()
case <-s.closedCh:
    return ErrSlotManagerClosed
}

// Release slot (non-blocking)
select {
case <-slots:
    // released
default:
    // already empty or released (idempotent)
}
```

### Per-User Slot Management

Per-user slots are created on first use and cached:

```go
func (s *SlotManager) getOrCreateUserSlot(username string) *userSlot {
    if v, ok := s.userSlots.Load(username); ok {
        slot := v.(*userSlot)
        slot.lastUsed.Store(time.Now())
        return slot
    }

    // Create new slot
    slot := &userSlot{
        sem: make(chan struct{}, 1),
    }
    slot.lastUsed.Store(time.Now())

    // LoadOrStore handles race condition
    if existing, loaded := s.userSlots.LoadOrStore(username, slot); loaded {
        return existing.(*userSlot)
    }
    return slot
}
```

### Upload Slot Acquisition Order

Upload slots are acquired in order: **per-user first, then global**.

```go
func (s *SlotManager) AcquireUploadSlot(ctx context.Context, username string) error {
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
```

### Release Order

Same order as acquisition (per-user first, then global):

```go
func (s *SlotManager) ReleaseUploadSlot(username string) {
    // Release per-user slot
    if v, ok := s.userSlots.Load(username); ok {
        v.(*userSlot).release()
    }

    // Release global slot
    s.releaseGlobalUpload()
}
```

### Cleanup Strategy

Per-user slots should be cleaned up when idle to prevent memory growth.
Two options:

**Option A: Background timer (like .NET)** - More complex, requires goroutine management
**Option B: Lazy cleanup during operations** - Simpler, checked periodically

We'll implement **Option A** with configurable interval:

```go
// Start cleanup goroutine
go s.cleanupLoop(cleanupInterval)

func (s *SlotManager) cleanupLoop(interval time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            s.cleanupIdleUserSlots()
        case <-s.closedCh:
            return
        }
    }
}

func (s *SlotManager) cleanupIdleUserSlots() {
    threshold := time.Now().Add(-15 * time.Minute)

    s.userSlots.Range(func(key, value any) bool {
        slot := value.(*userSlot)
        lastUsed := slot.lastUsed.Load().(time.Time)

        if lastUsed.Before(threshold) {
            // Only delete if slot is idle (can acquire without blocking)
            select {
            case slot.sem <- struct{}{}:
                // Was idle, now we hold it
                <-slot.sem // release
                s.userSlots.Delete(key)
            default:
                // In use, skip
            }
        }
        return true
    })
}
```

## Configuration Integration

Add slot configuration to `client/options.go`:

```go
type Options struct {
    // ... existing fields ...

    // MaxConcurrentDownloads limits concurrent downloads (0 = unlimited).
    // Default: 10
    MaxConcurrentDownloads int

    // MaxConcurrentUploads limits concurrent uploads (0 = unlimited).
    // Default: 10
    MaxConcurrentUploads int
}
```

## Error Types

```go
var (
    // ErrSlotManagerClosed is returned when acquiring slots on a closed manager.
    ErrSlotManagerClosed = errors.New("slot manager closed")
)
```

## File Structure

New file: `client/slots.go`
New file: `client/slots_test.go`
Modify: `client/options.go` (add configuration)

## Test Plan

### Unit Tests

1. **Basic acquisition/release**
   - Acquire and release download slot
   - Acquire and release upload slot (both tiers)
   - Verify slot is available after release

2. **Blocking behavior**
   - Acquire all slots, verify next acquisition blocks
   - Release one slot, verify blocked goroutine proceeds
   - Test with timeout context

3. **Context cancellation**
   - Start acquisition, cancel context mid-wait
   - Verify cancellation error returned
   - Verify no slot was consumed

4. **Per-user slot isolation**
   - User A acquires upload slot
   - User B can acquire upload slot (different user)
   - User A cannot acquire second upload slot (same user)

5. **Concurrent access**
   - Multiple goroutines acquiring/releasing concurrently
   - Verify no deadlocks or race conditions

6. **Stats accuracy**
   - Verify Stats() reflects current state
   - Test after various acquire/release patterns

7. **Close behavior**
   - Blocked acquisitions return error on Close()
   - New acquisitions after Close() return error

8. **Cleanup**
   - Create per-user slots
   - Wait for cleanup interval
   - Verify idle slots are removed
   - Verify in-use slots are preserved

### Table-Driven Test Example

```go
func TestSlotManager_AcquireDownload(t *testing.T) {
    tests := []struct {
        name        string
        maxSlots    int
        acquireN    int
        wantBlocked bool
    }{
        {"single slot available", 1, 1, false},
        {"all slots used", 2, 3, true},
        {"unlimited slots", 0, 100, false},
    }
    // ...
}
```

## Implementation Order

1. **Define types and constructor** (`slots.go`)
   - SlotManager struct
   - userSlot struct
   - SlotStats struct
   - NewSlotManager()

2. **Implement download slots**
   - AcquireDownloadSlot()
   - ReleaseDownloadSlot()
   - Tests for download slots

3. **Implement upload slots**
   - getOrCreateUserSlot()
   - AcquireUploadSlot() (two-tier)
   - ReleaseUploadSlot()
   - Tests for upload slots

4. **Implement stats and close**
   - Stats()
   - Close()
   - Tests for stats and close

5. **Implement cleanup**
   - cleanupLoop()
   - cleanupIdleUserSlots()
   - Tests for cleanup

6. **Update options.go**
   - Add MaxConcurrentDownloads
   - Add MaxConcurrentUploads
   - Update DefaultOptions()

## Edge Cases

1. **Zero limit = unlimited**: When max is 0, slots should never block
2. **Release without acquire**: Should be idempotent/safe
3. **Double release**: Should not panic or cause issues
4. **Close during acquisition**: Blocked goroutines should return error
5. **Cleanup during use**: Active slots must not be cleaned up
6. **Username edge cases**: Empty string, special characters

## Dependencies

- None (Task 3.1 is standalone)

## Future Integration

The SlotManager will be used by:
- Task 2.1: Download flow - acquire download slot before starting
- Task 4.3: Upload execution - acquire upload slots (both tiers)

Integration pattern:
```go
// In download.go
if err := client.slots.AcquireDownloadSlot(ctx); err != nil {
    return nil, fmt.Errorf("waiting for download slot: %w", err)
}
defer client.slots.ReleaseDownloadSlot()

// In upload.go
if err := client.slots.AcquireUploadSlot(ctx, username); err != nil {
    return nil, fmt.Errorf("waiting for upload slot: %w", err)
}
defer client.slots.ReleaseUploadSlot(username)
```
