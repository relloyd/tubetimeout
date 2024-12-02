package queue

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"example.com/youtube-nfqueue/models"
	"example.com/youtube-nfqueue/tracker"
	nfqueue "github.com/florianl/go-nfqueue"
	"github.com/mdlayher/netlink"
)

type packetIP struct {
	src net.IP
	dst net.IP
}

type NFQueueFilter struct {
	Nfq *nfqueue.Nfqueue
	t   *tracker.Tracker
	models.IpSet
}

// NewNFQueueFilter creates a new nfqueue filtering outbound packets.
// The returned NFQueueFilter implements the IPListReceiver interface so this stuct can be supplied with a list of
// IP addresses for which to perform filtering.
// If the packets are destined for any of the injected Ips then filtering happens based on
// <LOGIC-TBC>
func NewNFQueueFilter(ctx context.Context, t *tracker.Tracker) (*NFQueueFilter, error) {
	var err error
	f := &NFQueueFilter{}
	f.Ips = make(models.MapIpDomain)
	f.Nfq, err = f.startNFQueueFilter(ctx)
	f.t = t
	if err != nil {
		return nil, err
	}
	return f, nil
}

// Notify implements the IPListReceiver interface.
func (f *NFQueueFilter) Notify(newIps models.MapIpDomain) {
	// TODO: don't trust the supplied map is good to just take as we want our own copy.
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.Ips = newIps
}

func (f *NFQueueFilter) ipIsKnown(ip net.IP) (models.Domain, bool) {
	f.Mu.RLock()
	defer f.Mu.RUnlock()
	d, ok := f.Ips[models.IP(ip.String())]
	return d, ok
}

func (f *NFQueueFilter) startNFQueueFilter(ctx context.Context) (*nfqueue.Nfqueue, error) {
	// Set configuration options for nfqueue
	config := nfqueue.Config{
		NetNS:        0,
		NfQueue:      100,
		MaxQueueLen:  0xFF,
		MaxPacketLen: 0xFFFF,
		Copymode:     nfqueue.NfQnlCopyPacket,
		Flags:        0,
		// AfFamily:     0,
		// ReadTimeout:  0,
		WriteTimeout: 15 * time.Millisecond,
		// WriteTimeout: 15 * time.Second,
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

		log.Printf("Received packet with ID: %d\n", id)

		if a.Payload == nil { // if there's no payload then accept the packet.
			fmt.Println("Payload is nil")
			err = nf.SetVerdict(id, nfqueue.NfAccept)
			if err != nil {
				fmt.Printf("error setting verdict: %v\n", err)
				return -1 // TODO: find out if this kills the nfqueue
			}
			return 0
		}

		ipData, err = getPacketIPs(a)
		if err != nil {
			fmt.Println(err)
			return -1 // TODO: find out if this kills the nfqueue
		}

		// Check if the packet is for any of the resolved IPs.
		d, ok := f.ipIsKnown(ipData.dst)
		if ok { // if the packet destination IP address is known...
			// Remember that we saw it.
			f.t.AddSample(ipData.src.String()) // TODO: add a source group identifier to the tracker
			if f.t.HasExceededThreshold(ipData.src.String()) {
				log.Printf("Dropping packet to %v (%v) threshold breached", d, ipData.dst)
				err = nf.SetVerdict(id, nfqueue.NfDrop)
			} else {
				fmt.Println("Accepting packet within threshold to known destination:", ipData.dst)
				err = nf.SetVerdict(id, nfqueue.NfAccept)
			}
		} else {
			fmt.Println("Accepting packet to unregistered destination:", ipData.dst)
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
			// TODO: decide how error handler should return 0 or -1 or cancel everything.
			// fnCancel() // cancel the context to stop the nfqueue
			// return -1
		}

		return -1 // to stop receiving messages return something different than 0.
	}

	err = nf.RegisterWithErrorFunc(ctx, fnPacketHandler, fnErrorHandler)
	if err != nil {
		return nil, fmt.Errorf("error registering nfqueue callback: %v", err)
	}

	return nf, nil
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

// Source IP (bytes 12-15 in IPv4 header)
// Destination IP (bytes 16-19 in IPv4 header)
// getPacketIPs extracts the source and destination IP addresses from the packet payload.
func getPacketIPs(a nfqueue.Attribute) (packetIP, error) {
	// TODO: handle empty payload and return nf -> Accept
	payload := *a.Payload

	if len(payload) < 20 { // if the payload is too short for ipv4 header...
		return packetIP{}, fmt.Errorf("payload too short for IPv4 header")
	}

	return packetIP{
		src: payload[12:16],
		dst: payload[16:20],
	}, nil
}
