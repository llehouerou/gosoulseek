# Task 3.2: Queue Management Implementation Plan

## Overview

Task 3.2 implements queue management for transfers, building on Task 3.1 (Slot Management). This provides:
- Queue position tracking for downloads waiting in remote peer queues
- Local upload queue management with position resolution
- PlaceInQueue request/response handling
- Queue state transitions integrated with the transfer state machine

## Analysis: .NET Reference vs Go Implementation

### What .NET Does

1. **Download Queue (Queued|Remotely)**: When peer responds with "Queued", download waits for:
   - `TransferRequest(Upload)` from peer (they're ready)
   - Polling queue position via `PlaceInQueueRequest`

2. **Upload Queue (Queued|Locally)**: When we receive `QueueDownloadRequest`:
   - Call `PlaceInQueueResolver` delegate to get position
   - Send `PlaceInQueueResponse` with position
   - Track queued uploads until ready to send

3. **PlaceInQueueResolver**: User-configurable delegate that returns queue position for a given (username, filename)

### What Already Exists in gosoulseek

| Component | Status | Location |
|-----------|--------|----------|
| TransferState flags | ✅ Complete | `client/transfer_state.go:29-31` |
| PlaceInQueue messages | ✅ Complete | `messages/peer/transfer.go:166-241` |
| SlotManager | ✅ Complete | `client/slots.go` |
| TransferProgress.QueuePosition | ✅ Exists | `client/transfer.go:79` (unused) |
| Queue handling in download | ⚠️ Partial | `client/download.go:318-321` |
| PlaceInQueueResponse parsing | ⚠️ Partial | `client/download.go:485-493` |

### What's Missing

1. **QueueManager** - Manages local upload queue with position tracking
2. **PlaceInQueueResolver** - Configurable callback for queue position
3. **GetDownloadPlaceInQueue** - Client method to poll queue position
4. **PlaceInQueueRequest handler** - Respond to incoming queue position requests
5. **Queue position updates** - Emit progress updates with queue position

## Implementation Plan

### Step 1: Define QueueManager Interface and Types

**File**: `client/queue.go`

```go
// QueueResolver returns the queue position for an upload request.
// Returns the 1-indexed position, or 0 if the transfer should start immediately.
// Returning an error causes the upload to be denied.
type QueueResolver func(username, filename string) (position uint32, err error)

// QueueManager tracks queued transfers and provides position resolution.
type QueueManager struct {
    resolver QueueResolver

    // Track queued uploads by priority/order
    uploadQueue []*queuedUpload
    mu          sync.RWMutex
}

// queuedUpload represents a file queued for upload
type queuedUpload struct {
    Username  string
    Filename  string
    Token     uint32
    QueuedAt  time.Time
}
```

**Design Decisions**:
- Simple FIFO queue by default
- Configurable resolver for custom queue logic
- Thread-safe with read-write mutex

### Step 2: Implement QueueManager Core Methods

**File**: `client/queue.go`

```go
// NewQueueManager creates a queue manager with default FIFO resolution.
func NewQueueManager() *QueueManager

// SetResolver configures a custom queue position resolver.
func (q *QueueManager) SetResolver(r QueueResolver)

// EnqueueUpload adds an upload to the queue.
func (q *QueueManager) EnqueueUpload(username, filename string, token uint32) uint32

// DequeueUpload removes a completed/cancelled upload from the queue.
func (q *QueueManager) DequeueUpload(token uint32)

// GetPosition returns the current queue position for an upload.
// Position is 1-indexed; 0 means ready to transfer immediately.
func (q *QueueManager) GetPosition(username, filename string) uint32

// ResolvePosition calls the resolver (or default FIFO) to get position.
func (q *QueueManager) ResolvePosition(username, filename string) (uint32, error)
```

### Step 3: Add Client Method to Poll Queue Position

**File**: `client/download.go` (add method)

```go
// GetDownloadPlaceInQueue requests the current queue position from the peer.
// Returns the 1-indexed position, or 0 if the file is ready for immediate transfer.
func (c *Client) GetDownloadPlaceInQueue(ctx context.Context, username, filename string) (uint32, error) {
    // 1. Validate state (connected, logged in)
    // 2. Verify download exists in registry
    // 3. Get/create peer connection
    // 4. Send PlaceInQueueRequest
    // 5. Wait for PlaceInQueueResponse
    // 6. Return position
}
```

### Step 4: Handle Incoming PlaceInQueueRequest

**File**: `client/client.go` (add handler registration)

When a peer asks "what's my position in your queue?":

```go
// registerPeerMessageHandlers sets up handlers for peer protocol messages.
func (c *Client) registerPeerMessageHandlers() {
    // ... existing handlers ...

    // Handle PlaceInQueueRequest - peer asking their queue position
    c.peerRouter.Register(uint32(protocol.PeerPlaceInQueueRequest),
        func(code uint32, payload []byte, username string, conn *connection.Conn) {
            c.handlePlaceInQueueRequest(payload, username, conn)
        })
}

func (c *Client) handlePlaceInQueueRequest(payload []byte, username string, conn *connection.Conn) {
    // 1. Decode request
    // 2. Call queueMgr.ResolvePosition(username, filename)
    // 3. Send PlaceInQueueResponse if position available
}
```

### Step 5: Update Transfer to Track Queue Position

**File**: `client/transfer.go`

```go
// Add field to Transfer struct
type Transfer struct {
    // ... existing fields ...
    QueuePosition uint32 // Current position in remote queue (for downloads)
}

// SetQueuePosition updates the queue position and emits progress.
func (t *Transfer) SetQueuePosition(position uint32) {
    t.mu.Lock()
    t.QueuePosition = position
    t.mu.Unlock()
    t.emitProgress()
}
```

Update `emitProgress()` to include `QueuePosition` in the progress update.

### Step 6: Integrate Queue Position Updates in Download Flow

**File**: `client/download.go`

Update `readPeerMessagesUntilReady` to update transfer queue position:

```go
case uint32(protocol.PeerPlaceInQueueResponse):
    resp, err := peer.DecodePlaceInQueueResponse(payload)
    if err != nil {
        continue
    }
    if resp.Filename == o.transfer.Filename {
        o.transfer.SetQueuePosition(resp.Place)
    }
```

### Step 7: Add Client Configuration Option

**File**: `client/options.go`

```go
// WithQueueResolver sets a custom queue position resolver.
func WithQueueResolver(resolver QueueResolver) Option {
    return func(c *Client) {
        c.queueMgr.SetResolver(resolver)
    }
}
```

### Step 8: Write Comprehensive Tests

**File**: `client/queue_test.go`

```go
func TestQueueManager_EnqueueDequeue(t *testing.T)
func TestQueueManager_GetPosition_FIFO(t *testing.T)
func TestQueueManager_CustomResolver(t *testing.T)
func TestQueueManager_ConcurrentAccess(t *testing.T)
```

**File**: `client/download_test.go` (add tests)

```go
func TestGetDownloadPlaceInQueue(t *testing.T)
func TestDownload_QueuePositionUpdates(t *testing.T)
```

## State Transitions

### Download State Flow with Queue
```
Queued|Locally (acquiring slot, waiting to request)
    ↓
Requested (sent TransferRequest)
    ↓
Queued|Remotely (peer responded "Queued")
    ↓ [poll PlaceInQueue, receive position updates]
Initializing (received TransferRequest(Upload))
    ↓
InProgress (transferring data)
    ↓
Completed|Succeeded
```

### Upload State Flow with Queue
```
Queued|Locally (received QueueDownloadRequest, in our queue)
    ↓ [respond to PlaceInQueueRequest with position]
Initializing (slot acquired, sending TransferRequest(Upload))
    ↓
InProgress (transferring data)
    ↓
Completed|Succeeded
```

## File Changes Summary

| File | Changes |
|------|---------|
| `client/queue.go` | **NEW** - QueueManager, QueueResolver, queue operations |
| `client/queue_test.go` | **NEW** - Unit tests for QueueManager |
| `client/transfer.go` | Add QueuePosition field, SetQueuePosition method |
| `client/download.go` | Add GetDownloadPlaceInQueue, update queue position handling |
| `client/download_test.go` | Add queue-related tests |
| `client/client.go` | Add queueMgr field, PlaceInQueueRequest handler |
| `client/options.go` | Add WithQueueResolver option |

## Testing Strategy

1. **Unit Tests** (queue_test.go):
   - FIFO queue ordering
   - Custom resolver integration
   - Concurrent enqueue/dequeue
   - Position recalculation after dequeue

2. **Integration Tests** (download_test.go):
   - GetDownloadPlaceInQueue with mock peer
   - Queue position updates during download
   - State transitions through queue states

3. **Message Tests** (already exist):
   - PlaceInQueueRequest/Response encoding/decoding

## Dependencies

- **Task 3.1 (Slot Management)**: ✅ Complete - SlotManager exists
- **TransferRegistry**: ✅ Complete - Already tracks transfers
- **PlaceInQueue messages**: ✅ Complete - Encoding/decoding exists

## Success Criteria

1. Downloads track and report queue position in progress updates
2. `GetDownloadPlaceInQueue()` successfully polls peer for position
3. Incoming `PlaceInQueueRequest` messages get responses
4. Custom `QueueResolver` can be configured
5. All tests pass with `make check`
6. No race conditions in concurrent queue operations
