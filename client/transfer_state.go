package client

import "strings"

// TransferState represents the current state of a file transfer.
// States are implemented as bit flags to allow compound states
// like "Queued | Remotely" or "Completed | Succeeded".
type TransferState uint32

const (
	// TransferStateNone is the initial state before any action.
	TransferStateNone TransferState = 0

	// Primary lifecycle states (mutually exclusive)
	TransferStateRequested    TransferState = 1 << 0 // Transfer request sent
	TransferStateQueued       TransferState = 1 << 1 // Waiting in queue
	TransferStateInitializing TransferState = 1 << 2 // Setting up connection
	TransferStateInProgress   TransferState = 1 << 3 // Actively transferring
	TransferStateCompleted    TransferState = 1 << 4 // Terminal state flag

	// Completion reasons (combined with Completed)
	TransferStateSucceeded TransferState = 1 << 5  // Completed successfully
	TransferStateCancelled TransferState = 1 << 6  // User cancelled
	TransferStateTimedOut  TransferState = 1 << 7  // Operation timed out
	TransferStateErrored   TransferState = 1 << 8  // General error occurred
	TransferStateRejected  TransferState = 1 << 9  // Peer rejected the transfer
	TransferStateAborted   TransferState = 1 << 10 // Size mismatch or unexpected termination

	// Queue location modifiers (combined with Queued)
	TransferStateLocally  TransferState = 1 << 11 // Queued by local constraints
	TransferStateRemotely TransferState = 1 << 12 // Queued by remote peer
)

// IsCompleted returns true if the transfer has reached a terminal state.
func (s TransferState) IsCompleted() bool {
	return s&TransferStateCompleted != 0
}

// IsQueued returns true if the transfer is waiting in a queue.
func (s TransferState) IsQueued() bool {
	return s&TransferStateQueued != 0
}

// IsActive returns true if the transfer is in progress or pending.
// Returns false for completed transfers.
func (s TransferState) IsActive() bool {
	if s.IsCompleted() {
		return false
	}
	return s&(TransferStateRequested|TransferStateQueued|TransferStateInitializing|TransferStateInProgress) != 0
}

// IsLocal returns true if the Locally flag is set.
func (s TransferState) IsLocal() bool {
	return s&TransferStateLocally != 0
}

// IsRemote returns true if the Remotely flag is set.
func (s TransferState) IsRemote() bool {
	return s&TransferStateRemotely != 0
}

// String returns a human-readable representation of the state.
// For compound states, flags are joined with ", ".
func (s TransferState) String() string {
	if s == TransferStateNone {
		return "None"
	}

	var parts []string

	// Primary states
	if s&TransferStateRequested != 0 {
		parts = append(parts, "Requested")
	}
	if s&TransferStateQueued != 0 {
		parts = append(parts, "Queued")
	}
	if s&TransferStateInitializing != 0 {
		parts = append(parts, "Initializing")
	}
	if s&TransferStateInProgress != 0 {
		parts = append(parts, "InProgress")
	}
	if s&TransferStateCompleted != 0 {
		parts = append(parts, "Completed")
	}

	// Completion reasons
	if s&TransferStateSucceeded != 0 {
		parts = append(parts, "Succeeded")
	}
	if s&TransferStateCancelled != 0 {
		parts = append(parts, "Cancelled")
	}
	if s&TransferStateTimedOut != 0 {
		parts = append(parts, "TimedOut")
	}
	if s&TransferStateErrored != 0 {
		parts = append(parts, "Errored")
	}
	if s&TransferStateRejected != 0 {
		parts = append(parts, "Rejected")
	}
	if s&TransferStateAborted != 0 {
		parts = append(parts, "Aborted")
	}

	// Queue location modifiers
	if s&TransferStateLocally != 0 {
		parts = append(parts, "Locally")
	}
	if s&TransferStateRemotely != 0 {
		parts = append(parts, "Remotely")
	}

	return strings.Join(parts, ", ")
}
