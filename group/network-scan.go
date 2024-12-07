package group

import (
	"fmt"
	"log"
	"os/exec"
	"strings"

	"example.com/youtube-nfqueue/config"
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
func ScanNetwork(yamlPath string, arpCmd ARPCommand) models.MapIpGroups {
	// Load YAML data
	gm, err := config.LoadGroupMACs(yamlPath)
	if err != nil {
		fmt.Printf("Error loading YAML: %v\n", err)
		return nil
	}

	// Initialize map
	mig := make(map[models.IP]models.Groups)

	// Execute ARP scan
	output, err := arpCmd()
	if err != nil {
		fmt.Printf("Error running ARP command: %v\n", err)
		return nil
	}

	// Parse ARP output
	arpLines := strings.Split(output, "\n")
	for _, line := range arpLines {
		fields := strings.Fields(line)
		if len(fields) < 3 { // if the line can be skipped...
			continue
		}

		ip := strings.Trim(fields[1], "()") // field zero may be '?' as the hostnames haven't been looked up
		arpMAC := fields[3]

		// Find group for MAC
		for group, macs := range gm.GroupMACs {
			for _, gmac := range macs {
				if gmac == arpMAC {
					groups := mig[models.IP(ip)]
					mig[models.IP(ip)] = append(groups, group)
				}
			}
		}
	}

	return mig
}
