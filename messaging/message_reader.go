package messaging

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

type MessageReader struct {
	message []byte
	reader  *bytes.Reader
}

func NewMessageReader(message []byte) *MessageReader {

	return (&MessageReader{
		message: message,
		reader:  bytes.NewReader(message),
	}).Reset()
}

func (r *MessageReader) Reset() *MessageReader {
	_, _ = r.reader.Seek(8, io.SeekStart)
	return r
}

func (r *MessageReader) ReadByte() (byte, error) {
	return r.reader.ReadByte()
}

func (r *MessageReader) ReadBytes(length int) ([]byte, error) {
	chunk := make([]byte, length)
	_, err := io.ReadFull(r.reader, chunk)
	if err != nil {
		return nil, fmt.Errorf("reading %d bytes: %v", length, err)
	}
	return chunk, nil
}

func (r *MessageReader) ReadInteger() (int, error) {
	chunk := make([]byte, 4)
	_, err := io.ReadFull(r.reader, chunk)
	if err != nil {
		return 0, fmt.Errorf("reading integer: %v", err)
	}
	return int(binary.LittleEndian.Uint32(chunk)), nil
}

func (r *MessageReader) ReadInt64() (int64, error) {
	chunk := make([]byte, 8)
	_, err := io.ReadFull(r.reader, chunk)
	if err != nil {
		return 0, fmt.Errorf("reading int64: %v", err)
	}
	return int64(binary.LittleEndian.Uint64(chunk)), nil
}

func (r *MessageReader) ReadString() (string, error) {
	length, err := r.ReadInteger()
	if err != nil {
		return "", fmt.Errorf("reading string length: %v", err)
	}
	chunk := make([]byte, length)
	_, err = io.ReadFull(r.reader, chunk)
	if err != nil {
		return "", fmt.Errorf("reading string: %v", err)
	}
	return string(chunk), nil
}

func (r *MessageReader) Length() (int, error) {
	if len(r.message) < 4 {
		return 0, fmt.Errorf("length of message is less than 4 bytes")
	}
	chunk := r.message[0:4]
	return int(binary.LittleEndian.Uint32(chunk)), nil
}

func (r *MessageReader) Code() (MessageCode, error) {
	if len(r.message) < 8 {
		return 0, fmt.Errorf("length of message is less than 8 bytes")
	}
	chunk := r.message[4:8]
	return MessageCode(binary.LittleEndian.Uint32(chunk)), nil
}
