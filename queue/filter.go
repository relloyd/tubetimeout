package queue

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"example.com/youtube-nfqueue/models"
	nfqueue "github.com/florianl/go-nfqueue"
	"github.com/mdlayher/netlink"
)

var (
	Ips = &models.IpSet{Ips: make(map[string]struct{})}
)

type packetIP struct {
	src net.IP
	dst net.IP
}

func ipIsResolved(ip net.IP) bool {
	Ips.Mu.RLock()
	defer Ips.Mu.RUnlock()
	_, ok := Ips.Ips[ip.String()]
	return ok
}

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

func parsePacketPayload(a nfqueue.Attribute) (packetIP, error) {
	// TODO: handle empty payload and return nf -> Accept
	payload := *a.Payload

	// Ensure we have enough data for an IPv4 header
	if len(payload) < 20 {
		return packetIP{}, fmt.Errorf("payload too short for IPv4 header")
	}

	return packetIP{
		src: payload[12:16],
		dst: payload[16:20],
	}, nil

	// Source IP (bytes 12-15 in IPv4 header)
	// srcIP := net.IP(payload[12:16])
	// fmt.Printf("Source IP: %s; ", srcIP)

	// Destination IP (bytes 16-19 in IPv4 header)
	// destIP := net.IP(payload[16:20])
	// fmt.Printf("Destination IP: %s\n", destIP)
}

func StartNFQueueFilter(ctx context.Context, fnCancel context.CancelFunc) (*nfqueue.Nfqueue, error) {
	// Set configuration options for nfqueue
	config := nfqueue.Config{
		NetNS:        0,
		NfQueue:      100,
		MaxQueueLen:  0xFF,
		MaxPacketLen: 0xFFFF,
		Copymode:     nfqueue.NfQnlCopyPacket,
		// Flags:        0,
		// AfFamily:     0,
		// ReadTimeout:  0,
		WriteTimeout: 15 * time.Millisecond,
		// Logger:       &log.Logger{},
	}

	// Open a new nfqueue
	nf, err := nfqueue.Open(&config)
	if err != nil {
		return nil, fmt.Errorf("could not open nfqueue socket: %v", err)
	}

	// Avoid receiving ENOBUFS errors.
	if err := nf.SetOption(netlink.NoENOBUFS, true); err != nil {
		return nil, fmt.Errorf("failed to set netlink option %v: %v", netlink.NoENOBUFS, err)
	}

	fnPacketHandler := func(a nfqueue.Attribute) int {
		var err error
		var ipData packetIP
		id := *a.PacketID

		if a.Payload == nil { // if there's no payload then accept the packet.
			fmt.Println("Payload is nil")
			err = nf.SetVerdict(id, nfqueue.NfAccept)
			if err != nil {
				fmt.Printf("error setting verdict: %v\n", err)
				return -1 // TODO: find out if this kills the nfqueue
			}
			return 0
		}

		// Decode the packet.
		// parseL2Hdr(a)
		// TODO: decide if this is ip4 or ip6 or some other packet type because there are ICMP and all sorts to deal with.
		// p := gopacket.NewPacket(*a.Payload, layers.LayerTypeIPv4, gopacket.Default)
		// p.Layer(layers.LayerTypeIPv4)

		// Just print out the id and payload of the nfqueue packet
		// fmt.Printf("[%d]:\t%v\n", id, *a.L2Hdr)
		// fmt.Printf("[%d]:\t%v\n", id, *a.Payload)

		// Check the hardware address of the packet.
		// if a.HwAddr != nil {
		// 	fmt.Printf("HwAddr: %v\n", *a.HwAddr)
		// }

		ipData, err = parsePacketPayload(a)
		if err != nil {
			fmt.Println(err)
			return -1 // TODO: find out if this kills the nfqueue
		}

		// Check if the packet is for any of the resolved IPs.
		if ipIsResolved(ipData.dst) {
			fmt.Println("Dropping packet to resolved IP:", ipData.dst)
			err = nf.SetVerdict(id, nfqueue.NfDrop)
		} else {
			fmt.Println("Accepting packet to:", ipData.dst)
			err = nf.SetVerdict(id, nfqueue.NfAccept)
		}

		if err != nil {
			fmt.Printf("error setting verdict: %v\n", err)
			return -1 // TODO: find out if this kills the nfqueue
		}

		return 0
	}

	fnErrorHandler := func(err error) int {
		if err != nil {
			fmt.Printf("error handler caught: %v\n", err)
			// fnCancel() // cancel the context to stop the nfqueue
			// return -1
		}

		return -1 // to stop receiving messages return something different than 0.
	}

	err = nf.RegisterWithErrorFunc(ctx, fnPacketHandler, fnErrorHandler)
	if err != nil {
		return nil, fmt.Errorf("error registering callback: %v", err)
	}

	return nf, nil
}
