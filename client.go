package gosoulseek

import (
	"fmt"
	"net"

	"github.com/llehouerou/gosoulseek/internal"
	"github.com/llehouerou/gosoulseek/messaging"

	log "github.com/sirupsen/logrus"
)

type ServerState int

const (
	Disconnected  ServerState = 0
	Connected     ServerState = 1
	Connecting    ServerState = 2
	Disconnecting ServerState = 3
)

type Client struct {
	server      net.Conn
	serverState ServerState
}

func NewClient() *Client {
	return &Client{
		serverState: Disconnected,
	}
}

func (c *Client) Connect() error {

	c.serverState = Connecting
	conn, err := net.Dial("tcp", "server.slsknet.org:2242")
	if err != nil {
		return fmt.Errorf("dialing server: %v", err)

	}
	c.server = conn
	c.serverState = Connected
	return nil
}

func (c *Client) Login(username string, password string) error {
	log.Infof("logging in as %s", username)
	request := messaging.NewMessageBuilder().
		Code(messaging.Login).
		String(username).
		String(password).
		Integer(181).
		String(internal.GetMD5Hash(username + password)).
		Integer(1).Build()

	_, err := c.server.Write(request)
	if err != nil {
		return fmt.Errorf("sending request: %v", err)
	}
	buffer := make([]byte, 4096)
	_, err = c.server.Read(buffer)
	if err != nil {
		return fmt.Errorf("receiving response: %v", err)
	}
	reader := messaging.NewMessageReader(buffer)
	code, err := reader.Code()
	if err != nil {
		return fmt.Errorf("reading response code: %v", err)
	}
	if code != messaging.Login {
		return fmt.Errorf("code mismatch in response, expected %v, received %v", messaging.Login, code)
	}
	result, err := reader.ReadByte()
	if err != nil {
		return fmt.Errorf("reading response result: %v", err)
	}
	mes, err := reader.ReadString()
	if err != nil {
		return fmt.Errorf("reading response message: %v", err)
	}

	if result != 1 {
		return fmt.Errorf("login failed with message: %s", mes)
	}
	return nil
}
