package messages

type OutputMessage interface {
	ToBytes() []byte
}
