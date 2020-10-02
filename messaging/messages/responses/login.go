package responses

import (
	"fmt"
	"net"

	"github.com/llehouerou/gosoulseek/messaging"
)

type LoginResponse struct {
	Succeeded bool
	Message   string
	IpAddress net.IP
}

func (r *LoginResponse) FromBytes(message []byte) (*LoginResponse, error) {
	reader := messaging.NewMessageReader(message)

	code, err := reader.Code()
	if err != nil {
		return nil, fmt.Errorf("reading response code: %v", err)
	}
	if code != messaging.ServerLogin {
		return nil, fmt.Errorf("code mismatch in response, expected %v, received %v", messaging.ServerLogin, code)
	}

	result, err := reader.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("reading response result: %v", err)
	}
	mes, err := reader.ReadString()
	if err != nil {
		return nil, fmt.Errorf("reading response message: %v", err)
	}
	ip := net.IP{}
	if result == 1 {
		ipBytes, err := reader.ReadBytes(4)
		if err != nil {
			return nil, fmt.Errorf("reading ip address: %v", err)
		}
		ip = net.IPv4(ipBytes[3], ipBytes[2], ipBytes[1], ipBytes[0])
	}

	r.Succeeded = result == 1
	r.Message = mes
	r.IpAddress = ip

	return r, nil
}
