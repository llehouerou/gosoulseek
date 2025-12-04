# Download/Upload Implementation Plan for gosoulseek

## Executive Summary

This document outlines the strategic task breakdown for implementing complete download/upload functionality in gosoulseek, ported from Soulseek.NET. The goal is to divide this complex feature into independently testable subtasks that can be validated before proceeding.

## Quick Reference: Task List

| Task | Name | Dependencies | Testable Outcome |
|------|------|--------------|------------------|
| **1.1** | TCP Listener | None | Accept inbound connections, route by token/type |
| **1.2** | Connection Manager | 1.1 | Cache P-type connections, direct+indirect strategy |
| **1.3** | Transfer Manager | None | Track transfers, state machine, progress channels |
| **2.1** | Download Implementation | 1.2, 1.3, 2.2 | Full download flow with new infrastructure |
| **2.2** | Transfer Connections | 1.1, 1.2 | F-type connections, concurrent strategy |
| **3.1** | Slot Management | None | Semaphore-based concurrency control |
| **3.2** | Queue Management | 3.1 | Local/remote queue state transitions |
| **4.1** | File Sharing | None | Index shared folders, path translation |
| **4.2** | Upload Requests | 1.3, 4.1 | Handle QueueDownloadRequest, queue uploads |
| **4.3** | Upload Execution | 2.2, 3.1, 4.1, 4.2 | Full upload flow with slot management |
| **5.1** | Error Handling | All | ✅ Robust recovery, proper cleanup |

**Approach**: Full rewrite of download code, aggressive defaults (10/10 concurrent), browse/speed-limit deferred.

---

## Current State Analysis

### What Exists in gosoulseek

| Component | Status | Notes |
|-----------|--------|-------|
| Server connection | ✅ Complete | Login, ping, basic messaging |
| Message encoding/decoding | ✅ Complete | All transfer messages implemented |
| Download orchestration | ⚠️ Partial | Works but needs refactoring |
| Peer connection (P-type) | ⚠️ Partial | Direct + indirect, needs cleanup |
| Transfer connection (F-type) | ⚠️ Partial | Basic implementation exists |
| TCP Listener | ❌ Stub | Methods exist, no real implementation |
| Upload functionality | ❌ Missing | Not implemented |
| Connection caching | ❌ Missing | No MessageConnection reuse |
| Concurrency control | ❌ Missing | No semaphores/slot management |
| File sharing/indexing | ❌ Missing | Required for uploads |

### Key Architectural Differences: .NET vs Go

| Aspect | .NET (Soulseek.NET) | Go (Idiomatic) |
|--------|---------------------|----------------|
| Async results | Events + Callbacks | Channels |
| Cancellation | CancellationToken | context.Context |
| Concurrent maps | ConcurrentDictionary | sync.Map or mutex-protected map |
| Task coordination | TaskCompletionSource | Channels + select |
| Semaphores | SemaphoreSlim | chan struct{} (buffered) |
| Disposal | IDisposable | io.Closer or deferred cleanup |

---

## Proposed Task Breakdown

### Phase 1: Foundation Infrastructure

#### Task 1.1: TCP Listener Implementation
**Goal**: Implement a fully functional TCP listener for inbound peer/transfer connections.

**Scope**:
- Accept incoming TCP connections
- Read initial handshake (PierceFirewall or PeerInit)
- Route connections by type ("P", "F", "D") and token
- Support connection registration/waiting mechanism
- Integrate with existing client lifecycle

**Testable Outcomes**:
- Unit tests with net.Pipe() for connection acceptance
- Integration test: accept inbound connection, verify handshake parsing
- Test token-based connection routing

**Dependencies**: None

---

#### Task 1.2: Connection Manager Abstraction
**Goal**: Create a dedicated connection manager for peer connections (similar to PeerConnectionManager in .NET).

**Scope**:
- Abstract peer connection lifecycle from Client
- Cache MessageConnections (P-type) per username
- Handle connection superseding (direct replaces indirect)
- Provide clean API for getting/creating connections
- Support concurrent direct+indirect connection strategy

**Go-Idiomatic Design**:
```go
type PeerConnectionManager interface {
    GetOrCreateMessageConnection(ctx context.Context, username string, endpoint net.Addr) (*MessageConnection, error)
    GetCachedConnection(username string) (*MessageConnection, bool)
    InvalidateConnection(username string)
    Close() error
}
```

**Testable Outcomes**:
- Test connection caching (same user = same connection)
- Test connection invalidation on disconnect
- Test concurrent access patterns

**Dependencies**: Task 1.1

---

#### Task 1.3: Transfer Manager Abstraction
**Goal**: Create a unified transfer manager to handle both downloads and uploads.

**Scope**:
- Unified `Transfer` type (direction-agnostic where possible)
- Transfer state machine with proper transitions
- Transfer registry (by token, by username+filename)
- Duplicate transfer detection
- Progress tracking with channel-based updates
- Cancellation support via context

**Go-Idiomatic Design**:
```go
type Transfer struct {
    Direction    TransferDirection
    Username     string
    Filename     string
    Token        uint32
    RemoteToken  uint32
    State        TransferState
    Size         int64
    Transferred  int64
    StartOffset  int64
    // ... progress, timing, error fields
}

type TransferManager interface {
    RegisterDownload(username, filename string, size int64) (*Transfer, error)
    RegisterUpload(username, filename string, size int64) (*Transfer, error)
    GetTransfer(token uint32) (*Transfer, bool)
    GetTransferByFile(username, filename string, direction TransferDirection) (*Transfer, bool)
    CancelTransfer(token uint32) error
    // Progress via channels, not callbacks
    Progress(token uint32) <-chan TransferProgress
}
```

**Testable Outcomes**:
- Test transfer registration and lookup
- Test duplicate detection
- Test state transitions
- Test progress channel updates

**Dependencies**: None

---

### Phase 2: Download Refactoring

#### Task 2.1: Refactor Download Flow
**Goal**: Refactor existing download code to use new infrastructure.

**Scope**:
- Use TransferManager for tracking
- Use PeerConnectionManager for connections
- Cleaner separation of concerns
- Proper error handling and state transitions
- Support for resume (StartOffset)

**Testable Outcomes**:
- Existing download tests continue to pass
- New unit tests for individual phases
- Integration test with mock peer

**Dependencies**: Tasks 1.2, 1.3

---

#### Task 2.2: Transfer Connection Establishment (Unified)
**Goal**: Create robust transfer connection establishment used by both download and upload.

**Scope**:
- Concurrent direct + indirect strategy
- Configurable timeouts
- Proper cleanup of losing connection attempt
- Support both initiator and receiver roles
- Token-based connection matching

**Go-Idiomatic Design**:
```go
type TransferConnectionManager interface {
    // We initiate connection to peer
    GetOutboundConnection(ctx context.Context, username string, endpoint net.Addr, token uint32) (net.Conn, error)

    // Wait for peer to connect to us
    AwaitInboundConnection(ctx context.Context, username string, token uint32) (net.Conn, error)
}
```

**Testable Outcomes**:
- Test direct connection path
- Test indirect connection path (via server)
- Test concurrent strategy (first wins)
- Test timeout behavior

**Dependencies**: Tasks 1.1, 1.2

---

### Phase 3: Concurrency Control

#### Task 3.1: Semaphore-Based Slot Management
**Goal**: Implement slot management for concurrent transfer limits.

**Scope**:
- Global download semaphore (configurable limit)
- Global upload semaphore (configurable limit)
- Per-user upload semaphore (Soulseek protocol: 1 upload per user)
- Context-aware acquisition (cancellable waits)
- Proper cleanup on completion/failure

**Go-Idiomatic Design**:
```go
// Using buffered channels as semaphores
type SlotManager struct {
    downloadSlots chan struct{}  // buffered to max concurrent downloads
    uploadSlots   chan struct{}  // buffered to max concurrent uploads
    userSlots     sync.Map       // username -> chan struct{} (1-buffered)
}

func (s *SlotManager) AcquireDownloadSlot(ctx context.Context) error
func (s *SlotManager) ReleaseDownloadSlot()
func (s *SlotManager) AcquireUploadSlot(ctx context.Context, username string) error
func (s *SlotManager) ReleaseUploadSlot(username string)
```

**Testable Outcomes**:
- Test slot acquisition blocks when full
- Test slot release unblocks waiters
- Test per-user upload limiting
- Test context cancellation during wait

**Dependencies**: None

---

#### Task 3.2: Queue Management
**Goal**: Implement local queue management for transfers.

**Scope**:
- Queue position tracking
- Queue state transitions (Queued|Locally → Requested → Queued|Remotely)
- PlaceInQueue request/response handling
- Queue notification to waiting downloads

**Testable Outcomes**:
- Test local queue ordering
- Test queue position updates
- Test state transitions through queue

**Dependencies**: Task 3.1

---

### Phase 4: Upload Implementation

#### Task 4.1: File Sharing Infrastructure
**Goal**: Implement file indexing and sharing rules.

**Scope**:
- Shared folder configuration
- File index building (scan directories)
- File lookup by Soulseek path
- Access control (who can download what)
- File attributes (size, bitrate, duration for audio)

**Go-Idiomatic Design**:
```go
type SharedFile struct {
    LocalPath    string
    SoulseekPath string
    Size         int64
    Attributes   map[string]interface{}
}

type FileSharer interface {
    AddSharedFolder(localPath, virtualPath string) error
    RemoveSharedFolder(virtualPath string)
    GetFile(soulseekPath string) (*SharedFile, error)
    GetSharedFiles() []*SharedFile
    Rescan() error
}
```

**Testable Outcomes**:
- Test folder scanning
- Test path translation (local ↔ Soulseek)
- Test file lookup
- Test access control

**Dependencies**: None

---

#### Task 4.2: Upload Request Handling
**Goal**: Handle incoming upload requests from peers.

**Scope**:
- Receive QueueDownloadRequest from peer
- Verify file exists and is shared
- Queue the upload or reject with UploadDenied
- Track pending uploads

**Testable Outcomes**:
- Test request acceptance for valid files
- Test rejection for invalid/unshared files
- Test queue registration

**Dependencies**: Tasks 1.3, 4.1

---

#### Task 4.3: Upload Execution
**Goal**: Implement the upload flow when ready to send.

**Scope**:
- Acquire upload slots (global + per-user)
- Send TransferRequest(Upload) to peer
- Handle TransferResponse
- Establish transfer connection
- Stream file data with progress updates
- Handle resume (StartOffset from peer)

**Flow**:
```
Slot acquired → TransferRequest(Upload) → TransferResponse(Allowed)
→ Transfer Connection → Read offset from peer → Send file data → Complete
```

**Testable Outcomes**:
- Test full upload flow with mock peer
- Test resume with offset
- Test cancellation mid-transfer
- Test slot release on completion/failure

**Dependencies**: Tasks 2.2, 3.1, 4.1, 4.2

---

### Phase 5: Robustness & Edge Cases

#### Task 5.1: Error Handling & Recovery
**Goal**: Comprehensive error handling for all transfer scenarios.

**Scope**:
- Connection failures mid-transfer
- Timeout handling
- Size mismatch detection
- Proper cleanup on all error paths
- UploadDenied/UploadFailed message handling

**Dependencies**: All previous tasks

---

### Deferred to Future Phases

The following are explicitly out of scope for this implementation:

- **Browse Functionality** (Task 5.1 in .NET): Respond to browse requests
- **User Info Response** (Task 5.2 in .NET): Respond to user info requests
- **Speed Limiting/Governor**: Per-transfer and global bandwidth management
- **Distributed Network**: D-type connections

---

## Recommended Implementation Order

```
Phase 1 (Foundation) - Can be parallelized:
├── Task 1.1: TCP Listener
├── Task 1.3: Transfer Manager
└── Task 3.1: Slot Management

Phase 2 (Downloads):
├── Task 1.2: Connection Manager (needs 1.1)
├── Task 2.2: Transfer Connections (needs 1.1, 1.2)
└── Task 2.1: Download Implementation (needs 1.2, 1.3, 2.2)

Phase 3 (Uploads):
├── Task 4.1: File Sharing (standalone)
├── Task 4.2: Upload Requests (needs 1.3, 4.1)
└── Task 4.3: Upload Execution (needs 2.2, 3.1, 4.1, 4.2)

Phase 4 (Completeness):
├── Task 3.2: Queue Management
└── Task 5.1: Error Handling
```

### Total: 11 Tasks across 4 Phases

---

## Go-Idiomatic Architecture Decisions

### 1. Channels Over Callbacks

**Instead of .NET's event-based progress:**
```csharp
// .NET
options.ProgressUpdated = (args) => { /* handle */ };
```

**Use Go channels:**
```go
// Go
progress := client.Download(ctx, username, filename, writer)
for p := range progress {
    fmt.Printf("Progress: %d%%\n", p.PercentComplete())
}
```

### 2. Context for Cancellation

All blocking operations accept `context.Context`:
```go
func (c *Client) Download(ctx context.Context, ...) (<-chan Progress, error)
func (c *Client) Upload(ctx context.Context, ...) (<-chan Progress, error)
func (m *SlotManager) AcquireSlot(ctx context.Context) error
```

### 3. Buffered Channels as Semaphores

```go
// Instead of SemaphoreSlim
slots := make(chan struct{}, maxConcurrent)

// Acquire
select {
case slots <- struct{}{}:
    // acquired
case <-ctx.Done():
    return ctx.Err()
}

// Release
<-slots
```

### 4. sync.Map for Concurrent Access

```go
// Instead of ConcurrentDictionary
type TransferRegistry struct {
    byToken sync.Map // uint32 -> *Transfer
    byFile  sync.Map // "user:file" -> *Transfer
}
```

### 5. Functional Options Pattern

```go
type DownloadOption func(*downloadConfig)

func WithStartOffset(offset int64) DownloadOption {
    return func(c *downloadConfig) { c.startOffset = offset }
}

func (c *Client) Download(ctx context.Context, user, file string, w io.Writer, opts ...DownloadOption) (<-chan Progress, error)
```

---

## Testing Strategy

Each task should include:

1. **Unit Tests**: Test individual functions/methods in isolation
2. **Integration Tests**: Test component interaction with net.Pipe()
3. **Mock Peers**: Create mock peer implementations for protocol testing
4. **Table-Driven Tests**: For message encoding/decoding variations

### Test Utilities to Build

- Mock peer that can respond to transfer requests
- Mock server for ConnectToPeer routing
- Progress channel assertion helpers
- Transfer state machine validators

---

## Files to Create/Modify

### New Files (Full Rewrite Approach)
- `client/listener.go` - TCP listener implementation
- `client/connmanager.go` - PeerConnectionManager
- `client/transfer.go` - Transfer struct, state machine, TransferManager
- `client/slots.go` - SlotManager (semaphore-based concurrency)
- `client/download.go` - Download orchestration (replaces existing)
- `client/upload.go` - Upload orchestration
- `client/fileshare.go` - File sharing infrastructure

### Files to Modify
- `client/client.go` - Wire up new components, remove old download code
- `client/peer.go` - Integrate with connection manager
- `client/options.go` - Add concurrency configuration options

### Files to Delete (or gut)
- Current `client/download.go` content (1,205 lines) - Full rewrite

### Existing Files to Keep
- `messages/peer/transfer.go` - Message types are good, keep as-is
- `messages/peer/init.go` - PeerInit/PierceFirewall messages
- `connection/connection.go` - Low-level connection wrapper
- `protocol/*` - All protocol encoding/decoding

---

## Design Decisions (Confirmed)

1. **Concurrency Limits**: Aggressive defaults (10 DL / 10 UL) but fully configurable
2. **Browse/User Info**: Deferred to later phase - focus on core transfers
3. **Speed Limiting**: Deferred to later phase - focus on functionality first
4. **Existing Download Code**: Full rewrite - clean break from current implementation
5. **Distributed Network**: Deferred (D-type connections not in scope)
