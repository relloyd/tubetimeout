package nfq

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"time"

	"github.com/florianl/go-nfqueue"
	"github.com/mdlayher/netlink"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/group"
	"relloyd/tubetimeout/models"
	"relloyd/tubetimeout/monitor"
)

type packetIPs struct {
	src net.IP
	dst net.IP
}

type NFQueueFilter struct {
	Nfq    []*nfqueue.Nfqueue
	ut     models.TrackerI
	gm     group.ManagerI
	tc     monitor.TrafficCounter
	logger *zap.Logger
}

// NewNFQueueFilter creates a new nfqueue filtering outbound packets.
// The returned NFQueueFilter implements the IPListReceiver interface so this struct can be supplied with a list of
// Ip addresses for which to perform filtering.
// If the packets are destined for any of the injected Ips then filtering happens based on
// <LOGIC-TBC>
// TODO: unit test captuing two NFQs to ensure they are both created and running.
func NewNFQueueFilter(ctx context.Context, logger *zap.SugaredLogger, cfg *config.FilterConfig, ut models.TrackerI, gm group.ManagerI, tc monitor.TrafficCounter, fnRecover func(logger *zap.Logger)) (*NFQueueFilter, error) {
	var err error

	if cfg.PacketDropPercentage < 0 || cfg.PacketDropPercentage > 1 {
		return nil, fmt.Errorf("packet drop percentage must be between 0 and 100")
	}

	if ut == nil {
		return nil, fmt.Errorf("tracker must be supplied")
	}

	if gm == nil {
		return nil, fmt.Errorf("manager must be supplied")
	}

	if tc == nil {
		return nil, fmt.Errorf("counter must be supplied")
	}

	f := &NFQueueFilter{}
	f.logger = logger.Desugar()
	f.gm = gm
	f.ut = ut
	f.tc = tc

	nfq1, err := f.startNFQueueFilter(ctx, cfg, cfg.OutboundQueueNumber, models.Egress, fnRecover)
	if err != nil {
		return nil, err
	}

	nfq2, err := f.startNFQueueFilter(ctx, cfg, cfg.InboundQueueNumber, models.Ingress, fnRecover)
	if err != nil {
		return nil, err
	}

	f.Nfq = []*nfqueue.Nfqueue{nfq1, nfq2}

	return f, nil
}

func acceptPacket(logger *zap.Logger, nf *nfqueue.Nfqueue, id uint32) {
	err := nf.SetVerdict(id, nfqueue.NfAccept)
	if err != nil {
		logger.Error("Error setting verdict", zap.Error(err))
	}
}

func (f *NFQueueFilter) startNFQueueFilter(ctx context.Context, cfg *config.FilterConfig, queueNumber uint16, direction models.Direction, fnRecover func(logger *zap.Logger)) (*nfqueue.Nfqueue, error) {
	// Open a new NFQueue
	nf, err := nfqueue.Open(&nfqueue.Config{
		NetNS:        0,
		NfQueue:      queueNumber,
		MaxQueueLen:  4096, // 0xFFFF, // 65535
		MaxPacketLen: 4096, // we only need enough length for a packet which is MTU bounced to user space.
		Copymode:     nfqueue.NfQnlCopyPacket,
		Flags:        0,
		WriteTimeout: 15 * time.Millisecond, // TODO: align timeout with packet delay ms
		AfFamily:     unix.AF_INET,
		// ReadTimeout:  0,
		// WriteTimeout: 15 * time.Second,
		// Logger:       &log.Logger{},
		// Flags:        nfqueue.NfQaCfgFlagFailOpen,
	})

	if err != nil {
		return nil, fmt.Errorf("could not open nfqueue socket: %w", err)
	}

	// Avoid receiving ENOBUFS errors.
	if err := nf.SetOption(netlink.NoENOBUFS, true); err != nil {
		return nil, fmt.Errorf("failed to set netlink option %v: %w", netlink.NoENOBUFS, err)
	}

	fnPacketHandler := func(a nfqueue.Attribute) int {
		defer fnRecover(f.logger)

		var retval = 0 // 0 to continue the loop; 1 to exit cleanly; -1 to stop receiving messages

		id := *a.PacketID

		pips, l, err := getPacketIPs(a)
		if err != nil {
			f.logger.Error("Error getting packet data", zap.Error(err))
			acceptPacket(f.logger, nf, id)
			return 0 // 1 to exit clean; -1 to signal error; 0 to continue
		}

		// Check if the packet is for any of the resolved IPs.
		// TODO: add a tracker for each group as there may be many.
		var groups []models.Group
		var ok bool
		var decision string
		var verdict = nfqueue.NfAccept
		var proto = "proto-unknown"
		var srcIp, dstIp models.Ip

		protocol := (*a.Payload)[9] // Protocol field in IPv4
		if protocol == 6 {
			proto = "TCP"
		} else if protocol == 17 {
			proto = "UDP"
		}

		// TODO: test that source and dest IPs are reversed in filter for Egress vs Ingress.
		if direction == models.Egress { // if the direction is outbound...
			srcIp = models.Ip(pips.src.String())
			dstIp = models.Ip(pips.dst.String())
		} else { // else if the mode is inbound...
			// Expect the source and destination to be reversed.
			// Source IPs will be the public IPs that we added to our destination mapping.
			// Destinations IPs will be the local network.
			srcIp = models.Ip(pips.dst.String())
			dstIp = models.Ip(pips.src.String())
		}

		groups, ok = f.gm.IsSrcDestIpKnown(srcIp, dstIp) // check if the source and destination Ip addresses are known.
		if ok {                                          // if the packet IPs are known...
			for _, grp := range groups { // for each group...
				decision = "accept" // assume success
				active := f.tc.CountTraffic(grp, srcIp, direction, 1, l)
				f.ut.AddSample(string(grp), active)         // remember that we saw this group (optionally count the sample if active)
				if f.ut.HasExceededThreshold(string(grp)) { // if the threshold is exceeded for this group...
					if rand.Float32() < cfg.PacketDropPercentage || (proto == "UDP" && cfg.PacketDropUDP) { // if we should drop the packet...
						decision = "drop"
						verdict = nfqueue.NfDrop
					} else { // else introduce a delay for the packet and accept...
						if cfg.PacketDelayMs > 0 && rand.Float32() < cfg.PacketDelayPercentage {
							decision = "delay"
							time.Sleep(ApplyJitter(cfg.PacketDelayMs, cfg.PacketJitterMs)) // Delay the packet
						} else {
							decision = "accept"
						}
					}
				} // else accept the packet as the threshold is not exceeded...
				f.logger.Debug("handled packet",
					zap.String("decision", decision),
					zap.String("direction", string(direction)),
					zap.String("proto", proto),
					zap.Uint8("protocol-byte", protocol),
					zap.String("src", pips.src.String()),
					zap.String("dest", pips.dst.String()),
					zap.String("group", string(grp)),
					zap.Bool("active", active))
			}
		} else { // else accept the packet since the src/dest are not known...
			f.logger.Debug("Accept unregistered",
				zap.String("direction", string(direction)),
				zap.String("proto", proto),
				zap.String("src", pips.src.String()),
				zap.String("dest", pips.dst.String()))
		}

		err = nf.SetVerdict(id, verdict)
		if err != nil {
			f.logger.Error("Error setting verdict", zap.Error(err))
			retval = 0 // 1 to exit clean; -1 to signal error; 0 to continue
		}

		return retval
	}

	fnErrorHandler := func(err error) int {
		if err != nil { // if there is an error...
			if err := ctx.Err(); err == nil { // if the context is still active...
				f.logger.Error("NFQ error handler caught", zap.Error(err))
			}
		}
		return -1 // 1 to exit clean; -1 to signal error; 0 to continue
	}

	err = nf.RegisterWithErrorFunc(ctx, fnPacketHandler, fnErrorHandler)
	if err != nil {
		return nil, fmt.Errorf("error registering nfqueue callback: %w", err)
	}

	return nf, nil
}

// Source Ip (bytes 12-15 in IPv4 header)
// Destination Ip (bytes 16-19 in IPv4 header)
// getPacketIPs extracts the source and destination Ip addresses, and packet length from the packet payload.
func getPacketIPs(a nfqueue.Attribute) (packetIPs, int, error) {
	if a.Payload == nil { // if there's no payload...
		return packetIPs{}, 0, fmt.Errorf("payload is nil")
		// f.logger.Warn("Payload is nil for packet", zap.Uint32("id", id))
		// acceptPacket(f.logger, nf, id)
		// return 0 // 1 to exit clean; -1 to signal error; 0 to continue
	}

	payload := *a.Payload
	length := len(payload)

	if length < 20 { // if the payload is too short for ipv4 header...
		return packetIPs{}, 0, fmt.Errorf("payload too short for IPv4 header")
	}

	return packetIPs{
		src: payload[12:16],
		dst: payload[16:20],
	}, length, nil
}

// applyJitter generates a random delay based on a base delay and jitter range.
// Suggest ms values for baseDelayMs and jitterRangeMs.
func ApplyJitter(baseDelayMs, jitterRangeMs time.Duration) time.Duration {
	// Generate a random jitter in the range [-jitterRange, +jitterRange]
	jitter := time.Duration((rand.Float64()*2 - 1) * float64(jitterRangeMs))
	totalDelay := baseDelayMs + jitter
	// Ensure the delay is not negative
	if totalDelay < 0 {
		totalDelay = 0
	}
	return totalDelay
}
