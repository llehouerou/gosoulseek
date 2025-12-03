// Command poc is a proof-of-concept CLI for testing Soulseek server connection.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/llehouerou/gosoulseek/client"
	"github.com/llehouerou/gosoulseek/messages/peer"
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
	searchQuery := flag.String("search", "", "Search query (if empty, just login and wait)")
	searchTimeout := flag.Duration("timeout", 30*time.Second, "Search timeout")
	downloadFirst := flag.Bool("download", false, "Download the first search result")
	outputDir := flag.String("out", ".", "Output directory for downloads")
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

	// Start listener for incoming peer connections if port configured
	if *listenPort > 0 {
		if err := c.StartListener(); err != nil {
			return fmt.Errorf("start listener: %w", err)
		}
		defer func() { _ = c.StopListener() }()
		fmt.Printf("  Listener:  port %d\n", c.ListenerPort())
	}
	fmt.Println()

	// If no search query, just wait for signals
	if *searchQuery == "" {
		fmt.Println("No search query provided. Waiting for messages (Ctrl+C to exit)...")
		waitForSignal()
		return nil
	}

	// Search with timeout
	fmt.Printf("Searching for: %s\n", *searchQuery)
	fmt.Printf("(waiting %s for results...)\n\n", *searchTimeout)

	searchCtx, cancel := context.WithTimeout(ctx, *searchTimeout)
	defer cancel()

	results, err := c.Search(searchCtx, *searchQuery)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	// Collect results
	var resultCount int
	var fileCount int
	var firstResult *peer.SearchResponse
	var firstFile *peer.File

	// Process results as they come in
	for resp := range results {
		resultCount++
		fileCount += len(resp.Files)
		printSearchResponse(resultCount, resp)

		// Save first result from a user with free slot (no queue) for potential download
		if firstResult == nil && len(resp.Files) > 0 && resp.HasFreeSlot && resp.QueueLength == 0 {
			firstResult = resp
			firstFile = &resp.Files[0]
			fmt.Printf("  ^^^ Selected for download (free slot, no queue)\n")
		}
	}

	fmt.Printf("\nSearch completed. Got %d results with %d total files.\n", resultCount, fileCount)

	// Download first result if requested
	if *downloadFirst && firstResult != nil && firstFile != nil {
		fmt.Printf("\nDownloading first result from %s...\n", firstResult.Username)
		fmt.Printf("  File: %s\n", firstFile.Filename)
		fmt.Printf("  Size: %d KB\n", firstFile.Size/1024)

		if err := downloadFile(ctx, c, firstResult.Username, firstFile, *outputDir); err != nil {
			return fmt.Errorf("download: %w", err)
		}
	}

	return nil
}

func downloadFile(ctx context.Context, c *client.Client, username string, file *peer.File, outputDir string) error {
	// Extract filename from path
	filename := filepath.Base(file.Filename)
	outputPath := filepath.Join(outputDir, filename)

	// Create output file
	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	// Start download with 5 minute timeout
	dlCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	progress, err := c.Download(dlCtx, username, file.Filename, f)
	if err != nil {
		return err
	}

	// Monitor progress
	for p := range progress {
		// Check for completed states first
		if p.State.IsCompleted() {
			switch {
			case p.State&client.TransferStateSucceeded != 0:
				fmt.Printf("\n  Download completed! Saved to: %s\n", outputPath)
			case p.State&client.TransferStateCancelled != 0:
				return errors.New("transfer cancelled")
			default:
				return fmt.Errorf("transfer failed: %w", p.Error)
			}
			continue
		}

		// Check active states
		switch {
		case p.State&client.TransferStateInProgress != 0:
			var pct float64
			if p.FileSize > 0 {
				pct = float64(p.BytesTransferred) / float64(p.FileSize) * 100
			}
			fmt.Printf("  Progress: %.1f%% (%d / %d bytes)\r", pct, p.BytesTransferred, p.FileSize)
		case p.State&client.TransferStateInitializing != 0:
			fmt.Println("  Status: connecting for transfer")
		case p.State.IsQueued() && p.State.IsRemote():
			if p.QueuePosition > 0 {
				fmt.Printf("  Status: queued remotely (position %d)\n", p.QueuePosition)
			} else {
				fmt.Println("  Status: queued remotely")
			}
		case p.State.IsQueued() && p.State.IsLocal():
			fmt.Println("  Status: queued locally")
		case p.State&client.TransferStateRequested != 0:
			fmt.Println("  Status: request sent")
		}
	}

	return nil
}

func printSearchResponse(count int, resp *peer.SearchResponse) {
	fmt.Printf("[%d] %s: %d files", count, resp.Username, len(resp.Files))
	if resp.HasFreeSlot {
		fmt.Printf(" (free slot)")
	}
	fmt.Printf(" - %d KB/s, queue: %d\n", resp.UploadSpeed/1024, resp.QueueLength)

	// Show first few files
	for i, f := range resp.Files {
		if i >= 3 {
			fmt.Printf("    ... and %d more files\n", len(resp.Files)-3)
			break
		}
		sizeKB := f.Size / 1024
		if f.BitRate > 0 {
			fmt.Printf("    %s (%d KB, %d kbps)\n", f.Filename, sizeKB, f.BitRate)
		} else {
			fmt.Printf("    %s (%d KB)\n", f.Filename, sizeKB)
		}
	}
}

func waitForSignal() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
}
