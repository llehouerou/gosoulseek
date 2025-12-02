package server

import (
	"github.com/llehouerou/gosoulseek/protocol"
)

// SharedFoldersAndFiles reports the number of shared directories and files to the server.
type SharedFoldersAndFiles struct {
	Directories uint32
	Files       uint32
}

// Code returns the server message code.
func (m *SharedFoldersAndFiles) Code() protocol.ServerCode {
	return protocol.ServerSharedFoldersAndFiles
}

// Encode writes the SharedFoldersAndFiles message.
func (m *SharedFoldersAndFiles) Encode(w *protocol.Writer) {
	w.WriteUint32(uint32(protocol.ServerSharedFoldersAndFiles))
	w.WriteUint32(m.Directories)
	w.WriteUint32(m.Files)
}
