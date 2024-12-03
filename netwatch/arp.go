package netwatch

import (
	"fmt"
	"log"
	"os/exec"
	"strings"

	"example.com/youtube-nfqueue/models"
)

// ARPCommand is a function type for executing the ARP command
type ARPCommand func() (string, error)

var (
	// ARPCmd is the default ARP command
	ARPCmd = func() (string, error) {
		output, err := exec.Command("arp", "-n", "-a").Output() // TODO: check compatibility with Linux
		return string(output), err
	}
)

func init() {
	err := checkARPAvailability()
	if err != nil {
		log.Fatalf("Error: %v. Please ensure the 'arp' command is installed and available on your PATH.", err)
	}
}

func checkARPAvailability() error {
	// Check if the `arp` command is available
	_, err := exec.LookPath("arp")
	if err != nil {
		return fmt.Errorf("arp command not found on the system: %w", err)
	}
	return nil
}

// ScanNetwork performs an ARP scan and maps MAC addresses to IPs
func ScanNetwork(yamlPath string, arpCmd ARPCommand) map[models.IP]MACGroup {
	// Load YAML data
	cfg, err := LoadMACGroups(yamlPath)
	if err != nil {
		fmt.Printf("Error loading YAML: %v\n", err)
		return nil
	}

	// Initialize map
	ipMap := make(map[models.IP]MACGroup)

	// Execute ARP scan
	output, err := ARPCmd()
	if err != nil {
		fmt.Printf("Error running ARP command: %v\n", err)
		return nil
	}

	// Parse ARP output
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		ip := fields[1] // field zero may be '?' as the hostnames haven't been looked up
		mac := fields[2]

		// Find group for MAC
		for group, macs := range cfg.Groups {
			for _, gmac := range macs {
				if gmac == mac {
					ipMap[models.IP(ip)] = MACGroup{MAC: mac, Group: group}
					break
				}
			}
		}
	}

	return ipMap
}
