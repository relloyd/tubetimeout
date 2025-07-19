package dhcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"go.uber.org/zap"
)

// dhcpService implements the restarter interface.
type dhcpService struct{}

func (d *dhcpService) isDNSMasqEnabledInConfig(cfg *DNSMasqConfig) bool {
	if cfg != nil && cfg.ServiceEnabled {
		return true
	}
	return false
}

// isDHCPServerRunning sends a DHCP DISCOVER message and waits for a DHCP OFFER.
// Returns:
//
//	false if DHCP server is not running
//	true if DHCP server was found to be running
//	other errors in case of failure
func (d *dhcpService) isDHCPServerRunning(logger *zap.SugaredLogger, mac net.HardwareAddr) (localDetected bool, routerDetected bool, err error) {
	waitDuration := 5 * time.Second

	// Use ListenConfig with a Control function to set SO_REUSEADDR.
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var controlErr error
			err := c.Control(func(fd uintptr) {
				if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
					controlErr = fmt.Errorf("failed to set SO_REUSEADDR: %v", err)
					return
				}
				// Optionally, if your OS supports it, you can also enable SO_REUSEPORT.
				/*
					if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEPORT, 1); err != nil {
						controlErr = fmt.Errorf("failed to set SO_REUSEPORT: %v", err)
						return
					}
				*/
			})
			if err != nil {
				return err
			}
			return controlErr
		},
	}

	// Bind to UDP port 68 (DHCP client port)
	conn, err := lc.ListenPacket(context.Background(), "udp4", ":68")
	if err != nil {
		return false, false, fmt.Errorf("failed to bind to UDP port 68: %v", err)
	}
	defer conn.Close()

	// Create a DHCP DISCOVER message with broadcast option.
	msg, err := dhcpv4.NewDiscovery(mac, dhcpv4.WithBroadcast(true))
	if err != nil {
		return false, false, fmt.Errorf("failed to create DHCPDISCOVER message: %v", err)
	}

	// The DHCP server listens on port 67, so we send to the broadcast address.
	broadcastAddr := &net.UDPAddr{IP: net.IPv4bcast, Port: 67}
	_, err = conn.WriteTo(msg.ToBytes(), broadcastAddr)
	if err != nil {
		return false, false, fmt.Errorf("failed to send DHCPDISCOVER message: %v", err)
	}

	// Set a deadline to wait for responses.
	conn.SetDeadline(time.Now().Add(waitDuration))

	// Assume getLocalIP() is defined on the receiver (d) to return the local interface IP.
	localIP := d.getLocalIP()

	for {
		buf := make([]byte, 1500)
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			// If the error is a timeout, we're done collecting responses.
			var nErr net.Error
			if errors.As(err, &nErr) && nErr.Timeout() {
				break
			}
			// Return any unexpected errors.
			return localDetected, routerDetected, fmt.Errorf("error reading from UDP socket: %v", err)
		}

		// Parse the response into a DHCP message.
		resp, err := dhcpv4.FromBytes(buf[:n])
		if err != nil {
			logger.Warnf("Failed to parse DHCP response: %v", err)
			continue
		}

		// Only process DHCP OFFER messages.
		if resp.MessageType() != dhcpv4.MessageTypeOffer {
			logger.Warnf("Received unexpected DHCP message type: %v", resp.MessageType())
			continue
		}

		logger.Infof("Received DHCPOFFER from DHCP server at %v", addr)

		// Determine whether this offer originates from the local machine or from the router.
		if udpAddr, ok := addr.(*net.UDPAddr); ok {
			if udpAddr.IP.Equal(localIP) {
				localDetected = true
			} else {
				routerDetected = true
			}
		} else {
			logger.Warnf("Unable to determine source IP from address: %v", addr)
		}
	}

	return localDetected, routerDetected, nil
}

// EnableDnsmasq updates the dnsmasq configuration with the given named MACs and restarts the service.
// Aim is that supplied MACs will be assigned a custom gateway, while anything else
// gets the default gateway that the device running this code has.
func (d *dhcpService) startDnsmasq(logger *zap.SugaredLogger, cfg *DNSMasqConfig, ifaceName string, hwAddr net.HardwareAddr) (err error) {
	defer func() {
		if err != nil {
			if unSetErr := d.unsetStaticIP(logger, ifaceName); err != nil {
				err = fmt.Errorf("%v: also failed to unset static IP on interface %v: %w", err, ifaceName, unSetErr)
			}
		}
	}()

	if err = d.setStaticIP(logger, ifaceName, cfg, findSmallestSingleCIDR); err != nil {
		err = fmt.Errorf("startDnsmasq: %w", err)
		return
	}

	var dat string
	dat, err = generateDnsmasqConfig(ifaceName, cfg.ThisGateway, cfg.LowerBound, cfg.UpperBound, hwAddr.String(), cfg.DnsIPs, cfg.AddressReservations)
	if err != nil {
		err = fmt.Errorf("error generating dnsmasq config: %v", err)
		return
	}

	// Write the configuration.
	if err = writeDnsmasqConfig(configFileDNSMasqService, dat); err != nil {
		err = fmt.Errorf("error writing dnsmasq config: %v", err)
		return
	}

	// Restart dnsmasq to apply the new configuration.
	if err = d.setDnsmasqServiceState(serviceRestart); err != nil {
		err = fmt.Errorf("error restarting dnsmasq: %v", err)
		return
	}

	ok := false
	if ok, err = d.isDnsmasqServiceActive(); !ok {
		if err != nil {
			err = fmt.Errorf("dnsmasq should have started: %w", err)
		} else {
			err = fmt.Errorf("dnsmasq should have started")
		}
		return
	}

	logger.Info("Dnsmasq service started successfully")
	return
}

func (d *dhcpService) setStaticIP(logger *zap.SugaredLogger, ifaceName string, cfg *DNSMasqConfig, fnFinder cidrFinderFunc) error {
	logger = logger.With("mode", "setting static IP")

	// Example:
	// nmcli dev mod eth0 ipv4.method manual ipv4.gateway "192.168.1.254" ipv4.addr "192.168.1.230/24" ipv4.dns "8.8.8.8 1.1.1.1"

	if cfg == nil {
		return fmt.Errorf("no config provided")
	}

	_, cidr := fnFinder(cfg.LowerBound, cfg.UpperBound)

	var ipStrings []string
	for _, ip := range cfg.DnsIPs {
		ipStrings = append(ipStrings, ip.String())
	}

	cmd := "nmcli"
	args := []string{"dev", "mod", ifaceName,
		"ipv4.method", "manual",
		"ipv4.gateway", cfg.DefaultGateway.To4().String(),
		"ipv4.addr", cfg.ThisGateway.To4().String() + "/" + cidr,
		"ipv4.dns", strings.Join(ipStrings, " "),
		"ipv6.method", "disabled",
	}
	logger.Infof("Configuring device: %v %v", cmd, strings.Join(args, " "))
	output, err := exec.Command(cmd, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error setting static IP: %v: %v", string(output), err)
	}
	logger.Infof("Command output: %v", strings.TrimRight(string(output), "\n"))
	return nil
}

func (d *dhcpService) unsetStaticIP(logger *zap.SugaredLogger, ifaceName string) error {
	logger = logger.With("mode", "unsetting static IP")
	cmd := "nmcli"

	// Cleanup
	// nmcli dev mod eth0 ipv4.method auto ipv4.gateway "" ipv4.addr "" ipv4.dns ""
	args := []string{"dev", "mod", ifaceName,
		"ipv4.method", "auto",
		"ipv4.gateway", "",
		"ipv4.addr", "",
		"ipv4.dns", "",
	}
	logger.Infof("Configuring device: %v %v", cmd, strings.Join(args, " "))
	output, err := exec.Command(cmd, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error unsetting static IP: %v: %v", string(output), err)
	}

	// Apply
	// nmcli dev up eth0
	args = []string{
		"dev", "up", ifaceName,
	}
	logger.Infof("Upping device: %v %v", cmd, strings.Join(args, " "))
	output, err = exec.Command(cmd, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error unsetting static IP: %v: %v", string(output), err)
	}

	logger.Infof("Command output: %v", string(output))
	return nil
}

func (d *dhcpService) isDnsmasqServiceActive() (bool, error) {
	cmd := exec.Command("systemctl", "is-active", "dnsmasq")
	output, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(output))
	// Check output before err since return code 3 = "inactive" while 0 = "active".
	if outStr == "active" {
		return true, nil
	} else if outStr == "inactive" {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("error checking if dnsmasq service is enabled: %v: %v", string(output), err)
	}
	return false, nil
}

// setDnsmasqServiceState restarts the dnsmasq service so that the new config takes effect.
func (d *dhcpService) setDnsmasqServiceState(action systemctlAction) error {
	cmd := exec.Command("sudo", "systemctl", string(action), "dnsmasq")
	return cmd.Run()
}

func (d *dhcpService) getLocalIP() net.IP { // new method to get local IP
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			return ipnet.IP
		}
	}
	return nil
}

func isLocalIP(ip net.IP, localIP net.IP) bool { // helper function to compare IPs
	return ip.Equal(localIP)
}
