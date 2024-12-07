package group

import (
	"context"
	"fmt"
	"log"
	"maps"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"example.com/youtube-nfqueue/config"
	"example.com/youtube-nfqueue/models"
)

func init() {
	err := checkARPAvailability()
	if err != nil {
		log.Fatalf("Error: %v. Please ensure the 'arp' command is installed and available on your PATH.", err)
	}
}

type MapIpGroupsReceiver interface {
	UpdateIpGroups(newData models.MapIpGroups)
}

// NetWatcher manages ARP scanning and registered callbacks
type NetWatcher struct {
	ipGroups  models.MapIpGroups
	callbacks []MapIpGroupsReceiver
	mutex       sync.RWMutex
}

// NewNetWatcher creates a new NetWatcher instance
func NewNetWatcher() *NetWatcher {
	return &NetWatcher{
		ipGroups:  make(map[models.IP]models.Groups),
		callbacks: []MapIpGroupsReceiver{},
	}
}

// RegisterIpMacGroupReceivers registers a callback to be called on updates
func (nw *NetWatcher) RegisterIpMacGroupReceivers(callback ...MapIpGroupsReceiver) {
	nw.mutex.Lock()
	defer nw.mutex.Unlock()
	nw.callbacks = append(nw.callbacks, callback...)
}

// Start begins the periodic ARP scanning process and supports cancellation using context
func (nw *NetWatcher) Start(ctx context.Context, yamlPath string) {
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		for {
			select {
			case <-ctx.Done():
				// Context was canceled, exit the loop
				return
			case <-ticker.C:
				// Perform ARP scan and get updated map
				newMapIpGroups := scanNetwork(yamlPath, ARPCmd)

				// Compare with existing data
				nw.mutex.Lock()
				if !maps.EqualFunc(nw.ipGroups, newMapIpGroups, func(m1 models.Groups, m2 models.Groups) bool {
					return slices.Equal(m1, m2)
				}) { // if there is new arp data...
					nw.ipGroups = newMapIpGroups
					// UpdateIPDomains all registered callbacks
					for _, cb := range nw.callbacks {
						cb.UpdateIpGroups(newMapIpGroups)
					}
				}
				nw.mutex.Unlock()
			}
		}
	}()
}

// arpCommand is a function type for executing the ARP command
type arpCommand func() (string, error)

var (
	// ARPCmd is the default ARP command
	ARPCmd = func() (string, error) {
		output, err := exec.Command("arp", "-n", "-a").Output() // TODO: check compatibility with Linux
		return string(output), err
	}
)

func checkARPAvailability() error {
	// Check if the `arp` command is available
	_, err := exec.LookPath("arp")
	if err != nil {
		return fmt.Errorf("arp command not found on the system: %w", err)
	}
	return nil
}

// scanNetwork performs an ARP scan and maps MAC addresses to IPs
func scanNetwork(yamlPath string, arpCmd arpCommand) models.MapIpGroups {
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
