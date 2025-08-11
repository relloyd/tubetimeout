package group

import (
	"context"
	"fmt"
	"maps"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"
)

type funcGroupDomainsLoader func(logger *zap.SugaredLogger) (models.MapGroupDomains, error)

var fnGroupDomainLoader = funcGroupDomainsLoader(config.FetchYouTubeDomains)

type DomainWatcher struct {
	logger                    *zap.SugaredLogger
	mu                        sync.RWMutex // TODO: tidy up use of locks on maps that don't need them; make locks consistent.
	interval                  time.Duration
	resolver                  resolver
	groupDomains              models.MapGroupDomains
	destIpDomains             models.IpDomains
	destIpGroups              models.IpGroups
	destDomainGroups          models.DomainGroups
	destIpDomainReceivers     []models.DestIpDomainReceiver
	destIpGroupReceivers      []models.DestIpGroupsReceiver
	destDomainGroupsReceivers []models.DestDomainGroupsReceiver
}

type resolver func(logger *zap.SugaredLogger, d []models.Domain) models.MapIpDomain

type ipDomain struct {
	ip     models.Ip
	domain models.Domain
}

var (
	defaultInterval = time.Minute * 5
)

func (dw *DomainWatcher) RegisterDestIpDomainReceivers(receivers ...models.DestIpDomainReceiver) {
	dw.mu.Lock()
	defer dw.mu.Unlock()
	dw.destIpDomainReceivers = append(dw.destIpDomainReceivers, receivers...)
}

func (dw *DomainWatcher) RegisterDestIpGroupReceivers(receivers ...models.DestIpGroupsReceiver) {
	dw.mu.Lock()
	defer dw.mu.Unlock()
	dw.destIpGroupReceivers = append(dw.destIpGroupReceivers, receivers...)
}

func (dw *DomainWatcher) RegisterDestDomainGroupReceivers(receivers ...models.DestDomainGroupsReceiver) {
	dw.mu.Lock()
	defer dw.mu.Unlock()
	dw.destDomainGroupsReceivers = append(dw.destDomainGroupsReceivers, receivers...)
}

func NewDomainWatcher(logger *zap.SugaredLogger) *DomainWatcher {
	return &DomainWatcher{
		logger:                    logger,
		mu:                        sync.RWMutex{},
		interval:                  defaultInterval,
		resolver:                  resolveDomainsConcurrently,
		groupDomains:              make(models.MapGroupDomains),
		destIpDomains:             models.IpDomains{Data: make(models.MapIpDomain)},
		destIpGroups:              models.IpGroups{Data: make(models.MapIpGroups)},
		destDomainGroups:          models.DomainGroups{Data: make(models.MapDomainGroups)},
		destIpDomainReceivers:     nil,
		destIpGroupReceivers:      nil,
		destDomainGroupsReceivers: nil,
	}
}

// Start starts a new ticket to resolve Ip addresses for the packaged domains and sends a copy to any
// registered receivers.
func (dw *DomainWatcher) Start(ctx context.Context) {
	fn := func() {
		dw.loadGroupDomains()
		// Collect all IPs for all domains in all groups.
		for _, domains := range dw.groupDomains {
			m := dw.resolver(dw.logger, domains)
			maps.Copy(dw.destIpDomains.Data, m)
		}
		dw.generateIPGroups()
		dw.notifyReceivers()
	}

	// Periodically resolve.
	ticker := time.NewTicker(defaultInterval)
	go func() {
		fn()
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				fn()
			}
		}
	}()
}

// TODO: fully replace the domains each time, rather than adding to them and test for this!
//
//	only notify if they're new
func (dw *DomainWatcher) loadGroupDomains() {
	var err error
	dw.groupDomains, err = fnGroupDomainLoader(dw.logger)
	if err != nil {
		dw.logger.Fatalf("Error loading group domain YAML: %v\n", err)
	}

	// Setup DomainGroups.
	dw.destDomainGroups.Mu.Lock()
	defer dw.destDomainGroups.Mu.Unlock()
	for group, domains := range dw.groupDomains { // for each group...
		for _, domain := range domains {
			// Save the domains for each group.
			// A domain may be in more than one group so we append to the list.
			dw.destDomainGroups.Data[domain] = append(dw.destDomainGroups.Data[domain], group)
		}
	}

	// Notify DomainGroup receivers.
	for _, gr := range dw.destDomainGroupsReceivers {
		newData := make(models.MapDomainGroups)
		for k, v := range dw.destDomainGroups.Data {
			newData[k] = v
		}
		gr.UpdateDestDomainGroups(newData)
	}
}

func (dw *DomainWatcher) generateIPGroups() {
	ipGroups := make(models.MapIpGroups)
	dw.destIpDomains.Mu.RLock()
	defer dw.destIpDomains.Mu.RUnlock()

	// TODO: tidy up use of locks on maps that don't need them; make locks consistent.

	for group, domains := range dw.groupDomains {
		for _, domain := range domains {
			for ip, resolvedDomain := range dw.destIpDomains.Data {
				if resolvedDomain == domain {
					ipGroups[ip] = append(ipGroups[ip], group)
				}
			}
		}
	}

	dw.destIpGroups.Mu.Lock()
	defer dw.destIpGroups.Mu.Unlock()
	dw.destIpGroups.Data = ipGroups
}

// notifyReceivers duplicates the cachedIPs map per receiver and sends it.
func (dw *DomainWatcher) notifyReceivers() {
	dw.mu.RLock()
	defer dw.mu.RUnlock()

	dw.logger.Infof("Domain watcher notifying receivers of %v IP domains", len(dw.destIpDomains.Data))
	dw.logger.Debugf("Domain watcher notifying receivers of IP domains: %v", dw.destIpDomains.Data)
	for _, receiver := range dw.destIpDomainReceivers {
		newData := make(models.MapIpDomain)
		dw.destIpDomains.Mu.RLock()
		for k, v := range dw.destIpDomains.Data {
			newData[k] = v
		}
		receiver.UpdateDestIpDomains(newData)
		dw.destIpDomains.Mu.RUnlock()
	}

	dw.logger.Infof("Domain watcher notifying receivers of %v IP groups", len(dw.destIpGroups.Data))
	dw.logger.Debugf("Domain watcher notifying receivers of IP groups: %v", dw.destIpGroups.Data)
	for _, gr := range dw.destIpGroupReceivers {
		newData := make(models.MapIpGroups)
		dw.destIpGroups.Mu.RLock()
		for k, v := range dw.destIpGroups.Data {
			newData[k] = v
		}
		gr.UpdateDestIpGroups(newData)
		dw.destIpGroups.Mu.RUnlock()
	}
}

// resolveDomainsConcurrently resolves a list of domains concurrently.
func resolveDomainsConcurrently(logger *zap.SugaredLogger, domains []models.Domain) models.MapIpDomain { // map[models.Domain][]models.Ip {
	var mu sync.Mutex
	var wg sync.WaitGroup
	var allIPs []ipDomain

	for _, domain := range domains {
		wg.Add(1)
		go func(d models.Domain) {
			defer wg.Done()
			ips, err := resolveDomainUsingGoogle(d)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				logger.Warnf("Error resolving %s: %v", d, err)
				// results[d] = nil
			} else {
				for _, ip := range ips {
					allIPs = append(allIPs, ipDomain{ip: models.Ip(ip), domain: domain})
				}
			}
		}(domain)
	}

	wg.Wait()
	logger.Info("Resolved domains")

	// Remove duplicates.
	mid := make(models.MapIpDomain)
	for _, ipd := range allIPs {
		mid[ipd.ip] = ipd.domain // last one wins! // TODO: understand how last one wins affects tracking when src IP and dest IPs are used, and dest IPs are in multiple domains.
	}

	return mid
}

func resolveDomains(logger *zap.SugaredLogger, domains []models.Domain) models.MapIpDomain {
	var allIPs []ipDomain

	for _, domain := range domains {
		ips, err := resolveDomainUsingSystem(domain)
		if err != nil {
			logger.Errorf("Failed to resolve %s: %v\n", domain, err)
			continue
		}
		for _, ip := range ips {
			allIPs = append(allIPs, ipDomain{ip: models.Ip(ip), domain: domain})
		}
	}

	logger.Info("Resolved domains")

	// Remove duplicates.
	mid := make(models.MapIpDomain)
	for _, ip := range allIPs {
		mid[ip.ip] = ip.domain // last one wins! // TODO: understand how last one wins affects tracking when src IP and dest IPs are used, and dest IPs are in multiple domains.
	}

	return mid
}

func resolveDomainUsingSystem(domain models.Domain) ([]string, error) {
	ips, err := net.LookupIP(string(domain))
	if err != nil {
		return nil, err
	}
	var ipStrings []string
	for _, ip := range ips {
		ipStrings = append(ipStrings, ip.String())
	}
	return ipStrings, nil
}

// resolveDomainUsingGoogle resolves a single domain to its IP addresses with a timeout.
func resolveDomainUsingGoogle(domain models.Domain) ([]models.Ip, error) {
	var result []models.Ip
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	customResolver := &net.Resolver{
		PreferGo:     true, // Use Go's resolver, not the system resolver
		StrictErrors: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{}
			return d.DialContext(ctx, "udp", "8.8.8.8:53") // Google's public DNS
		},
	}

	ips, err := customResolver.LookupIP(ctx, "ip4", string(domain)) // TODO: support IPv6
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", domain, err)
	}

	for _, ip := range ips {
		result = append(result, models.Ip(ip.String()))
	}
	return result, nil
}
