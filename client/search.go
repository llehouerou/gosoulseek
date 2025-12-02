package client

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/llehouerou/gosoulseek/messages/peer"
	"github.com/llehouerou/gosoulseek/messages/server"
	"github.com/llehouerou/gosoulseek/protocol"
)

// searchToken is an atomic counter for generating unique search tokens.
var searchToken uint32

// activeSearch tracks an ongoing search with its result channel.
type activeSearch struct {
	ch     chan *peer.SearchResponse
	cancel context.CancelFunc
}

// searchRegistry manages active searches.
type searchRegistry struct {
	mu       sync.RWMutex
	searches map[uint32]*activeSearch
}

func newSearchRegistry() *searchRegistry {
	return &searchRegistry{
		searches: make(map[uint32]*activeSearch),
	}
}

func (r *searchRegistry) add(token uint32, search *activeSearch) {
	r.mu.Lock()
	r.searches[token] = search
	r.mu.Unlock()
}

func (r *searchRegistry) get(token uint32) (*activeSearch, bool) {
	r.mu.RLock()
	search, ok := r.searches[token]
	r.mu.RUnlock()
	return search, ok
}

func (r *searchRegistry) remove(token uint32) {
	r.mu.Lock()
	delete(r.searches, token)
	r.mu.Unlock()
}

// deliver sends a search response to the appropriate channel if the search is active.
// Returns true if the response was delivered.
func (r *searchRegistry) deliver(resp *peer.SearchResponse) bool {
	search, ok := r.get(resp.Token)
	if !ok {
		return false
	}

	// Non-blocking send - drop if channel is full
	select {
	case search.ch <- resp:
		return true
	default:
		return false
	}
}

// closeAll closes all active search channels.
func (r *searchRegistry) closeAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for token, search := range r.searches {
		search.cancel()
		close(search.ch)
		delete(r.searches, token)
	}
}

// Search sends a file search query and returns a channel of results.
// The channel is closed when the context is cancelled or times out.
// Results are buffered (up to 1000) to avoid blocking peer connections.
//
// Example usage:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//
//	results, err := client.Search(ctx, "artist album")
//	if err != nil {
//	    return err
//	}
//
//	for resp := range results {
//	    fmt.Printf("%s: %d files\n", resp.Username, len(resp.Files))
//	}
func (c *Client) Search(ctx context.Context, query string) (<-chan *peer.SearchResponse, error) {
	c.mu.Lock()
	if !c.loggedIn {
		c.mu.Unlock()
		return nil, errors.New("not logged in")
	}
	c.mu.Unlock()

	token := atomic.AddUint32(&searchToken, 1)

	req := &server.FileSearch{
		Token: token,
		Query: query,
	}

	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	req.Encode(w)
	if err := w.Error(); err != nil {
		return nil, err
	}

	// Create buffered channel for results
	ch := make(chan *peer.SearchResponse, 1000)

	// Create cancellable context for this search
	searchCtx, cancel := context.WithCancel(ctx)

	// Register the search
	c.searches.add(token, &activeSearch{
		ch:     ch,
		cancel: cancel,
	})

	// Send the search request
	if err := c.conn.WriteMessage(buf.Bytes()); err != nil {
		c.searches.remove(token)
		cancel()
		close(ch)
		return nil, err
	}

	// Goroutine to clean up when context is done
	go func() {
		<-searchCtx.Done()
		c.searches.remove(token)
		close(ch)
	}()

	return ch, nil
}
