package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"example.com/youtube-nfqueue/domains"
	"example.com/youtube-nfqueue/nft"
	"example.com/youtube-nfqueue/queue"
)

// TODO:
//   test that blocking works by running mitm attack for my mac IP
// TODO: implement filtering logic
//   count time slots
//   block after time limit
//   time limit per MAC -- supply the MACs somehow
// TODO: understand groups of MACs, apply time limits to groups, time limits per custom period
// TODO: implement another filter for return/incoming traffic from YouTube
//   do rate limiting

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up nft rules to send traffic to nfqueue.
	// There won't be any rules until IPs are supplied by PeriodicResolver.
	rules, err := nft.NewNFTRules()
	if err != nil {
		fmt.Println("failed to setup nft rules:", err)
		os.Exit(1)
	}
	fmt.Println("nft rules setup.")

	// Create our NFQueue to listen for packets in user space.
	nfq, err := queue.NewNFQueueFilter(ctx)
	if err != nil {
		fmt.Println("failed to setup nfqueue filter:", err)
		os.Exit(1)
	}
	fmt.Println("nfq filter setup.")

	// Register interfaces to receive updated IPs periodically.
	domains.RegisterIPListReceivers(rules, nfq)

	// Start a goroutine to periodically resolve the domains.
	go domains.PeriodicResolver(ctx)

	// Capture SIGINT and SIGTERM to gracefully shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-ctx.Done():
		fmt.Println("Context done.")
	case <-sigs:
		fmt.Println("Signal received, shutting down...")
		cancel() // cancel before closing the nfq else it will block.
		err = rules.Clean()
		if err != nil {
			fmt.Println("Error: unable to remove NFT rules")
			os.Exit(1)
		}
		_ = nfq.Nfq.Close() // kill the context before closing else it will block.
	}

	return
}
