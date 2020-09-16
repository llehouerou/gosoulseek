package messaging

import (
	"bytes"
	"encoding/binary"
)

type MessageBuilder struct {
	buffer bytes.Buffer
}

func NewMessageBuilder() *MessageBuilder {

	return &MessageBuilder{}
}

func (b *MessageBuilder) Build() []byte {
	var res bytes.Buffer
	res.Write(toByteArray32(uint32(b.buffer.Len())))
	res.Write(b.buffer.Bytes())
	return res.Bytes()
}

func (b *MessageBuilder) Code(code MessageCode) *MessageBuilder {
	b.buffer.Write(toByteArray32(uint32(code)))
	return b
}

func (b *MessageBuilder) Byte(value byte) *MessageBuilder {
	b.buffer.WriteByte(value)
	return b
}

func (b *MessageBuilder) Integer(value int) *MessageBuilder {
	b.buffer.Write(toByteArray32(uint32(value)))
	return b
}

func (b *MessageBuilder) Int64(value int64) *MessageBuilder {
	b.buffer.Write(toByteArray64(uint64(value)))
	return b
}

func (b *MessageBuilder) String(value string) *MessageBuilder {
	b.buffer.Write(toByteArray32(uint32(len(value))))
	b.buffer.WriteString(value)
	return b
}

func toByteArray32(i uint32) []byte {
	res := make([]byte, 4)
	binary.BigEndian.PutUint32(res, i)
	return res
}

func toByteArray64(i uint64) []byte {
	res := make([]byte, 8)
	binary.BigEndian.PutUint64(res, i)
	return res
}
