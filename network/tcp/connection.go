package tcp

import (
	"fmt"
	"io"
	"net"
	"sync"

	log "github.com/sirupsen/logrus"
)

type Connection struct {
	address              string
	port                 int
	server               net.Conn
	connectionState      ConnectionState
	connectionStateMutex *sync.RWMutex
}

type ConnectionState int

const (
	Disconnected  ConnectionState = 0
	Connected     ConnectionState = 1
	Connecting    ConnectionState = 2
	Disconnecting ConnectionState = 3
)

func NewConnection(address string, port int) *Connection {
	return &Connection{
		address:              address,
		port:                 port,
		server:               nil,
		connectionState:      Disconnected,
		connectionStateMutex: &sync.RWMutex{},
	}
}

func (c *Connection) Connect() error {
	c.setState(Connecting)
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.address, c.port))
	if err != nil {
		return fmt.Errorf("dialing server: %v", err)
	}
	c.server = conn
	c.setState(Connected)
	return nil
}

func (c *Connection) Disconnect() error {
	c.setState(Disconnecting)
	err := c.server.Close()
	c.server = nil
	c.setState(Disconnected)
	return err
}

func (c *Connection) Read(length int) ([]byte, error) {
	if c.GetState() != Connected {
		return nil, fmt.Errorf("not connected to server")
	}
	buffer := make([]byte, length)
	_, err := io.ReadFull(c.server, buffer)
	if err != nil {
		return nil, fmt.Errorf("reading %d bytes from server: %v", length, err)
	}
	return buffer, nil
}

func (c *Connection) Write(buffer []byte) error {
	if c.GetState() != Connected {
		return fmt.Errorf("not connected to server")
	}
	log.Debugf("-> %v", buffer)
	_, err := c.server.Write(buffer)
	return err
}

func (c *Connection) GetState() ConnectionState {
	c.connectionStateMutex.RLock()
	defer c.connectionStateMutex.RUnlock()
	return c.connectionState
}

func (c *Connection) setState(state ConnectionState) {
	c.connectionStateMutex.Lock()
	defer c.connectionStateMutex.Unlock()
	c.connectionState = state
}
