package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/youtube-nfqueue/config"
	"example.com/youtube-nfqueue/domains"
	"example.com/youtube-nfqueue/netwatch"
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

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load config from the environment.
	var debugCfg config.DebugConfig
	err := envconfig.Process("", &debugCfg)
	if err != nil {
		fmt.Println("failed to process debug config:", err)
		os.Exit(1)
	}

	// Load app config from the environment.
	var appCfg config.AppConfig
	err = envconfig.Process("", &appCfg)
	if err != nil {
		fmt.Println("failed to process app config:", err)
		os.Exit(1)
	}

	if debugCfg.DebugEnabled {
		// Allow debug connection timeout.
		tc := time.After(debugCfg.DebugTime)
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
	t := tracker.NewTracker(appCfg.TrackerConfig.Retention, appCfg.TrackerConfig.Granularity, appCfg.TrackerConfig.Threshold, appCfg.TrackerConfig.StartDay, appCfg.TrackerConfig.StartTime)

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

	// Create a new NetWatcher
	watcher := netwatch.NewNetWatcher()

	// Register a callback
	watcher.RegisterCallback(func(data map[string]netwatch.MACGroup) {
		fmt.Println("Updated IP map:")
		for ip, mapping := range data {
			fmt.Printf("IP: %s, MAC: %s, Group: %s\n", ip, mapping.MAC, mapping.Group)
		}
	})

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
		_ = nfq.Nfq.Close() // cancel its context above before calling Close() else it will block.
	}

	return
}
