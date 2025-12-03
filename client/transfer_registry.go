package client

import (
	"sync"

	"github.com/llehouerou/gosoulseek/messages/peer"
)

// TransferRegistry tracks all active file transfers using concurrent-safe maps.
// It supports lookup by token, by remote token, and by file key.
type TransferRegistry struct {
	// Primary index by token
	byToken sync.Map // uint32 -> *Transfer

	// Secondary index by remote token (set after token exchange)
	byRemoteToken sync.Map // "username:remoteToken" -> *Transfer

	// Index by file key for duplicate detection
	byFileKey sync.Map // "direction:username:filename" -> *Transfer

	mu sync.Mutex // protects registration/removal atomicity
}

// NewTransferRegistry creates a new transfer registry.
func NewTransferRegistry() *TransferRegistry {
	return &TransferRegistry{}
}

// RegisterDownload registers a new download transfer.
// Returns a DuplicateTransferError if a transfer for the same file already exists.
func (r *TransferRegistry) RegisterDownload(username, filename string, token uint32) (*Transfer, error) {
	return r.register(peer.TransferDownload, username, filename, token, 0)
}

// RegisterUpload registers a new upload transfer with a known size.
// Returns a DuplicateTransferError if a transfer for the same file already exists.
func (r *TransferRegistry) RegisterUpload(username, filename string, token uint32, size int64) (*Transfer, error) {
	return r.register(peer.TransferUpload, username, filename, token, size)
}

// register is the internal registration method.
func (r *TransferRegistry) register(direction peer.TransferDirection, username, filename string, token uint32, size int64) (*Transfer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	tr := NewTransfer(direction, username, filename, token)
	tr.Size = size

	fileKey := tr.FileKey()

	// Check for duplicate by file key
	if _, loaded := r.byFileKey.LoadOrStore(fileKey, tr); loaded {
		return nil, &DuplicateTransferError{
			Direction: direction,
			Username:  username,
			Filename:  filename,
		}
	}

	// Check for token collision (should never happen with proper token generation)
	if _, loaded := r.byToken.LoadOrStore(token, tr); loaded {
		// Rollback file key
		r.byFileKey.Delete(fileKey)
		return nil, &DuplicateTransferError{
			Direction: direction,
			Username:  username,
			Filename:  filename,
		}
	}

	return tr, nil
}

// GetByToken returns the transfer with the given token.
func (r *TransferRegistry) GetByToken(token uint32) (*Transfer, bool) {
	if v, ok := r.byToken.Load(token); ok {
		tr, valid := v.(*Transfer)
		return tr, valid
	}
	return nil, false
}

// GetByRemoteToken returns the transfer with the given username and remote token.
func (r *TransferRegistry) GetByRemoteToken(username string, remoteToken uint32) (*Transfer, bool) {
	key := username + ":" + itoa(remoteToken)
	if v, ok := r.byRemoteToken.Load(key); ok {
		tr, valid := v.(*Transfer)
		return tr, valid
	}
	return nil, false
}

// GetByFile returns the transfer for the given username, filename, and direction.
func (r *TransferRegistry) GetByFile(username, filename string, direction peer.TransferDirection) (*Transfer, bool) {
	key := fileKey(direction, username, filename)
	if v, ok := r.byFileKey.Load(key); ok {
		tr, valid := v.(*Transfer)
		return tr, valid
	}
	return nil, false
}

// SetRemoteToken sets the remote token for a transfer and indexes it.
func (r *TransferRegistry) SetRemoteToken(token, remoteToken uint32) error {
	tr, ok := r.GetByToken(token)
	if !ok {
		return &TransferNotFoundError{Token: token}
	}

	tr.mu.Lock()
	tr.RemoteToken = remoteToken
	tr.mu.Unlock()

	// Index by remote token
	key := tr.Username + ":" + itoa(remoteToken)
	r.byRemoteToken.Store(key, tr)

	return nil
}

// SetState updates the state of a transfer.
func (r *TransferRegistry) SetState(token uint32, state TransferState) error {
	tr, ok := r.GetByToken(token)
	if !ok {
		return &TransferNotFoundError{Token: token}
	}

	tr.SetState(state)
	return nil
}

// UpdateProgress updates the bytes transferred for a transfer.
func (r *TransferRegistry) UpdateProgress(token uint32, bytesTransferred int64) error {
	tr, ok := r.GetByToken(token)
	if !ok {
		return &TransferNotFoundError{Token: token}
	}

	tr.UpdateProgress(bytesTransferred)
	return nil
}

// SetSize sets the file size for a transfer.
func (r *TransferRegistry) SetSize(token uint32, size int64) error {
	tr, ok := r.GetByToken(token)
	if !ok {
		return &TransferNotFoundError{Token: token}
	}

	tr.mu.Lock()
	tr.Size = size
	tr.mu.Unlock()

	return nil
}

// Complete marks a transfer as completed with the given state and optional error.
func (r *TransferRegistry) Complete(token uint32, state TransferState, err error) error {
	tr, ok := r.GetByToken(token)
	if !ok {
		return &TransferNotFoundError{Token: token}
	}

	tr.mu.Lock()
	tr.Error = err
	tr.mu.Unlock()

	tr.SetState(state)
	return nil
}

// Remove removes a transfer from all indexes and closes its progress channel.
func (r *TransferRegistry) Remove(token uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()

	v, ok := r.byToken.LoadAndDelete(token)
	if !ok {
		return
	}

	tr, ok := v.(*Transfer)
	if !ok {
		return
	}

	// Remove from file key index
	r.byFileKey.Delete(tr.FileKey())

	// Remove from remote token index if set
	if tr.RemoteToken != 0 {
		key := tr.Username + ":" + itoa(tr.RemoteToken)
		r.byRemoteToken.Delete(key)
	}

	// Close the progress channel
	tr.Close()
}

// All returns all active transfers.
func (r *TransferRegistry) All() []*Transfer {
	var result []*Transfer
	r.byToken.Range(func(_, v any) bool {
		if tr, ok := v.(*Transfer); ok {
			result = append(result, tr)
		}
		return true
	})
	return result
}

// Downloads returns all active download transfers.
func (r *TransferRegistry) Downloads() []*Transfer {
	var result []*Transfer
	r.byToken.Range(func(_, v any) bool {
		if tr, ok := v.(*Transfer); ok && tr.Direction == peer.TransferDownload {
			result = append(result, tr)
		}
		return true
	})
	return result
}

// Uploads returns all active upload transfers.
func (r *TransferRegistry) Uploads() []*Transfer {
	var result []*Transfer
	r.byToken.Range(func(_, v any) bool {
		if tr, ok := v.(*Transfer); ok && tr.Direction == peer.TransferUpload {
			result = append(result, tr)
		}
		return true
	})
	return result
}

// fileKey generates a unique key for a file transfer.
func fileKey(direction peer.TransferDirection, username, filename string) string {
	return itoa(uint32(direction)) + ":" + username + ":" + filename
}

// itoa converts a uint32 to a string without importing strconv.
func itoa(n uint32) string {
	if n == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
