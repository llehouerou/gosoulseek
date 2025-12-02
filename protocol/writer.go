// Package protocol provides binary encoding/decoding for the Soulseek protocol.
package protocol

import (
	"encoding/binary"
	"io"
)

// Writer encodes Soulseek protocol data to an io.Writer.
// All integers are little-endian. Strings are length-prefixed UTF-8.
//
// Writer uses the sticky error pattern: after an error occurs, all subsequent
// operations become no-ops. Check Error() after a series of writes.
type Writer struct {
	w   io.Writer
	err error
}

// NewWriter creates a Writer that writes to w.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// Error returns the first error encountered during writes.
func (w *Writer) Error() error {
	return w.err
}

// WriteUint8 writes a single byte.
func (w *Writer) WriteUint8(v uint8) {
	if w.err != nil {
		return
	}
	w.err = binary.Write(w.w, binary.LittleEndian, v)
}

// WriteUint32 writes a 32-bit unsigned integer in little-endian format.
func (w *Writer) WriteUint32(v uint32) {
	if w.err != nil {
		return
	}
	w.err = binary.Write(w.w, binary.LittleEndian, v)
}

// WriteUint64 writes a 64-bit unsigned integer in little-endian format.
func (w *Writer) WriteUint64(v uint64) {
	if w.err != nil {
		return
	}
	w.err = binary.Write(w.w, binary.LittleEndian, v)
}

// WriteString writes a length-prefixed UTF-8 string.
// Format: [4-byte length][UTF-8 bytes]
func (w *Writer) WriteString(s string) {
	if w.err != nil {
		return
	}
	b := []byte(s)
	//nolint:gosec // String lengths in protocol are always < 4GB
	w.WriteUint32(uint32(len(b)))
	if w.err != nil {
		return
	}
	_, w.err = w.w.Write(b)
}

// WriteBytes writes raw bytes without a length prefix.
func (w *Writer) WriteBytes(b []byte) {
	if w.err != nil {
		return
	}
	_, w.err = w.w.Write(b)
}
