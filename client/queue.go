package client

import (
	"sync"
	"time"
)

// QueueResolver returns the queue position for an upload request.
// Returns the 1-indexed position, or 0 if the transfer should start immediately.
// Returning an error causes the upload to be denied.
type QueueResolver func(username, filename string) (position uint32, err error)

// QueueEntry represents a file queued for upload.
type QueueEntry struct {
	Username string
	Filename string
	Token    uint32
	QueuedAt time.Time
}

// QueueManager tracks queued uploads and provides position resolution.
// It implements a FIFO queue by default, but supports custom resolution logic.
type QueueManager struct {
	resolver QueueResolver
	queue    []*QueueEntry
	mu       sync.RWMutex
}

// NewQueueManager creates a queue manager with default FIFO resolution.
func NewQueueManager() *QueueManager {
	return &QueueManager{}
}

// SetResolver configures a custom queue position resolver.
// If set to nil, the default FIFO resolver is used.
func (q *QueueManager) SetResolver(r QueueResolver) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.resolver = r
}

// EnqueueUpload adds an upload to the queue and returns its 1-indexed position.
func (q *QueueManager) EnqueueUpload(username, filename string, token uint32) uint32 {
	q.mu.Lock()
	defer q.mu.Unlock()

	entry := &QueueEntry{
		Username: username,
		Filename: filename,
		Token:    token,
		QueuedAt: time.Now(),
	}
	q.queue = append(q.queue, entry)

	return uint32(len(q.queue))
}

// DequeueUpload removes a completed/cancelled upload from the queue by token.
func (q *QueueManager) DequeueUpload(token uint32) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i, entry := range q.queue {
		if entry.Token == token {
			q.queue = append(q.queue[:i], q.queue[i+1:]...)
			return
		}
	}
}

// GetPosition returns the current 1-indexed queue position for an upload.
// Returns 0 if the file is not in the queue.
func (q *QueueManager) GetPosition(username, filename string) uint32 {
	q.mu.RLock()
	defer q.mu.RUnlock()

	for i, entry := range q.queue {
		if entry.Username == username && entry.Filename == filename {
			return uint32(i + 1)
		}
	}
	return 0
}

// ResolvePosition calls the custom resolver or uses default FIFO logic.
// Returns the queue position for the given username and filename.
func (q *QueueManager) ResolvePosition(username, filename string) (uint32, error) {
	q.mu.RLock()
	resolver := q.resolver
	q.mu.RUnlock()

	if resolver != nil {
		return resolver(username, filename)
	}

	// Default FIFO resolution
	return q.GetPosition(username, filename), nil
}

// GetEntry returns the queue entry for the given token, or nil if not found.
func (q *QueueManager) GetEntry(token uint32) *QueueEntry {
	q.mu.RLock()
	defer q.mu.RUnlock()

	for _, entry := range q.queue {
		if entry.Token == token {
			return entry
		}
	}
	return nil
}

// Len returns the number of entries in the queue.
func (q *QueueManager) Len() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.queue)
}
