package group

import (
	"context"
	"log"
	"maps"
	"net"
	"sync"
	"time"

	"example.com/youtube-nfqueue/config"
	"example.com/youtube-nfqueue/models"
)

type FuncGroupDomainsLoader func() (models.MapGroupDomains, error)

var groupDomainLoaderFunc = FuncGroupDomainsLoader(config.LoadGroupDomains)

type DestIpDomainReceiver interface {
	UpdateDestIpDomains(newIps models.MapIpDomain)
}

type DestIpGroupsReceiver interface {
	UpdateDestIpGroups(newGroups models.MapIpGroups)
}

type DestDomainGroupsReceiver interface {
	UpdateDestDomainGroups(newGroups models.MapDomainGroups)
}

type DomainWatcher struct {
	mu                        sync.RWMutex // TODO: tidy up use of locks on maps that don't need them; make locks consistent.
	interval                  time.Duration
	resolver                  resolver
	groupDomains              models.MapGroupDomains
	destIpDomains             models.IpDomains
	destIpGroups              models.IpGroups
	destDomainGroups          models.DomainGroups
	destIpDomainReceivers     []DestIpDomainReceiver
	destIpGroupReceivers      []DestIpGroupsReceiver
	destDomainGroupsReceivers []DestDomainGroupsReceiver
}

type resolver func(d []models.Domain) models.MapIpDomain

type ipDomain struct {
	ip     models.Ip
	domain models.Domain
}

var (
	defaultInterval = time.Minute * 5
)

func (dw *DomainWatcher) RegisterDestIpDomainReceivers(receivers ...DestIpDomainReceiver) {
	dw.mu.Lock()
	defer dw.mu.Unlock()
	dw.destIpDomainReceivers = append(dw.destIpDomainReceivers, receivers...)
}

func (dw *DomainWatcher) RegisterDestIpGroupReceivers(receivers ...DestIpGroupsReceiver) {
	dw.mu.Lock()
	defer dw.mu.Unlock()
	dw.destIpGroupReceivers = append(dw.destIpGroupReceivers, receivers...)
}

func (dw *DomainWatcher) RegisterDestDomainGroupReceivers(receivers ...DestDomainGroupsReceiver) {
	dw.mu.Lock()
	defer dw.mu.Unlock()
	dw.destDomainGroupsReceivers = append(dw.destDomainGroupsReceivers, receivers...)
}

func NewDomainWatcher() *DomainWatcher {
	return &DomainWatcher{
		mu:                        sync.RWMutex{},
		interval:                  defaultInterval,
		resolver:                  resolveDomains,
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
	dw.loadGroupDomains()
	// Collect all IPs for all domains in all groups.
	for _, domains := range dw.groupDomains { // for each domain in each group...
		m := dw.resolver(domains)
		maps.Copy(dw.destIpDomains.Data, m)
	}
	dw.generateIPToGroups()
	dw.notifyReceivers()

	// Periodically resolve.
	ticker := time.NewTicker(defaultInterval)
	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				dw.loadGroupDomains()
				// Collect all IPs for all domains in all groups.
				for _, domains := range dw.groupDomains {
					m := dw.resolver(domains)
					maps.Copy(dw.destIpDomains.Data, m)
				}
				dw.generateIPToGroups()
				dw.notifyReceivers()
			}
		}
	}()
}

func (dw *DomainWatcher) loadGroupDomains() {
	var err error
	dw.groupDomains, err = groupDomainLoaderFunc()
	if err != nil {
		log.Fatalf("Error loading group domain YAML: %v\n", err)
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

func (dw *DomainWatcher) generateIPToGroups() {
	dw.loadGroupDomains()

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
	for _, receiver := range dw.destIpDomainReceivers {
		newData := make(models.MapIpDomain)
		dw.destIpDomains.Mu.RLock()
		for k, v := range dw.destIpDomains.Data {
			newData[k] = v
		}
		receiver.UpdateDestIpDomains(newData)
		dw.destIpDomains.Mu.RUnlock()
	}
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

func resolveDomains(domains []models.Domain) models.MapIpDomain {
	var allIPs []ipDomain

	for _, domain := range domains {
		ips, err := resolveOneDomain(domain)
		if err != nil {
			log.Printf("Failed to resolve %s: %v\n", domain, err)
			continue
		}
		for _, ip := range ips {
			allIPs = append(allIPs, ipDomain{ip: models.Ip(ip), domain: domain})
		}
	}

	log.Printf("Resolved domains")

	// Remove duplicates.
	mid := make(models.MapIpDomain)
	for _, ip := range allIPs {
		mid[ip.ip] = ip.domain // last one wins! // TODO: understand how last one wins affects tracking when src IP and dest IPs are used, and dest IPs are in mutiple domains.
	}

	return mid
}

func resolveOneDomain(domain models.Domain) ([]string, error) {
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
