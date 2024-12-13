package nfq

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"example.com/youtube-nfqueue/group"
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
	Nfq *nfqueue.Nfqueue
	t   *usage.Tracker
	g   *group.Manager
	// dstIps models.IpDomains
	// srcIps models.IpGroups
}

// NewNFQueueFilter creates a new nfqueue filtering outbound packets.
// The returned NFQueueFilter implements the IPListReceiver interface so this struct can be supplied with a list of
// Ip addresses for which to perform filtering.
// If the packets are destined for any of the injected Ips then filtering happens based on
// <LOGIC-TBC>
func NewNFQueueFilter(ctx context.Context, t *usage.Tracker, g *group.Manager) (*NFQueueFilter, error) {
	var err error
	f := &NFQueueFilter{}
	f.g = g
	f.t = t
	f.Nfq, err = f.startNFQueueFilter(ctx)
	if err != nil {
		return nil, err
	}
	return f, nil
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
			log.Printf("Error getting packet IPs: %v\n", err)
			return -1
		}

		// Check if the packet is for any of the resolved IPs.
		grps, ok := f.g.IsSrcDestIpKnown(models.Ip(pips.src.String()), models.Ip(pips.dst.String()))
		if ok { // if the packet source and destination IP address is known...
			for _, grp := range grps { // for each group
				// Remember that we saw it.
				// TODO: add a tracker for each group as there may be many.
				f.t.AddSample(string(grp))
				if f.t.HasExceededThreshold(string(grp)) { // if the threshold is exceeded for this group...
					// Drop the packet.
					log.Printf("Dropping packet from %v to %v (threshold breached for group %v)", pips.src, pips.dst, grp)
					err = nf.SetVerdict(id, nfqueue.NfDrop)
					retval = 1
				} else { // else the threshold is not exceeded...
					// Accept the packet.
					log.Printf("Accepting packet from %v to %v", pips.src, pips.dst)
					err = nf.SetVerdict(id, nfqueue.NfAccept)
				}
			}
		} else { // else the packet destination Ip address is not known...
			// Accept the packet.
			log.Printf("Accepting packet from %v to unregistered destination: %v", pips.src, pips.dst)
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

// Source Ip (bytes 12-15 in IPv4 header)
// Destination Ip (bytes 16-19 in IPv4 header)
// getPacketIPs extracts the source and destination Ip addresses from the packet payload.
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
