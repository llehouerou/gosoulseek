package responses

import (
	"fmt"

	"github.com/llehouerou/gosoulseek/messaging"
)

type PrivilegedUsersResponse struct {
	PrivilegedUsers []string
}

func PrivilegedUsersResponseFromBytes(message []byte) (PrivilegedUsersResponse, error) {
	reader := messaging.NewMessageReader(message)

	code, err := reader.Code()
	if err != nil {
		return PrivilegedUsersResponse{}, fmt.Errorf("reading response code: %v", err)
	}
	if code != messaging.ServerPrivilegedUsers {
		return PrivilegedUsersResponse{}, fmt.Errorf("code mismatch in response, expected %v, received %v", messaging.ServerPrivilegedUsers, code)
	}

	count, err := reader.ReadInteger()
	if err != nil {
		return PrivilegedUsersResponse{}, fmt.Errorf("reading privileged users count: %v", err)
	}
	var res PrivilegedUsersResponse
	for i := 0; i < count; i++ {
		user, err := reader.ReadString()
		if err != nil {
			return PrivilegedUsersResponse{}, fmt.Errorf("reading privileged user #%d: %v", i, err)
		}
		res.PrivilegedUsers = append(res.PrivilegedUsers, user)
	}
	return res, nil
}
