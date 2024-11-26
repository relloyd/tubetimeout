package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"example.com/youtube-nfqueue/domains"
	"example.com/youtube-nfqueue/queue"
	"github.com/florianl/go-nfqueue"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Register interfaces to receive updated list of IPs periodically.
	domains.RegisterIPListReceiver(queue.Ips)

	// Start a goroutine to periodically resolve the domains.
	go domains.PeriodicResolver(ctx)

	// Set up nft rules to send traffic to nfqueue.

	// Setup nfqueue.
	nf, err := queue.StartNFQueueFilter(ctx, cancel)
	if err != nil {
		fmt.Println("failed to setup nfqueue filter:", err)
		return
	}
	defer func(nf *nfqueue.Nfqueue) {
		_ = nf.Close()
	}(nf)
	fmt.Println("NFQueue filter started")

	// Capture SIGINT and SIGTERM to gracefully shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-ctx.Done():
		fmt.Println("Context done.")
	case <-sigs:
		fmt.Println("Signal received, shutting down...")
		cancel()
		_ = nf.Close() // kill the context before closing else it will block.
	}

	return
}
