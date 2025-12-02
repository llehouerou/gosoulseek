package client_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/llehouerou/gosoulseek/client"
)

func TestMessageRouter_Register(t *testing.T) {
	r := client.NewMessageRouter()

	assert.False(t, r.HasHandler(1))

	r.Register(1, func(_ uint32, _ []byte) {})

	assert.True(t, r.HasHandler(1))
	assert.False(t, r.HasHandler(2))
}

func TestMessageRouter_Dispatch(t *testing.T) {
	r := client.NewMessageRouter()

	var called bool
	var receivedCode uint32
	var receivedPayload []byte

	r.Register(42, func(code uint32, payload []byte) {
		called = true
		receivedCode = code
		receivedPayload = payload
	})

	r.Dispatch(42, []byte{0x01, 0x02, 0x03})

	assert.True(t, called)
	assert.Equal(t, uint32(42), receivedCode)
	assert.Equal(t, []byte{0x01, 0x02, 0x03}, receivedPayload)
}

func TestMessageRouter_DispatchNoHandler(_ *testing.T) {
	r := client.NewMessageRouter()

	// Should not panic when no handler is registered
	r.Dispatch(99, []byte{0x01})
}

func TestMessageRouter_MultipleHandlers(t *testing.T) {
	r := client.NewMessageRouter()

	var calls []int

	r.Register(1, func(_ uint32, _ []byte) {
		calls = append(calls, 1)
	})
	r.Register(1, func(_ uint32, _ []byte) {
		calls = append(calls, 2)
	})
	r.Register(1, func(_ uint32, _ []byte) {
		calls = append(calls, 3)
	})

	r.Dispatch(1, nil)

	assert.Equal(t, []int{1, 2, 3}, calls)
}

func TestMessageRouter_Unregister(t *testing.T) {
	r := client.NewMessageRouter()

	var called bool
	id := r.Register(1, func(_ uint32, _ []byte) {
		called = true
	})

	r.Unregister(1, id)
	r.Dispatch(1, nil)

	assert.False(t, called)
	assert.False(t, r.HasHandler(1))
}

func TestMessageRouter_UnregisterSpecific(t *testing.T) {
	r := client.NewMessageRouter()

	var calls []int
	id1 := r.Register(1, func(_ uint32, _ []byte) {
		calls = append(calls, 1)
	})
	r.Register(1, func(_ uint32, _ []byte) {
		calls = append(calls, 2)
	})

	// Unregister only the first handler
	r.Unregister(1, id1)
	r.Dispatch(1, nil)

	// Only handler 2 should be called
	assert.Equal(t, []int{2}, calls)
}

func TestMessageRouter_UnregisterAll(t *testing.T) {
	r := client.NewMessageRouter()

	var called bool
	r.Register(1, func(_ uint32, _ []byte) {
		called = true
	})
	r.Register(1, func(_ uint32, _ []byte) {
		called = true
	})

	r.UnregisterAll(1)
	r.Dispatch(1, nil)

	assert.False(t, called)
	assert.False(t, r.HasHandler(1))
}

func TestMessageRouter_Concurrent(t *testing.T) {
	r := client.NewMessageRouter()

	var wg sync.WaitGroup
	var count int
	var mu sync.Mutex

	r.Register(1, func(_ uint32, _ []byte) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	// Dispatch concurrently
	for range 100 {
		wg.Go(func() {
			r.Dispatch(1, nil)
		})
	}

	wg.Wait()
	assert.Equal(t, 100, count)
}
