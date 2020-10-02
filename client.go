package gosoulseek

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/llehouerou/gosoulseek/messaging"

	"github.com/llehouerou/gosoulseek/messaging/messages/responses"

	"github.com/llehouerou/gosoulseek/messaging/messages/requests"

	"github.com/llehouerou/gosoulseek/network"

	log "github.com/sirupsen/logrus"
)

type Client struct {
	serverConnection *network.MessageConnection
	address          string
	port             int

	listenPort int
}

func NewClient(address string, port int) *Client {
	return &Client{
		serverConnection: network.NewMessageConnection("", address, port),
		address:          address,
		port:             port,
		listenPort:       16118,
	}
}

func (c *Client) Connect(ctx context.Context) error {

	go func() {
		l, err := net.Listen("tcp", ":16118")
		if err != nil {
			fmt.Println("Error listening:", err.Error())
			os.Exit(1)
		}
		// Close the listener when the application closes.
		defer l.Close()
		for {
			// Listen for an incoming connection.
			conn, err := l.Accept()
			if err != nil {
				fmt.Println("Error accepting: ", err.Error())
				os.Exit(1)
			}
			// Handle connections in a new goroutine.
			go handleRequest(conn)
		}

	}()

	err := c.serverConnection.Connect(ctx)
	if err != nil {
		return err
	}

	return nil
}

func handleRequest(conn net.Conn) {
	// Make a buffer to hold incoming data.
	buf := make([]byte, 1024)
	// Read the incoming connection into the buffer.
	_, err := conn.Read(buf)
	if err != nil {
		fmt.Println("Error reading:", err.Error())
	}
	log.Debugf("%s : %v", conn.RemoteAddr(), buf)
	// Close the connection when you're done with it.
	conn.Close()
}

func (c *Client) Login(ctx context.Context, username string, password string) error {
	log.Infof("logging in as %s", username)

	loginctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	responseChan, err := c.serverConnection.SendAndReceiveOne(loginctx, requests.NewLoginRequest(username, password), messaging.ServerLogin)
	if err != nil {
		return fmt.Errorf("sending request: %v", err)
	}
	for {
		select {
		case <-loginctx.Done():
			return fmt.Errorf("context done")
		case response := <-responseChan:
			loginresp, err := (&responses.LoginResponse{}).FromBytes(response)
			if err != nil {
				return fmt.Errorf("decoding response: %v", err)
			}
			if !loginresp.Succeeded {
				return fmt.Errorf("login failed with message: %s", loginresp.Message)
			}
			log.Debugf("login response: %+v", loginresp)
			err = c.serverConnection.Send(requests.NewSetListenPortRequest(c.listenPort))
			if err != nil {
				return fmt.Errorf("sending listen port: %v", err)
			}
			return nil
		}
	}
}

func (c *Client) Search(searchText string) {
	err := c.serverConnection.Send(requests.NewSearchRequest(searchText, 1))
	if err != nil {
		panic(err)
	}
}
