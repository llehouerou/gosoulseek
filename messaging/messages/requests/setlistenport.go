package requests

import (
	"github.com/llehouerou/gosoulseek/messaging"
)

type SetListenPortRequest struct {
	port int
}

func NewSetListenPortRequest(port int) SetListenPortRequest {
	return SetListenPortRequest{
		port: port,
	}
}

func (r SetListenPortRequest) ToBytes() []byte {
	return messaging.NewMessageBuilder().
		Code(messaging.ServerSetListenPort).
		Integer(r.port).
		Build()
}
