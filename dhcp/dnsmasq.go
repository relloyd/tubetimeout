package main

import (
	"fmt"
	"io/ioutil"
	"net"
	"os/exec"
	"strings"

	"go.uber.org/zap"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"
)

type systemctlAction string

const (
	systemctlStop    = "stop"
	systemctlStart   = "start"
	systemctlRestart = "restart"
)

var (
	defaultInterfaceName = "eth0"
	defaultLeaseDuration = "12h"
	configFileDNSMasq    = "/etc/dnsmasq.conf"
	configFileSettings   = ""
)

type IfaceAddrGetterFunc func(ifaceName string) (net.IP, error)

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

// writeDnsmasqConfig writes the generated config to the given file path.
func writeDnsmasqConfig(configPath string, configContent string) error {
	return ioutil.WriteFile(configPath, []byte(configContent), 0644)
}

// dnsmasqServiceSetState restarts the dnsmasq service so that the new config takes effect.
func dnsmasqServiceSetState(action systemctlAction) error {
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

func getSubnetBounds(interfaceName string) (net.IP, net.IP, error) {
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get interface: %w", err)
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get addresses: %w", err)
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.To4() == nil { // Ignore IPv6
			continue
		}

		ip := ipNet.IP.Mask(ipNet.Mask) // Network (lower bound)

		// Compute the broadcast address (upper bound)
		broadcast := make(net.IP, len(ip))
		for i := 0; i < len(ip); i++ {
			broadcast[i] = ip[i] | ^ipNet.Mask[i]
		}

		return ip, broadcast, nil // TODO: should the upper bound be one less than the broadcast address?
	}

	return nil, nil, fmt.Errorf("no valid IPv4 address found on %s", interfaceName)
}

// UpdateDnsmasq updates the dnsmasq configuration with the given named MACs and restarts the service.
// Aim is that supplied MACs will be assigned a custom gateway, while anything else
// gets the default gateway that the device running this code has.
func UpdateDnsmasq(logger *zap.SugaredLogger, namedMACs []models.NamedMAC) error {
	// Figure out the current IP address for the interface, to use it as the gateway.
	ips, err := getIfaceAddresses(defaultInterfaceName)
	if err != nil {
		return fmt.Errorf("error fetching IPs for net adapter %v: %v", defaultInterfaceName, err)
	}

	// Find lower/upper bounds for dhcp server.

	cfg, err := generateDnsmasqConfig(ips[0], namedMACs)
	if err != nil {
		return fmt.Errorf("error generating dnsmasq config: %v", err)
	}

	// Write the configuration.
	if err := writeDnsmasqConfig(configFileDNSMasq, cfg); err != nil {
		return fmt.Errorf("error writing dnsmasq config: %v", err)
	}

	// Restart dnsmasq to apply the new configuration.
	if err := dnsmasqServiceSetState(systemctlRestart); err != nil {
		return fmt.Errorf("error restarting dnsmasq: %v", err)
	}

	logger.Info("dnsmasq configuration updated and service restarted successfully")
	return nil
}

type DNSMasqConfig struct {
	DefaultGateway      net.IP   `json:"defaultGateway"`
	ThisGateway         net.IP   `json:"thisGateway"`
	LowerBound          net.IP   `json:"subnetRangeLowerIP"`
	UpperBound          net.IP   `json:"subnetRangeUpperIP"`
	AddressReservations []string `json:"addressReservations"`
}

func GetConfig() {
	// Load from file.
	config.FnDefaultCreateAppHomeDirAndGetConfigFilePath()
}

func SetConfig(cfg DNSMasqConfig) {

}
