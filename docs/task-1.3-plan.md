# Task 1.3: Transfer Manager Abstraction - Implementation Plan

## Executive Summary

This task creates a unified Transfer Manager to handle both downloads and uploads with proper state machine, progress tracking, and duplicate detection. The implementation follows Go idioms while porting the core concepts from Soulseek.NET's `Transfer` and `TransferInternal` classes.

## Current State Analysis

### What Exists in gosoulseek

| Component | Location | Status |
|-----------|----------|--------|
| `TransferState` enum | `client/download.go:24-44` | ⚠️ Partial - basic states, no flags |
| `DownloadProgress` struct | `client/download.go:70-77` | ⚠️ Partial - limited fields |
| `activeDownload` struct | `client/download.go:93-123` | ⚠️ Tightly coupled to download flow |
| `downloadRegistry` struct | `client/download.go:125-194` | ⚠️ Download-only, not unified |
| Transfer messages | `messages/peer/transfer.go` | ✅ Complete |
| `TransferDirection` enum | `messages/peer/transfer.go:10-18` | ✅ Complete |

### Key Gaps

1. **No unified Transfer type** - download-specific `activeDownload` struct
2. **No flag-based states** - `.NET uses `[Flags]` for compound states like `Queued | Remotely`
3. **No progress calculation** - no speed, ETA, or completion percentage tracking
4. **No duplicate detection** - missing atomic uniqueness checks
5. **No upload support** - current registry is download-only
6. **No state machine validation** - no enforcement of valid transitions

## Architecture Design

### Design Decisions

| Aspect | Decision | Rationale |
|--------|----------|-----------|
| State representation | Bitwise flags (like .NET) | Allows compound states `Queued | Remotely` |
| Progress updates | Channel-based | Idiomatic Go, composable with `select` |
| Duplicate detection | Atomic check with sync.Map | Thread-safe without global locks |
| Transfer struct | Single unified type | Shared by downloads and uploads |
| Speed calculation | Exponential Moving Average | Matches .NET, smooths fluctuations |
| Registry indexing | By token + by file key | Fast lookup for both scenarios |

### Core Types

```go
// TransferDirection in messages/peer/transfer.go (already exists)
type TransferDirection uint32

const (
    TransferDownload TransferDirection = 0
    TransferUpload   TransferDirection = 1
)

// TransferState uses bitwise flags for compound states
type TransferState uint32

const (
    TransferStateNone        TransferState = 0
    TransferStateRequested   TransferState = 1 << 0  // Request sent
    TransferStateQueued      TransferState = 1 << 1  // Waiting (local or remote)
    TransferStateInitializing TransferState = 1 << 2 // Setting up connection
    TransferStateInProgress  TransferState = 1 << 3  // Actively transferring
    TransferStateCompleted   TransferState = 1 << 4  // Terminal flag

    // Completion reasons (combined with Completed)
    TransferStateSucceeded   TransferState = 1 << 5
    TransferStateCancelled   TransferState = 1 << 6
    TransferStateTimedOut    TransferState = 1 << 7
    TransferStateErrored     TransferState = 1 << 8
    TransferStateRejected    TransferState = 1 << 9
    TransferStateAborted     TransferState = 1 << 10 // Size mismatch

    // Queue location (combined with Queued)
    TransferStateLocally     TransferState = 1 << 11 // Queued by local constraints
    TransferStateRemotely    TransferState = 1 << 12 // Queued by remote peer
)

// Helper methods for state checks
func (s TransferState) IsCompleted() bool
func (s TransferState) IsQueued() bool
func (s TransferState) IsActive() bool
func (s TransferState) String() string
```

### Transfer Struct

```go
// Transfer represents an active file transfer (download or upload).
type Transfer struct {
    // Identity
    Direction   TransferDirection
    Username    string
    Filename    string
    Token       uint32  // Our local token
    RemoteToken uint32  // Peer's token (set during exchange)

    // Size and progress
    Size         int64
    StartOffset  int64  // Resume position
    Transferred  int64  // Bytes transferred so far

    // State machine
    state       TransferState
    prevState   TransferState

    // Timing
    StartTime   time.Time // When InProgress started
    EndTime     time.Time // When Completed started

    // Progress calculation (exponential moving average)
    avgSpeed         float64
    lastProgressTime time.Time
    lastProgressBytes int64
    speedInitialized bool

    // Error info
    Error       error

    // Channels for signaling (internal use)
    progressCh     chan TransferProgress  // Progress updates
    readyCh        chan struct{}          // Transfer ready signal
    cancelFunc     context.CancelFunc     // Cancellation

    mu sync.RWMutex
}

// TransferProgress is sent on the progress channel.
type TransferProgress struct {
    State            TransferState
    BytesTransferred int64
    FileSize         int64
    AverageSpeed     float64   // Bytes per second
    QueuePosition    uint32    // Position in remote queue (0 if not queued)
    Error            error     // Set when State includes Errored/Failed
}

// Computed properties
func (t *Transfer) PercentComplete() float64
func (t *Transfer) BytesRemaining() int64
func (t *Transfer) RemainingTime() time.Duration // Returns 0 if unknown
func (t *Transfer) StateString() string
```

### TransferManager Interface

```go
// TransferManager tracks and coordinates all file transfers.
type TransferManager interface {
    // Registration
    RegisterDownload(username, filename string, token uint32) (*Transfer, error)
    RegisterUpload(username, filename string, token uint32, size int64) (*Transfer, error)

    // Lookup
    GetByToken(token uint32) (*Transfer, bool)
    GetByRemoteToken(username string, remoteToken uint32) (*Transfer, bool)
    GetByFile(username, filename string, direction TransferDirection) (*Transfer, bool)

    // State management
    SetState(token uint32, state TransferState) error
    UpdateProgress(token uint32, bytesTransferred int64) error
    SetRemoteToken(token uint32, remoteToken uint32) error
    SetSize(token uint32, size int64) error
    Complete(token uint32, finalState TransferState, err error) error

    // Cancellation
    Cancel(token uint32) error

    // Cleanup
    Remove(token uint32)

    // Iteration
    All() []*Transfer
    Downloads() []*Transfer
    Uploads() []*Transfer

    // Events (channel-based)
    StateChanges() <-chan TransferStateChange
    ProgressUpdates() <-chan TransferProgress
}

// TransferStateChange represents a state transition event.
type TransferStateChange struct {
    Transfer      *Transfer
    PreviousState TransferState
    NewState      TransferState
}
```

### TransferRegistry Implementation

```go
// transferRegistry is the concrete implementation of TransferManager.
type transferRegistry struct {
    // Primary index - by token
    byToken sync.Map // uint32 -> *Transfer

    // Secondary indexes
    byRemoteToken sync.Map // "username:remoteToken" -> *Transfer
    byFileKey     sync.Map // "direction:username:filename" -> *Transfer (for duplicate detection)

    // Event channels
    stateChangeCh chan TransferStateChange
    progressCh    chan TransferProgress

    // Token generator
    nextToken atomic.Uint32
}

func NewTransferManager() TransferManager {
    return &transferRegistry{
        stateChangeCh: make(chan TransferStateChange, 100),
        progressCh:    make(chan TransferProgress, 100),
    }
}
```

## Implementation Steps

### Step 1: Create Transfer State Types (client/transfer_state.go)

Create the flag-based state enum with helper methods:

- [ ] Define `TransferState` as `uint32` with flag constants
- [ ] Add compound state helpers (`IsCompleted`, `IsQueued`, `IsActive`)
- [ ] Add `String()` method for human-readable output
- [ ] Add state transition validation function
- [ ] Write comprehensive unit tests for state logic

**Test Cases:**
- Compound state creation: `TransferStateQueued | TransferStateRemotely`
- State checks: `IsCompleted()`, `IsQueued()`, etc.
- String representation for all states
- Valid state transitions

### Step 2: Create Transfer Struct (client/transfer.go)

Create the unified transfer type with progress tracking:

- [ ] Define `Transfer` struct with all fields
- [ ] Define `TransferProgress` struct for channel updates
- [ ] Implement `UpdateProgress()` with EMA speed calculation
- [ ] Implement computed properties (`PercentComplete`, `BytesRemaining`, `RemainingTime`)
- [ ] Implement state setter that auto-sets `StartTime`/`EndTime`
- [ ] Write unit tests for progress calculation

**Test Cases:**
- Progress calculation with EMA
- State transitions trigger time updates
- Computed properties at various progress levels
- Thread-safety of state updates

### Step 3: Create Transfer Registry (client/transfer_registry.go)

Create the concurrent registry with multiple indexes:

- [ ] Define `transferRegistry` struct with `sync.Map` indexes
- [ ] Implement `RegisterDownload` with duplicate detection
- [ ] Implement `RegisterUpload` with duplicate detection
- [ ] Implement lookup methods (`GetByToken`, `GetByRemoteToken`, `GetByFile`)
- [ ] Implement `SetRemoteToken` with index update
- [ ] Implement `SetState` with event emission
- [ ] Implement `UpdateProgress` with event emission
- [ ] Implement `Complete` for terminal states
- [ ] Implement `Cancel` with context cancellation
- [ ] Implement `Remove` with cleanup
- [ ] Write comprehensive unit tests

**Test Cases:**
- Registration creates entries in all indexes
- Duplicate detection for same file
- Token collision detection
- Lookup by all indexes
- State change events are emitted
- Progress events are emitted
- Removal cleans all indexes
- Concurrent access patterns

### Step 4: Create Transfer Errors (client/transfer_errors.go)

Create specific error types for transfer failures:

- [ ] Define `DuplicateTransferError`
- [ ] Define `TransferNotFoundError`
- [ ] Define `TransferRejectedError` (with reason)
- [ ] Define `TransferSizeMismatchError` (with local/remote sizes)
- [ ] All errors implement `error` interface

**Test Cases:**
- Error messages include relevant context
- Errors can be checked with `errors.Is`/`errors.As`

### Step 5: Integrate with Client (client/client.go)

Wire up the transfer manager:

- [ ] Add `transfers` field to `Client` struct
- [ ] Initialize in `New()`
- [ ] Expose via `Transfers() TransferManager` method
- [ ] Update `Disconnect()` to cancel all transfers

### Step 6: Update Download Flow (client/download.go) - Refactor

Migrate existing download code to use TransferManager:

- [ ] Remove `activeDownload` struct (replaced by `Transfer`)
- [ ] Remove `downloadRegistry` (replaced by `TransferManager`)
- [ ] Update `Download()` to use `TransferManager.RegisterDownload()`
- [ ] Update `runDownload()` to use `TransferManager` methods
- [ ] Update all state transitions to use `TransferManager.SetState()`
- [ ] Update progress reporting to use `TransferManager.UpdateProgress()`
- [ ] Ensure all existing tests pass

**Migration Steps:**
1. Create parallel implementation using TransferManager
2. Update handlers to check both registries
3. Remove old registry once verified
4. Update all dependent code

## File Structure

```
client/
├── transfer_state.go       # TransferState enum and helpers
├── transfer_state_test.go  # State tests
├── transfer.go             # Transfer struct and methods
├── transfer_test.go        # Transfer tests
├── transfer_registry.go    # TransferManager implementation
├── transfer_registry_test.go # Registry tests
├── transfer_errors.go      # Error types
├── transfer_errors_test.go # Error tests
├── download.go             # Refactored to use TransferManager
└── client.go               # Wiring
```

## Progress Calculation Algorithm

Match Soulseek.NET's exponential moving average:

```go
const (
    progressUpdateInterval = time.Second    // Throttle to once per second
    speedAlpha            = 0.2             // EMA smoothing factor (2/(N+1) where N=9)
)

func (t *Transfer) UpdateProgress(bytesTransferred int64) {
    t.mu.Lock()
    defer t.mu.Unlock()

    t.Transferred = bytesTransferred
    now := time.Now()

    // For completed transfers, use total time
    if t.state.IsCompleted() && !t.StartTime.IsZero() && !t.EndTime.IsZero() {
        duration := t.EndTime.Sub(t.StartTime).Seconds()
        if duration > 0 {
            t.avgSpeed = float64(bytesTransferred - t.StartOffset) / duration
        }
        return
    }

    // For in-progress, use EMA
    if t.lastProgressTime.IsZero() || now.Sub(t.lastProgressTime) < progressUpdateInterval {
        return // Throttle updates
    }

    elapsed := now.Sub(t.lastProgressTime).Seconds()
    if elapsed > 0 {
        currentSpeed := float64(bytesTransferred - t.lastProgressBytes) / elapsed

        if !t.speedInitialized {
            t.avgSpeed = currentSpeed
            t.speedInitialized = true
        } else {
            t.avgSpeed = speedAlpha*currentSpeed + (1-speedAlpha)*t.avgSpeed
        }
    }

    t.lastProgressTime = now
    t.lastProgressBytes = bytesTransferred
}
```

## Duplicate Detection Strategy

Two-layer detection matching .NET:

```go
func (r *transferRegistry) RegisterDownload(username, filename string, token uint32) (*Transfer, error) {
    fileKey := fmt.Sprintf("%d:%s:%s", TransferDownload, username, filename)

    // Layer 1: Atomic uniqueness check
    if _, loaded := r.byFileKey.LoadOrStore(fileKey, true); loaded {
        return nil, &DuplicateTransferError{
            Direction: TransferDownload,
            Username:  username,
            Filename:  filename,
        }
    }

    // Layer 2: Token collision check (should never happen with atomic counter)
    if _, loaded := r.byToken.LoadOrStore(token, transfer); loaded {
        r.byFileKey.Delete(fileKey) // Rollback
        return nil, fmt.Errorf("token collision: %d", token)
    }

    return transfer, nil
}
```

## State Transition Diagram

```
                                 ┌─────────────────────────────────────────────────────────────────────────┐
                                 │                                                                         │
                                 │  TERMINAL STATES (Completed | X)                                        │
                                 │  ┌─────────────┬─────────────┬────────────┬──────────┬────────────────┐ │
                                 │  │ Succeeded   │ Cancelled   │ TimedOut   │ Errored  │ Rejected/Aborted│ │
                                 │  └─────────────┴─────────────┴────────────┴──────────┴────────────────┘ │
                                 │        ▲             ▲             ▲           ▲            ▲           │
                                 └────────┼─────────────┼─────────────┼───────────┼────────────┼───────────┘
                                          │             │             │           │            │
                                          │             │             │           │            │
    ┌────────────┐    ┌───────────┐    ┌──┴──────────────┴─────────────┴───────────┴────────────┴──┐
    │            │    │           │    │                                                          │
    │   None     ├───►│ Requested ├───►│  InProgress                                              │
    │            │    │           │    │                                                          │
    └────────────┘    └─────┬─────┘    └──────────────────────────────────────────────────────────┘
                            │                           ▲
                            │                           │
                            ▼                           │
                      ┌───────────────┐    ┌────────────┴───────────┐
                      │               │    │                        │
                      │ Queued|Locally├───►│ Initializing           │
                      │               │    │                        │
                      └───────┬───────┘    └────────────────────────┘
                              │                       ▲
                              ▼                       │
                      ┌───────────────┐               │
                      │               │               │
                      │Queued|Remotely├───────────────┘
                      │               │
                      └───────────────┘
```

## Testing Strategy

### Unit Tests

1. **State Tests** (`transfer_state_test.go`)
   - Flag combinations
   - State checks
   - String representation

2. **Transfer Tests** (`transfer_test.go`)
   - Progress calculation
   - Speed EMA calculation
   - State transitions
   - Time tracking

3. **Registry Tests** (`transfer_registry_test.go`)
   - Registration
   - Duplicate detection
   - Lookups by all indexes
   - State/progress events
   - Concurrent access

### Integration Tests

4. **Download Integration** (existing tests should pass)
   - Full download flow uses new infrastructure
   - State transitions are correct
   - Progress updates are accurate

## Success Criteria

- [ ] All existing download tests pass
- [ ] New unit tests for all components
- [ ] No data races with `-race` flag
- [ ] Duplicate detection works correctly
- [ ] State transitions emit events
- [ ] Progress calculation matches .NET behavior
- [ ] Upload registration works (for future use)

## Dependencies

- **None** (Task 1.3 is standalone foundation work)

## Used By

- **Task 2.1**: Download Implementation
- **Task 4.2**: Upload Request Handling
- **Task 4.3**: Upload Execution

## Estimated Complexity

| Component | Lines of Code | Test Lines |
|-----------|--------------|------------|
| transfer_state.go | ~120 | ~200 |
| transfer.go | ~200 | ~250 |
| transfer_registry.go | ~300 | ~400 |
| transfer_errors.go | ~80 | ~50 |
| download.go changes | ~-100 (net reduction) | ~0 (existing) |
| **Total** | ~600 | ~900 |
