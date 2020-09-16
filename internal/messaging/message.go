package messaging

import "bytes"

type MessageSegment interface {
	ToBytes() []bytes.Buffer
}

type Message struct {
	Length  int
	Code    MessageCode
	segment []MessageSegment
}

func NewMessage(code MessageCode, segments ...MessageSegment) Message {
	return Message{
		Code:    code,
		segment: segments,
	}
}
