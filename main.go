package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/youtube-nfqueue/config"
	"example.com/youtube-nfqueue/group"
	"example.com/youtube-nfqueue/nfq"
	"example.com/youtube-nfqueue/nft"
	"example.com/youtube-nfqueue/proxy"
	"example.com/youtube-nfqueue/usage"
	"github.com/kelseyhightower/envconfig"
)

// Functionality:
//   INPUT
//     Domains    - resolve IPs for a list of domains and supply to callbacks like NFT rules and NFQueue
//     NetWatcher - MAC IP GroupConfig
//     Tracker    - count usage stats by a thing like dest IP or any string
//   DOES STUFF
//     NFT rules  - add NFT rules to capture traffic going to a set of dest IP addresses
//     NFQueue    - inspect packets in user space (relies on NFT rules to receive them)
//
// TODO: blocking doesn't work by running mitm attacks for my RPi
//
// TODO: implement another filter for return/incoming traffic from YouTube
//       do rate limiting

// TODO: notify if another device hits youtube not via the proxy

type cleanupFunc func() error

func handleDebugging(appCfg *config.AppConfig) {
	if appCfg.DebugConfig.DebugEnabled {
		// Allow debug connection timeout.
		tc := time.After(appCfg.DebugConfig.DebugTime)
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT)
		log.Println("Waiting for debug time or CTRL-C signal...")
		select {
		case <-tc:
			log.Println("Debug time is up; continuing...")
		case <-sigs:
			log.Println("Signal received, continuing...")
		}
		time.Sleep(1 * time.Second) // allow more time for debugger/dlv to attach 🤷‍♂️
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var cleanupFuncs []cleanupFunc

	// Load app config from the environment.
	var appCfg config.AppConfig
	err := envconfig.Process("", &appCfg)
	if err != nil {
		log.Fatalln("failed to process app config:", err)
	}

	handleDebugging(&appCfg)

	// NFT rules to send traffic to NFQueue.
	// There won't be any NFT rules until dest IPs are supplied by manager callbacks.
	rules, err := nft.NewNFTRules(&appCfg.FilterConfig)
	if err != nil {
		log.Fatalln("Failed to setup nft rules:", err)
	}
	log.Println("NFTables rules created")

	// Usage tracker.
	t, err := usage.NewTracker(ctx, &appCfg.TrackerConfig)
	if err != nil {
		log.Fatalln("Failed to setup usage tracker:", err)
	}
	log.Println("Usage tracker created")

	// Group manager.
	mgr := group.NewManager()
	log.Println("Group manager created")

	// Sources.
	w := group.NewNetWatcher()
	w.RegisterSourceIpGroupsReceivers(mgr)
	w.RegisterSourceIpGroupsReceivers(rules)
	w.Start(ctx)
	log.Println("Sources mapped")

	// Destinations.
	dw := group.NewDomainWatcher()
	dw.RegisterDestIpGroupReceivers(mgr)
	dw.RegisterDestIpDomainReceivers(mgr)
	dw.RegisterDestDomainGroupReceivers(mgr)
	dw.RegisterDestIpDomainReceivers(rules)
	dw.Start(ctx)
	log.Println("Destinations mapped")

	// NFQueue to listen to and track packets in user space.
	q, err := nfq.NewNFQueueFilter(ctx, appCfg, t, mgr)
	if err != nil {
		log.Fatalln("Failed to setup NFQueue filter:", err)
	}
	log.Println("NFQueue listener started")

	// Cleanup functions.
	cleanupFuncs = append(cleanupFuncs, func() error {
		cancel() // call cancel before closing NFQ else it will block!
		err = rules.Clean()
		if err != nil {
			return fmt.Errorf("error removing NFT rules: %w", err)
		}
		err = q.Nfq.Close() // cancel its context above before calling Close() else it will block.
		if err != nil {
			return fmt.Errorf("error closing NFQ: %w", err)
		}
		return nil
	})

	// Proxy server start.
	if appCfg.ProxyConfig.ProxyEnabled {
		s := proxy.NewServer(mgr, t)
		go func() {
			if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatalln("Error starting proxy server:", err)
			}
			log.Println("Proxy server quit")
		}()
		log.Println("Proxy server started")

		cleanupFuncs = append(cleanupFuncs, func() error {
			// Shutdown the proxy server.
			ctxSrv, cancelSrv := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelSrv()
			if err = s.Shutdown(ctxSrv); err != nil {
				return fmt.Errorf("error shutting down proxy server: %w", err)
			}
			return nil
		})
	}

	// Capture SIGINT and SIGTERM to gracefully shutdown.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	log.Printf("\nSignal received, shutting down...\n")

	// Cleanup and exit.
	failure := false
	for _, f := range cleanupFuncs {
		if err := f(); err != nil {
			log.Printf("Error during cleanup: %v", err)
			failure = true
		}
	}
	if failure {
		os.Exit(1)
	}
	return
}
