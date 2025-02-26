package main

import (
	"fmt"
	"io/ioutil"
	"net"
	"os/exec"
	"strings"

	"go.uber.org/zap"
	"relloyd/tubetimeout/models"
)

var (
	defaultInterfaceName    = "eth0"
	defaultSubnetRangeLower = "192.168.1.5"
	defaultSubnetRangeUpper = "192.168.1.250"
	defaultLeaseDuration    = "12h"
	dnsMasqConfigPath       = "/etc/dnsmasq.conf"
)

type IfaceAddrGetterFunc func(ifaceName string) (net.IP, error)

func getIfaceAddr(ifaceName string) (net.IP, error) {
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
	return ips[0], nil
}

// generateDnsmasqConfig builds the full dnsmasq configuration as a string.
func generateDnsmasqConfig(fnGetIfaceAddr IfaceAddrGetterFunc, namedMACs []models.NamedMAC) (string, error) {
	// Figure out the current IP address for the interface, as it is the custom gateway used below.
	thisGateway, err := fnGetIfaceAddr(defaultInterfaceName)
	if err != nil {
		return "", fmt.Errorf("error getting interface address: %v", err)
	}

	// Global configuration settings.
	lines := []string{
		"# dnsmasq configuration generated programmatically",
		"port=67",
		fmt.Sprintf("interface=%v", defaultInterfaceName),
		fmt.Sprintf("dhcp-range=%v,%v,%v", defaultSubnetRangeLower, defaultSubnetRangeUpper, defaultLeaseDuration),
		"",
	}

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
	return ioutil.WriteFile(configPath, []byte(configContent), 0644)
}

// restartDnsmasq restarts the dnsmasq service so that the new config takes effect.
func restartDnsmasq() error {
	cmd := exec.Command("sudo", "systemctl", "restart", "dnsmasq")
	return cmd.Run()
}

// UpdateDnsmasq updates the dnsmasq configuration with the given named MACs and restarts the service.
// Aim is that supplied MACs will be assigned a custom gateway, while anything else
// gets the default gateway that the device running this code has.
func UpdateDnsmasq(logger *zap.SugaredLogger, namedMACs []models.NamedMAC) error {
	// Generate the full dnsmasq config.
	cfg, err := generateDnsmasqConfig(getIfaceAddr, namedMACs)
	if err != nil {
		return fmt.Errorf("error generating dnsmasq config: %v", err)
	}

	// Write the configuration.
	if err := writeDnsmasqConfig(dnsMasqConfigPath, cfg); err != nil {
		return fmt.Errorf("error writing dnsmasq config: %v", err)
	}

	// Restart dnsmasq to apply the new configuration.
	if err := restartDnsmasq(); err != nil {
		return fmt.Errorf("error restarting dnsmasq: %v", err)
	}

	logger.Info("dnsmasq configuration updated and service restarted successfully")
	return nil
}
