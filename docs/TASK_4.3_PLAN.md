# Task 4.3: Upload Execution - Implementation Plan

## Overview

**Goal**: Implement the upload execution flow when we're ready to send a file to a peer.

**Dependencies** (all implemented):
- Task 2.2: Transfer Connections ✅
- Task 3.1: Slot Management ✅
- Task 4.1: File Sharing ✅
- Task 4.2: Upload Requests ✅

## Current State Analysis

### What Exists
| Component | Status | Location |
|-----------|--------|----------|
| SlotManager | ✅ Complete | `client/slots.go` |
| FileSharer | ✅ Complete | `client/fileshare.go` |
| TransferRegistry | ✅ Complete | `client/transfer_registry.go` |
| Transfer struct | ✅ Complete | `client/transfer.go` |
| QueueManager | ✅ Complete | `client/queue.go` |
| QueueDownload handler | ✅ Complete | `client/upload_request.go` |
| TransferConnectionManager | ✅ Complete | `client/transfer_conn.go` |
| Download orchestrator | ✅ Complete | `client/download.go` |

### What Needs to Be Built
1. **Upload orchestrator** - mirrors download orchestrator pattern
2. **Upload processor goroutine** - processes queued uploads when slots available
3. **TransferRequest(Upload) sending** - notify peer we're ready
4. **TransferResponse handling** - peer's acceptance
5. **File streaming with resume** - read offset from peer, stream file data
6. **Cleanup and error handling** - UploadDenied/UploadFailed messages

---

## Architecture Design

### Upload Flow (Step by Step)

```
1. Upload queued via handleQueueDownload (Task 4.2) ✅
2. Upload processor picks up queued upload
3. Acquire slots (per-user + global)
4. Get peer address from server
5. Get/create P-type message connection
6. Send TransferRequest(direction=Upload, token, filename, size)
7. Wait for TransferResponse(allowed=true)
8. Establish F-type transfer connection
9. Read 8-byte offset from peer (resume support)
10. Stream file data from offset to end
11. Complete: release slots, remove from queue/registry
```

### Key Components

#### 1. Upload Orchestrator (`uploadOrchestrator`)
Mirrors `downloadOrchestrator` pattern:
```go
type uploadOrchestrator struct {
    client   *Client
    transfer *Transfer
    file     *SharedFile
    peerAddr string
    ctx      context.Context
    cancel   context.CancelFunc
}

func (o *uploadOrchestrator) run() {
    defer o.cleanup()

    // Phase 1: Acquire slots
    if err := o.acquireSlots(); err != nil { o.fail(err); return }

    // Phase 2: Get peer address
    if err := o.getPeerAddress(); err != nil { o.fail(err); return }

    // Phase 3: Send TransferRequest, get response
    if err := o.sendTransferRequest(); err != nil { o.fail(err); return }

    // Phase 4: Establish transfer connection
    if err := o.establishConnection(); err != nil { o.fail(err); return }

    // Phase 5: Stream file data
    if err := o.streamFile(); err != nil { o.fail(err); return }

    o.complete()
}
```

#### 2. Upload Processor
Background goroutine that monitors the queue and starts uploads:
```go
type uploadProcessor struct {
    client  *Client
    stopCh  chan struct{}
    doneCh  chan struct{}
}

func (p *uploadProcessor) run() {
    ticker := time.NewTicker(uploadProcessInterval)
    for {
        select {
        case <-p.stopCh:
            return
        case <-ticker.C:
            p.processQueue()
        }
    }
}

func (p *uploadProcessor) processQueue() {
    // For each queued upload, try to start it
    // (slot acquisition will block if limits reached)
}
```

#### 3. Transfer Coordination Channels
Add upload-specific channels to Transfer:
```go
// Transfer additions
transferResponseCh chan *peer.TransferResponse  // For uploads
```

---

## Detailed Implementation Steps

### Step 1: Extend Transfer for Uploads

Add upload coordination fields to `Transfer`:
```go
// In transfer.go
type Transfer struct {
    // ... existing fields ...

    // Coordination channels for uploads (initialized via InitUploadChannels)
    transferResponseCh chan *peer.TransferResponse

    // File reference for uploads
    sharedFile *SharedFile
}

func (t *Transfer) InitUploadChannels() {
    t.mu.Lock()
    defer t.mu.Unlock()
    t.transferResponseCh = make(chan *peer.TransferResponse, 1)
}

func (t *Transfer) TransferResponseCh() chan *peer.TransferResponse {
    t.mu.RLock()
    defer t.mu.RUnlock()
    return t.transferResponseCh
}
```

### Step 2: Create Upload Orchestrator

New file: `client/upload.go`
```go
package client

type uploadOrchestrator struct {
    client   *Client
    transfer *Transfer
    peerAddr string
    ctx      context.Context
    cancel   context.CancelFunc
}

func (o *uploadOrchestrator) run() {
    defer o.cleanup()

    // Set initial state
    o.transfer.SetState(TransferStateQueued | TransferStateLocally)
    o.transfer.emitProgress()

    // Phase 1: Acquire slots
    if err := o.acquireSlots(); err != nil {
        o.fail(err)
        return
    }

    // Phase 2: Get peer address
    o.transfer.SetState(TransferStateRequested)
    o.transfer.emitProgress()
    if err := o.getPeerAddress(); err != nil {
        o.fail(fmt.Errorf("get peer address: %w", err))
        return
    }

    // Phase 3: Connect and send TransferRequest
    peerConn, err := o.connectAndSendRequest()
    if err != nil {
        o.fail(err)
        return
    }
    defer peerConn.Close()

    // Phase 4: Wait for TransferResponse
    resp, err := o.waitForResponse(peerConn)
    if err != nil {
        o.fail(err)
        return
    }
    if !resp.Allowed {
        o.fail(&TransferRejectedError{Reason: resp.Reason})
        return
    }

    // Phase 5: Establish transfer connection
    o.transfer.SetState(TransferStateInitializing)
    o.transfer.emitProgress()

    transferConn, err := o.establishTransferConnection()
    if err != nil {
        o.fail(fmt.Errorf("transfer connection: %w", err))
        return
    }
    defer transferConn.Close()

    // Phase 6: Stream file
    if err := o.streamFile(transferConn); err != nil {
        o.fail(fmt.Errorf("stream file: %w", err))
        return
    }

    o.complete()
}

func (o *uploadOrchestrator) acquireSlots() error {
    return o.client.slots.AcquireUploadSlot(o.ctx, o.transfer.Username)
}

func (o *uploadOrchestrator) getPeerAddress() error {
    addr, err := o.client.getPeerAddress(o.ctx, o.transfer.Username)
    if err != nil {
        return err
    }
    o.peerAddr = addr
    return nil
}

func (o *uploadOrchestrator) connectAndSendRequest() (*connection.Conn, error) {
    // Get P-type connection
    conn, _, err := o.client.peerConnMgr.GetOrCreateEx(o.ctx, o.transfer.Username, o.peerAddr)
    if err != nil {
        return nil, fmt.Errorf("connect to peer: %w", err)
    }

    // Send TransferRequest(Upload)
    var buf bytes.Buffer
    w := protocol.NewWriter(&buf)
    req := &peer.TransferRequest{
        Direction: peer.TransferUpload,
        Token:     o.transfer.Token,
        Filename:  o.transfer.Filename,
        FileSize:  o.transfer.Size,
    }
    req.Encode(w)
    if err := w.Error(); err != nil {
        return nil, err
    }
    if err := conn.WriteMessage(buf.Bytes()); err != nil {
        return nil, err
    }

    return conn, nil
}

func (o *uploadOrchestrator) waitForResponse(conn *connection.Conn) (*peer.TransferResponse, error) {
    // Set read deadline
    if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
        return nil, err
    }

    for {
        payload, err := conn.ReadMessage()
        if err != nil {
            return nil, err
        }

        if len(payload) < 4 {
            continue
        }

        code := binary.LittleEndian.Uint32(payload[:4])
        if code == uint32(protocol.PeerTransferResponse) {
            resp, err := peer.DecodeTransferResponse(payload)
            if err != nil {
                return nil, err
            }
            if resp.Token == o.transfer.Token {
                return resp, nil
            }
        }
    }
}

func (o *uploadOrchestrator) establishTransferConnection() (*connection.Conn, error) {
    // Use TransferConnectionManager to establish F-type connection
    // As uploader, WE initiate the connection
    return o.client.transferConnMgr.GetConnection(o.ctx, o.transfer.Username, o.peerAddr, o.transfer.Token)
}

func (o *uploadOrchestrator) streamFile(conn *connection.Conn) error {
    o.transfer.SetState(TransferStateInProgress)
    o.transfer.emitProgress()

    // Read 8-byte offset from peer (resume support)
    var offsetBuf [8]byte
    if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
        return err
    }
    if _, err := io.ReadFull(conn, offsetBuf[:]); err != nil {
        return fmt.Errorf("read offset: %w", err)
    }
    offset := int64(binary.LittleEndian.Uint64(offsetBuf[:]))

    // Validate offset
    if offset > o.transfer.Size {
        return fmt.Errorf("invalid offset %d > size %d", offset, o.transfer.Size)
    }

    o.transfer.mu.Lock()
    o.transfer.StartOffset = offset
    o.transfer.Transferred = offset
    o.transfer.mu.Unlock()

    // Open file
    file, err := os.Open(o.transfer.sharedFile.LocalPath)
    if err != nil {
        return fmt.Errorf("open file: %w", err)
    }
    defer file.Close()

    // Seek to offset
    if offset > 0 {
        if _, err := file.Seek(offset, io.SeekStart); err != nil {
            return fmt.Errorf("seek: %w", err)
        }
    }

    // Clear deadline for streaming
    if err := conn.SetWriteDeadline(time.Time{}); err != nil {
        return err
    }

    // Stream file data
    toSend := o.transfer.Size - offset
    sent := int64(0)
    buf := make([]byte, 64*1024)

    for sent < toSend {
        select {
        case <-o.ctx.Done():
            return o.ctx.Err()
        default:
        }

        remaining := toSend - sent
        toRead := min(int64(len(buf)), remaining)

        n, err := file.Read(buf[:toRead])
        if err != nil && err != io.EOF {
            return fmt.Errorf("read file: %w", err)
        }
        if n == 0 {
            break
        }

        if _, err := conn.Write(buf[:n]); err != nil {
            return fmt.Errorf("write: %w", err)
        }

        sent += int64(n)
        o.transfer.UpdateProgress(offset + sent)
        o.transfer.emitProgress()
    }

    return nil
}

func (o *uploadOrchestrator) fail(err error) {
    o.transfer.mu.Lock()
    o.transfer.Error = err
    o.transfer.mu.Unlock()

    state := TransferStateCompleted
    switch {
    case errors.Is(err, context.Canceled):
        state |= TransferStateCancelled
    case errors.Is(err, context.DeadlineExceeded):
        state |= TransferStateTimedOut
    default:
        var rejected *TransferRejectedError
        if errors.As(err, &rejected) {
            state |= TransferStateRejected
        } else {
            state |= TransferStateErrored
        }
    }

    o.transfer.SetState(state)
    o.transfer.emitProgress()

    // Send failure notification to peer
    o.notifyFailure()
}

func (o *uploadOrchestrator) notifyFailure() {
    // Best-effort notification - errors are swallowed
    peerConn, _, err := o.client.peerConnMgr.GetOrCreateEx(o.ctx, o.transfer.Username, o.peerAddr)
    if err != nil {
        return
    }

    var buf bytes.Buffer
    w := protocol.NewWriter(&buf)

    if errors.Is(o.transfer.Error, context.Canceled) {
        msg := &peer.UploadDenied{
            Filename: o.transfer.Filename,
            Reason:   "Cancelled",
        }
        msg.Encode(w)
    } else {
        msg := &peer.UploadFailed{
            Filename: o.transfer.Filename,
        }
        msg.Encode(w)
    }

    if w.Error() == nil {
        _ = peerConn.WriteMessage(buf.Bytes())
    }
}

func (o *uploadOrchestrator) complete() {
    o.transfer.mu.Lock()
    o.transfer.Transferred = o.transfer.Size
    o.transfer.mu.Unlock()

    o.transfer.SetState(TransferStateCompleted | TransferStateSucceeded)
    o.transfer.emitProgress()
}

func (o *uploadOrchestrator) cleanup() {
    o.cancel()

    // Release slots
    o.client.slots.ReleaseUploadSlot(o.transfer.Username)

    // Remove from queue and registry
    o.client.queueMgr.DequeueUpload(o.transfer.Token)
    o.client.transfers.Remove(o.transfer.Token)
}
```

### Step 3: Create Upload Processor

Add to `client/upload.go`:
```go
const uploadProcessInterval = 1 * time.Second

type uploadProcessor struct {
    client  *Client
    stopCh  chan struct{}
    doneCh  chan struct{}
}

func newUploadProcessor(c *Client) *uploadProcessor {
    return &uploadProcessor{
        client: c,
        stopCh: make(chan struct{}),
        doneCh: make(chan struct{}),
    }
}

func (p *uploadProcessor) Start() {
    go p.run()
}

func (p *uploadProcessor) Stop() {
    close(p.stopCh)
    <-p.doneCh
}

func (p *uploadProcessor) run() {
    defer close(p.doneCh)

    ticker := time.NewTicker(uploadProcessInterval)
    defer ticker.Stop()

    for {
        select {
        case <-p.stopCh:
            return
        case <-ticker.C:
            p.processNextUpload()
        }
    }
}

func (p *uploadProcessor) processNextUpload() {
    // Get first queued upload that isn't already in progress
    // This is a simple approach - could be enhanced with priorities

    // Iterate through queue and find first pending
    // For now, we'll use a simple approach that processes in FIFO order

    p.client.queueMgr.mu.RLock()
    if len(p.client.queueMgr.queue) == 0 {
        p.client.queueMgr.mu.RUnlock()
        return
    }
    entry := p.client.queueMgr.queue[0]
    p.client.queueMgr.mu.RUnlock()

    // Get the transfer
    tr, ok := p.client.transfers.Get(entry.Token)
    if !ok {
        // Transfer was cancelled/removed, dequeue
        p.client.queueMgr.DequeueUpload(entry.Token)
        return
    }

    // Check if already in progress
    state := tr.State()
    if state != (TransferStateQueued | TransferStateLocally) {
        return // Already processing
    }

    // Get shared file
    var sharedFile *SharedFile
    if p.client.opts.FileSharer != nil {
        sharedFile = p.client.opts.FileSharer.GetFile(tr.Filename)
    }
    if sharedFile == nil {
        // File no longer shared, fail the transfer
        tr.mu.Lock()
        tr.Error = errors.New("file no longer shared")
        tr.mu.Unlock()
        tr.SetState(TransferStateCompleted | TransferStateErrored)
        tr.emitProgress()
        p.client.queueMgr.DequeueUpload(entry.Token)
        p.client.transfers.Remove(entry.Token)
        return
    }

    // Start upload orchestrator
    ctx, cancel := context.WithCancel(context.Background())
    orch := &uploadOrchestrator{
        client:   p.client,
        transfer: tr,
        ctx:      ctx,
        cancel:   cancel,
    }
    tr.sharedFile = sharedFile

    go orch.run()
}
```

### Step 4: Integrate with Client

Update `client/client.go`:
```go
type Client struct {
    // ... existing fields ...
    slots           *SlotManager      // Manages upload/download concurrency
    uploadProcessor *uploadProcessor  // Processes queued uploads
}

func New(opts *Options) *Client {
    // ... existing code ...
    c.slots = NewSlotManagerWithCleanup(
        opts.MaxConcurrentDownloads,
        opts.MaxConcurrentUploads,
        opts.SlotCleanupInterval,
        opts.SlotIdleThreshold,
    )
    c.uploadProcessor = newUploadProcessor(c)
    return c
}

func (c *Client) Login(ctx context.Context, username, password string) error {
    // ... existing code ...

    // Start upload processor
    c.uploadProcessor.Start()

    return nil
}

func (c *Client) Disconnect() error {
    // ... existing code ...

    // Stop upload processor
    if c.uploadProcessor != nil {
        c.uploadProcessor.Stop()
    }

    // Close slot manager
    if c.slots != nil {
        c.slots.Close()
    }

    return err
}
```

### Step 5: Update Transfer Struct

Add `sharedFile` field to Transfer in `client/transfer.go`:
```go
type Transfer struct {
    // ... existing fields ...

    // sharedFile reference for uploads (set by upload processor)
    sharedFile *SharedFile
}
```

---

## Test Plan

### Unit Tests (`client/upload_test.go`)

#### 1. Test Upload Orchestrator Phases
```go
func TestUploadOrchestrator_AcquireSlots(t *testing.T) {
    // Test slot acquisition blocks when full
    // Test slot release on failure
}

func TestUploadOrchestrator_TransferRequest(t *testing.T) {
    // Test TransferRequest encoding
    // Test correct direction (Upload)
    // Test token and filename
}

func TestUploadOrchestrator_TransferResponse(t *testing.T) {
    // Test allowed=true proceeds
    // Test allowed=false with rejection
    // Test timeout handling
}

func TestUploadOrchestrator_ResumeOffset(t *testing.T) {
    // Test offset=0 (full file)
    // Test offset>0 (resume)
    // Test invalid offset (> file size)
}

func TestUploadOrchestrator_StreamFile(t *testing.T) {
    // Test complete file streaming
    // Test resume from offset
    // Test progress updates
    // Test cancellation mid-stream
}
```

#### 2. Test Upload Processor
```go
func TestUploadProcessor_ProcessQueue(t *testing.T) {
    // Test processes in FIFO order
    // Test skips already-in-progress
    // Test handles missing transfer
    // Test handles file no longer shared
}

func TestUploadProcessor_StartStop(t *testing.T) {
    // Test graceful shutdown
    // Test no panic on double-stop
}
```

#### 3. Test Error Handling
```go
func TestUploadOrchestrator_FailureNotification(t *testing.T) {
    // Test sends UploadDenied on cancel
    // Test sends UploadFailed on error
}

func TestUploadOrchestrator_SlotRelease(t *testing.T) {
    // Test slots released on success
    // Test slots released on failure
    // Test slots released on cancel
}
```

### Integration Tests

#### 1. Full Upload Flow Test
```go
func TestUpload_FullFlow(t *testing.T) {
    // Create client with shared file
    // Simulate peer requesting download
    // Verify TransferRequest sent
    // Simulate TransferResponse
    // Verify file data received
    // Verify state transitions
}
```

#### 2. Resume Test
```go
func TestUpload_Resume(t *testing.T) {
    // Start upload
    // Simulate peer requesting resume at offset
    // Verify only remaining data sent
}
```

---

## Checklist

- [ ] **Step 1**: Extend Transfer struct for uploads
  - [ ] Add `sharedFile` field
  - [ ] Add upload coordination channels (if needed)

- [ ] **Step 2**: Create upload orchestrator
  - [ ] `acquireSlots()` - get per-user + global slots
  - [ ] `getPeerAddress()` - resolve peer IP
  - [ ] `connectAndSendRequest()` - P-type + TransferRequest
  - [ ] `waitForResponse()` - read TransferResponse
  - [ ] `establishTransferConnection()` - F-type connection
  - [ ] `streamFile()` - read offset, send data
  - [ ] `fail()` - error handling + notification
  - [ ] `complete()` - success handling
  - [ ] `cleanup()` - slot release, queue/registry removal

- [ ] **Step 3**: Create upload processor
  - [ ] Background goroutine with ticker
  - [ ] FIFO queue processing
  - [ ] Handle edge cases (missing transfer, file not shared)

- [ ] **Step 4**: Integrate with Client
  - [ ] Add `slots` field
  - [ ] Add `uploadProcessor` field
  - [ ] Initialize in `New()`
  - [ ] Start processor in `Login()`
  - [ ] Stop processor in `Disconnect()`

- [ ] **Step 5**: Write unit tests
  - [ ] Orchestrator phase tests
  - [ ] Processor tests
  - [ ] Error handling tests

- [ ] **Step 6**: Write integration tests
  - [ ] Full upload flow
  - [ ] Resume flow

- [ ] **Step 7**: Run `make check`
  - [ ] All tests pass
  - [ ] No lint errors
  - [ ] Code formatted

---

## Files to Create/Modify

| File | Action | Description |
|------|--------|-------------|
| `client/upload.go` | Create | Upload orchestrator and processor |
| `client/upload_test.go` | Create | Unit tests |
| `client/transfer.go` | Modify | Add `sharedFile` field |
| `client/client.go` | Modify | Add slots, processor, lifecycle |
| `client/options.go` | No change | Already has slot config |

---

## Design Decisions

1. **Mirror download pattern**: Upload orchestrator follows same structure as download orchestrator for consistency and maintainability.

2. **Background processor**: Uploads are processed by a background goroutine that monitors the queue, allowing natural backpressure via slot management.

3. **FIFO queue processing**: Simple, fair ordering. Can be enhanced later with priorities.

4. **Best-effort failure notification**: Send UploadDenied/UploadFailed on error but swallow failures (peer may be offline).

5. **Slot release in cleanup**: Always release slots in deferred cleanup to prevent leaks.

6. **No linger logic**: Unlike .NET, we don't wait for peer to disconnect. Go's connection handling is simpler - just close when done.
