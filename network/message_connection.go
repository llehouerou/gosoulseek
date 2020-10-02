package network

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/llehouerou/gosoulseek/messaging/messages"

	"github.com/llehouerou/gosoulseek/messaging"

	log "github.com/sirupsen/logrus"

	"github.com/llehouerou/gosoulseek/network/tcp"
)

type MessageConnection struct {
	connection              *tcp.Connection
	Username                string
	messagesubscribers      map[chan []byte]messaging.MessageCode
	messagesubscribersmutex *sync.RWMutex
}

func NewMessageConnection(username string, address string, port int) *MessageConnection {
	return &MessageConnection{
		connection:              tcp.NewConnection(address, port),
		Username:                username,
		messagesubscribers:      make(map[chan []byte]messaging.MessageCode),
		messagesubscribersmutex: &sync.RWMutex{},
	}
}

func (c *MessageConnection) Connect(ctx context.Context) error {

	err := c.connection.Connect()
	if err != nil {
		return err
	}
	c.dispatchMessage(c.readContinuously(ctx))
	return nil
}

func (c *MessageConnection) Send(message messages.OutputMessage) error {
	return c.connection.Write(message.ToBytes())
}

func (c *MessageConnection) SendAndReceiveOne(ctx context.Context, message messages.OutputMessage, expectedCode messaging.MessageCode) (chan []byte, error) {
	err := c.connection.Write(message.ToBytes())
	if err != nil {
		return nil, fmt.Errorf("sending message: %v", err)
	}
	return c.waitFor(ctx, expectedCode), nil
}

func (c *MessageConnection) ReadMessageBytes() ([]byte, error) {
	var res []byte
	lengthBytes, err := c.connection.Read(4)
	if err != nil {
		return nil, fmt.Errorf("reading length: %v", err)
	}
	length := int(binary.LittleEndian.Uint32(lengthBytes))
	res = append(res, lengthBytes...)

	payloadBytes, err := c.connection.Read(length)
	if err != nil {
		return nil, fmt.Errorf("reading payload: %v", err)
	}
	res = append(res, payloadBytes...)

	log.Debugf("<- %v", res)

	return res, nil
}

func (c *MessageConnection) waitFor(ctx context.Context, code messaging.MessageCode) chan []byte {
	responseChannel := make(chan []byte)

	c.messagesubscribersmutex.Lock()
	c.messagesubscribers[responseChannel] = code
	c.messagesubscribersmutex.Unlock()
	go func() {
		<-ctx.Done()
		c.unsubscribeChannel(responseChannel)
	}()
	return responseChannel
}

func (c *MessageConnection) unsubscribeChannel(channel chan []byte) {
	c.messagesubscribersmutex.Lock()
	defer c.messagesubscribersmutex.Unlock()
	delete(c.messagesubscribers, channel)

	_, ok := <-channel
	if ok {
		close(channel)
	}
}

func (c *MessageConnection) readContinuously(ctx context.Context) chan []byte {
	out := make(chan []byte)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				mes, err := c.ReadMessageBytes()
				if err != nil {
					log.Errorf("error while reading message: %v", err)
					continue
				}
				out <- mes
			}
		}
	}()
	return out
}

func (c *MessageConnection) dispatchMessage(messageChan chan []byte) {
	go func() {
		for message := range messageChan {
			mescode, err := messaging.NewMessageReader(message).Code()
			if err != nil {
				return
			}

			c.messagesubscribersmutex.RLock()
			for meschan, code := range c.messagesubscribers {
				if code == mescode {
					meschan <- message
				}
			}
			c.messagesubscribersmutex.RUnlock()

		}
	}()

}
