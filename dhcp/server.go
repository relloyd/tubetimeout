package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"log"
	"net"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/krolaw/dhcp4"
	"github.com/krolaw/dhcp4/conn"
)

// allowedMACs holds the MAC addresses that should receive the preferred gateway.
var allowedMACs = map[string]bool{
	"00:11:22:33:44:55": true,
	"66:77:88:99:AA:BB": true,
	// Add more allowed MAC addresses as needed.
}

// leaseInfo holds the allocated IP and its expiration time.
type leaseInfo struct {
	ip         net.IP
	expiration time.Time
}

// IPPool manages a pool of IPv4 addresses with lease tracking.
type IPPool struct {
	start     net.IP
	end       net.IP
	allocated map[string]leaseInfo // key: MAC address
	available []net.IP
	mutex     sync.Mutex
}

// NewIPPool creates an IP pool for addresses from start to end (inclusive).
func NewIPPool(start, end net.IP) *IPPool {
	pool := &IPPool{
		start:     start.To4(),
		end:       end.To4(),
		allocated: make(map[string]leaseInfo),
	}
	// Build the available IP list.
	for ip := cloneIP(pool.start); !ipAfter(ip, pool.end); ip = incrementIP(ip) {
		pool.available = append(pool.available, cloneIP(ip))
	}
	return pool
}

// Allocate returns an IP address for a given MAC address.
// If a lease exists and is still valid, it returns that IP.
func (p *IPPool) Allocate(mac string) net.IP {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Check if a lease exists.
	if lease, ok := p.allocated[mac]; ok {
		if time.Now().Before(lease.expiration) {
			return lease.ip
		}
		// Lease expired: reclaim the IP.
		p.available = append(p.available, lease.ip)
		delete(p.allocated, mac)
	}

	// Allocate a new IP if available.
	if len(p.available) == 0 {
		return nil
	}
	ip := p.available[0]
	p.available = p.available[1:]
	// Set lease duration to 2 hours.
	p.allocated[mac] = leaseInfo{
		ip:         ip,
		expiration: time.Now().Add(2 * time.Hour),
	}
	return ip
}

// Release frees the allocated IP for a given MAC address.
func (p *IPPool) Release(mac string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if lease, ok := p.allocated[mac]; ok {
		p.available = append(p.available, lease.ip)
		delete(p.allocated, mac)
	}
}

// PeriodicallyExpireLeases scans for expired leases and reclaims IPs.
func (p *IPPool) PeriodicallyExpireLeases(interval time.Duration) {
	ticker := time.NewTicker(interval)
	for range ticker.C {
		now := time.Now()
		p.mutex.Lock()
		for mac, lease := range p.allocated {
			if now.After(lease.expiration) {
				log.Printf("Lease for MAC %s with IP %s expired", mac, lease.ip)
				p.available = append(p.available, lease.ip)
				delete(p.allocated, mac)
			}
		}
		p.mutex.Unlock()
	}
}

// cloneIP returns a copy of an IP.
func cloneIP(ip net.IP) net.IP {
	return append(net.IP(nil), ip...)
}

// ipAfter checks whether ip is greater than end.
func ipAfter(ip, end net.IP) bool {
	return bytes.Compare(ip, end) > 0
}

// incrementIP returns the next IPv4 address.
func incrementIP(ip net.IP) net.IP {
	ip = cloneIP(ip).To4()
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}
	return ip
}

// myDHCPHandler implements the dhcp4.Handler interface.
type myDHCPHandler struct{}

// Global IP pool.
var ipPool *IPPool

// ServeDHCP processes incoming DHCP packets based on their MessageType.
func (h *myDHCPHandler) ServeDHCP(pkt dhcp4.Packet, msgType dhcp4.MessageType, options dhcp4.Options) dhcp4.Packet {
	mac := pkt.CHAddr().String()
	log.Printf("Received %s from %s", msgType, mac)

	switch msgType {
	case dhcp4.Discover:
		// Allocate IP and send an Offer.
		ip := ipPool.Allocate(mac)
		if ip == nil {
			log.Printf("No available IP for MAC %s", mac)
			return nil
		}
		reply := dhcp4.ReplyPacket(pkt, dhcp4.Offer, net.IPv4(192, 168, 1, 55), ip, 2*time.Hour, nil)
		addCommonOptions(reply, mac)
		return reply

	case dhcp4.Request:
		// Allocate (or re-use) IP and send an ACK.
		ip := ipPool.Allocate(mac)
		if ip == nil {
			log.Printf("No available IP for MAC %s", mac)
			return nil
		}
		reply := dhcp4.ReplyPacket(pkt, dhcp4.ACK, net.IPv4(192, 168, 1, 55), ip, 2*time.Hour, nil)
		addCommonOptions(reply, mac)
		return reply

	case dhcp4.Decline:
		log.Printf("Received Decline from %s; releasing any allocated IP", mac)
		ipPool.Release(mac)
		return nil

	case dhcp4.Release:
		log.Printf("Received Release from %s; releasing allocated IP", mac)
		ipPool.Release(mac)
		return nil

	case dhcp4.Inform:
		// For Inform, reply with an ACK containing configuration options.
		ciaddr := pkt.CIAddr()
		if ciaddr.Equal(net.IPv4zero) {
			// If the client IP is not set, try to allocate one.
			ip := ipPool.Allocate(mac)
			if ip != nil {
				ciaddr = ip
			}
		}
		reply := dhcp4.ReplyPacket(pkt, dhcp4.ACK, net.IPv4(192, 168, 1, 55), ciaddr, 0, nil)
		addCommonOptions(reply, mac)
		return reply

	default:
		log.Printf("Unhandled DHCP message type: %v", msgType)
		return nil
	}
}

// addCommonOptions adds standard DHCP options (subnet mask, lease time, router, DNS) to the reply.
func addCommonOptions(reply dhcp4.Packet, mac string) {
	// Subnet mask option.
	reply.AddOption(dhcp4.OptionSubnetMask, []byte(net.IPv4(255, 255, 255, 0).To4()))

	// Lease time option (if lease duration > 0).
	// Note: For DHCP Inform the lease time is 0.
	leaseTime := uint32(2 * 3600)
	leaseBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(leaseBytes, leaseTime)
	reply.AddOption(dhcp4.OptionIPAddressLeaseTime, leaseBytes)

	// Router (gateway) option based on allowed MAC addresses.
	if allowedMACs[mac] {
		reply.AddOption(dhcp4.OptionRouter, net.ParseIP("192.168.1.55").To4())
	} else {
		reply.AddOption(dhcp4.OptionRouter, net.ParseIP("192.168.1.254").To4())
	}

	// Domain Name Server (DNS) option with 1.1.1.1 and 8.8.8.8.
	dnsOption := append(net.ParseIP("1.1.1.1").To4(), net.ParseIP("8.8.8.8").To4()...)
	reply.AddOption(dhcp4.OptionDomainNameServer, dnsOption)
}

func main() {
	// Initialize your IP pool and DHCP handler as before.
	ipPool = NewIPPool(net.ParseIP("192.168.1.1"), net.ParseIP("192.168.1.253"))
	go ipPool.PeriodicallyExpireLeases(1 * time.Minute)
	handler := &myDHCPHandler{}

	// Create a UDP listener bound to your network interface on port 67.
	l, err := conn.NewUDP4FilterListener("eth0", ":67")
	if err != nil {
		log.Fatalf("Failed to create listener: %v", err)
	}

	// Set up a context that cancels on SIGINT or SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Run the DHCP server in a separate goroutine.
	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- dhcp4.Serve(l, handler)
	}()

	log.Println("DHCP server started on port 67")

	// Wait for a shutdown signal or an error from the server.
	select {
	case <-ctx.Done():
		log.Println("Shutdown signal received; shutting down DHCP server...")
		// Closing the listener will cause dhcp4.Serve() to return.
		l.Close()
		// Optionally wait for Serve() to return its error.
		err := <-serverErrCh
		if err != nil {
			log.Printf("DHCP server shutdown with error: %v", err)
		} else {
			log.Println("DHCP server shut down gracefully")
		}
	case err := <-serverErrCh:
		log.Fatalf("DHCP server error: %v", err)
	}
}