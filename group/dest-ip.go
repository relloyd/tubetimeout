package group

import (
	"context"
	"log"
	"maps"
	"net"
	"time"

	"example.com/youtube-nfqueue/config"
	"example.com/youtube-nfqueue/models"
)

type MapIPDomainReceiver interface {
	UpdateIPDomains(newIps models.MapIpDomain)
}

type MapIPGroupReceiver interface {
	UpdateIpGroups(newGroups models.MapIpGroups)
}

type DomainWatcher struct {
	interval                    time.Duration
	resolver                    resolver
	groupDomains                models.MapGroupDomains
	destIpDomains               models.IpDomains
	destIpGroups                models.IpGroups
	registeredIPDomainReceivers []MapIPDomainReceiver
	registeredIPGroupReceivers  []MapIPGroupReceiver
}

type resolver func(d []models.Domain) models.MapIpDomain

type ipDomain struct {
	ip     models.IP
	domain models.Domain
}

var (
	defaultInterval = time.Minute * 5
)

func (dw *DomainWatcher) RegisterIPDomainReceivers(receiver ...MapIPDomainReceiver) {
	for _, r := range receiver {
		if r != nil {
			dw.registeredIPDomainReceivers = append(dw.registeredIPDomainReceivers, r)
		}
	}
}

func (dw *DomainWatcher) RegisterIPGroupReceivers(receiver ...MapIPGroupReceiver) {
	for _, r := range receiver {
		if r != nil {
			dw.registeredIPGroupReceivers = append(dw.registeredIPGroupReceivers, r)
		}
	}
}

func NewDomainWatcher() *DomainWatcher {
	return &DomainWatcher{
		resolver:      resolveDomains,
		interval:      defaultInterval,
		destIpDomains: models.IpDomains{Data: make(models.MapIpDomain)},
		destIpGroups:  models.IpGroups{Data: make(models.MapIpGroups)},
	}
}

// Start starts a new ticket to resolve IP addresses for the packaged domains and sends a copy to any
// registered receivers.
func (dw *DomainWatcher) Start(ctx context.Context) {
	ticker := time.NewTicker(defaultInterval)
	defer ticker.Stop()

	dw.loadGroupDomains()

	// Collect all IPs for all domains in all groups.
	for _, domains := range dw.groupDomains { // for each domain in each group...
		m := dw.resolver(domains)
		maps.Copy(dw.destIpDomains.Data, m)
	}

	dw.generateIPToGroups()
	dw.notifyReceivers()

	// Periodically resolve.
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, domains := range dw.groupDomains {
				m := dw.resolver(domains)
				maps.Copy(dw.destIpDomains.Data, m)
			}
			dw.generateIPToGroups()
			dw.notifyReceivers()
		}
	}
}

func (dw *DomainWatcher) loadGroupDomains() {
	var err error
	dw.groupDomains, err = config.LoadGroupDomains()
	if err != nil {
		log.Fatalf("Error loading group domain YAML: %v\n", err)
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
	for _, receiver := range dw.registeredIPDomainReceivers {
		newData := make(models.MapIpDomain)
		dw.destIpDomains.Mu.RLock()
		for k, v := range dw.destIpDomains.Data {
			newData[k] = v
		}
		receiver.UpdateIPDomains(newData)
		dw.destIpDomains.Mu.RUnlock()
	}
	for _, gr := range dw.registeredIPGroupReceivers {
		newData := make(models.MapIpGroups)
		dw.destIpGroups.Mu.RLock()
		for k, v := range dw.destIpGroups.Data {
			newData[k] = v
		}
		gr.UpdateIpGroups(newData)
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
			allIPs = append(allIPs, ipDomain{ip: models.IP(ip), domain: domain})
		}
		log.Printf("Resolved domains")
	}

	// Remove duplicates.
	mid := make(models.MapIpDomain)
	for _, ip := range allIPs {
		mid[ip.ip] = ip.domain
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
