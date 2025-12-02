# gosoulseek - Implementation Plan

## Project Goal

Port the Soulseek.NET library to idiomatic Go, providing a complete Soulseek protocol client library.

## Design Principles

1. **Idiomatic Go** - Not a literal .NET translation; use Go patterns and conventions
2. **Testable** - Interfaces for dependencies, pure functions where possible
3. **Readable** - Clear naming, small functions, minimal nesting
4. **Extensible** - Easy to add new message types without modifying core code
5. **Maintainable** - Separation of concerns, no god objects
6. **Channels over Callbacks** - Use channels for async results, context for cancellation

## Implementation Phases

### Phase 1: Minimal POC ✅ COMPLETE

**Goal**: Prove the binary protocol works by connecting and logging in.

| Component | Status | Description |
|-----------|--------|-------------|
| `protocol/writer.go` | ✅ | Binary encoding (io.Writer based) |
| `protocol/reader.go` | ✅ | Binary decoding (io.Reader based) |
| `protocol/codes.go` | ✅ | Message code constants |
| `messages/server/login.go` | ✅ | Login request/response |
| `messages/server/ping.go` | ✅ | Server ping message |
| `connection/connection.go` | ✅ | TCP connection with framing |
| `cmd/poc/main.go` | ✅ | POC CLI |

**Result**: Successfully logged in to `vps.slsknet.org:2271` and sent ping.

---

### Phase 2: Server Connection Management ✅ COMPLETE

**Goal**: Establish a robust server connection with client API.

| Component | Status | Description |
|-----------|--------|-------------|
| `client/client.go` | ✅ | Main Client struct |
| `client/options.go` | ✅ | Client configuration options |
| `messages/server/setlistenport.go` | ✅ | SetListenPort message |
| `messages/server/setonlinestatus.go` | ✅ | SetOnlineStatus message |
| `messages/server/sharedfoldersandfiles.go` | ✅ | SharedFoldersAndFiles message |

**Result**: Client with Connect/Login/Disconnect, automatic post-login configuration.

---

### Phase 3: Search ✅ COMPLETE

**Goal**: Perform searches and receive results via channels.

| Component | Status | Description |
|-----------|--------|-------------|
| `protocol/compress.go` | ✅ | zlib compression/decompression |
| `messages/peer/file.go` | ✅ | File and FileAttribute types |
| `messages/peer/searchresponse.go` | ✅ | SearchResponse parsing |
| `messages/peer/init.go` | ✅ | Peer initialization message |
| `messages/server/filesearch.go` | ✅ | FileSearch request |
| `messages/server/connecttopeer.go` | ✅ | ConnectToPeer response |
| `client/router.go` | ✅ | Message dispatching |
| `client/search.go` | ✅ | Search with channel-based results |
| `client/peer.go` | ✅ | Peer connection handling |

**API**:
```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

results, err := client.Search(ctx, "artist album")
for resp := range results {
    fmt.Printf("%s: %d files\n", resp.Username, len(resp.Files))
}
```

**Result**: Full search working with channel-based results, automatic peer connections.

---

### Phase 4: File Downloads ✅ COMPLETE

**Goal**: Download files from peers.

| Component | Status | Description |
|-----------|--------|-------------|
| `messages/peer/transfer.go` | ✅ | Transfer request/response messages |
| `messages/server/getpeeraddress.go` | ✅ | GetPeerAddress request/response |
| `client/download.go` | ✅ | Download manager with progress tracking |
| `client/router.go` | ✅ | Updated with handler unregistration |

**Peer messages implemented**:
- `TransferRequest` (peer code 40)
- `TransferResponse` (peer code 41)
- `QueueDownload` (peer code 43)
- `PlaceInQueueRequest` (peer code 51)
- `PlaceInQueueResponse` (peer code 44)
- `UploadFailed` (peer code 46)
- `UploadDenied` (peer code 50)

**API**:
```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
defer cancel()

file, _ := os.Create("output.mp3")
progress, err := client.Download(ctx, "username", "@@music/file.mp3", file)
for p := range progress {
    fmt.Printf("%.1f%% complete\n", p.PercentComplete())
    if p.Error != nil {
        return p.Error
    }
}
```

**Result**: Channel-based download progress, automatic peer connection, queue handling.

---

### Phase 5: Browse and User Info

**Goal**: Browse user shares and get user information.

| Component | Status | Description |
|-----------|--------|-------------|
| Browse request | ⬚ | Request user's file list |
| Browse response parser | ⬚ | Parse compressed file list |
| User info request | ⬚ | Get user description/picture |

**Key messages to implement**:
- `BrowseRequest` (peer code 4)
- `BrowseResponse` (peer code 5)
- `InfoRequest` (peer code 15)
- `InfoResponse` (peer code 16)

---

### Phase 6: Upload Support

**Goal**: Allow others to download from us.

| Component | Status | Description |
|-----------|--------|-------------|
| Share scanner | ⬚ | Scan local directories |
| Share index | ⬚ | Searchable file index |
| Search responder | ⬚ | Answer incoming searches |
| Upload handler | ⬚ | Serve file requests |
| Queue management | ⬚ | Manage upload queue |

---

### Phase 7: Distributed Network (Optional)

**Goal**: Participate in the distributed search network.

| Component | Status | Description |
|-----------|--------|-------------|
| Distributed connection manager | ⬚ | Parent/child connections |
| Search request forwarding | ⬚ | Route searches |
| Branch level management | ⬚ | Track network position |

---

### Phase 8: Room and Chat (Low Priority)

**Goal**: Join rooms, send/receive chat messages.

| Component | Status | Description |
|-----------|--------|-------------|
| Room list | ⬚ | Get available rooms |
| Join/Leave room | ⬚ | Room membership |
| Room messages | ⬚ | Send/receive chat |
| Private messages | ⬚ | Direct user messages |

---

## Go Idioms Used

| Pattern | Where | Why |
|---------|-------|-----|
| `io.Reader`/`io.Writer` wrapping | protocol package | Composable, works with any stream |
| `io.Writer` for download dest | client.Download | Flexible output (file, buffer, etc.) |
| Sticky errors | Reader/Writer | Cleaner API, check once at end |
| `context.Context` for cancellation | all blocking ops | Standard Go timeout/cancellation |
| Channels for async results | Search, Download | Idiomatic Go concurrency |
| `net.Pipe()` for testing | connection tests | In-memory testing without network |
| Error wrapping with `%w` | everywhere | Proper error chain for debugging |
| `run() error` pattern | main.go | Testable main, proper exit codes |
| Interface-based DI | NewConn(net.Conn) | Inject mocks for testing |
| Table-driven tests | *_test.go | Comprehensive test coverage |
| State machines | download progress | Clear state transitions |

---

## Testing Strategy

### Unit Tests
- Protocol encoding/decoding with known byte sequences
- Message serialization roundtrips
- Connection framing with `net.Pipe()`

### Integration Tests
- POC against real server
- Full login/search/download flow

### Test Commands
```bash
make test          # Run all unit tests
make coverage      # Run with coverage report
make check         # fmt + lint + test
make run-poc       # Test against real server
```
