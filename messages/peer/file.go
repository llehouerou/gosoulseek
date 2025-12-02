// Package peer provides encoding and decoding for Soulseek peer messages.
package peer

import (
	"github.com/llehouerou/gosoulseek/protocol"
)

// AttributeType identifies a file attribute type.
type AttributeType uint32

const (
	// AttributeBitRate is the audio bit rate in kbps.
	AttributeBitRate AttributeType = 0
	// AttributeLength is the audio duration in seconds.
	AttributeLength AttributeType = 1
	// AttributeBitDepth is the audio bit depth (e.g., 16, 24).
	AttributeBitDepth AttributeType = 2
	// AttributeSampleRate is the audio sample rate in Hz.
	AttributeSampleRate AttributeType = 4
	// AttributeVBR indicates variable bit rate (1 = VBR, 0 = CBR).
	AttributeVBR AttributeType = 5
)

// FileAttribute represents a single file attribute.
type FileAttribute struct {
	Type  AttributeType
	Value uint32
}

// File represents a file in search results or browse responses.
type File struct {
	Code       uint8  // Internal code (usually 1)
	Filename   string // Full path within user's share
	Size       int64  // File size in bytes
	Extension  string // File extension (e.g., "mp3")
	Attributes []FileAttribute

	// Parsed attributes (populated from Attributes for convenience)
	BitRate    uint32 // kbps, 0 if not available
	Duration   uint32 // seconds, 0 if not available
	BitDepth   uint32 // 0 if not available
	SampleRate uint32 // Hz, 0 if not available
	IsVBR      bool   // true if variable bit rate
}

// DecodeFile reads a file from the protocol reader.
func DecodeFile(r *protocol.Reader) File {
	f := File{
		Code:      r.ReadUint8(),
		Filename:  r.ReadString(),
		Size:      int64(r.ReadUint64()), //nolint:gosec // file sizes are always positive
		Extension: r.ReadString(),
	}

	attrCount := r.ReadUint32()
	f.Attributes = make([]FileAttribute, 0, attrCount)

	for range attrCount {
		attr := FileAttribute{
			Type:  AttributeType(r.ReadUint32()),
			Value: r.ReadUint32(),
		}
		f.Attributes = append(f.Attributes, attr)

		// Populate convenience fields
		switch attr.Type {
		case AttributeBitRate:
			f.BitRate = attr.Value
		case AttributeLength:
			f.Duration = attr.Value
		case AttributeBitDepth:
			f.BitDepth = attr.Value
		case AttributeSampleRate:
			f.SampleRate = attr.Value
		case AttributeVBR:
			f.IsVBR = attr.Value == 1
		}
	}

	return f
}

// EncodeFile writes a file to the protocol writer.
func EncodeFile(w *protocol.Writer, f *File) {
	w.WriteUint8(f.Code)
	w.WriteString(f.Filename)
	w.WriteUint64(uint64(f.Size)) //nolint:gosec // file sizes are always positive
	w.WriteString(f.Extension)

	w.WriteUint32(uint32(len(f.Attributes))) //nolint:gosec // attribute count is small
	for _, attr := range f.Attributes {
		w.WriteUint32(uint32(attr.Type))
		w.WriteUint32(attr.Value)
	}
}
