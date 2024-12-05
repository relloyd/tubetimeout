package domains

import (
	"context"
	"log"
	"net"
	"time"

	"example.com/youtube-nfqueue/models"
)

type DomainListReceiver interface {
	UpdateIPDomains(newIps models.MapIpDomain)
}

type resolver func(d []models.Domain)

type ipDomain struct {
	ip     models.IP
	domain models.Domain
}

var (
	defaultDomains            = []models.Domain{"www.youtube.com", "youtube.com", "googlevideo.com"}
	defaultResolver           = resolver(resolveDomains)
	defaultInterval             = time.Minute * 5
	registeredIPDomainReceivers []DomainListReceiver
	Ips                         = &models.IpDomains{Data: make(models.MapIpDomain)}
)

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

func resolveDomains(domains []models.Domain) {
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

	// Remove duplicates
	newIpSet := make(map[models.IP]models.Domain)
	for _, ip := range allIPs {
		newIpSet[ip.ip] = ip.domain
	}

	// Save them all.
	Ips.Mu.Lock()
	defer Ips.Mu.Unlock()
	Ips.Data = newIpSet
}

// notifyIPListReceivers duplicates the cachedIPs map per receiver and sends it.
func notifyIPListReceivers() {
	for _, receiver := range registeredIPDomainReceivers {
		newData := make(models.MapIpDomain)
		Ips.Mu.RLock()
		for k, v := range Ips.Data {
			newData[k] = v
		}
		receiver.UpdateIPDomains(newData)
		Ips.Mu.RUnlock()
	}
}

// PeriodicResolver starts a new ticket to resolve IP addresses for the packaged domains and sends a copy to any
// registered receivers.
func PeriodicResolver(ctx context.Context) {
	ticker := time.NewTicker(defaultInterval) // Update every 5 minutes
	defer ticker.Stop()
	// Initial resolve & notify.  // TODO: test that notifications happen at startup
	defaultResolver(defaultDomains)
	notifyIPListReceivers()
	// Periodically resolve.
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			defaultResolver(defaultDomains)
			notifyIPListReceivers()
		}
	}
}

func RegisterIPDomainReceivers(receiver ...DomainListReceiver) {
	for _, r := range receiver {
		if r != nil {
			registeredIPDomainReceivers = append(registeredIPDomainReceivers, r)
		}
	}
}
