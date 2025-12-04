package client

import (
	"testing"
	"time"

	"github.com/llehouerou/gosoulseek/messages/peer"
)

func TestTransfer_NewDownload(t *testing.T) {
	tr := NewTransfer(peer.TransferDownload, "user1", "@@music/song.mp3", 12345)

	if tr.Direction != peer.TransferDownload {
		t.Errorf("Direction = %v, want %v", tr.Direction, peer.TransferDownload)
	}
	if tr.Username != "user1" {
		t.Errorf("Username = %q, want %q", tr.Username, "user1")
	}
	if tr.Filename != "@@music/song.mp3" {
		t.Errorf("Filename = %q, want %q", tr.Filename, "@@music/song.mp3")
	}
	if tr.Token != 12345 {
		t.Errorf("Token = %d, want %d", tr.Token, 12345)
	}
	if tr.State() != TransferStateNone {
		t.Errorf("State = %v, want %v", tr.State(), TransferStateNone)
	}
	if tr.Size != 0 {
		t.Errorf("Size = %d, want 0", tr.Size)
	}
}

func TestTransfer_NewUpload(t *testing.T) {
	tr := NewTransfer(peer.TransferUpload, "user2", "@@files/doc.pdf", 54321)

	if tr.Direction != peer.TransferUpload {
		t.Errorf("Direction = %v, want %v", tr.Direction, peer.TransferUpload)
	}
	if tr.Username != "user2" {
		t.Errorf("Username = %q, want %q", tr.Username, "user2")
	}
}

func TestTransfer_SetState_UpdatesStartTime(t *testing.T) {
	tr := NewTransfer(peer.TransferDownload, "user", "file", 1)

	// StartTime should be zero initially
	if !tr.StartTime.IsZero() {
		t.Error("StartTime should be zero initially")
	}

	// Set to InProgress
	before := time.Now()
	tr.SetState(TransferStateInProgress)
	after := time.Now()

	// StartTime should now be set
	if tr.StartTime.IsZero() {
		t.Error("StartTime should be set after InProgress")
	}
	if tr.StartTime.Before(before) || tr.StartTime.After(after) {
		t.Errorf("StartTime = %v, want between %v and %v", tr.StartTime, before, after)
	}
}

func TestTransfer_SetState_UpdatesEndTime(t *testing.T) {
	tr := NewTransfer(peer.TransferDownload, "user", "file", 1)

	// EndTime should be zero initially
	if !tr.EndTime.IsZero() {
		t.Error("EndTime should be zero initially")
	}

	// Set to Completed
	before := time.Now()
	tr.SetState(TransferStateCompleted | TransferStateSucceeded)
	after := time.Now()

	// EndTime should now be set
	if tr.EndTime.IsZero() {
		t.Error("EndTime should be set after Completed")
	}
	if tr.EndTime.Before(before) || tr.EndTime.After(after) {
		t.Errorf("EndTime = %v, want between %v and %v", tr.EndTime, before, after)
	}
}

func TestTransfer_SetState_PreservesPreviousState(t *testing.T) {
	tr := NewTransfer(peer.TransferDownload, "user", "file", 1)

	tr.SetState(TransferStateRequested)
	tr.SetState(TransferStateQueued | TransferStateRemotely)

	if tr.PreviousState() != TransferStateRequested {
		t.Errorf("PreviousState = %v, want %v", tr.PreviousState(), TransferStateRequested)
	}
}

func TestTransfer_PercentComplete(t *testing.T) {
	tests := []struct {
		name        string
		size        int64
		transferred int64
		want        float64
	}{
		{"empty file", 0, 0, 0},
		{"zero progress", 1000, 0, 0},
		{"half done", 1000, 500, 50},
		{"complete", 1000, 1000, 100},
		{"quarter done", 400, 100, 25},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := NewTransfer(peer.TransferDownload, "user", "file", 1)
			tr.Size = tt.size
			tr.Transferred = tt.transferred

			if got := tr.PercentComplete(); got != tt.want {
				t.Errorf("PercentComplete() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTransfer_BytesRemaining(t *testing.T) {
	tr := NewTransfer(peer.TransferDownload, "user", "file", 1)
	tr.Size = 1000
	tr.Transferred = 300

	if got := tr.BytesRemaining(); got != 700 {
		t.Errorf("BytesRemaining() = %d, want %d", got, 700)
	}
}

func TestTransfer_UpdateProgress_UpdatesTransferred(t *testing.T) {
	tr := NewTransfer(peer.TransferDownload, "user", "file", 1)
	tr.Size = 1000

	tr.UpdateProgress(500)

	if tr.Transferred != 500 {
		t.Errorf("Transferred = %d, want %d", tr.Transferred, 500)
	}
}

func TestTransfer_AverageSpeed_AfterCompletion(t *testing.T) {
	tr := NewTransfer(peer.TransferDownload, "user", "file", 1)
	tr.Size = 1000

	// Start the transfer - this sets StartTime
	tr.SetState(TransferStateInProgress)
	startTime := tr.StartTime

	// Simulate 2 seconds passing and full transfer
	tr.Transferred = 1000
	tr.mu.Lock()
	tr.StartTime = startTime.Add(-2 * time.Second) // Fake start was 2 seconds ago
	tr.mu.Unlock()

	// Complete it - this sets EndTime and calculates final speed
	tr.SetState(TransferStateCompleted | TransferStateSucceeded)

	// Speed should be approximately 500 bytes/sec
	speed := tr.AverageSpeed()
	if speed < 400 || speed > 600 {
		t.Errorf("AverageSpeed() = %v, want ~500", speed)
	}
}

func TestTransfer_RemainingTime(t *testing.T) {
	tr := NewTransfer(peer.TransferDownload, "user", "file", 1)
	tr.Size = 1000
	tr.Transferred = 500
	tr.setAverageSpeed(100) // 100 bytes/sec

	// Remaining: 500 bytes at 100 bytes/sec = 5 seconds
	remaining := tr.RemainingTime()
	if remaining != 5*time.Second {
		t.Errorf("RemainingTime() = %v, want %v", remaining, 5*time.Second)
	}
}

func TestTransfer_RemainingTime_ZeroSpeed(t *testing.T) {
	tr := NewTransfer(peer.TransferDownload, "user", "file", 1)
	tr.Size = 1000
	tr.Transferred = 500
	tr.setAverageSpeed(0)

	// With zero speed, should return 0
	if remaining := tr.RemainingTime(); remaining != 0 {
		t.Errorf("RemainingTime() = %v, want 0", remaining)
	}
}

func TestTransfer_FileKey(t *testing.T) {
	dlTr := NewTransfer(peer.TransferDownload, "user1", "file.mp3", 1)
	ulTr := NewTransfer(peer.TransferUpload, "user1", "file.mp3", 2)

	dlKey := dlTr.FileKey()
	ulKey := ulTr.FileKey()

	// Same file, different directions should have different keys
	if dlKey == ulKey {
		t.Errorf("FileKey should differ by direction: dl=%q, ul=%q", dlKey, ulKey)
	}

	// Same direction+user+file should have same key
	dlTr2 := NewTransfer(peer.TransferDownload, "user1", "file.mp3", 3)
	if dlTr.FileKey() != dlTr2.FileKey() {
		t.Errorf("FileKey should match: %q != %q", dlTr.FileKey(), dlTr2.FileKey())
	}
}

func TestTransfer_RemoteTokenKey(t *testing.T) {
	tr := NewTransfer(peer.TransferDownload, "user1", "file", 1)
	tr.RemoteToken = 9999

	key := tr.RemoteTokenKey()
	expected := "user1:9999"

	if key != expected {
		t.Errorf("RemoteTokenKey() = %q, want %q", key, expected)
	}
}

func TestTransfer_Progress_Channel(t *testing.T) {
	tr := NewTransfer(peer.TransferDownload, "user", "file", 1)
	tr.Size = 1000

	progressCh := tr.Progress()
	if progressCh == nil {
		t.Fatal("Progress() should return non-nil channel")
	}

	// Send an update
	tr.UpdateProgress(500)
	tr.emitProgress()

	// Should be able to receive
	select {
	case p := <-progressCh:
		if p.BytesTransferred != 500 {
			t.Errorf("BytesTransferred = %d, want 500", p.BytesTransferred)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected to receive progress update")
	}
}

func TestTransfer_InitDownloadChannels(t *testing.T) {
	tr := NewTransfer(peer.TransferDownload, "user", "file", 1)

	// Initially channels should be nil
	if tr.TransferReadyCh() != nil {
		t.Error("TransferReadyCh should be nil before InitDownloadChannels")
	}
	if tr.TransferConnCh() != nil {
		t.Error("TransferConnCh should be nil before InitDownloadChannels")
	}

	// Initialize channels
	tr.InitDownloadChannels()

	// Now channels should be non-nil
	if tr.TransferReadyCh() == nil {
		t.Error("TransferReadyCh should be non-nil after InitDownloadChannels")
	}
	if tr.TransferConnCh() == nil {
		t.Error("TransferConnCh should be non-nil after InitDownloadChannels")
	}
}

func TestTransfer_TransferReadyCh_Signaling(t *testing.T) {
	tr := NewTransfer(peer.TransferDownload, "user", "file", 1)
	tr.InitDownloadChannels()

	info := TransferReadyInfo{RemoteToken: 12345, FileSize: 1000}

	// Send signal
	select {
	case tr.TransferReadyCh() <- info:
		// sent
	default:
		t.Fatal("TransferReadyCh should accept signal")
	}

	// Receive signal
	select {
	case received := <-tr.TransferReadyCh():
		if received.RemoteToken != 12345 {
			t.Errorf("RemoteToken = %d, want 12345", received.RemoteToken)
		}
		if received.FileSize != 1000 {
			t.Errorf("FileSize = %d, want 1000", received.FileSize)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected to receive TransferReadyInfo")
	}
}

func TestTransfer_SetQueuePosition(t *testing.T) {
	tr := NewTransfer(peer.TransferDownload, "user", "file", 1)

	// Initially should be 0
	if tr.QueuePosition() != 0 {
		t.Errorf("initial QueuePosition = %d, want 0", tr.QueuePosition())
	}

	// Set position
	tr.SetQueuePosition(5)

	if tr.QueuePosition() != 5 {
		t.Errorf("QueuePosition = %d, want 5", tr.QueuePosition())
	}
}

func TestTransfer_SetQueuePosition_EmitsProgress(t *testing.T) {
	tr := NewTransfer(peer.TransferDownload, "user", "file", 1)
	tr.Size = 1000

	progressCh := tr.Progress()

	// Set queue position - should emit progress
	tr.SetQueuePosition(3)

	// Should receive progress with queue position
	select {
	case p := <-progressCh:
		if p.QueuePosition != 3 {
			t.Errorf("progress QueuePosition = %d, want 3", p.QueuePosition)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected progress update after SetQueuePosition")
	}
}

func TestTransfer_Progress_IncludesQueuePosition(t *testing.T) {
	tr := NewTransfer(peer.TransferDownload, "user", "file", 1)
	tr.Size = 1000
	tr.SetQueuePosition(7)

	// Emit progress
	tr.emitProgress()

	progressCh := tr.Progress()
	select {
	case p := <-progressCh:
		if p.QueuePosition != 7 {
			t.Errorf("QueuePosition in progress = %d, want 7", p.QueuePosition)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected progress update")
	}
}
