package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"go.uber.org/zap"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/dhcp"
	"relloyd/tubetimeout/group"
	"relloyd/tubetimeout/ipv6"
	"relloyd/tubetimeout/led"
	"relloyd/tubetimeout/monitor"
	"relloyd/tubetimeout/nfq"
	"relloyd/tubetimeout/nft"
	"relloyd/tubetimeout/usage"
	"relloyd/tubetimeout/web"
)

// Functionality:
//   INPUT
//     Domains    - resolve IPs for a list of domains and supply to callbacks like NFT rules and NFQueue
//     NetWatcher - MAC IP GroupMACsConfig
//     UsageTracker    - count usage stats by a thing like dest IP or any string
//   DOES STUFF
//     NFT rules  - add NFT rules to capture traffic going to a set of dest IP addresses
//     NFQueue    - inspect packets in user space (relies on NFT rules to receive them)

type cleanupFunc func() error

func handleDelayedStart(logger *zap.SugaredLogger, appConfig *config.AppConfig) {
	if appConfig.DelayStart && !appConfig.DebugConfig.DebugEnabled { // if we should delay startup, and we're not in debug mode...
		delay := time.Second * 30
		logger.Infof("Delaying startup for %v", delay)
		time.Sleep(delay)
	}
}

func handleDebugging(logger *zap.SugaredLogger, appCfg *config.DebugConfig) {
	if appCfg.DebugEnabled {
		tc := time.After(appCfg.DebugTime) // sleep to help debugger connections
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

func recoverFunc(logger *zap.Logger) {
	if r := recover(); r != nil {
		logger.Error("Recovered from panic",
			zap.Any("message", r),
			zap.String("stack", string(debug.Stack())),
		)
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

	logger.Infof("Build version %v", config.BuildVersion)

	// Recovery.
	defer recoverFunc(logger.Desugar())

	// Cleanup functions.
	var cleanupFuncs []cleanupFunc

	handleDelayedStart(logger, &config.AppCfg)
	handleDebugging(logger, &config.AppCfg.DebugConfig)

	// IPv6 status checker.
	ipv6Checker := ipv6.NewIPv6Checker(ctx, logger)
	logger.Info("IPv6 status checker created")

	// Maybe start DHCP server.
	dhcpServer, err := dhcp.NewServer(ctx, logger, config.AppCfg.DHCPServerDisabled, led.NewController(logger))
	if err != nil {
		logger.Fatalf("Failed to setup DHCP server: %v", err)
	}
	cleanupFuncs = append(cleanupFuncs, dhcpServer.Stop)

	// NFT rules to send traffic to NFQueue.
	// There won't be any NFT rules until dest IPs are supplied by manager callbacks.
	rules, err := nft.NewNFTRules(logger, &config.AppCfg.FilterConfig)
	if err != nil {
		logger.Fatal("Failed to setup nft rules:", err)
	}
	logger.Info("NFTables rules created")

	// Usage tracker.
	t, err := usage.NewTracker(ctx, logger, &config.AppCfg.TrackerConfig)
	if err != nil {
		logger.Fatalln("Failed to setup usage tracker:", err)
	}
	logger.Info("Usage tracker created")

	// Traffic Monitor.
	trafficMap := monitor.NewTrafficMap(logger, 5)
	logger.Info("Traffic monitor started")

	// Group manager.
	mgr := group.NewManager(logger)
	logger.Info("Group manager created")

	// Sources.
	w := group.NewNetWatcher(logger)
	w.RegisterSourceIpGroupsReceivers(mgr, rules)
	w.RegisterSourceIpMACReceivers(trafficMap)
	w.Start(ctx)
	logger.Info("Sources mapped")

	// Destinations.
	dw := group.NewDomainWatcher(logger)
	dw.RegisterDestIpGroupReceivers(mgr)
	dw.RegisterDestDomainGroupReceivers(mgr)     // TODO: remove unused DestDomainGroupReceivers in mgr if/when the proxy feature is removed as it is essentially wasted effort keeping the structs sync'd.
	dw.RegisterDestIpDomainReceivers(mgr, rules) // TODO: remove unused DestIpDomainReceivers in mgr if/when the proxy feature is removed as it is essentially wasted effort keeping the structs sync'd.
	dw.Start(ctx)
	logger.Info("Destinations mapped")

	// NFQueue to process packets in user space.
	q, err := nfq.NewNFQueueFilter(ctx, logger, &config.AppCfg.FilterConfig, t, mgr, trafficMap, recoverFunc)
	if err != nil {
		logger.Fatalln("Failed to setup NFQueue filter:", err)
	}
	logger.Info("NFQueue listener started")

	cleanupFuncs = append(cleanupFuncs, func() error {
		// Cancel the NFQ before closing NFQ else it will block!
		// We probably want to remove the NFT rules before closing the NFQ but NFQ will have packets in flight that it cannot Accept with error: "netlink send: sendmsg: bad file descriptor".
		// This is good enough:
		cancel()
		err = rules.Clean(logger)
		if err != nil {
			return fmt.Errorf("error removing NFT rules: %w", err)
		}
		for _, nf := range q.Nfq {
			err = nf.Close() // cancel its context above before calling Close() else it will block.
			if err != nil {
				return fmt.Errorf("error closing NFQ: %w", err)
			}
		}
		return nil
	})

	// Web server start.
	if config.AppCfg.WebConfig.WebEnabled {
		s := web.NewServer(logger, t, config.GroupMACs, trafficMap, dhcpServer, ipv6Checker)
		go func() {
			if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Fatalln("Error starting web server:", err)
			}
			logger.Info("Web server quit")
		}()
		logger.Info("Web server started")

		cleanupFuncs = append(cleanupFuncs, func() error {
			// Shutdown the web server.
			ctxSrv, cancelSrv := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelSrv()
			if err = s.Shutdown(ctxSrv); err != nil {
				return fmt.Errorf("error shutting down web server: %w", err)
			}
			return nil
		})
	}

	// Capture SIGINT and SIGTERM to shut down gracefully.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	logger.Info("Signal received, shutting down...")

	// Clean up and exit.
	failure := false
	for _, f := range cleanupFuncs {
		if err := f(); err != nil {
			logger.Errorf("Error during cleanup: %v", err)
			failure = true
		}
	}
	if failure {
		os.Exit(1)
	}
	return
}
