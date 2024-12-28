package nfq

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"time"

	"example.com/tubetimeout/config"
	"example.com/tubetimeout/models"
	"example.com/tubetimeout/usage"
	"github.com/florianl/go-nfqueue"
	"github.com/mdlayher/netlink"
	"go.uber.org/zap"
)

const (
	packetDirectionOutbound packetDirection = "outbound"
	packetDirectionInbound  packetDirection = "inbound"
)

type ManagerI interface {
	IsSrcDestIpKnown(srcIp, dstIp models.Ip) ([]models.Group, bool)
}

type packetDirection string

type packetIPs struct {
	src net.IP
	dst net.IP
}

type NFQueueFilter struct {
	Nfq    *nfqueue.Nfqueue
	t      *usage.Tracker
	g      ManagerI
	logger *zap.Logger
}

// NewNFQueueFilter creates a new nfqueue filtering outbound packets.
// The returned NFQueueFilter implements the IPListReceiver interface so this struct can be supplied with a list of
// Ip addresses for which to perform filtering.
// If the packets are destined for any of the injected Ips then filtering happens based on
// <LOGIC-TBC>
func NewNFQueueFilter(ctx context.Context, logger *zap.SugaredLogger, cfg *config.FilterConfig, t *usage.Tracker, g ManagerI) (*NFQueueFilter, error) {
	var err error

	if cfg.PacketDropPercentage < 0 || cfg.PacketDropPercentage > 1 {
		return nil, fmt.Errorf("packet drop percentage must be between 0 and 100")
	}

	f := &NFQueueFilter{}
	f.logger = logger.Desugar()
	f.g = g
	f.t = t
	f.Nfq, err = f.startNFQueueFilter(ctx, cfg, cfg.OutboundQueueNumber, packetDirectionOutbound)
	if err != nil {
		return nil, err
	}
	f.Nfq, err = f.startNFQueueFilter(ctx, cfg, cfg.InboundQueueNumber, packetDirectionInbound)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (f *NFQueueFilter) startNFQueueFilter(ctx context.Context, cfg *config.FilterConfig, queueNumber uint16, mode packetDirection) (*nfqueue.Nfqueue, error) {
	// Open a new NFQueue
	nf, err := nfqueue.Open(&nfqueue.Config{
		NetNS:        0,
		NfQueue:      queueNumber,
		MaxQueueLen:  0xFF,
		MaxPacketLen: 0xFFFF,
		Copymode:     nfqueue.NfQnlCopyPacket,
		Flags:        0,
		WriteTimeout: 15 * time.Millisecond, // TODO: align timeout with packet delay ms
		// AfFamily:     0,
		// ReadTimeout:  0,
		// WriteTimeout: 15 * time.Second,
		// Logger:       &log.Logger{},
	})

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
		var retval = 0 // 0 to continue the loop; 1 to exit cleanly; -1 to stop receiving messages

		id := *a.PacketID

		if a.Payload == nil { // if there's no payload then accept the packet.
			f.logger.Warn("Payload is nil")
			err = nf.SetVerdict(id, nfqueue.NfAccept)
			if err != nil {
				f.logger.Error("Error setting verdict", zap.Error(err))
				return -1
			}
			return 0
		}

		pips, err = getPacketIPs(a)
		if err != nil {
			f.logger.Error("Error getting packet IPs", zap.Error(err))
			return -1
		}

		// Check if the packet is for any of the resolved IPs.
		// TODO: add a tracker for each group as there may be many.
		var groups []models.Group
		var ok bool
		var direction, decision string
		var verdict = nfqueue.NfAccept
		var proto = "proto-unknown"

		protocol := (*a.Payload)[9] // Protocol field in IPv4
		if protocol == 6 {
			proto = "TCP"
		} else if protocol == 17 {
			proto = "UDP"
		}

		if mode == packetDirectionOutbound { // if the mode is outbound...
			// Check if the source and destination Ip addresses are known.
			groups, ok = f.g.IsSrcDestIpKnown(models.Ip(pips.src.String()), models.Ip(pips.dst.String()))
			direction = "outbound"
		} else { // else if the mode is inbound...
			// Expect the source and destination to be reversed.
			// Source IPs will be the public IPs that we added to our destination mapping.
			// Destinations IPs will be the local network.
			groups, ok = f.g.IsSrcDestIpKnown(models.Ip(pips.dst.String()), models.Ip(pips.src.String()))
			direction = "inbound"
		}

		if ok { // if the packet IPs are known...
			for _, grp := range groups { // for each group...
				decision = "accept"                        // assume success
				f.t.AddSample(string(grp))                 // remember that we saw it
				if f.t.HasExceededThreshold(string(grp)) { // if the threshold is exceeded for this group...
					if rand.Float32() < cfg.PacketDropPercentage || (proto == "UDP" && cfg.PacketDropUDP) { // if we should drop the packet...
						decision = "drop"
						verdict = nfqueue.NfDrop
					} else { // else introduce a delay for the packet and accept...
						if cfg.PacketDelayMs > 0 {
							decision = "delay"
							time.Sleep(applyJitter(cfg.PacketDelayMs, cfg.PacketJitterMs)) // Delay the packet
						}
					}
				} // else accept the packet as the threshold is not exceeded...
				// f.logger.Debug("%v %v %v packet from %v to %v (group %v)", decision, direction, proto, pips.src, pips.dst, grp)
				f.logger.Debug("handled packet",
					zap.String("decision", decision),
					zap.String("direction", direction),
					zap.String("proto", proto),
					zap.String("src", pips.src.String()),
					zap.String("dest", pips.dst.String()),
					zap.String("group", string(grp)))
			}
		} else { // else accept the packet since the src/dest are not known...
			f.logger.Debug("Accept unregistered",
				zap.String("direction", direction),
				zap.String("proto", proto),
				zap.String("src", pips.src.String()),
				zap.String("dest", pips.dst.String()))
		}

		err = nf.SetVerdict(id, verdict)
		if err != nil {
			f.logger.Error("Error setting verdict: %v", zap.Error(err))
			retval = 1 // 1 to exit clean; -1 to signal error; 0 to continue
		}

		return retval
	}

	fnErrorHandler := func(err error) int {
		if err != nil { // if there is an error...
			if err := ctx.Err(); err == nil { // if the context is still active...
				f.logger.Error("NFQ error handler caught", zap.Error(err))
			}
			// TODO: decide how error handler should return 0 or -1 or cancel everything.
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

// applyJitter generates a random delay based on a base delay and jitter range.
// Suggest ms values for baseDelayMs and jitterRangeMs.
func applyJitter(baseDelayMs, jitterRangeMs time.Duration) time.Duration {
	// Generate a random jitter in the range [-jitterRange, +jitterRange]
	jitter := time.Duration((rand.Float64()*2 - 1) * float64(jitterRangeMs))
	totalDelay := baseDelayMs + jitter
	// Ensure the delay is not negative
	if totalDelay < 0 {
		totalDelay = 0
	}
	return totalDelay
}
