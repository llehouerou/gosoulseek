package server

import (
	"github.com/llehouerou/gosoulseek/protocol"
)

// FileSearch sends a file search query to the server.
// The server distributes the query to the distributed network,
// and search results are returned via SearchResponse messages.
type FileSearch struct {
	Token uint32 // Unique identifier to match responses
	Query string // Search query
}

// Code returns the server message code.
func (m *FileSearch) Code() protocol.ServerCode {
	return protocol.ServerFileSearch
}

// Encode writes the FileSearch message.
func (m *FileSearch) Encode(w *protocol.Writer) {
	w.WriteUint32(uint32(protocol.ServerFileSearch))
	w.WriteUint32(m.Token)
	w.WriteString(m.Query)
}
