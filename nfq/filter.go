package nfq

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"example.com/youtube-nfqueue/models"
	"example.com/youtube-nfqueue/usage"
	"github.com/florianl/go-nfqueue"
	"github.com/mdlayher/netlink"
)

type packetIPs struct {
	src net.IP
	dst net.IP
}

type NFQueueFilter struct {
	Nfq    *nfqueue.Nfqueue
	t      *usage.Tracker
	dstIps models.IpDomains
	srcIps models.IpMacGroups
}

// NewNFQueueFilter creates a new nfqueue filtering outbound packets.
// The returned NFQueueFilter implements the IPListReceiver interface so this struct can be supplied with a list of
// IP addresses for which to perform filtering.
// If the packets are destined for any of the injected Ips then filtering happens based on
// <LOGIC-TBC>
func NewNFQueueFilter(ctx context.Context, t *usage.Tracker) (*NFQueueFilter, error) {
	var err error
	f := &NFQueueFilter{}
	f.dstIps.Data = make(models.MapIpDomain)
	f.Nfq, err = f.startNFQueueFilter(ctx)
	f.t = t
	if err != nil {
		return nil, err
	}
	return f, nil
}

// UpdateIPDomains implements the IPListReceiver interface.
func (f *NFQueueFilter) UpdateIPDomains(newData models.MapIpDomain) {
	// TODO: don't trust the supplied map is good to just take as we want our own copy.
	f.dstIps.Mu.Lock()
	defer f.dstIps.Mu.Unlock()
	f.dstIps.Data = newData
}

func (f *NFQueueFilter) UpdateI pMacGroups(newData models.MapIpMacGroup) {
	// TODO: don't trust the supplied map is good to just take as we want our own copy.
	f.srcIps.Mu.Lock()
	defer f.srcIps.Mu.Unlock()
	f.srcIps.Data = newData
}

func (f *NFQueueFilter) IsDstIpKnown(ip net.IP) (models.Domain, bool) {
	f.dstIps.Mu.RLock()
	defer f.dstIps.Mu.RUnlock()
	d, ok := f.dstIps.Data[models.IP(ip.String())]
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
		var pips packetIPs
		var retval = 0 // 0 to accept, 1 to drop, -1 to stop receiving messages

		id := *a.PacketID

		if a.Payload == nil { // if there's no payload then accept the packet.
			log.Println("Payload is nil")
			err = nf.SetVerdict(id, nfqueue.NfAccept)
			if err != nil {
				log.Printf("Error setting verdict: %v\n", err)
				return -1
			}
			return 0
		}

		pips, err = getPacketIPs(a)
		if err != nil {
			log.Println(err)
			return -1
		}

		// Check if the packet is for any of the resolved IPs.
		d, ok := f.IsDstIpKnown(pips.dst)
		if ok { // if the packet destination IP address is known...
			// Remember that we saw it.
			f.t.AddSample(pips.src.String())                 // TODO: add a source group identifier to the tracker
			if f.t.HasExceededThreshold(pips.src.String()) { // if the threshold is exceeded for this source IP...
				// Drop the packet.
				log.Printf("Dropping packet (threshold breached) from %v to %v", pips.src, d)
				err = nf.SetVerdict(id, nfqueue.NfDrop)
				retval = 1
			} else { // else the threshold is not exceeded...
				// Accept the packet.
				log.Printf("Accepting packet from %v to %v", pips.src, d)
				err = nf.SetVerdict(id, nfqueue.NfAccept)
			}
		} else { // else the packet destination IP address is not known...
			// Accept the packet.
			log.Println("Accepting packet to unregistered destination:", pips.dst)
			err = nf.SetVerdict(id, nfqueue.NfAccept)
		}

		if err != nil {
			log.Printf("Error setting verdict: %v", err)
			retval = -1
		}
		return retval
	}

	fnErrorHandler := func(err error) int {
		if err != nil {
			fmt.Printf("error handler caught: %v\n", err)
			// TODO: decide how error handler should return 0 or -1 or cancel everything.
			// fnCancel() // cancel the context to stop the nfqueue
			// return -1
		}

		return -1 // to stop receiving messages return something different from 0.
	}

	err = nf.RegisterWithErrorFunc(ctx, fnPacketHandler, fnErrorHandler)
	if err != nil {
		return nil, fmt.Errorf("error registering nfqueue callback: %v", err)
	}

	return nf, nil
}

// Source IP (bytes 12-15 in IPv4 header)
// Destination IP (bytes 16-19 in IPv4 header)
// getPacketIPs extracts the source and destination IP addresses from the packet payload.
func getPacketIPs(a nfqueue.Attribute) (packetIPs, error) {
	// TODO: handle empty payload and return nf -> Accept
	payload := *a.Payload

	if len(payload) < 20 { // if the payload is too short for ipv4 header...
		return packetIPs{}, fmt.Errorf("payload too short for IPv4 header")
	}

	return packetIPs{
		src: payload[12:16],
		dst: payload[16:20],
	}, nil
}
