package main

import (
	"context"
	"errors"
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
	"github.com/elazarl/goproxy"
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
// TODO:
//   blocking doesn't work by running mitm attacks for my RPi
//   fire up goproxy as a transparent proxy
//     track dest IP or domain usage
//     if dest is for any of the knwon targets and threshold breached then (deny it, optionally drop it)
//
// TODO: implement another filter for return/incoming traffic from YouTube
//       do rate limiting
// TODO: notify if another device hits youtube not via the proxy

// TODO: add
//  domains to groups
//  dest IP to groups
//  src IPs to groups
// TODO: swap IpDomains for DestIpGroups in nfq

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

	// Load app config from the environment.
	var appCfg config.AppConfig
	err := envconfig.Process("", &appCfg)
	if err != nil {
		log.Println("failed to process app config:", err)
		os.Exit(1)
	}

	handleDebugging(&appCfg)

	// NFT rules to send traffic to NFQueue.
	// There won't be any NFT rules until dest IPs are supplied by manager callbacks.
	rules, err := nft.NewNFTRules()
	if err != nil {
		log.Println("Failed to setup nft rules:", err)
		os.Exit(1)
	}
	log.Println("NFTables rules created")

	// Group manager.
	mgr := group.NewManager()

	// Destinations.
	dw := group.NewDomainWatcher()
	dw.RegisterDestIpGroupReceivers(mgr)
	dw.RegisterDestIpDomainReceivers(mgr)
	dw.RegisterDestDomainGroupReceivers(mgr)

	// Sources.
	w := group.NewNetWatcher()
	w.RegisterSourceIpGroupsReceivers(mgr)

	// Usage tracker.
	t := usage.NewTracker(appCfg.TrackerConfig.Retention, appCfg.TrackerConfig.Granularity, appCfg.TrackerConfig.Threshold, appCfg.TrackerConfig.StartDay, appCfg.TrackerConfig.StartTime)

	// NF Queue to listen to and track packets in user space.
	// TODO: supply manager to the NFQueue.
	q, err := nfq.NewNFQueueFilter(ctx, t)
	if err != nil {
		log.Println("failed to setup nfqueue filter:", err)
		os.Exit(1)
	}
	log.Println("NFQueue listener running")

	// Configure GoProxy.
	p := goproxy.NewProxyHttpServer()
	p.Verbose = true
	p.OnRequest().DoFunc(proxy.Handler)
	s := &http.Server{
		Addr:                         ":8080",
		Handler:                      p,
		DisableGeneralOptionsHandler: false,
		TLSConfig:                    nil,
		ReadTimeout:                  30 * time.Second, // Maximum duration for reading the request body
		ReadHeaderTimeout:            5 * time.Second,  // Time to read headers before timing out
		WriteTimeout:                 30 * time.Second, // Maximum duration for writing the response
		IdleTimeout:                  30 * time.Second, // Maximum amount of time to keep idle connections alive
		MaxHeaderBytes:               1 << 20,          // Maximum size of request headers (1 MB)
	}

	// Start proxy server.
	done := make(chan struct{}, 1)
	go func() {
		if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Println("Error starting proxy server:", err)
			os.Exit(1)
		}
		close(done)
	}()

	// Capture SIGINT and SIGTERM to gracefully shutdown.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	log.Println("Signal received, shutting down...")

	// Shutdown the server.
	ctxSrv, cancelSrv := context.WithTimeout(context.Background(), 5*time.Second)
	if err = s.Shutdown(ctxSrv); err != nil {
		log.Println("Error shutting down server:", err)
		os.Exit(1)
	}
	cancelSrv()

	// More cleanup.
	cancel() // call cancel before closing the rules/nfq else it will block.
	err = rules.Clean()
	if err != nil {
		log.Println("Error: unable to remove NFT rules")
		os.Exit(1)
	}
	_ = q.Nfq.Close() // cancel its context above before calling Close() else it will block.

	return
}
