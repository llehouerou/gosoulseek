package client_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/llehouerou/gosoulseek/client"
)

func TestDefaultOptions(t *testing.T) {
	opts := client.DefaultOptions()

	assert.Equal(t, uint32(0), opts.ListenPort)
	assert.Equal(t, client.DefaultServerAddress, opts.ServerAddress)
	assert.Equal(t, client.DefaultConnectTimeout, opts.ConnectTimeout)
	assert.Equal(t, client.DefaultMessageTimeout, opts.MessageTimeout)
}

func TestNew_WithNilOptions(t *testing.T) {
	c := client.New(nil)

	assert.NotNil(t, c)
	assert.False(t, c.Connected())
	assert.False(t, c.LoggedIn())
}

func TestNew_WithOptions(t *testing.T) {
	opts := &client.Options{
		ListenPort:     50000,
		ServerAddress:  "localhost:2271",
		ConnectTimeout: 5 * time.Second,
		MessageTimeout: 3 * time.Second,
	}
	c := client.New(opts)

	assert.NotNil(t, c)
	assert.False(t, c.Connected())
}

func TestClient_InitialState(t *testing.T) {
	c := client.New(nil)

	assert.False(t, c.Connected())
	assert.False(t, c.LoggedIn())
	assert.Empty(t, c.Username())
	assert.Nil(t, c.IPAddress())
	assert.False(t, c.IsSupporter())
}

func TestClient_LoginWithoutConnect(t *testing.T) {
	c := client.New(nil)

	err := c.Login(context.Background(), "user", "pass")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestClient_DisconnectWhenNotConnected(t *testing.T) {
	c := client.New(nil)

	// Should not error when disconnecting an already disconnected client
	err := c.Disconnect()

	assert.NoError(t, err)
}

func TestClient_ConnectToInvalidAddress(t *testing.T) {
	opts := &client.Options{
		ServerAddress:  "localhost:99999", // Invalid port
		ConnectTimeout: 100 * time.Millisecond,
	}
	c := client.New(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := c.Connect(ctx)

	assert.Error(t, err)
	assert.False(t, c.Connected())
}

func TestClient_ConnectTimeout(t *testing.T) {
	opts := &client.Options{
		ServerAddress:  "10.255.255.1:2271", // Non-routable address
		ConnectTimeout: 100 * time.Millisecond,
	}
	c := client.New(opts)

	ctx := context.Background()

	err := c.Connect(ctx)

	assert.Error(t, err)
	assert.False(t, c.Connected())
}

func TestOptions_Constants(t *testing.T) {
	assert.Equal(t, "vps.slsknet.org:2271", client.DefaultServerAddress)
	assert.Equal(t, 10*time.Second, client.DefaultConnectTimeout)
	assert.Equal(t, 5*time.Second, client.DefaultMessageTimeout)
}

func TestClient_Transfers(t *testing.T) {
	c := client.New(nil)

	transfers := c.Transfers()
	assert.NotNil(t, transfers)

	// Registry should be empty initially
	all := transfers.All()
	assert.Empty(t, all)
}
