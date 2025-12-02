# gosoulseek - Go Port of Soulseek.NET

## Dev Workflow

```bash
make fmt           # Format code (goimports-reviser)
make lint          # Run golangci-lint
make test          # Run tests
make coverage      # Run tests with coverage
make check         # Format + lint + test
make build         # Verify compilation (no binary output)
make install-hooks # Install git pre-commit hook
```

Run `make install-hooks` after cloning. Pre-commit hook runs `make check` before each commit.

## Git Workflow

Always wait for user confirmation before committing or pushing changes.

## Project Goal

Port the Soulseek.NET library (https://github.com/jpdillingham/Soulseek.NET) to Go. The reference .NET project is cloned in `Soulseek.NET/` (gitignored).

## Reference Project Structure

The Soulseek.NET project is located in `Soulseek.NET/` for inspection during porting. Use it to understand the **protocol**, not as an API design reference.

## Architecture Principles

### Idiomatic Go Over .NET Patterns

The .NET reference is for understanding the Soulseek **protocol**, not for copying API design. Always prefer idiomatic Go patterns:

| .NET Pattern | Go Alternative |
|-------------|----------------|
| Events/Callbacks | Channels |
| Async/Await | Goroutines + channels |
| Task<T> | `(<-chan T, error)` |
| CancellationToken | `context.Context` |
| IDisposable | `io.Closer` or deferred cleanup |

### Channels Over Callbacks

**IMPORTANT**: Use channels for asynchronous results, not callbacks.

Callbacks are NOT idiomatic Go. Always prefer channels:

```go
// BAD - callback pattern (DO NOT USE)
client.OnSearchResponse = func(resp *SearchResponse) {
    // handle result
}
client.Search("query")

// GOOD - channel pattern
results, err := client.Search(ctx, "query")
for resp := range results {
    // handle result - channel closes when ctx is done
}
```

Benefits of channels:
- Composable with `select` for timeouts and cancellation
- Context-aware lifecycle management
- Natural backpressure with buffered channels
- Idiomatic Go concurrency primitive

### Context for Cancellation and Timeouts

All blocking or long-running operations must accept `context.Context`:

```go
// Operations that may block should take context
func (c *Client) Search(ctx context.Context, query string) (<-chan *SearchResponse, error)
func (c *Client) Connect(ctx context.Context) error
func (c *Client) Login(ctx context.Context, user, pass string) error
```

### Error Handling

- Use `error` return values, not panics
- Wrap errors with context using `fmt.Errorf("operation: %w", err)`
- Return early on errors (no deep nesting)

### Disconnect/Event Notification

For events like disconnect, use channels that close when the event occurs:

```go
// Monitor for disconnects
select {
case <-client.Disconnected():
    err := client.DisconnectError()
    // handle disconnect
case result := <-results:
    // handle result
}
```

## Code Organization

```
gosoulseek/
├── client/           # High-level client API
│   ├── client.go     # Main client struct
│   ├── options.go    # Configuration
│   ├── search.go     # Search functionality
│   ├── peer.go       # Peer connection handling
│   └── router.go     # Message dispatching
├── connection/       # Low-level TCP connection
├── messages/
│   ├── server/       # Server message types
│   └── peer/         # Peer message types
├── protocol/         # Binary encoding/decoding
└── cmd/poc/          # Proof-of-concept CLI
```

## Testing

- Use `net.Pipe()` for connection tests (no real network)
- Table-driven tests for encoding/decoding
- Test files alongside source: `foo.go` -> `foo_test.go`
