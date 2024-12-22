package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/tubetimeout/config"
	"example.com/tubetimeout/group"
	"example.com/tubetimeout/nfq"
	"example.com/tubetimeout/nft"
	"example.com/tubetimeout/proxy"
	"example.com/tubetimeout/usage"
	"github.com/kelseyhightower/envconfig"
	"go.uber.org/zap"
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

func handleDebugging(logger *zap.SugaredLogger, appCfg *config.DebugConfig) {
	if appCfg.DebugEnabled {
		// Allow debug connection timeout.
		tc := time.After(appCfg.DebugTime)
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT)
		logger.Info("Waiting for debug time or CTRL-C signal...")
		select {
		case <-tc:
			logger.Info("Debug time is up; continuing...")
		case <-sigs:
			logger.Info("Signal received, continuing...")
		}
		time.Sleep(1 * time.Second) // allow more time for debugger/dlv to attach 🤷‍♂️
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup logger.
	logger := config.MustGetLogger()
	defer func(logger *zap.SugaredLogger) {
		_ = logger.Sync()
	}(logger)

	// Cleanup functions.
	var cleanupFuncs []cleanupFunc

	// Load app config from the environment.
	var appCfg config.AppConfig
	err := envconfig.Process("", &appCfg)
	if err != nil {
		logger.Fatalln("failed to process app config:", err)
	}

	handleDebugging(logger, &appCfg.DebugConfig)

	// NFT rules to send traffic to NFQueue.
	// There won't be any NFT rules until dest IPs are supplied by manager callbacks.
	rules, err := nft.NewNFTRules(logger, &appCfg.FilterConfig)
	if err != nil {
		logger.Fatal("Failed to setup nft rules:", err)
	}
	logger.Info("NFTables rules created")

	// Usage tracker.
	t, err := usage.NewTracker(ctx, &appCfg.TrackerConfig)
	if err != nil {
		logger.Fatalln("Failed to setup usage tracker:", err)
	}
	logger.Info("Usage tracker created")

	// Group manager.
	mgr := group.NewManager()
	logger.Info("Group manager created")

	// Sources.
	w := group.NewNetWatcher(logger)
	w.RegisterSourceIpGroupsReceivers(mgr)
	w.RegisterSourceIpGroupsReceivers(rules)
	w.Start(ctx)
	logger.Info("Sources mapped")

	// Destinations.
	dw := group.NewDomainWatcher(logger)
	dw.RegisterDestIpGroupReceivers(mgr)
	dw.RegisterDestIpDomainReceivers(mgr)
	dw.RegisterDestDomainGroupReceivers(mgr)
	dw.RegisterDestIpDomainReceivers(rules)
	dw.Start(ctx)
	logger.Info("Destinations mapped")

	// NFQueue to listen to and track packets in user space.
	q, err := nfq.NewNFQueueFilter(ctx, logger, &appCfg.FilterConfig, t, mgr)
	if err != nil {
		logger.Fatalln("Failed to setup NFQueue filter:", err)
	}
	logger.Info("NFQueue listener started")

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
		s := proxy.NewServer(logger, mgr, t)
		go func() {
			if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Fatalln("Error starting proxy server:", err)
			}
			logger.Info("Proxy server quit")
		}()
		logger.Info("Proxy server started")

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
	logger.Info("Signal received, shutting down...")

	// Cleanup and exit.
	failure := false
	for _, f := range cleanupFuncs {
		if err := f(); err != nil {
			logger.Info("Error during cleanup: %v", err)
			failure = true
		}
	}
	if failure {
		os.Exit(1)
	}
	return
}
