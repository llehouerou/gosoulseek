package client

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/llehouerou/gosoulseek/connection"
	"github.com/llehouerou/gosoulseek/messages/peer"
)

// TransferReadyInfo contains info from TransferRequest(Upload) message.
// This is sent by the peer when they're ready to upload a file we requested.
type TransferReadyInfo struct {
	RemoteToken uint32
	FileSize    int64
}

const (
	// progressUpdateInterval is the minimum time between EMA speed updates.
	progressUpdateInterval = time.Second

	// speedAlpha is the exponential moving average smoothing factor.
	// Using 2/(N+1) where N=9, matching Soulseek.NET.
	speedAlpha = 0.2
)

// Transfer represents an active file transfer (download or upload).
type Transfer struct {
	// Identity
	Direction   peer.TransferDirection
	Username    string
	Filename    string
	Token       uint32 // Our local token
	RemoteToken uint32 // Peer's token (set during exchange)

	// Size and progress
	Size        int64 // Total file size in bytes
	StartOffset int64 // Resume position in bytes
	Transferred int64 // Bytes transferred so far

	// Timing
	StartTime time.Time // When InProgress started
	EndTime   time.Time // When Completed started

	// Error info
	Error error

	// State machine
	state     TransferState
	prevState TransferState

	// Progress calculation (exponential moving average)
	avgSpeed          float64
	lastProgressTime  time.Time
	lastProgressBytes int64
	speedInitialized  bool

	// Progress channel for external consumers
	progressCh chan TransferProgress

	// Coordination channels for downloads (initialized via InitDownloadChannels)
	transferReadyCh chan TransferReadyInfo // TransferRequest(Upload) signal
	transferConnCh  chan *connection.Conn  // F-type connection delivery

	// Writer for download destination (set by DownloadV2)
	writer io.Writer

	mu sync.RWMutex
}

// TransferProgress represents a progress update for a transfer.
type TransferProgress struct {
	State            TransferState
	BytesTransferred int64
	FileSize         int64
	AverageSpeed     float64 // Bytes per second
	QueuePosition    uint32  // Position in remote queue (0 if not queued)
	Error            error   // Set when State includes Errored/Failed
}

// NewTransfer creates a new transfer with the given parameters.
func NewTransfer(direction peer.TransferDirection, username, filename string, token uint32) *Transfer {
	return &Transfer{
		Direction:  direction,
		Username:   username,
		Filename:   filename,
		Token:      token,
		progressCh: make(chan TransferProgress, 100),
	}
}

// State returns the current transfer state.
func (t *Transfer) State() TransferState {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.state
}

// PreviousState returns the previous transfer state before the last transition.
func (t *Transfer) PreviousState() TransferState {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.prevState
}

// SetState updates the transfer state and handles automatic time tracking.
// When entering InProgress, StartTime is set.
// When entering Completed, EndTime is set.
func (t *Transfer) SetState(state TransferState) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.prevState = t.state
	t.state = state

	// Auto-set StartTime when entering InProgress
	if state&TransferStateInProgress != 0 && t.StartTime.IsZero() {
		t.StartTime = time.Now()
	}

	// Auto-set EndTime when entering Completed
	if state&TransferStateCompleted != 0 && t.EndTime.IsZero() {
		t.EndTime = time.Now()
		// Finalize speed calculation
		t.calculateFinalSpeed()
	}
}

// calculateFinalSpeed computes the average speed based on total transfer time.
// Must be called with lock held.
func (t *Transfer) calculateFinalSpeed() {
	if t.StartTime.IsZero() || t.EndTime.IsZero() {
		return
	}
	duration := t.EndTime.Sub(t.StartTime).Seconds()
	if duration > 0 {
		t.avgSpeed = float64(t.Transferred-t.StartOffset) / duration
	}
}

// PercentComplete returns the transfer progress as a percentage (0-100).
func (t *Transfer) PercentComplete() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.Size == 0 {
		return 0
	}
	return float64(t.Transferred) / float64(t.Size) * 100
}

// BytesRemaining returns the number of bytes left to transfer.
func (t *Transfer) BytesRemaining() int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Size - t.Transferred
}

// AverageSpeed returns the current average transfer speed in bytes per second.
func (t *Transfer) AverageSpeed() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.avgSpeed
}

// setAverageSpeed sets the average speed (for testing).
func (t *Transfer) setAverageSpeed(speed float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.avgSpeed = speed
}

// RemainingTime returns the estimated time remaining based on current speed.
// Returns 0 if speed is zero or unknown.
func (t *Transfer) RemainingTime() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.avgSpeed <= 0 {
		return 0
	}
	remaining := t.Size - t.Transferred
	if remaining <= 0 {
		return 0
	}
	seconds := float64(remaining) / t.avgSpeed
	return time.Duration(seconds * float64(time.Second))
}

// UpdateProgress updates the bytes transferred and recalculates speed.
// Speed is calculated using exponential moving average, throttled to once per second.
func (t *Transfer) UpdateProgress(bytesTransferred int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.Transferred = bytesTransferred
	now := time.Now()

	// For completed transfers, use total time (already handled in SetState)
	if t.state.IsCompleted() {
		return
	}

	// Throttle EMA updates to once per progressUpdateInterval
	if !t.lastProgressTime.IsZero() && now.Sub(t.lastProgressTime) < progressUpdateInterval {
		return
	}

	// Calculate current speed
	if !t.lastProgressTime.IsZero() {
		elapsed := now.Sub(t.lastProgressTime).Seconds()
		if elapsed > 0 {
			currentSpeed := float64(bytesTransferred-t.lastProgressBytes) / elapsed

			if !t.speedInitialized {
				t.avgSpeed = currentSpeed
				t.speedInitialized = true
			} else {
				// Exponential moving average
				t.avgSpeed = speedAlpha*currentSpeed + (1-speedAlpha)*t.avgSpeed
			}
		}
	}

	t.lastProgressTime = now
	t.lastProgressBytes = bytesTransferred
}

// FileKey returns a unique key for this transfer based on direction, username, and filename.
// Used for duplicate detection.
func (t *Transfer) FileKey() string {
	return fmt.Sprintf("%d:%s:%s", t.Direction, t.Username, t.Filename)
}

// RemoteTokenKey returns a key for looking up this transfer by remote token.
func (t *Transfer) RemoteTokenKey() string {
	return fmt.Sprintf("%s:%d", t.Username, t.RemoteToken)
}

// Progress returns a channel that receives progress updates.
func (t *Transfer) Progress() <-chan TransferProgress {
	return t.progressCh
}

// emitProgress sends a progress update to the progress channel.
// Non-blocking; skips if channel is full.
func (t *Transfer) emitProgress() {
	t.mu.RLock()
	progress := TransferProgress{
		State:            t.state,
		BytesTransferred: t.Transferred,
		FileSize:         t.Size,
		AverageSpeed:     t.avgSpeed,
		Error:            t.Error,
	}
	t.mu.RUnlock()

	select {
	case t.progressCh <- progress:
	default:
		// Channel full, skip update
	}
}

// Close closes the progress channel.
func (t *Transfer) Close() {
	close(t.progressCh)
}

// InitDownloadChannels initializes the coordination channels for downloads.
// This must be called before using TransferReadyCh or TransferConnCh.
func (t *Transfer) InitDownloadChannels() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.transferReadyCh = make(chan TransferReadyInfo, 1)
	t.transferConnCh = make(chan *connection.Conn, 1)
}

// TransferReadyCh returns the channel that receives TransferRequest(Upload) signals.
// Returns nil if InitDownloadChannels hasn't been called.
func (t *Transfer) TransferReadyCh() chan TransferReadyInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.transferReadyCh
}

// TransferConnCh returns the channel that receives transfer connections.
// Returns nil if InitDownloadChannels hasn't been called.
func (t *Transfer) TransferConnCh() chan *connection.Conn {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.transferConnCh
}
