package requests

import (
	"github.com/llehouerou/gosoulseek/internal"
	"github.com/llehouerou/gosoulseek/messaging"
)

type LoginRequest struct {
	username     string
	password     string
	version      int
	minorversion int
	hash         string
}

func NewLoginRequest(username string, password string) LoginRequest {
	return LoginRequest{
		username:     username,
		password:     password,
		version:      157,
		minorversion: 17,
		hash:         internal.GetMD5Hash(username + password),
	}
}

func (r LoginRequest) ToBytes() []byte {
	return messaging.NewMessageBuilder().
		Code(messaging.ServerLogin).
		String(r.username).
		String(r.password).
		Integer(r.version).
		String(r.hash).
		Integer(r.minorversion).
		Build()
}
