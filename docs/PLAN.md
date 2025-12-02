# gosoulseek - Implementation Plan

## Project Goal

Port the Soulseek.NET library to idiomatic Go, providing a complete Soulseek protocol client library.

## Design Principles

1. **Idiomatic Go** - Not a literal .NET translation; use Go patterns and conventions
2. **Testable** - Interfaces for dependencies, pure functions where possible
3. **Readable** - Clear naming, small functions, minimal nesting
4. **Extensible** - Easy to add new message types without modifying core code
5. **Maintainable** - Separation of concerns, no god objects

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

### Phase 2: Server Connection Management

**Goal**: Establish a robust server connection with automatic reconnection.

| Component | Status | Description |
|-----------|--------|-------------|
| `client/client.go` | ⬚ | Main SoulseekClient struct |
| `client/options.go` | ⬚ | Client configuration options |
| Server message handler | ⬚ | Route incoming server messages |
| SetListenPort message | ⬚ | Required after login |
| SetOnlineStatus message | ⬚ | Set online/away status |
| Automatic ping/keepalive | ⬚ | Maintain connection |

**Key messages to implement**:
- `SetListenPort` (code 2)
- `SetOnlineStatus` (code 28)
- `SharedFoldersAndFiles` (code 35)

---

### Phase 3: Room and Chat

**Goal**: Join rooms, send/receive chat messages.

| Component | Status | Description |
|-----------|--------|-------------|
| Room list request/response | ⬚ | Get available rooms |
| Join/Leave room | ⬚ | Room membership |
| Room messages | ⬚ | Send/receive chat |
| Private messages | ⬚ | Direct user messages |
| User status tracking | ⬚ | Watch users online/offline |

**Key messages to implement**:
- `RoomList` (code 64)
- `JoinRoom` (code 14)
- `LeaveRoom` (code 15)
- `SayInChatRoom` (code 13)
- `PrivateMessage` (code 22)
- `WatchUser` (code 5)
- `GetStatus` (code 7)

---

### Phase 4: Search

**Goal**: Perform searches and receive results.

| Component | Status | Description |
|-----------|--------|-------------|
| Search request | ⬚ | Send file search query |
| Search response handler | ⬚ | Process search results |
| Search result aggregation | ⬚ | Collect results from peers |
| Wishlist search | ⬚ | Scheduled searches |

**Key messages to implement**:
- `FileSearch` (code 26)
- `UserSearch` (code 42)
- `RoomSearch` (code 120)
- `WishlistSearch` (code 103)

---

### Phase 5: Peer Connections

**Goal**: Establish direct connections with other users.

| Component | Status | Description |
|-----------|--------|-------------|
| Peer connection manager | ⬚ | Pool of peer connections |
| PeerInit message | ⬚ | Connection handshake |
| PierceFirewall | ⬚ | NAT traversal |
| ConnectToPeer handling | ⬚ | Server-mediated connections |
| Waiter pattern | ⬚ | Async request-response matching |

**Key messages to implement**:
- `ConnectToPeer` (code 18)
- `PeerInit` (init code 1)
- `PierceFirewall` (init code 0)
- Peer message codes (4-byte)

---

### Phase 6: Browse and User Info

**Goal**: Browse user shares and get user information.

| Component | Status | Description |
|-----------|--------|-------------|
| Browse request | ⬚ | Request user's file list |
| Browse response parser | ⬚ | Parse compressed file list |
| User info request | ⬚ | Get user description/picture |
| Folder contents | ⬚ | Get specific folder contents |

**Key messages to implement**:
- `BrowseRequest` (peer code 4)
- `BrowseResponse` (peer code 5)
- `InfoRequest` (peer code 15)
- `InfoResponse` (peer code 16)
- `FolderContentsRequest` (peer code 36)
- `FolderContentsResponse` (peer code 37)

---

### Phase 7: File Transfers

**Goal**: Download and upload files.

| Component | Status | Description |
|-----------|--------|-------------|
| Transfer manager | ⬚ | Queue and track transfers |
| Download initiation | ⬚ | Request file downloads |
| Upload handling | ⬚ | Serve upload requests |
| Transfer progress | ⬚ | Track bytes transferred |
| Queue management | ⬚ | Place in queue handling |
| Rate limiting | ⬚ | Token bucket for bandwidth |

**Key messages to implement**:
- `TransferRequest` (peer code 40)
- `TransferResponse` (peer code 41)
- `QueueDownload` (peer code 43)
- `PlaceInQueueRequest` (peer code 51)
- `PlaceInQueueResponse` (peer code 44)
- `UploadDenied` (peer code 50)
- `UploadFailed` (peer code 46)

---

### Phase 8: Distributed Network

**Goal**: Participate in the distributed search network.

| Component | Status | Description |
|-----------|--------|-------------|
| Distributed connection manager | ⬚ | Parent/child connections |
| Search request forwarding | ⬚ | Route searches |
| Branch level management | ⬚ | Track network position |

**Key messages to implement**:
- `DistributedSearchRequest` (dist code 3)
- `DistributedPing` (dist code 0)
- `DistributedBranchLevel` (dist code 4)
- `DistributedBranchRoot` (dist code 5)
- `EmbeddedMessage` (code 93)

---

### Phase 9: Share Management

**Goal**: Manage local shared files.

| Component | Status | Description |
|-----------|--------|-------------|
| Share scanner | ⬚ | Scan local directories |
| Share database | ⬚ | Index shared files |
| Search responder | ⬚ | Answer incoming searches |
| Share count reporting | ⬚ | Report to server |

---

### Phase 10: Polish and Production Ready

| Component | Status | Description |
|-----------|--------|-------------|
| Comprehensive error handling | ⬚ | All error cases covered |
| Logging/diagnostics | ⬚ | Structured logging |
| Metrics/observability | ⬚ | Prometheus metrics |
| Documentation | ⬚ | GoDoc, examples |
| Integration tests | ⬚ | End-to-end tests |

---

## Go Idioms Used

| Pattern | Where | Why |
|---------|-------|-----|
| `io.Reader`/`io.Writer` wrapping | protocol package | Composable, works with any stream |
| Sticky errors | Reader/Writer | Cleaner API, check once at end |
| `context.Context` for cancellation | connection.Dial | Standard Go timeout/cancellation |
| `net.Pipe()` for testing | connection tests | In-memory testing without network |
| Error wrapping with `%w` | everywhere | Proper error chain for debugging |
| `run() error` pattern | main.go | Testable main, proper exit codes |
| Interface-based DI | NewConn(net.Conn) | Inject mocks for testing |
| Table-driven tests | *_test.go | Comprehensive test coverage |

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
