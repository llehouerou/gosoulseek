package client

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestQueueManager_EnqueueUpload_ReturnsPosition(t *testing.T) {
	qm := NewQueueManager()

	// First enqueue should be position 1
	pos1 := qm.EnqueueUpload("user1", "file1.mp3", 100)
	if pos1 != 1 {
		t.Errorf("first enqueue: got position %d, want 1", pos1)
	}

	// Second enqueue should be position 2
	pos2 := qm.EnqueueUpload("user2", "file2.mp3", 101)
	if pos2 != 2 {
		t.Errorf("second enqueue: got position %d, want 2", pos2)
	}

	// Third enqueue should be position 3
	pos3 := qm.EnqueueUpload("user1", "file3.mp3", 102)
	if pos3 != 3 {
		t.Errorf("third enqueue: got position %d, want 3", pos3)
	}
}

func TestQueueManager_GetPosition_FIFO(t *testing.T) {
	qm := NewQueueManager()

	qm.EnqueueUpload("user1", "file1.mp3", 100)
	qm.EnqueueUpload("user2", "file2.mp3", 101)
	qm.EnqueueUpload("user3", "file3.mp3", 102)

	// Check positions
	if pos := qm.GetPosition("user1", "file1.mp3"); pos != 1 {
		t.Errorf("user1/file1: got %d, want 1", pos)
	}
	if pos := qm.GetPosition("user2", "file2.mp3"); pos != 2 {
		t.Errorf("user2/file2: got %d, want 2", pos)
	}
	if pos := qm.GetPosition("user3", "file3.mp3"); pos != 3 {
		t.Errorf("user3/file3: got %d, want 3", pos)
	}
}

func TestQueueManager_GetPosition_NotFound(t *testing.T) {
	qm := NewQueueManager()

	// Non-existent file should return 0
	pos := qm.GetPosition("unknown", "unknown.mp3")
	if pos != 0 {
		t.Errorf("unknown file: got %d, want 0", pos)
	}
}

func TestQueueManager_DequeueUpload_RecalculatesPositions(t *testing.T) {
	qm := NewQueueManager()

	qm.EnqueueUpload("user1", "file1.mp3", 100)
	qm.EnqueueUpload("user2", "file2.mp3", 101)
	qm.EnqueueUpload("user3", "file3.mp3", 102)

	// Remove the first item
	qm.DequeueUpload(100)

	// Positions should shift
	if pos := qm.GetPosition("user1", "file1.mp3"); pos != 0 {
		t.Errorf("removed file: got %d, want 0", pos)
	}
	if pos := qm.GetPosition("user2", "file2.mp3"); pos != 1 {
		t.Errorf("user2/file2 after dequeue: got %d, want 1", pos)
	}
	if pos := qm.GetPosition("user3", "file3.mp3"); pos != 2 {
		t.Errorf("user3/file3 after dequeue: got %d, want 2", pos)
	}
}

func TestQueueManager_DequeueUpload_Middle(t *testing.T) {
	qm := NewQueueManager()

	qm.EnqueueUpload("user1", "file1.mp3", 100)
	qm.EnqueueUpload("user2", "file2.mp3", 101)
	qm.EnqueueUpload("user3", "file3.mp3", 102)

	// Remove the middle item
	qm.DequeueUpload(101)

	// First stays at 1, third moves to 2
	if pos := qm.GetPosition("user1", "file1.mp3"); pos != 1 {
		t.Errorf("user1/file1: got %d, want 1", pos)
	}
	if pos := qm.GetPosition("user2", "file2.mp3"); pos != 0 {
		t.Errorf("removed file: got %d, want 0", pos)
	}
	if pos := qm.GetPosition("user3", "file3.mp3"); pos != 2 {
		t.Errorf("user3/file3 after dequeue: got %d, want 2", pos)
	}
}

func TestQueueManager_DequeueUpload_NonExistent(t *testing.T) {
	qm := NewQueueManager()

	qm.EnqueueUpload("user1", "file1.mp3", 100)

	// Dequeue non-existent should not panic
	qm.DequeueUpload(999)

	// Original should still be at position 1
	if pos := qm.GetPosition("user1", "file1.mp3"); pos != 1 {
		t.Errorf("user1/file1: got %d, want 1", pos)
	}
}

func TestQueueManager_CustomResolver(t *testing.T) {
	qm := NewQueueManager()

	// Custom resolver that gives priority to "vip" users
	qm.SetResolver(func(username, _ string) (uint32, error) {
		if username == "vip" {
			return 0, nil // Immediate transfer
		}
		return 999, nil // Back of queue
	})

	qm.EnqueueUpload("regular", "file1.mp3", 100)
	qm.EnqueueUpload("vip", "file2.mp3", 101)

	// VIP should get position 0 (immediate)
	pos, err := qm.ResolvePosition("vip", "file2.mp3")
	if err != nil {
		t.Fatalf("resolve vip: %v", err)
	}
	if pos != 0 {
		t.Errorf("vip position: got %d, want 0", pos)
	}

	// Regular user should get position 999
	pos, err = qm.ResolvePosition("regular", "file1.mp3")
	if err != nil {
		t.Fatalf("resolve regular: %v", err)
	}
	if pos != 999 {
		t.Errorf("regular position: got %d, want 999", pos)
	}
}

func TestQueueManager_ResolverError(t *testing.T) {
	qm := NewQueueManager()

	expectedErr := errors.New("file not shared")
	qm.SetResolver(func(_, _ string) (uint32, error) {
		return 0, expectedErr
	})

	_, err := qm.ResolvePosition("user", "file.mp3")
	if err == nil {
		t.Error("expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("got error %v, want %v", err, expectedErr)
	}
}

func TestQueueManager_ResolvePosition_DefaultFIFO(t *testing.T) {
	qm := NewQueueManager()

	qm.EnqueueUpload("user1", "file1.mp3", 100)
	qm.EnqueueUpload("user2", "file2.mp3", 101)

	// Without custom resolver, should use FIFO position
	pos, err := qm.ResolvePosition("user2", "file2.mp3")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if pos != 2 {
		t.Errorf("position: got %d, want 2", pos)
	}
}

func TestQueueManager_ResolvePosition_NotInQueue(t *testing.T) {
	qm := NewQueueManager()

	// File not in queue should still resolve (returns 0 = not queued)
	pos, err := qm.ResolvePosition("unknown", "unknown.mp3")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if pos != 0 {
		t.Errorf("unknown file position: got %d, want 0", pos)
	}
}

func TestQueueManager_ConcurrentAccess(t *testing.T) {
	qm := NewQueueManager()

	var wg sync.WaitGroup
	const goroutines = 10
	const opsPerGoroutine = 100

	// Concurrent enqueues
	for i := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range opsPerGoroutine {
				token := uint32(id*opsPerGoroutine + j)
				qm.EnqueueUpload("user", "file.mp3", token)
			}
		}(i)
	}
	wg.Wait()

	// Concurrent dequeues
	for i := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range opsPerGoroutine {
				token := uint32(id*opsPerGoroutine + j)
				qm.DequeueUpload(token)
			}
		}(i)
	}
	wg.Wait()

	// Queue should be empty
	if pos := qm.GetPosition("user", "file.mp3"); pos != 0 {
		t.Errorf("after all dequeues: got position %d, want 0", pos)
	}
}

func TestQueueManager_QueuedAt_Tracking(t *testing.T) {
	qm := NewQueueManager()

	before := time.Now()
	qm.EnqueueUpload("user", "file.mp3", 100)
	after := time.Now()

	entry := qm.GetEntry(100)
	if entry == nil {
		t.Fatal("entry not found")
	}

	if entry.QueuedAt.Before(before) || entry.QueuedAt.After(after) {
		t.Errorf("QueuedAt %v not between %v and %v", entry.QueuedAt, before, after)
	}
}

func TestQueueManager_GetEntry(t *testing.T) {
	qm := NewQueueManager()

	qm.EnqueueUpload("testuser", "testfile.mp3", 123)

	entry := qm.GetEntry(123)
	if entry == nil {
		t.Fatal("entry not found")
	}
	if entry.Username != "testuser" {
		t.Errorf("username: got %q, want %q", entry.Username, "testuser")
	}
	if entry.Filename != "testfile.mp3" {
		t.Errorf("filename: got %q, want %q", entry.Filename, "testfile.mp3")
	}
	if entry.Token != 123 {
		t.Errorf("token: got %d, want %d", entry.Token, 123)
	}
}

func TestQueueManager_GetEntry_NotFound(t *testing.T) {
	qm := NewQueueManager()

	entry := qm.GetEntry(999)
	if entry != nil {
		t.Error("expected nil for non-existent entry")
	}
}

func TestQueueManager_Len(t *testing.T) {
	qm := NewQueueManager()

	if qm.Len() != 0 {
		t.Errorf("empty queue: got %d, want 0", qm.Len())
	}

	qm.EnqueueUpload("user1", "file1.mp3", 100)
	if qm.Len() != 1 {
		t.Errorf("after 1 enqueue: got %d, want 1", qm.Len())
	}

	qm.EnqueueUpload("user2", "file2.mp3", 101)
	if qm.Len() != 2 {
		t.Errorf("after 2 enqueues: got %d, want 2", qm.Len())
	}

	qm.DequeueUpload(100)
	if qm.Len() != 1 {
		t.Errorf("after dequeue: got %d, want 1", qm.Len())
	}
}
