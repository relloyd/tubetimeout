package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/youtube-nfqueue/domains"
	"example.com/youtube-nfqueue/nftables"
	"example.com/youtube-nfqueue/queue"
	"example.com/youtube-nfqueue/tracker"
	"github.com/kelseyhightower/envconfig"
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

type Config struct {
	DebugEnabled bool          `envconfig:"DEBUG_ENABLED" default:"false"`
	DebugTime    time.Duration `envconfig:"DEBUG_TIME_SECONDS" default:"30s"`
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load config from the environment.
	var cfg Config
	err := envconfig.Process("", &cfg)
	if err != nil {
		fmt.Println("failed to process env vars:", err)
		os.Exit(1)
	}

	if cfg.DebugEnabled {
		// Allow debug connection timeout.
		tc := time.After(cfg.DebugTime)
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT)
		fmt.Println("Waiting for debug time or CTRL-C signal...")
		select {
		case <-tc:
			fmt.Println("Debug time is up; continuing...")
		case <-sigs:
			fmt.Println("Signal received, continuing...")
		}
		time.Sleep(1 * time.Second) // allow more time for debugger/dlv to attach 🤷‍♂️
	}

	// Set up nft rules to send traffic to nfqueue.
	// There won't be any rules until IPs are supplied by PeriodicResolver.
	rules, err := nftables.NewNFTRules()
	if err != nil {
		fmt.Println("failed to setup nft rules:", err)
		os.Exit(1)
	}
	fmt.Println("nft rules setup.")

	// Create a tracker.
	t := tracker.NewTracker(1*time.Minute, 10*time.Second, 1*time.Second)

	// Create our NFQueue to listen for packets in user space.
	nfq, err := queue.NewNFQueueFilter(ctx, t)
	if err != nil {
		fmt.Println("failed to setup nfqueue filter:", err)
		os.Exit(1)
	}
	fmt.Println("nfq filter setup.")

	// Register interfaces to receive updated IPs periodically.
	// rules.Notify(map[string]struct{}{"142.250.179.238": {}})
	// nfq.Notify(map[string]struct{}{"142.250.179.238": {}})
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
