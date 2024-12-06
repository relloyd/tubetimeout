package nfq

import (
	"fmt"
	"net"

	"github.com/florianl/go-nfqueue"
)

func parseL2Hdr(a nfqueue.Attribute) {
	if a.L2Hdr != nil && len(*a.L2Hdr) < 14 { // Minimum Ethernet header size
		fmt.Println("L2Hdr is too short")
		return
	} else if a.L2Hdr == nil {
		fmt.Println("L2Hdr is nil")
		return
	}

	// Destination MAC: bytes 0-5
	destMAC := net.HardwareAddr((*a.L2Hdr)[0:6])
	// Source MAC: bytes 6-11
	srcMAC := net.HardwareAddr((*a.L2Hdr)[6:12])
	// EtherType: bytes 12-13
	etherType := uint16((*a.L2Hdr)[12])<<8 | uint16((*a.L2Hdr)[13])

	fmt.Printf("Destination MAC: %s\t", destMAC)
	fmt.Printf("Source MAC: %s\t", srcMAC)
	fmt.Printf("EtherType: 0x%04X\t", etherType)

	// Check for IPv4 or IPv6 EtherType
	switch etherType {
	case 0x0800:
		fmt.Println("Payload contains an IPv4 packet")
	case 0x86DD:
		fmt.Println("Payload contains an IPv6 packet")
	default:
		fmt.Println("Unknown EtherType")
	}
}
