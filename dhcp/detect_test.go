package dhcp

import (
	"fmt"
	"log"
	"net"
	"testing"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
)

// getHwAddr returns the first non-loopback interface with a hardware address.
func getHwAddr() net.HardwareAddr {
	interfaces, err := net.Interfaces()
	if err != nil {
		log.Fatalf("failed to get network interfaces: %v", err)
	}
	for _, iface := range interfaces {
		// Skip loopback or down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		if len(iface.HardwareAddr) == 0 {
			continue
		}
		return iface.HardwareAddr
	}
	log.Fatal("no suitable network interface found")
	return nil
}

func TestDHCP(t *testing.T) {
	// Obtain a hardware (MAC) address from a suitable interface.
	hwAddr := []byte("92:97:24:fc:c7:58") // getHwAddr()
	fmt.Printf("Using interface with hardware address: %s\n", hwAddr)

	// Create a DHCP DISCOVER packet.
	discover, err := dhcpv4.NewDiscovery(hwAddr)
	if err != nil {
		log.Fatalf("failed to create DHCP DISCOVER packet: %v", err)
	}

	// Listen on UDP port 68 (the DHCP client port).
	// This may require elevated privileges.
	conn, err := net.ListenPacket("udp4", ":68")
	if err != nil {
		log.Fatalf("failed to bind on UDP port 68: %v", err)
	}
	defer conn.Close()

	// Broadcast the DISCOVER packet to port 67 (the DHCP server port).
	broadcastAddr := &net.UDPAddr{
		IP:   net.IPv4bcast,
		Port: 67,
	}
	n, err := conn.WriteTo(discover.ToBytes(), broadcastAddr)
	if err != nil {
		log.Fatalf("failed to send DHCP DISCOVER: %v", err)
	}
	fmt.Printf("Sent %d bytes to broadcast address %s\n", n, broadcastAddr)

	// Listen for DHCP OFFER responses.
	// We wait a few seconds for responses.
	buf := make([]byte, 1500)
	deadline := time.Now().Add(5 * time.Second)
	for {
		// Set the read deadline for the packet.
		conn.SetReadDeadline(deadline)
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			// Exit loop on timeout.
			if nErr, ok := err.(net.Error); ok && nErr.Timeout() {
				fmt.Println("No more responses received.")
				break
			}
			log.Fatalf("error reading response: %v", err)
		}

		// Parse the received DHCP packet.
		pkt, err := dhcpv4.FromBytes(buf[:n])
		if err != nil {
			log.Printf("failed to parse DHCP packet from %v: %v", addr, err)
			continue
		}

		// Check if this is a DHCP OFFER.
		if pkt.MessageType() == dhcpv4.MessageTypeOffer {
			fmt.Printf("Detected DHCP server at %v\n", addr)
			// Optionally, break if one detection is sufficient:
			// break
		}
	}
}
