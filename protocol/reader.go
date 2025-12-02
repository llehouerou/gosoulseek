package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Reader decodes Soulseek protocol data from an io.Reader.
// All integers are little-endian. Strings are length-prefixed UTF-8.
//
// Reader uses the sticky error pattern: after an error occurs, all subsequent
// operations return zero values. Check Error() after a series of reads.
type Reader struct {
	r   io.Reader
	err error
}

// NewReader creates a Reader that reads from r.
func NewReader(r io.Reader) *Reader {
	return &Reader{r: r}
}

// Error returns the first error encountered during reads.
func (r *Reader) Error() error {
	return r.err
}

// ReadUint8 reads a single byte.
func (r *Reader) ReadUint8() uint8 {
	if r.err != nil {
		return 0
	}
	var v uint8
	r.err = binary.Read(r.r, binary.LittleEndian, &v)
	return v
}

// ReadUint32 reads a 32-bit unsigned integer in little-endian format.
func (r *Reader) ReadUint32() uint32 {
	if r.err != nil {
		return 0
	}
	var v uint32
	r.err = binary.Read(r.r, binary.LittleEndian, &v)
	return v
}

// ReadUint64 reads a 64-bit unsigned integer in little-endian format.
func (r *Reader) ReadUint64() uint64 {
	if r.err != nil {
		return 0
	}
	var v uint64
	r.err = binary.Read(r.r, binary.LittleEndian, &v)
	return v
}

// ReadString reads a length-prefixed UTF-8 string.
// Format: [4-byte length][UTF-8 bytes]
func (r *Reader) ReadString() string {
	if r.err != nil {
		return ""
	}
	length := r.ReadUint32()
	if r.err != nil {
		return ""
	}

	// Sanity check to prevent OOM on malformed data
	const maxStringLength = 10 * 1024 * 1024 // 10MB
	if length > maxStringLength {
		r.err = fmt.Errorf("string length %d exceeds maximum %d", length, maxStringLength)
		return ""
	}

	b := make([]byte, length)
	_, r.err = io.ReadFull(r.r, b)
	if r.err != nil {
		return ""
	}
	return string(b)
}

// ReadBytes reads exactly n bytes.
func (r *Reader) ReadBytes(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 {
		r.err = fmt.Errorf("invalid byte count: %d", n)
		return nil
	}
	b := make([]byte, n)
	_, r.err = io.ReadFull(r.r, b)
	if r.err != nil {
		return nil
	}
	return b
}
