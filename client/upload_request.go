package client

import (
	"bytes"
	"sync/atomic"

	"github.com/llehouerou/gosoulseek/connection"
	"github.com/llehouerou/gosoulseek/messages/peer"
	"github.com/llehouerou/gosoulseek/protocol"
)

// uploadDeniedFileNotShared is the standard message sent when a file is not shared.
const uploadDeniedFileNotShared = "File not shared"

// handleQueueDownload handles QueueDownload messages from peers.
// Called when a peer wants to queue a file download from us (i.e., we upload to them).
func (c *Client) handleQueueDownload(payload []byte, username string, conn *connection.Conn) {
	// 1. Decode message
	msg, err := peer.DecodeQueueDownload(payload)
	if err != nil {
		return
	}

	filename := msg.Filename

	// 2. Check if file exists in FileSharer
	var sharedFile *SharedFile
	if c.opts.FileSharer != nil {
		sharedFile = c.opts.FileSharer.GetFile(filename)
	}

	if sharedFile == nil {
		c.sendUploadDenied(conn, filename, uploadDeniedFileNotShared)
		return
	}

	// 3. Call UploadValidator if configured
	if c.opts.UploadValidator != nil {
		if err := c.opts.UploadValidator(username, filename); err != nil {
			c.sendUploadDenied(conn, filename, err.Error())
			return
		}
	}

	// 4. Check for existing transfer (duplicate detection)
	existingTr, exists := c.transfers.GetByFile(username, filename, peer.TransferUpload)
	if exists {
		// Re-request: respond with current queue position
		position := c.queueMgr.GetPosition(username, filename)
		if position == 0 {
			position = 1 // If not in queue, default to 1
		}
		c.sendPlaceInQueueResponse(conn, filename, position)
		return
	}
	_ = existingTr // Silence unused warning

	// 5. Register new upload in TransferRegistry
	token := atomic.AddUint32(&transferToken, 1)
	tr, err := c.transfers.RegisterUpload(username, filename, token, sharedFile.Size)
	if err != nil {
		// This shouldn't happen since we already checked for duplicates,
		// but handle it gracefully
		c.sendUploadDenied(conn, filename, "Internal error")
		return
	}

	// 6. Enqueue in QueueManager
	position := c.queueMgr.EnqueueUpload(username, filename, tr.Token)

	// 7. Send PlaceInQueueResponse
	c.sendPlaceInQueueResponse(conn, filename, position)
}

// sendUploadDenied sends an UploadDenied message to a peer.
func (c *Client) sendUploadDenied(conn *connection.Conn, filename, reason string) {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	msg := &peer.UploadDenied{
		Filename: filename,
		Reason:   reason,
	}
	msg.Encode(w)
	if err := w.Error(); err != nil {
		return
	}
	_ = conn.WriteMessage(buf.Bytes())
}

// sendPlaceInQueueResponse sends a PlaceInQueueResponse message to a peer.
func (c *Client) sendPlaceInQueueResponse(conn *connection.Conn, filename string, position uint32) {
	var buf bytes.Buffer
	w := protocol.NewWriter(&buf)
	msg := &peer.PlaceInQueueResponse{
		Filename: filename,
		Place:    position,
	}
	msg.Encode(w)
	if err := w.Error(); err != nil {
		return
	}
	_ = conn.WriteMessage(buf.Bytes())
}
