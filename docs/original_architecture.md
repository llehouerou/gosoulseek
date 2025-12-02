# Soulseek.NET Architecture

This document describes the architecture of the reference Soulseek.NET project that we are porting to Go.

**Source**: https://github.com/jpdillingham/Soulseek.NET

## Overview

Soulseek.NET is a comprehensive C# implementation of the Soulseek protocol with **~271 source files** organized into a well-structured component-based architecture. The main client class (`SoulseekClient.cs`) alone is ~4,693 lines.

## Project Structure

```
Soulseek.NET/src/
├── SoulseekClient.cs           # Main client (~4,693 lines)
├── ISoulseekClient.cs          # Public interface
├── SoulseekClientOptions.cs    # Configuration
├── Network/                    # Connection management
│   ├── Tcp/                    # Low-level TCP
│   ├── MessageConnection.cs    # Message-framed connections
│   ├── PeerConnectionManager.cs
│   └── DistributedConnectionManager.cs
├── Messaging/                  # Protocol layer
│   ├── MessageBuilder.cs       # Binary encoding
│   ├── MessageReader.cs        # Binary decoding
│   ├── MessageCode.cs          # All message codes
│   ├── Compression/            # zlib implementation
│   ├── Handlers/               # Message routing
│   └── Messages/               # 97+ message types
│       ├── Server/             # 74 server messages
│       ├── Peer/               # 15 peer messages
│       ├── Distributed/        # 6 distributed messages
│       └── Initialization/     # 2 init messages
├── Options/                    # Configuration classes
├── EventArgs/                  # 30 event argument types
├── Exceptions/                 # 26 custom exceptions
├── Diagnostics/                # Logging infrastructure
└── Common/                     # Utilities (Waiter, TokenBucket, etc.)
```

## Protocol Characteristics

### Server
- **Address**: `vps.slsknet.org:2271`
- **Protocol**: Custom binary over TCP

### Message Format
```
[4 bytes: length of (code + payload)]
[code: 1 or 4 bytes depending on message type]
[payload: variable length]
```

### Encoding
- **Integers**: Little-endian
- **Strings**: Length-prefixed UTF-8 `[4-byte length][UTF-8 bytes]`
- **Compression**: zlib (DEFLATE) for some payloads

### Message Categories

| Category | Code Size | Count | Purpose |
|----------|-----------|-------|---------|
| Server | 4 bytes | 74 | Client-server communication |
| Peer | 4 bytes | 15 | Direct peer-to-peer |
| Distributed | 1 byte | 6 | Distributed search network |
| Initialization | 1 byte | 2 | Connection handshake |

---

## Core Components

### 1. SoulseekClient

The main entry point managing all client functionality:

- **Server Connection**: Single connection to central server
- **Peer Connections**: Pool of connections to other users
- **Distributed Network**: Tree structure for search propagation
- **Search Management**: Track active searches and results
- **Transfer Management**: Upload/download queues
- **Room Management**: Chat room membership
- **Event Broadcasting**: 30+ events for state changes

### 2. Network Layer

#### Connection (`Network/Tcp/Connection.cs`)

Low-level TCP connection with:
- **State Machine**: Pending → Connecting → Connected → Disconnecting → Disconnected
- **Inactivity Timer**: Auto-disconnect after 15s of no activity
- **Watchdog Timer**: 250ms interval checking connection health
- **Write Queue**: Double-buffered with 250 item limit
- **Timeout Handling**: Configurable connect/read/write timeouts

#### MessageConnection (`Network/MessageConnection.cs`)

Extends Connection with message framing:
- Continuous read loop after connection
- Events: `MessageReceived`, `MessageRead`, `MessageWritten`, `MessageDataRead`
- Distinguishes server vs peer connections for error handling

#### PeerConnectionManager (`Network/PeerConnectionManager.cs`)

~900 lines managing peer connections:
```csharp
ConcurrentDictionary<string, Lazy<Task<IMessageConnection>>> MessageConnectionDictionary
ConcurrentDictionary<int, string> PendingSolicitationDictionary
ConcurrentDictionary<string, CancellationTokenSource> PendingInboundIndirectConnectionDictionary
```

Key features:
- Connection caching per username
- `Lazy<Task<>>` for deferred initialization
- Race handling between direct and indirect connections
- Automatic connection superseding

#### DistributedConnectionManager (`Network/DistributedConnectionManager.cs`)

~1,132 lines managing distributed network:
- Parent connection (single)
- Child connections (limited pool)
- Branch level and root tracking
- Watchdog timer (15 minutes)
- Latency tracking with exponential smoothing

### 3. Messaging Layer

#### MessageBuilder (`Messaging/MessageBuilder.cs`)

Fluent API for building messages:
```csharp
new MessageBuilder()
    .WriteCode(MessageCode.Server.Login)
    .WriteString(Username)
    .WriteString(Password)
    .WriteInteger(Version)
    .WriteString(Hash)
    .WriteInteger(MinorVersion)
    .Build();
```

Methods:
- `WriteCode(code)` - 1 or 4 bytes depending on type
- `WriteInteger(value)` - 4 bytes little-endian
- `WriteLong(value)` - 8 bytes little-endian
- `WriteString(value)` - Length-prefixed UTF-8
- `WriteByte(value)` - Single byte
- `WriteBytes(bytes)` - Raw bytes
- `Compress()` - Apply zlib compression
- `Build()` - Return final bytes with length prefix

#### MessageReader (`Messaging/MessageReader.cs`)

Generic parser with position tracking:
```csharp
var reader = new MessageReader<MessageCode.Server>(bytes);
var code = reader.ReadCode();
var succeeded = reader.ReadByte() == 1;
var message = reader.ReadString();
```

Features:
- Generic over code enum type
- Automatic code length detection (1 vs 4 bytes)
- Position tracking with `Seek()`
- `Decompress()` for zlib payloads
- UTF-8 with ISO-8859-1 fallback

#### MessageCode (`Messaging/MessageCode.cs`)

Static enum definitions:

```csharp
public enum Server {
    Login = 1,
    SetListenPort = 2,
    GetPeerAddress = 3,
    // ... 74 total
}

public enum Peer {
    BrowseRequest = 4,
    BrowseResponse = 5,
    SearchResponse = 9,
    // ... 15 total
}

public enum Distributed : byte {
    Ping = 0,
    SearchRequest = 3,
    // ... 6 total
}

public enum Initialization : byte {
    PierceFirewall = 0,
    PeerInit = 1,
}
```

### 4. Message Handlers

#### ServerMessageHandler

Processes incoming server messages, raises events:
- `PrivateMessageReceived`
- `GlobalMessageReceived`
- `KickedFromServer`
- `DistributedNetworkReset`
- Room events, privilege events, etc.

#### PeerMessageHandler

Handles peer-to-peer messages:
- Browse requests/responses
- Search responses
- Transfer negotiations
- User info

#### DistributedMessageHandler

Handles distributed network:
- Search request forwarding
- Ping/pong
- Branch level updates

### 5. Waiter Pattern (`Common/Waiter.cs`)

Matches async request-response across connections:

```csharp
ConcurrentDictionary<WaitKey, ConcurrentQueue<PendingWait>> Waits
ConcurrentDictionary<WaitKey, ReaderWriterLockSlim> Locks
```

#### WaitKey

Composite key for matching:
```csharp
new WaitKey(Constants.WaitKey.DirectTransfer, username, remoteToken)
// Creates: "DirectTransfer:username:12345"
```

#### API
```csharp
Task<T> Wait<T>(WaitKey key, int? timeout = null, CancellationToken? = null)
void Complete<T>(WaitKey key, T result)
void Throw(WaitKey key, Exception exception)
void Timeout(WaitKey key)
void Cancel(WaitKey key)
```

Uses `TaskCompletionSource<T>` internally with configurable timeout (default 5s).

### 6. Rate Limiting (`Common/TokenBucket.cs`)

Token bucket algorithm for bandwidth control:
- Used for upload/download speed limits
- Configurable capacity and refill rate

---

## Key Message Types

### Server Messages (74 total)

#### Authentication
| Code | Name | Description |
|------|------|-------------|
| 1 | Login | Authenticate with server |
| 2 | SetListenPort | Report listening port |
| 41 | KickedFromServer | Forced disconnect |

#### Chat
| Code | Name | Description |
|------|------|-------------|
| 13 | SayInChatRoom | Send room message |
| 14 | JoinRoom | Join chat room |
| 15 | LeaveRoom | Leave chat room |
| 22 | PrivateMessage | Direct message |
| 64 | RoomList | Get room list |

#### Search
| Code | Name | Description |
|------|------|-------------|
| 26 | FileSearch | Global file search |
| 42 | UserSearch | Search specific user |
| 120 | RoomSearch | Search in room |
| 103 | WishlistSearch | Scheduled search |

#### Peer Coordination
| Code | Name | Description |
|------|------|-------------|
| 3 | GetPeerAddress | Get user's IP/port |
| 18 | ConnectToPeer | Request peer connection |
| 1001 | CannotConnect | Connection failed |

#### Distributed Network
| Code | Name | Description |
|------|------|-------------|
| 71 | HaveNoParents | Request parent assignment |
| 73 | ParentsIP | List of potential parents |
| 93 | EmbeddedMessage | Wrapped distributed message |
| 102 | NetInfo | Network topology info |

### Peer Messages (15 total)

| Code | Name | Description |
|------|------|-------------|
| 4 | BrowseRequest | Request file list |
| 5 | BrowseResponse | File list (compressed) |
| 9 | SearchResponse | Search results |
| 15 | InfoRequest | Request user info |
| 16 | InfoResponse | User description/picture |
| 36 | FolderContentsRequest | Get folder contents |
| 37 | FolderContentsResponse | Folder contents |
| 40 | TransferRequest | Request file transfer |
| 41 | TransferResponse | Accept/reject transfer |
| 43 | QueueDownload | Queue a download |
| 44 | PlaceInQueueResponse | Queue position |
| 50 | UploadDenied | Reject upload |
| 46 | UploadFailed | Transfer failed |
| 51 | PlaceInQueueRequest | Request queue position |

### Distributed Messages (6 total)

| Code | Name | Description |
|------|------|-------------|
| 0 | Ping | Keep-alive |
| 3 | SearchRequest | Distributed search |
| 4 | BranchLevel | Network depth |
| 5 | BranchRoot | Root of branch |
| 7 | ChildDepth | Children depth |
| 93 | EmbeddedMessage | Server message wrapper |

### Initialization Messages (2 total)

| Code | Name | Description |
|------|------|-------------|
| 0 | PierceFirewall | NAT traversal response |
| 1 | PeerInit | Direct connection handshake |

---

## Connection Types

### Server Connection
- Single persistent connection
- No inactivity timeout
- Handles all server messages

### Peer Connections

**Types** (flags, can be combined):
- `Outbound` - Client initiates
- `Inbound` - Server accepts
- `Direct` - P2P connection
- `Indirect` - Via firewall piercing

**Connection Flow**:
1. Request via `ConnectToPeer` message
2. Attempt direct connection
3. If fails, attempt indirect via `PierceFirewall`
4. First successful connection wins, other cancelled

### Distributed Connections
- Parent connection (single)
- Child connections (pool)
- Used for search propagation

---

## Compression

Uses zlib (DEFLATE) for:
- Browse responses
- Folder contents responses
- Some search responses

Implementation in `Messaging/Compression/`:
- `ZOutputStream` for compression
- `ZInputStream` for decompression
- 262KB chunk processing

---

## Configuration (`SoulseekClientOptions.cs`)

Key options:
- `ListenPort` - Port for incoming connections
- `EnableDistributedNetwork` - Participate in distributed search
- `MaximumConcurrentUploads/Downloads` - Transfer limits
- `UploadSpeedLimit/DownloadSpeedLimit` - Bandwidth limits
- `ConnectionOptions` - Timeout settings
- Response delegates for dynamic responses

---

## Events (30 types)

### Client State
- `SoulseekClientStateChanged`
- `SoulseekClientDisconnected`

### Search
- `SearchStateChanged`
- `SearchResponseReceived`
- `SearchRequestReceived`

### Transfer
- `TransferStateChanged`
- `TransferProgressUpdated`
- `DownloadDenied/Failed`

### Chat
- `PrivateMessageReceived`
- `RoomMessageReceived`
- `PublicChatMessageReceived`

### Rooms
- `RoomJoined/Left`
- `RoomTickerAdded/Removed`

### Users
- `UserCannotConnect`
- `UserStatusChanged`

### Distributed
- `DistributedChildAdded/Removed`
- `DistributedParentAdded/Removed`

---

## Error Handling (26 exception types)

### Connection
- `ConnectionException`
- `ConnectionReadException`
- `ConnectionWriteException`
- `ConnectionWriteDroppedException`

### Network
- `AddressException`
- `ProxyException`
- `UserEndPointException`

### Message
- `MessageException`
- `MessageReadException`
- `MessageCompressionException`

### Authentication
- `LoginRejectedException`
- `KickedFromServerException`

### Transfer
- `TransferException`
- `DuplicateTransferException`
- `TransferNotFoundException`
- `TransferRejectedException`

### Room
- `RoomException`
- `RoomJoinForbiddenException`

---

## Key Design Patterns

1. **Interface-Based Design**: Heavy use of interfaces for DI and testability
2. **Async/Await**: Task-based asynchronous programming throughout
3. **Event-Driven**: Client exposes 30+ events for state changes
4. **Message-Handler Pattern**: Route messages through specialized handlers
5. **Factory Pattern**: ConnectionFactory, message factories
6. **Semaphore-Based Concurrency**: Limits on concurrent operations
7. **Token Bucket Rate Limiting**: For bandwidth management
8. **Connection Pooling**: Reuse peer connections
9. **Lazy Initialization**: `Lazy<Task<>>` for deferred connections

---

## Reference Files

| Purpose | Path |
|---------|------|
| Main client | `src/SoulseekClient.cs` |
| Client interface | `src/ISoulseekClient.cs` |
| Message builder | `src/Messaging/MessageBuilder.cs` |
| Message reader | `src/Messaging/MessageReader.cs` |
| Message codes | `src/Messaging/MessageCode.cs` |
| TCP connection | `src/Network/Tcp/Connection.cs` |
| Message connection | `src/Network/MessageConnection.cs` |
| Peer manager | `src/Network/PeerConnectionManager.cs` |
| Distributed manager | `src/Network/DistributedConnectionManager.cs` |
| Waiter | `src/Common/Waiter.cs` |
| Server messages | `src/Messaging/Messages/Server/` |
| Peer messages | `src/Messaging/Messages/Peer/` |
| Distributed messages | `src/Messaging/Messages/Distributed/` |
