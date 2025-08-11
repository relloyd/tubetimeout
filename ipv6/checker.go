package ipv6

import (
	"context"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
)

var (
	checker *Checker
)

type Checker struct {
	ipv6Enabled bool
	mu          sync.RWMutex
}

type Status struct {
	Enabled bool `json:"enabled"`
}

func NewIPv6Checker(ctx context.Context, log *zap.SugaredLogger) *Checker {
	if checker != nil {
		return checker
	}

	checker = &Checker{
		mu: sync.RWMutex{},
	}

	t := time.NewTicker(15 * time.Second)

	go func() {
		for {
			select {
			case <-t.C:
				v := isIPv6Enabled()
				checker.mu.Lock()
				checker.ipv6Enabled = v
				checker.mu.Unlock()
				log.Debug("IPv6 detected: ", checker.ipv6Enabled)
			case <-ctx.Done():
				t.Stop()
				return
			}
		}
	}()

	return checker
}

func (c *Checker) IsEnabled() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Status{
		Enabled: c.ipv6Enabled,
	}
}

// isIPv6Enabled tries to dial a public IPv6 address.
// Returns true if connection is successful, else false.
func isIPv6Enabled() bool {
	// Cloudflare's public DNS IPv6 address, port 53 (DNS)
	const ipv6Addr = "[2606:4700:4700::1111]:53"
	timeout := 2 * time.Second

	conn, err := net.DialTimeout("udp", ipv6Addr, timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
