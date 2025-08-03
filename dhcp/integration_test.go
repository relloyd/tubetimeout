//go:build integration

package dhcp

import (
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"relloyd/tubetimeout/config"
)

var (
	testIfaceName = "wlan0"
)

func TestSetStaticIP(t *testing.T) {
	cfg := &DNSMasqConfig{
		mu:                  &sync.Mutex{},
		DefaultGateway:      net.ParseIP("192.168.1.254"),
		ThisGateway:         net.ParseIP("192.168.1.253"),
		LowerBound:          net.ParseIP("192.168.1.1"),
		UpperBound:          net.ParseIP("192.168.1.253"),
		DnsIPs:              []string{"1.1.1.1", "8.8.8.8"},
		AddressReservations: nil,
		ServiceEnabled:      false,
	}
	err := setStaticIP(config.MustGetLogger(), testIfaceName, cfg, findSmallestSingleCIDR)
	assert.NoError(t, err, "unexpected error while setting static IP")
}

func TestUnsetStaticIP(t *testing.T) {
	err := unsetStaticIP(config.MustGetLogger(), testIfaceName)
	assert.NoError(t, err, "unexpected error while unsetting static IP")
}
