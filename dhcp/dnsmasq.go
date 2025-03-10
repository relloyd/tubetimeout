package dhcp

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"

	"github.com/insomniacslk/dhcp/dhcpv4"
)

var (
	defaultInterfaceName     = "eth0"
	defaultLeaseDuration     = "12h"
	defaultGetConfig         = GetConfig
	defaultSetConfig         = SetConfig
	configFileDNSMasqService = "/etc/dnsmasq.conf"
	configFileDHCPSettings   = "dhcp-config.yaml"
	dnsMasqConfig            *DNSMasqConfig // expect NewServer() to set this up
	routeCmd                 = defaultRouteCmd
	routeCmdArgs             = []string{"-rn"}
)

type Server struct{}

func NewServer() (*Server, error) {
	var err error
	dnsMasqConfig, err = defaultGetConfig(config.MustGetLogger())
	if err != nil {
		return nil, fmt.Errorf("failed to get dnsmasq config: %w", err)
	}
	return &Server{}, nil
}

func (s *Server) GetConfig(logger *zap.SugaredLogger) (*DNSMasqConfig, error) {
	return defaultGetConfig(logger)
}

func (s *Server) SetConfig(logger *zap.SugaredLogger, cfg *DNSMasqConfig) error {
	return defaultSetConfig(logger, cfg)
}

// MaybeStartDnsmasq checks if it's okay to start dnsmasq based on config.
// If the service is config disabled then return false without an error.
// Return true if config wants dnsmasq started and there isn't already a DHCP server on the network.
// If there is a DHCP server on the network then return an error.
func (s *Server) MaybeStartDnsmasq(logger *zap.SugaredLogger) (bool, error) {
	if !isDNSMasqEnabledInConfig() {
		return false, nil
	}
	ifaceName, err := getPrimaryInterfaceName()
	if err != nil {
		return false, fmt.Errorf("failed to get primary interface: %w", err)
	}
	hwAddr, err := getIfaceHardwareAddress(ifaceName)
	numAttempts := 5
	running := false
	for idx := numAttempts; idx > 0; idx-- {
		running, err = isDHCPServerRunning(logger, hwAddr)
		if err != nil {
			logger.Warnf("Error while checking if DHCP server is running: %v", err)
			continue // proceed to the next attempt
		}
		if running {
			logger.Info("DHCP server is running, not starting dnsmasq")
			return false, fmt.Errorf("attempt to start dnsmasq when a DHCP server is already running")
		} else {
			logger.Info("DHCP server is not running, attempting to start dnsmasq")
			if err := startDnsmasq(logger, dnsMasqConfig); err != nil {
				logger.Warnf("Failed to start dnsmasq on attempt %d: %v", numAttempts-idx+1, err)
				continue // Retry in case of failure
			}
			logger.Info("Successfully started dnsmasq")
			return true, nil
		}
	}
	return false, fmt.Errorf("failed to start dnsmasq after %d attempts", numAttempts)
}

type systemctlAction string

const (
	systemctlStop    = "stop"
	systemctlStart   = "start"
	systemctlRestart = "restart"
)

// type IfaceAddrGetterFunc func(ifaceName string) (net.IP, error)

type DNSMasqConfig struct {
	mu                  *sync.Mutex
	DefaultGateway      net.IP   `json:"defaultGateway"`
	ThisGateway         net.IP   `json:"thisGateway"`
	LowerBound          net.IP   `json:"subnetRangeLowerIP"`
	UpperBound          net.IP   `json:"subnetRangeUpperIP"`
	AddressReservations []string `json:"addressReservations"`
	ServiceEnabled      bool     `json:"serviceEnabled"`
}

func newDNSMasqConfig() *DNSMasqConfig {
	return &DNSMasqConfig{
		mu:                  &sync.Mutex{},
		AddressReservations: make([]string, 0),
	}
}

func defaultRouteCmd() (string, error) {
	output, err := exec.Command("netstat", routeCmdArgs...).Output() // -n: show numerical addresses, -a: show all hosts
	return string(output), err
}

func GetConfig(logger *zap.SugaredLogger) (*DNSMasqConfig, error) {
	if dnsMasqConfig != nil { // if dns config is already loaded...
		return dnsMasqConfig, nil
	}

	// Load from file.
	mu := &sync.Mutex{}
	cfg, err := config.GetConfig[*DNSMasqConfig](mu, configFileDHCPSettings, newDNSMasqConfig)
	if err != nil {
		logger.Warnf("Failed to get dnsmasq config: %v", err)
		return nil, fmt.Errorf("failed to get dnsmasq config: %w", err)
	}

	// Assume defaults if empty.
	if cfg.DefaultGateway == nil {
		cfg.DefaultGateway, err = getDefaultGateway()
		if err != nil {
			logger.Warnf("Failed to get default gateway: %v", err)
			return nil, fmt.Errorf("failed to get default gateway: %w", err)
		}
	}

	ifaceName, err := getPrimaryInterfaceName()
	if err != nil {
		logger.Warnf("Failed to get primary interface (check your o/s is listed): %v", err)
		return nil, fmt.Errorf("failed to get primary interface (check your o/s is listed): %w", err)
	}

	if cfg.LowerBound == nil || cfg.UpperBound == nil || cfg.ThisGateway == nil {
		lowerBound, upperBound, err := getSubnetBoundsForInterface(ifaceName)
		if err != nil {
			logger.Warnf("Failed to get subnet range for interface %s: %v", ifaceName, err)
			return nil, fmt.Errorf("failed to get subnet range for interface %s: %w", ifaceName, err)
		}
		cfg.LowerBound, cfg.UpperBound, cfg.ThisGateway, err = adjustSubnetRange(lowerBound, upperBound, cfg.DefaultGateway)
		if err != nil {
			logger.Warnf("Failed to adjust subnet range for interface %s: %v", ifaceName, err)
			return nil, fmt.Errorf("failed to adjust subnet range for interface %s: %w", ifaceName, err)
		}
	}

	// Set the package global variable to the new value.
	dnsMasqConfig = cfg

	return cfg, nil
}

func SetConfig(logger *zap.SugaredLogger, cfg *DNSMasqConfig) error {
	if cfg == nil {
		return fmt.Errorf("supplied dnsmasq config is nil")
	}

	if cfg.mu == nil {
		cfg.mu = &sync.Mutex{}
	}

	err := config.SetConfig[*DNSMasqConfig](cfg.mu, configFileDHCPSettings, nil, nil, cfg) // TODO: validate the incoming config but don't override any yet
	if err != nil {
		return fmt.Errorf("failed to set dnsmasq config: %w", err)
	}

	// Set the package global variable to the new value.
	dnsMasqConfig = cfg

	return nil
}

func isDNSMasqEnabledInConfig() bool {
	if dnsMasqConfig != nil && dnsMasqConfig.ServiceEnabled {
		return true
	}
	return false
}

func getPrimaryInterfaceName() (string, error) {
	switch runtime.GOOS {
	case "linux":
		return "eth0", nil
	case "darwin":
		return "en0", nil
	default:
		return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// func getEth0IP() (net.IP, error) {
// 	ips, err := getIfaceAddresses(defaultInterfaceName)
// 	return ips[0], err
// }
//
// func getIfaceAddresses(ifaceName string) ([]net.IP, error) {
// 	ni, err := net.InterfaceByName(ifaceName)
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	addrs, err := ni.Addrs()
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	var ips []net.IP
// 	for _, addr := range addrs {
// 		var ip net.IP
// 		switch v := addr.(type) {
// 		case *net.IPNet:
// 			ip = v.IP
// 		case *net.IPAddr:
// 			ip = v.IP
// 		}
//
// 		if ip != nil && ip.To4() != nil {
// 			ips = append(ips, ip)
// 		}
// 	}
//
// 	if len(ips) == 0 {
// 		return nil, fmt.Errorf("no IPs found for interface %v", ifaceName)
// 	}
// 	return ips, nil
// }

// GetHardwareAddress returns the hardware (MAC) address of the given network interface.
// If the interface cannot be found or does not have a hardware address, it returns an error.
func getIfaceHardwareAddress(ifaceName string) (net.HardwareAddr, error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, err
	}
	if len(iface.HardwareAddr) == 0 {
		return nil, errors.New("interface does not have a hardware address")
	}
	return iface.HardwareAddr, nil
}

// isDHCPServerRunning sends a DHCP DISCOVER message and waits for a DHCP OFFER.
func isDHCPServerRunning(logger *zap.SugaredLogger, mac net.HardwareAddr) (bool, error) {
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
	conn.SetDeadline(time.Now().Add(waitDuration))
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
		logger.Infof("Received DHCPOFFER from DHCP server at %v", addr)
		return true, nil
	}

	return false, fmt.Errorf("received unexpected DHCP message type")
}

// getDefaultGateway reads /proc/net/route to obtain the default gateway for interface eth0.
func getDefaultGateway() (net.IP, error) {
	// Use "netstat -rn" for both macOS and Linux.
	// Assume routeCmd now takes the command name and its arguments.
	output, err := routeCmd()
	if err != nil {
		return nil, fmt.Errorf("failed to execute netstat command: %v", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		// Skip header lines and empty lines.
		if strings.HasPrefix(line, "Destination") || strings.HasPrefix(line, "Kernel") || line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// On macOS, the default route is marked as "default"
		// On Linux, it is usually marked as "0.0.0.0"
		if (runtime.GOOS == "darwin" && fields[0] == "default") ||
			(runtime.GOOS != "darwin" && fields[0] == "0.0.0.0") {
			ip := net.ParseIP(fields[1])
			if ip == nil {
				return nil, fmt.Errorf("failed to parse gateway IP: %s", fields[1])
			}
			return ip, nil
		}
	}

	if err = scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("default gateway not found")
}

// getSubnetBoundsForInterface queries the interface by name,
// finds the first IPv4 address, calculates the network and broadcast addresses,
// and returns the first usable IP (network + 1) and the last usable IP (broadcast - 1).
func getSubnetBoundsForInterface(ifaceName string) (net.IP, net.IP, error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get interface %s: %w", ifaceName, err)
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get addresses for interface %s: %w", ifaceName, err)
	}

	var ipnet *net.IPNet
	for _, addr := range addrs {
		if tmp, ok := addr.(*net.IPNet); ok && tmp.IP.To4() != nil {
			ipnet = tmp
			break
		}
	}
	if ipnet == nil {
		return nil, nil, errors.New("no IPv4 address found on interface")
	}

	// Compute the network IP by applying the mask.
	networkIP := ipnet.IP.Mask(ipnet.Mask)

	// Compute the broadcast address:
	// broadcast = network IP OR (inverse of subnet mask)
	broadcast := make(net.IP, len(networkIP))
	for i := 0; i < len(networkIP); i++ {
		broadcast[i] = networkIP[i] | ^ipnet.Mask[i]
	}

	// Convert to uint32 for simple arithmetic.
	networkUint := ipToUint32(networkIP)
	broadcastUint := ipToUint32(broadcast)

	if broadcastUint <= networkUint+1 {
		return nil, nil, errors.New("invalid subnet range, no usable addresses")
	}

	lowerIP := uint32ToIP(networkUint + 1)
	upperIP := uint32ToIP(broadcastUint - 1)

	return lowerIP, upperIP, nil
}

// adjustSubnetRange takes a lower IP, an upper IP and the default gateway IP,
// and returns a new subnet range that excludes the gateway IP, plus a sensible
// local IP (at the end of the range) that does not clash with the default gateway.
func adjustSubnetRange(lowerIP, upperIP, gateway net.IP) (net.IP, net.IP, net.IP, error) {
	// Convert IP addresses to uint32.
	lw := ipToUint32(lowerIP)
	up := ipToUint32(upperIP)
	gw := ipToUint32(gateway)

	// Ensure that the range is valid.
	if lw >= up {
		return nil, nil, nil, fmt.Errorf("invalid range: lower IP must be less than upper IP")
	}

	// If the gateway is not within the range, no adjustments are necessary.
	if gw < lw || gw > up {
		return lowerIP, upperIP, upperIP, nil
	}

	var newLower, newUpper, chosenIP uint32

	// Handle cases when the gateway is at the very beginning or end.
	if gw == lw {
		if lw+1 > up {
			return nil, nil, nil, fmt.Errorf("no usable addresses available after excluding the gateway")
		}
		newLower = lw + 1
		newUpper = up
		chosenIP = newUpper
	} else if gw == up {
		if up-1 < lw {
			return nil, nil, nil, fmt.Errorf("no usable addresses available after excluding the gateway")
		}
		newLower = lw
		newUpper = up - 1
		chosenIP = newUpper
	} else {
		// The gateway is somewhere in the middle.
		// Split the range into two segments:
		//  - Lower segment: [lw, gw-1]
		//  - Upper segment: [gw+1, up]
		segLowerSize := gw - lw // size of the lower segment
		segUpperSize := up - gw // size of the upper segment

		// Choose the segment with more addresses (or the upper segment if they are equal)
		if segUpperSize >= segLowerSize && segUpperSize > 0 {
			newLower = gw + 1
			newUpper = up
			chosenIP = newUpper
		} else if segLowerSize > 0 {
			newLower = lw
			newUpper = gw - 1
			chosenIP = newUpper
		} else {
			return nil, nil, nil, fmt.Errorf("no usable addresses available after excluding the gateway")
		}
	}

	return uint32ToIP(newLower), uint32ToIP(newUpper), uint32ToIP(chosenIP), nil
}

// ipToUint32 converts a net.IP (IPv4) to a uint32.
func ipToUint32(ip net.IP) uint32 {
	ip = ip.To4()
	return binary.BigEndian.Uint32(ip)
}

// uint32ToIP converts a uint32 to a net.IP (IPv4).
func uint32ToIP(n uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, n)
	return ip
}

// incrementIP returns a new IP incremented by one.
func incrementIP(ip net.IP) net.IP {
	ip = append(net.IP(nil), ip...)
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}
	return ip
}

// compareIP returns -1 if a < b, 0 if a == b, and 1 if a > b.
func compareIP(a, b net.IP) int {
	for i := 0; i < len(a); i++ {
		if a[i] < b[i] {
			return -1
		} else if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

// ChooseIPFromBottom picks the lowest IP in the range and updates the range.
func chooseIPFromBottom(lower, upper net.IP) (chosenIP, newLower, newUpper net.IP, err error) {
	if len(lower) != len(upper) || (lower.To4() == nil) != (upper.To4() == nil) {
		return nil, nil, nil, errors.New("IP versions (IPv4/IPv6) do not match")
	}
	if compareIP(lower, upper) > 0 {
		return nil, nil, nil, errors.New("lower IP is greater than upper IP")
	}
	chosenIP = append(net.IP(nil), lower...)
	newLower = incrementIP(lower)
	if compareIP(newLower, upper) > 0 { // if the range is now empty...
		return chosenIP, nil, nil, fmt.Errorf("range exhausted: lower IP is greater than upper IP")
	}
	return chosenIP, newLower, upper, nil
}

// generateDnsmasqConfig builds the full dnsmasq configuration as a string.
func generateDnsmasqConfig(defaultGateway, thisGateway, subnetLower, subnetUpper net.IP, thisGatewayHardwareAddress string, reservations []string, namedMACs []models.NamedMAC) (string, error) {
	// Global configuration settings.
	lines := []string{
		"# dnsmasq configuration generated programmatically",
		fmt.Sprintf("interface=%v", defaultInterfaceName),
		fmt.Sprintf("dhcp-range=%v,%v,%v", subnetLower, subnetUpper, defaultLeaseDuration),
		fmt.Sprintf("dhcp-option=3,%v", thisGateway),
		"",
	}

	// # Static IP reservations take the form:
	// dhcp-host=dc:a6:32:68:47:ea,192.168.1.52
	// dhcp-host=dc:a6:32:68:47:e9,192.168.1.53
	// dhcp-host=2c:cf:67:b6:37:7e,192.168.1.54
	// dhcp-host=58:ef:68:e5:f5:8c,192.168.1.55

	// Reserve an IP for thisGateway.
	lines = append(lines, "# static IP reservations")
	lines = append(lines, fmt.Sprintf("dhcp-host=%v,%v", thisGatewayHardwareAddress, thisGateway))
	lines = append(lines, "")

	// Configure a tag to use for custom host entries for each supplied known MAC; assign a tag and set a custom router.
	// TODO: consider given the real gateway to MACs not explicitly configured to use tubetimeout.
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

// setDnsmasqServiceState restarts the dnsmasq service so that the new config takes effect.
func setDnsmasqServiceState(action systemctlAction) error {
	cmd := exec.Command("sudo", "systemctl", string(action), "dnsmasq")
	return cmd.Run()
}

// EnableDnsmasq updates the dnsmasq configuration with the given named MACs and restarts the service.
// Aim is that supplied MACs will be assigned a custom gateway, while anything else
// gets the default gateway that the device running this code has.
func startDnsmasq(logger *zap.SugaredLogger, cfg *DNSMasqConfig) error {
	hwAddr, err := getIfaceHardwareAddress(defaultInterfaceName)
	if err != nil {
		return fmt.Errorf("error fetching hardware address for net adapter %v: %v", defaultInterfaceName, err)
	}

	dat, err := generateDnsmasqConfig(cfg.DefaultGateway, cfg.ThisGateway, cfg.LowerBound, cfg.UpperBound, hwAddr.String(), nil, nil)
	if err != nil {
		return fmt.Errorf("error generating dnsmasq config: %v", err)
	}

	// Write the configuration.
	if err := writeDnsmasqConfig(configFileDNSMasqService, dat); err != nil {
		return fmt.Errorf("error writing dnsmasq config: %v", err)
	}

	// Restart dnsmasq to apply the new configuration.
	if err := setDnsmasqServiceState(systemctlRestart); err != nil {
		return fmt.Errorf("error restarting dnsmasq: %v", err)
	}

	logger.Info("dnsmasq configuration updated and service restarted successfully")
	return nil
}
