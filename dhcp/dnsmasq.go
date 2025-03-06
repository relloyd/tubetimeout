package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"

	"github.com/insomniacslk/dhcp/dhcpv4"
)

var (
	defaultInterfaceName = "eth0"
	defaultLeaseDuration = "12h"
	configFileDNSMasq    = "/etc/dnsmasq.conf"
	dnsMasqConfig        *DNSMasqConfig
)

type systemctlAction string

const (
	systemctlStop    = "stop"
	systemctlStart   = "start"
	systemctlRestart = "restart"
)

type DNSMasqConfig struct {
	mu                  *sync.Mutex
	DefaultGateway      net.IP   `json:"defaultGateway"`
	ThisGateway         net.IP   `json:"thisGateway"`
	LowerBound          net.IP   `json:"subnetRangeLowerIP"`
	UpperBound          net.IP   `json:"subnetRangeUpperIP"`
	AddressReservations []string `json:"addressReservations"`
}

func newDNSMasqConfig() *DNSMasqConfig {
	return &DNSMasqConfig{
		mu:                  &sync.Mutex{},
		AddressReservations: make([]string, 0),
	}
}

func init() {
	// at startup check if dhcp is enabled on the network.
	//   if it is then eth0 should have some defaults to get us started
	//   grab the gateway from the default route
	//   assume a subnet range
	//   this way, GetConfig will return some starter values
	// if dhcp is enabled then we don't want to start dnsmasq
	// if dhcp isn't enabled then we check if there are enough details to start dnsmasq.
	// assuming we have enough detail from eth0 we can persist config to load in the event that
	// neither dhcp server somewhere else nor dnsmasq are running.
	// we can set the IP of eth0 using config if we don't find DHCP running on the network.
	// if dhcp isn't running elsewhere and we have nothing to go with then we do nothing and expect someone to boot strap.

	// functions we need:
	//   ✅getEth0IP
	//   ✅getSubnetRangeFromInterface
	//   ✅getGateway
	//   getDHCPStatus
	//   ✅startDNSMasq
	//   ✅stopDNSMasq
	//   ✅restartDNSMasq
	//   ✅writeDnsmasqConfig
	//   ✅generateDNSMasqConfig
	//   getConfig for DNSMasq
	//   setConfig for DNSMasq
	//   isDNSMasqEnabledInConfig

}

func GetConfig() (*DNSMasqConfig, error) {
	// Load from file.
	cfg, err := config.GetConfig[*DNSMasqConfig](dnsMasqConfig.mu, configFileDNSMasq, newDNSMasqConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to get dnsmasq config: %w", err)
	}

	if cfg == nil { // if config is nil figure out defaults...

	}

	return cfg, nil
}

func SetConfig(cfg DNSMasqConfig) {

}

type IfaceAddrGetterFunc func(ifaceName string) (net.IP, error)

func getEth0IP() (net.IP, error) {
	ips, err := getIfaceAddresses(defaultInterfaceName)
	return ips[0], err
}

func getIfaceAddresses(ifaceName string) ([]net.IP, error) {
	ni, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, err
	}

	addrs, err := ni.Addrs()
	if err != nil {
		return nil, err
	}

	var ips []net.IP
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}

		if ip != nil && ip.To4() != nil {
			ips = append(ips, ip)
		}
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("no IPs found for interface %v", ifaceName)
	}
	return ips, nil
}

// checkDHCPServer sends a DHCP DISCOVER message and waits for a DHCP OFFER.
func checkDHCPServer(mac net.HardwareAddr) (bool, error) {
	// Bind to UDP port 68 (DHCP client port)
	conn, err := net.ListenPacket("udp4", ":68")
	if err != nil {
		return false, fmt.Errorf("failed to bind to UDP port 68: %v", err)
	}
	defer conn.Close()

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
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1500)
	n, addr, err := conn.ReadFrom(buf)
	if err != nil {
		return false, fmt.Errorf("no DHCP server response received: %v", err)
	}

	// Parse the response into a DHCP message.
	resp, err := dhcpv4.FromBytes(buf[:n])
	if err != nil {
		return false, fmt.Errorf("failed to parse DHCP response: %v", err)
	}

	// Check that the response is a DHCP OFFER.
	if msgType := resp.MessageType(); msgType == dhcpv4.MessageTypeOffer {
		fmt.Printf("Received DHCPOFFER from DHCP server at %v\n", addr)
		return true, nil
	}

	return false, fmt.Errorf("received unexpected DHCP message type")
}

// GetDefaultGateway reads /proc/net/route to obtain the default gateway for interface eth0.
func GetDefaultGateway() (string, error) {
	file, err := os.Open("/proc/net/route")
	if err != nil {
		return "", fmt.Errorf("failed to open /proc/net/route: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Skip header line.
	if !scanner.Scan() {
		return "", fmt.Errorf("failed to scan /proc/net/route")
	}
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		iface, dest, gateway := fields[0], fields[1], fields[2]
		// Check if this line is for eth0 and if it represents the default route.
		if iface != "eth0" || dest != "00000000" {
			continue
		}
		flags, err := strconv.ParseInt(fields[3], 16, 32) // Parse flags as hex.
		if err != nil {
			return "", fmt.Errorf("failed to parse flags: %v", err)
		}
		// Check that both RTF_UP (0x1) and RTF_GATEWAY (0x2) bits are set.
		if flags&0x3 != 0x3 {
			continue
		}
		// Parse the gateway from a hex string.
		gwHex, err := strconv.ParseUint(gateway, 16, 32)
		if err != nil {
			return "", fmt.Errorf("failed to parse gateway: %v", err)
		}
		// Convert the hex value (little-endian) to an IPv4 address.
		ip := net.IPv4(byte(gwHex&0xFF), byte((gwHex>>8)&0xFF), byte((gwHex>>16)&0xFF), byte((gwHex>>24)&0xFF))
		return ip.String(), nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("default gateway not found for eth0")
}

// func getSubnetBounds(interfaceName string) (net.IP, net.IP, error) {
// 	iface, err := net.InterfaceByName(interfaceName)
// 	if err != nil {
// 		return nil, nil, fmt.Errorf("failed to get interface: %w", err)
// 	}
//
// 	addrs, err := iface.Addrs()
// 	if err != nil {
// 		return nil, nil, fmt.Errorf("failed to get addresses: %w", err)
// 	}
//
// 	for _, addr := range addrs {
// 		ipNet, ok := addr.(*net.IPNet)
// 		if !ok || ipNet.IP.To4() == nil { // Ignore IPv6
// 			continue
// 		}
//
// 		ip := addr.Mask(ipNet.Mask) // Network (lower bound)
//
// 		// Compute the broadcast address (upper bound)
// 		broadcast := make(net.IP, len(ip))
// 		for i := 0; i < len(ip); i++ {
// 			broadcast[i] = ip[i] | ^ipNet.Mask[i]
// 		}
//
// 		return ip, broadcast, nil // TODO: should the upper bound be one less than the broadcast address?
// 	}
//
// 	return nil, nil, fmt.Errorf("no valid IPv4 address found on %s", interfaceName)
// }

// setDnsmasqServiceState restarts the dnsmasq service so that the new config takes effect.
func setDnsmasqServiceState(action systemctlAction) error {
	cmd := exec.Command("sudo", "systemctl", string(action), "dnsmasq")
	return cmd.Run()
}

// generateDnsmasqConfig builds the full dnsmasq configuration as a string.
func generateDnsmasqConfig(defaultGateway, thisGateway net.IP, subnetLower, subnetUpper net.IP, reservations []string, namedMACs []models.NamedMAC) (string, error) {
	// Global configuration settings.
	lines := []string{
		"# dnsmasq configuration generated programmatically",
		fmt.Sprintf("interface=%v", defaultInterfaceName),
		fmt.Sprintf("dhcp-range=%v,%v,%v", subnetLower, subnetUpper, defaultLeaseDuration),
		fmt.Sprintf("dhcp-option=3,%v", thisGateway),
		"",
	}

	// # Static IP reservations
	// dhcp-host=dc:a6:32:68:47:ea,192.168.1.52
	// dhcp-host=dc:a6:32:68:47:e9,192.168.1.53
	// dhcp-host=2c:cf:67:b6:37:7e,192.168.1.54
	// dhcp-host=58:ef:68:e5:f5:8c,192.168.1.55

	// Append custom host entries for each supplied known MAC; assign a tag and set a custom router.
	lines = append(lines, fmt.Sprintf("dhcp-option=tag:customgw,option:router,%s # this gateway", thisGateway))
	for _, v := range namedMACs {
		name := v.Name
		if name == "" {
			name = "un-named"
		}
		lines = append(lines, fmt.Sprintf("dhcp-host=%s,set:customgw # %v", v.MAC, name))
	}

	lines = append(lines, "")

	return strings.Join(lines, "\n"), nil
}

// writeDnsmasqConfig writes the generated config to the given file path.
func writeDnsmasqConfig(configPath string, configContent string) error {
	return os.WriteFile(configPath, []byte(configContent), 0644)
}

// EnableDnsmasq updates the dnsmasq configuration with the given named MACs and restarts the service.
// Aim is that supplied MACs will be assigned a custom gateway, while anything else
// gets the default gateway that the device running this code has.
// func EnableDnsmasq(logger *zap.SugaredLogger, namedMACs []models.NamedMAC) error {
// 	// Figure out the current IP address for the interface, to use it as the gateway.
// 	ips, err := getIfaceAddresses(defaultInterfaceName)
// 	if err != nil {
// 		return fmt.Errorf("error fetching IPs for net adapter %v: %v", defaultInterfaceName, err)
// 	}
//
// 	// Find lower/upper bounds for dhcp server.
//
// 	cfg, err := generateDnsmasqConfig(ips[0], namedMACs)
// 	if err != nil {
// 		return fmt.Errorf("error generating dnsmasq config: %v", err)
// 	}
//
// 	// Write the configuration.
// 	if err := writeDnsmasqConfig(configFileDNSMasq, cfg); err != nil {
// 		return fmt.Errorf("error writing dnsmasq config: %v", err)
// 	}
//
// 	// Restart dnsmasq to apply the new configuration.
// 	if err := setDnsmasqServiceState(systemctlRestart); err != nil {
// 		return fmt.Errorf("error restarting dnsmasq: %v", err)
// 	}
//
// 	logger.Info("dnsmasq configuration updated and service restarted successfully")
// 	return nil
// }
