package domains

import (
	"context"
	"fmt"
	"net"
	"time"

	"example.com/youtube-nfqueue/models"
)

type IPListReceiver interface {
	Notify(newIps map[string]struct{})
}

type resolver func(d []domain)

type domain string

var (
	defaultDomains            = []domain{"www.youtube.com", "youtube.com", "googlevideo.com"}
	defaultResolver           = resolver(resolveDomains)
	defaultInterval           = time.Minute * 1
	registeredIPListReceivers []IPListReceiver
	Ips                       = &models.IpSet{Ips: make(map[string]struct{})}
)

func resolveOneDomain(domain domain) ([]string, error) {
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

func resolveDomains(domains []domain) {
	var allIPs []string

	for _, domain := range domains {
		ips, err := resolveOneDomain(domain)
		if err != nil {
			fmt.Printf("Failed to resolve %s: %v\n", domain, err)
			continue
		}
		allIPs = append(allIPs, ips...)
	}

	// Remove duplicates
	newIpSet := make(map[string]struct{})
	for _, ip := range allIPs {
		newIpSet[ip] = struct{}{}
	}

	// Save them all.
	Ips.Mu.Lock()
	defer Ips.Mu.Unlock()
	Ips.Ips = newIpSet

	fmt.Println("Resolved IPs:", newIpSet)
}

// notifyIPListReceivers duplicates the cachedIPs map per receiver and sends it.
func notifyIPListReceivers() {
	for _, receiver := range registeredIPListReceivers {
		newIps := make(map[string]struct{})
		Ips.Mu.RLock()
		for k, v := range Ips.Ips {
			newIps[k] = v
		}
		receiver.Notify(newIps)
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

func RegisterIPListReceivers(receiver ...IPListReceiver) {
	for _, r := range receiver {
		if r != nil {
			registeredIPListReceivers = append(registeredIPListReceivers, r)
		}
	}
}
