package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"testing"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"golang.org/x/net/ipv4"
)

// checksum computes the Internet checksum (RFC 1071).
func checksum(data []byte) uint16 {
	sum := uint32(0)
	// Sum 16-bit words.
	for i := 0; i < len(data)-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}
	// Add leftover byte, if any.
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}
	// Fold 32-bit sum to 16 bits.
	for (sum >> 16) > 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}
	return ^uint16(sum)
}

// udpChecksum computes the UDP checksum including the pseudo-header.
func udpChecksum(src, dst net.IP, udpHeader, payload []byte) uint16 {
	pseudo := make([]byte, 12)
	copy(pseudo[0:4], src.To4())
	copy(pseudo[4:8], dst.To4())
	pseudo[8] = 0
	pseudo[9] = 17 // UDP protocol
	udpLen := uint16(len(udpHeader) + len(payload))
	binary.BigEndian.PutUint16(pseudo[10:12], udpLen)

	total := append(pseudo, udpHeader...)
	total = append(total, payload...)
	// If total length is odd, pad with zero.
	if len(total)%2 != 0 {
		total = append(total, 0)
	}
	return checksum(total)
}

// getHwAddr returns the first non-loopback, up interface with a hardware address.
func getHwAddr() net.HardwareAddr {
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Fatalf("Unable to list interfaces: %v", err)
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		if len(iface.HardwareAddr) == 0 {
			continue
		}
		return iface.HardwareAddr
	}
	log.Fatal("No suitable interface found")
	return nil
}

func TestDHCP(t *testing.T) {
	// Obtain a hardware address.
	hwAddr := getHwAddr()
	fmt.Printf("Using interface with MAC: %s\n", hwAddr)

	// Create a DHCP DISCOVER packet.
	discover, err := dhcpv4.NewDiscovery(hwAddr)
	if err != nil {
		log.Fatalf("Failed to create DHCP DISCOVER packet: %v", err)
	}
	dhcpPayload := discover.ToBytes()

	// --- Build UDP Header ---
	// DHCP uses UDP source port 68 and destination port 67.
	udpLen := 8 + len(dhcpPayload)
	udpHeader := make([]byte, 8)
	binary.BigEndian.PutUint16(udpHeader[0:2], 68)       // Source port: 68
	binary.BigEndian.PutUint16(udpHeader[2:4], 67)       // Destination port: 67
	binary.BigEndian.PutUint16(udpHeader[4:6], uint16(udpLen))
	// Checksum initially 0.
	binary.BigEndian.PutUint16(udpHeader[6:8], 0)
	// Compute UDP checksum with pseudo-header (using 0.0.0.0 as src and 255.255.255.255 as dst).
	udpCsum := udpChecksum(net.IPv4zero, net.IPv4bcast, udpHeader, dhcpPayload)
	binary.BigEndian.PutUint16(udpHeader[6:8], udpCsum)

	// --- Build IP Header ---
	// IP header length is 20 bytes.
	ipTotalLen := 20 + udpLen
	ipHeader := &ipv4.Header{
		Version:  4,
		Len:      20,
		TOS:      0,
		TotalLen: ipTotalLen,
		ID:       rand.Intn(0xffff),
		Flags:    0,
		FragOff:  0,
		TTL:      64,
		Protocol: 17, // UDP
		Checksum: 0,  // To be computed.
		Src:      net.IPv4zero,   // 0.0.0.0 for DHCP DISCOVER.
		Dst:      net.IPv4bcast,  // Broadcast.
	}

	// Marshal header to get bytes and compute checksum.
	ipHeaderBytes, err := ipHeader.Marshal()
	if err != nil {
		log.Fatalf("Error marshaling IP header: %v", err)
	}
	ipHeader.Checksum = int(checksum(ipHeaderBytes))
	// Re-marshal with the checksum now set.
	ipHeaderBytes, err = ipHeader.Marshal()
	if err != nil {
		log.Fatalf("Error marshaling IP header with checksum: %v", err)
	}

	// Build the UDP datagram (header + DHCP payload).
	udpPacket := append(udpHeader, dhcpPayload...)

	// --- Open a Raw Socket ---
	// We open a raw IPv4 socket that will let us supply our own IP header.
	pc, err := net.ListenPacket("ip4:udp", "0.0.0.0")
	if err != nil {
		log.Fatalf("Failed to open raw socket: %v", err)
	}
	defer pc.Close()

	rawConn, err := ipv4.NewRawConn(pc)
	if err != nil {
		log.Fatalf("Failed to create raw connection: %v", err)
	}

	// Send the packet.
	if err := rawConn.WriteTo(ipHeader, udpPacket, nil); err != nil {
		log.Fatalf("Failed to send DHCP DISCOVER: %v", err)
	}
	fmt.Println("DHCP DISCOVER packet sent via raw socket.")

	// --- Listen for DHCP OFFER Responses ---
	// DHCP servers will respond from UDP port 67 to client port 68.
	// We use the same raw socket to capture responses.
	if err := pc.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		log.Fatalf("Failed to set deadline: %v", err)
	}

	for {
		buf := make([]byte, 1500)
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			if os.IsTimeout(err) {
				fmt.Println("Timeout waiting for DHCP OFFER responses.")
				break
			}
			log.Fatalf("Error reading from raw socket: %v", err)
		}

		// Parse the IP header.
		receivedIPHdr, err := ipv4.ParseHeader(buf[:n])
		if err != nil {
			log.Printf("Failed to parse IP header: %v", err)
			continue
		}

		// Ensure this is a UDP packet.
		if receivedIPHdr.Protocol != 17 {
			continue
		}

		// UDP header is located immediately after the IP header.
		if n < receivedIPHdr.Len+8 {
			continue
		}
		udpHdrBytes := buf[receivedIPHdr.Len : receivedIPHdr.Len+8]
		srcPort := binary.BigEndian.Uint16(udpHdrBytes[0:2])
		dstPort := binary.BigEndian.Uint16(udpHdrBytes[2:4])
		// Check for DHCP OFFER (from server port 67 to client port 68).
		if srcPort != 67 || dstPort != 68 {
			continue
		}

		// The DHCP payload follows the UDP header.
		dhcpData := buf[receivedIPHdr.Len+8 : n]
		dhcpPkt, err := dhcpv4.FromBytes(dhcpData)
		if err != nil {
			log.Printf("Failed to parse DHCP packet from %v: %v", addr, err)
			continue
		}
		if dhcpPkt.MessageType() == dhcpv4.MessageTypeOffer {
			fmt.Printf("Received DHCP OFFER from %v\n", addr)
		}
	}
}