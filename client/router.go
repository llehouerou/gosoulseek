package client

import (
	"sync"
	"sync/atomic"
)

// HandlerID uniquely identifies a registered handler.
type HandlerID uint64

// handlerIDCounter generates unique handler IDs.
var handlerIDCounter uint64

// Handler is called when a message with the registered code is received.
// The payload includes the message code prefix.
type Handler func(code uint32, payload []byte)

// handlerEntry pairs a handler with its ID for management.
type handlerEntry struct {
	id      HandlerID
	handler Handler
}

// MessageRouter dispatches incoming messages to registered handlers.
type MessageRouter struct {
	handlers map[uint32][]handlerEntry
	mu       sync.RWMutex
}

// NewMessageRouter creates a new message router.
func NewMessageRouter() *MessageRouter {
	return &MessageRouter{
		handlers: make(map[uint32][]handlerEntry),
	}
}

// Register adds a handler for the given message code.
// Multiple handlers can be registered for the same code.
// Returns a HandlerID that can be used to unregister this specific handler.
func (r *MessageRouter) Register(code uint32, h Handler) HandlerID {
	id := HandlerID(atomic.AddUint64(&handlerIDCounter, 1))

	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[code] = append(r.handlers[code], handlerEntry{id: id, handler: h})
	return id
}

// Unregister removes a specific handler by its ID.
// Returns true if the handler was found and removed.
func (r *MessageRouter) Unregister(code uint32, id HandlerID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries := r.handlers[code]
	for i, entry := range entries {
		if entry.id == id {
			// Remove by replacing with last element and truncating
			r.handlers[code] = append(entries[:i], entries[i+1:]...)
			return true
		}
	}
	return false
}

// UnregisterAll removes all handlers for the given message code.
func (r *MessageRouter) UnregisterAll(code uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.handlers, code)
}

// Dispatch calls all registered handlers for the given message code.
// Handlers are called synchronously in registration order.
func (r *MessageRouter) Dispatch(code uint32, payload []byte) {
	r.mu.RLock()
	entries := r.handlers[code]
	r.mu.RUnlock()

	for _, entry := range entries {
		entry.handler(code, payload)
	}
}

// HasHandler returns true if at least one handler is registered for the code.
func (r *MessageRouter) HasHandler(code uint32) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.handlers[code]) > 0
}
