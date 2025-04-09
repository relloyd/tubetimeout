package dhcp

import (
	"context"
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

func (d *dhcpService) isDNSMasqEnabledInConfig() bool {
	if dnsMasqConfig != nil && dnsMasqConfig.ServiceEnabled {
		return true
	}
	return false
}

// isDHCPServerRunning sends a DHCP DISCOVER message and waits for a DHCP OFFER.
// Returns:
//
//	false if DHCP server is not running
//	errDHCPServerNotRunning if no DHCP server was found running at all
//	true if DHCP server was found to be running
//	other errors in case of failure
func (d *dhcpService) isDHCPServerRunning(logger *zap.SugaredLogger, mac net.HardwareAddr) (bool, error) {
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
				// Uncomment the following lines if desired:
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
		return false, fmt.Errorf("failed to bind to UDP port 68: %v", err)
	}
	defer func(conn net.PacketConn) {
		_ = conn.Close()
	}(conn)

	// Create a DHCP DISCOVER message with broadcast option.
	msg, err := dhcpv4.NewDiscovery(mac, dhcpv4.WithBroadcast(true))
	if err != nil {
		return false, fmt.Errorf("failed to create DHCPDISCOVER message: %v", err)
	}

	// The DHCP server listens on port 67, so we send to the broadcast address.
	broadcastAddr := &net.UDPAddr{IP: net.IPv4bcast, Port: 67}
	_, err = conn.WriteTo(msg.ToBytes(), broadcastAddr)
	if err != nil {
		return false, fmt.Errorf("failed to send DHCPDISCOVER message: %v", err)
	}

	// Set a deadline to wait for a response.
	conn.SetDeadline(time.Now().Add(waitDuration))
	buf := make([]byte, 1500)
	n, addr, err := conn.ReadFrom(buf)
	if err != nil { // if we timed out and suppose there is no DHCP server running...
		return false, nil // alternatively return errDHCPServerNotRunning for the positive false case.
	}

	// Parse the response into a DHCP message.
	resp, err := dhcpv4.FromBytes(buf[:n])
	if err != nil {
		return false, fmt.Errorf("failed to parse DHCP response: %v", err)
	}

	// Check that the response is a DHCP OFFER.
	if msgType := resp.MessageType(); msgType == dhcpv4.MessageTypeOffer {
		logger.Infof("Received DHCPOFFER from DHCP server at %v", addr)
		return true, nil
	}

	return false, fmt.Errorf("received unexpected DHCP message type")
}

// EnableDnsmasq updates the dnsmasq configuration with the given named MACs and restarts the service.
// Aim is that supplied MACs will be assigned a custom gateway, while anything else
// gets the default gateway that the device running this code has.
func (d *dhcpService) startDnsmasq(logger *zap.SugaredLogger, cfg *DNSMasqConfig) error {
	ifaceName, err := getPrimaryInterfaceName()
	if err != nil {
		return fmt.Errorf("error fetching primary interface name: %v", err)
	}
	hwAddr, err := getIfaceHardwareAddress(ifaceName)
	if err != nil {
		return fmt.Errorf("error fetching hardware address for net adapter %v: %v", defaultInterfaceName, err)
	}

	dat, err := generateDnsmasqConfig(cfg.DefaultGateway, cfg.ThisGateway, cfg.LowerBound, cfg.UpperBound, hwAddr.String(), cfg.DnsIPs, cfg.AddressReservations)
	if err != nil {
		return fmt.Errorf("error generating dnsmasq config: %v", err)
	}

	// Write the configuration.
	if err := writeDnsmasqConfig(configFileDNSMasqService, dat); err != nil {
		return fmt.Errorf("error writing dnsmasq config: %v", err)
	}

	// Restart dnsmasq to apply the new configuration.
	if err := d.setDnsmasqServiceState(serviceRestart); err != nil {
		return fmt.Errorf("error restarting dnsmasq: %v", err)
	}

	logger.Info("dnsmasq configuration updated and service restarted successfully")
	return nil
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
	}
	logger.Info("configuring device: ", cmd, strings.Join(args, " "))
	output, err := exec.Command(cmd, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error setting static IP: %v: %v", string(output), err)
	}
	logger.Infof("command output: %v", strings.TrimRight(string(output), "\n"))
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
	logger.Info("configuring device: ", cmd, strings.Join(args, " "))
	output, err := exec.Command(cmd, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error unsetting static IP: %v: %v", string(output), err)
	}

	// Apply
	// nmcli dev up eth0
	args = []string{
		"dev", "up", ifaceName,
	}
	logger.Info("upping device: ", cmd, strings.Join(args, ""))
	output, err = exec.Command(cmd, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("error unsetting static IP: %v: %v", string(output), err)
	}

	logger.Infof("command output: %v", string(output))
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
