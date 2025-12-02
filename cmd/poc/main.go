// Command poc is a proof-of-concept CLI for testing Soulseek server connection.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/joho/godotenv"

	"github.com/llehouerou/gosoulseek/client"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load .env file if it exists (ignore error if not found)
	_ = godotenv.Load()

	username := flag.String("user", "", "Soulseek username (or set SOULSEEK_USERNAME)")
	password := flag.String("pass", "", "Soulseek password (or set SOULSEEK_PASSWORD)")
	serverAddr := flag.String("server", client.DefaultServerAddress, "Soulseek server address")
	listenPort := flag.Uint("port", 0, "Listen port for incoming connections (0 = disabled)")
	flag.Parse()

	// Use environment variables as fallback
	if *username == "" {
		*username = os.Getenv("SOULSEEK_USERNAME")
	}
	if *password == "" {
		*password = os.Getenv("SOULSEEK_PASSWORD")
	}

	if *username == "" || *password == "" {
		return errors.New("credentials required: use -user/-pass flags or set SOULSEEK_USERNAME/SOULSEEK_PASSWORD in .env")
	}

	// Configure client
	opts := client.DefaultOptions()
	opts.ServerAddress = *serverAddr
	opts.ListenPort = uint32(*listenPort) //nolint:gosec // port is validated by flag parsing

	c := client.New(opts)

	// Connect
	ctx := context.Background()
	fmt.Printf("Connecting to %s...\n", opts.ServerAddress)

	if err := c.Connect(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = c.Disconnect() }()

	fmt.Println("Connected!")

	// Login
	fmt.Printf("Logging in as '%s'...\n", *username)

	if err := c.Login(ctx, *username, *password); err != nil {
		return fmt.Errorf("login: %w", err)
	}

	fmt.Println()
	fmt.Println("Login successful!")
	fmt.Printf("  Username:  %s\n", c.Username())
	fmt.Printf("  Public IP: %s\n", c.IPAddress())
	fmt.Printf("  Supporter: %v\n", c.IsSupporter())
	fmt.Println()

	fmt.Println("Post-login configuration sent (status: Online, shares: 0)")

	return nil
}
