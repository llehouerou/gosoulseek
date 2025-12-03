package client

import (
	"testing"
)

func TestTransferState_Flags(t *testing.T) {
	tests := []struct {
		name     string
		state    TransferState
		expected string
	}{
		{"None", TransferStateNone, "None"},
		{"Requested", TransferStateRequested, "Requested"},
		{"Queued", TransferStateQueued, "Queued"},
		{"Initializing", TransferStateInitializing, "Initializing"},
		{"InProgress", TransferStateInProgress, "InProgress"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.String(); got != tt.expected {
				t.Errorf("TransferState.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestTransferState_CompoundStates(t *testing.T) {
	// Queued locally
	queuedLocally := TransferStateQueued | TransferStateLocally
	if got := queuedLocally.String(); got != "Queued, Locally" {
		t.Errorf("Queued|Locally.String() = %q, want %q", got, "Queued, Locally")
	}

	// Queued remotely
	queuedRemotely := TransferStateQueued | TransferStateRemotely
	if got := queuedRemotely.String(); got != "Queued, Remotely" {
		t.Errorf("Queued|Remotely.String() = %q, want %q", got, "Queued, Remotely")
	}

	// Completed successfully
	succeeded := TransferStateCompleted | TransferStateSucceeded
	if got := succeeded.String(); got != "Completed, Succeeded" {
		t.Errorf("Completed|Succeeded.String() = %q, want %q", got, "Completed, Succeeded")
	}

	// Completed with error
	errored := TransferStateCompleted | TransferStateErrored
	if got := errored.String(); got != "Completed, Errored" {
		t.Errorf("Completed|Errored.String() = %q, want %q", got, "Completed, Errored")
	}
}

func TestTransferState_IsCompleted(t *testing.T) {
	tests := []struct {
		name     string
		state    TransferState
		expected bool
	}{
		{"None", TransferStateNone, false},
		{"Requested", TransferStateRequested, false},
		{"Queued", TransferStateQueued, false},
		{"InProgress", TransferStateInProgress, false},
		{"Completed", TransferStateCompleted, true},
		{"Completed|Succeeded", TransferStateCompleted | TransferStateSucceeded, true},
		{"Completed|Cancelled", TransferStateCompleted | TransferStateCancelled, true},
		{"Completed|TimedOut", TransferStateCompleted | TransferStateTimedOut, true},
		{"Completed|Errored", TransferStateCompleted | TransferStateErrored, true},
		{"Completed|Rejected", TransferStateCompleted | TransferStateRejected, true},
		{"Completed|Aborted", TransferStateCompleted | TransferStateAborted, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.IsCompleted(); got != tt.expected {
				t.Errorf("TransferState.IsCompleted() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestTransferState_IsQueued(t *testing.T) {
	tests := []struct {
		name     string
		state    TransferState
		expected bool
	}{
		{"None", TransferStateNone, false},
		{"Requested", TransferStateRequested, false},
		{"Queued", TransferStateQueued, true},
		{"Queued|Locally", TransferStateQueued | TransferStateLocally, true},
		{"Queued|Remotely", TransferStateQueued | TransferStateRemotely, true},
		{"InProgress", TransferStateInProgress, false},
		{"Completed", TransferStateCompleted, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.IsQueued(); got != tt.expected {
				t.Errorf("TransferState.IsQueued() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestTransferState_IsActive(t *testing.T) {
	tests := []struct {
		name     string
		state    TransferState
		expected bool
	}{
		{"None", TransferStateNone, false},
		{"Requested", TransferStateRequested, true},
		{"Queued", TransferStateQueued, true},
		{"Queued|Locally", TransferStateQueued | TransferStateLocally, true},
		{"Queued|Remotely", TransferStateQueued | TransferStateRemotely, true},
		{"Initializing", TransferStateInitializing, true},
		{"InProgress", TransferStateInProgress, true},
		{"Completed", TransferStateCompleted, false},
		{"Completed|Succeeded", TransferStateCompleted | TransferStateSucceeded, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.IsActive(); got != tt.expected {
				t.Errorf("TransferState.IsActive() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestTransferState_IsLocal(t *testing.T) {
	tests := []struct {
		name     string
		state    TransferState
		expected bool
	}{
		{"None", TransferStateNone, false},
		{"Queued", TransferStateQueued, false},
		{"Queued|Locally", TransferStateQueued | TransferStateLocally, true},
		{"Queued|Remotely", TransferStateQueued | TransferStateRemotely, false},
		{"Locally alone", TransferStateLocally, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.IsLocal(); got != tt.expected {
				t.Errorf("TransferState.IsLocal() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestTransferState_IsRemote(t *testing.T) {
	tests := []struct {
		name     string
		state    TransferState
		expected bool
	}{
		{"None", TransferStateNone, false},
		{"Queued", TransferStateQueued, false},
		{"Queued|Locally", TransferStateQueued | TransferStateLocally, false},
		{"Queued|Remotely", TransferStateQueued | TransferStateRemotely, true},
		{"Remotely alone", TransferStateRemotely, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.IsRemote(); got != tt.expected {
				t.Errorf("TransferState.IsRemote() = %v, want %v", got, tt.expected)
			}
		})
	}
}
