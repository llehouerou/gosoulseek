# Task 2.2: Transfer Connection Establishment (Unified)

## Overview

Create a `TransferConnectionManager` that provides a unified API for establishing F-type (file transfer) connections. It abstracts the complexity of concurrent direct/indirect connection strategies and supports both initiator (uploader) and receiver (downloader) roles.

## Interface

```go
// TransferConnectionManager handles F-type transfer connection establishment.
// It supports two roles:
// - Initiator (uploader): actively connects to peer
// - Receiver (downloader): waits for peer to connect, with fallback strategies
type TransferConnectionManager struct {
    client *Client
}

func NewTransferConnectionManager(client *Client) *TransferConnectionManager

// GetConnection initiates a transfer connection to the peer (initiator/uploader role).
// Uses concurrent direct + indirect strategy, first successful connection wins.
// The token is sent to the peer to identify the transfer.
func (m *TransferConnectionManager) GetConnection(
    ctx context.Context,
    username string,
    peerAddr string,
    token uint32,
) (*connection.Conn, error)

// AwaitConnection waits for a transfer connection from the peer (receiver/downloader role).
// Uses triple strategy: inbound (wait on listener), direct, and indirect - racing all three.
// The transfer must have RemoteToken set and TransferConnCh initialized.
func (m *TransferConnectionManager) AwaitConnection(
    ctx context.Context,
    transfer *Transfer,
    peerAddr string,
) (*connection.Conn, error)
```

## GetConnection (Initiator/Uploader Role)

The uploader needs to establish a connection to send file data. Uses **dual strategy** (direct + indirect) racing concurrently.

### Direct Connection Flow
1. Dial TCP to `peerAddr`
2. Send `PeerInit{Username, Type: "F", Token: token}` as framed message
3. Send `token` as raw 4-byte little-endian (protocol quirk)
4. Return connection ready for file transfer

### Indirect Connection Flow
1. Generate solicitation token
2. Register in `client.solicitations` map
3. Send `ConnectToPeerRequest{Token, Username, Type: "F"}` to server
4. Server tells peer to connect to us
5. Peer connects via PierceFirewall, listener delivers to solicitation channel
6. Read 4-byte remote token from peer (validates it matches expected)
7. Return connection ready for file transfer

**Why dual not triple?** The uploader is the initiator - there's no "wait for inbound" because *we* are the one who should connect.

## AwaitConnection (Receiver/Downloader Role)

The downloader waits for the uploader to connect and send file data. Uses **triple strategy** racing concurrently.

### Inbound Flow (Primary)
1. Wait on `transfer.TransferConnCh()` for connection from listener
2. Listener receives peer's PeerInit(F) or PierceFirewall
3. Listener reads 4-byte remote token, looks up transfer, delivers connection
4. Return connection ready for file transfer

### Direct Flow (Fallback)
1. Poll until `transfer.RemoteToken` is set (with timeout)
2. Dial TCP to `peerAddr`
3. Send `PeerInit{Username, Type: "F", Token: remoteToken}`
4. Send `remoteToken` as raw 4-byte little-endian
5. Return connection ready for file transfer

### Indirect Flow (Fallback)
1. Send `ConnectToPeerRequest{Token, Username, Type: "F"}` to server
2. Wait for peer to connect via listener
3. Read 4-byte token, verify it matches `transfer.RemoteToken` if set
4. Return connection ready for file transfer

**Why triple for receiver?** Some uploader clients (like Nicotine+) expect the downloader to initiate. Having all three strategies maximizes compatibility.

## Integration

### Client Integration

```go
type Client struct {
    // ... existing fields
    peerConnMgr     *PeerConnectionManager      // P-type connections
    transferConnMgr *TransferConnectionManager  // F-type connections (NEW)
    transfers       *TransferRegistry
    // ...
}

func NewClient(...) *Client {
    c := &Client{...}
    c.peerConnMgr = NewPeerConnectionManager(c)
    c.transferConnMgr = NewTransferConnectionManager(c)  // NEW
    // ...
}
```

### Download Flow Refactoring

Replace `downloadOrchestrator.getTransferConnection()` (~70 lines) with:

```go
conn, err := o.client.transferConnMgr.AwaitConnection(o.ctx, o.transfer, o.peerAddr)
```

### Upload Flow (Future Task 4.3)

```go
conn, err := o.client.transferConnMgr.GetConnection(o.ctx, o.transfer.Username, peerAddr, o.transfer.Token)
```

## File Structure

### New File
- `client/transfer_conn.go` - TransferConnectionManager implementation (~200-250 lines)

### Modified Files
- `client/client.go` - Add `transferConnMgr` field and initialization
- `client/download.go` - Replace inline racing logic with manager call, remove helper methods

## Error Handling

```go
// Returned when all connection strategies fail
type TransferConnectionError struct {
    DirectErr   error
    IndirectErr error
    InboundErr  error  // only for AwaitConnection
}

func (e *TransferConnectionError) Error() string
```

### Edge Cases
1. **Context cancellation** - All goroutines check context, clean up connections on cancel
2. **Partial success** - First success cancels other attempts, losing connections are closed
3. **No listener running** - Indirect strategy returns error immediately if `ListenerPort() == 0`
4. **RemoteToken not set** - Direct strategy in `AwaitConnection` polls with timeout (30s)
5. **Token mismatch** - Indirect strategy validates received token matches expected
6. **Connection closed during handshake** - Wrapped as connection error, doesn't panic

### Cleanup Pattern

```go
go func() {
    conn, err := m.connectDirect(ctx, ...)
    select {
    case resultCh <- result{conn, err}:
    case <-ctx.Done():
        if conn != nil {
            conn.Close()
        }
    }
}()
```

## Testing Strategy

### Test Cases

```go
// client/transfer_conn_test.go

func TestTransferConnectionManager_GetConnection_DirectSuccess(t *testing.T)
func TestTransferConnectionManager_GetConnection_IndirectFallback(t *testing.T)
func TestTransferConnectionManager_GetConnection_BothFail(t *testing.T)
func TestTransferConnectionManager_AwaitConnection_InboundSuccess(t *testing.T)
func TestTransferConnectionManager_AwaitConnection_DirectFallback(t *testing.T)
func TestTransferConnectionManager_AwaitConnection_Cancellation(t *testing.T)
func TestTransferConnectionManager_AwaitConnection_AllFail(t *testing.T)
```

### Test Utilities
- Use `net.Pipe()` for mock connections
- Same patterns as existing download tests

## Configuration

Hardcoded sensible defaults (can add options later if needed):
- Connection timeout: 30 seconds
- Token read timeout: 10 seconds
- RemoteToken poll interval: 100ms

## Implementation Steps

1. Create `client/transfer_conn.go` with `TransferConnectionManager` struct
2. Implement `connectDirect()` helper (extracted from download.go)
3. Implement `connectIndirect()` helper (extracted from download.go)
4. Implement `waitInbound()` helper
5. Implement `GetConnection()` with dual racing
6. Implement `AwaitConnection()` with triple racing
7. Add `TransferConnectionError` type
8. Add `transferConnMgr` to Client
9. Refactor `downloadOrchestrator` to use manager
10. Remove old helper methods from download.go
11. Write tests
12. Run existing download tests to verify no regression
