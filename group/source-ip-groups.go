package group

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"example.com/tubetimeout/config"
	"example.com/tubetimeout/models"
	"go.uber.org/zap"
)

const (
	defaultGroupName = "default"
)

func init() {
	err := checkARPAvailability()
	if err != nil {
		config.MustGetLogger().Fatalf("Error: %v. Please ensure the 'arp' command is installed and available on your PATH.", err)
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

// arpCommand is a function type for executing the ARP command
type arpCommand func() (string, error)

var (
	// ARPCmd is the default ARP command
	ARPCmd = func() (string, error) {
		output, err := exec.Command("arp", "-n", "-a").Output() // TODO: check compatibility with Linux
		return string(output), err
	}
)

type FuncGroupMacsLoader func() (config.GroupConfig, error)

var groupMacsLoaderFunc = FuncGroupMacsLoader(config.LoadGroupMACs)

type SourceIpGroupsReceiver interface {
	UpdateSourceIpGroups(newData models.MapIpGroups)
}

// NetWatcher manages ARP scanning and registered callbacks
type NetWatcher struct {
	logger         *zap.SugaredLogger
	sourceIpGroups models.MapIpGroups
	callbacks      []SourceIpGroupsReceiver
	mutex          sync.RWMutex
}

// NewNetWatcher creates a new NetWatcher instance
func NewNetWatcher(logger *zap.SugaredLogger) *NetWatcher {
	return &NetWatcher{
		logger:         logger,
		sourceIpGroups: make(map[models.Ip][]models.Group),
		callbacks:      []SourceIpGroupsReceiver{},
	}
}

// RegisterSourceIpGroupsReceivers registers a callback to be called on updates
func (nw *NetWatcher) RegisterSourceIpGroupsReceivers(receivers ...SourceIpGroupsReceiver) {
	nw.mutex.Lock()
	defer nw.mutex.Unlock()
	nw.callbacks = append(nw.callbacks, receivers...)
}

// Start begins the periodic ARP scanning process and supports cancellation using context
// TODO: add a test to check that scanNetworkAndSaveResults is called immediately and repeatedly.
func (nw *NetWatcher) Start(ctx context.Context) {
	scanNetworkAndSaveResults(nw)
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				scanNetworkAndSaveResults(nw)
			}
		}
	}()
}

func scanNetworkAndSaveResults(nw *NetWatcher) {
	// Perform ARP scan and get updated map
	newMapIpGroups := scanNetwork(nw.logger, ARPCmd) // Empty map returned if no groups are set up.

	// Compare with existing data
	nw.mutex.Lock()
	// TODO: return all IPs if there is an error loading the YAML data.
	if managerModeMatchAllSourceIps || !maps.EqualFunc(nw.sourceIpGroups, newMapIpGroups, func(m1 []models.Group, m2 []models.Group) bool {
		return slices.Equal(m1, m2)
	}) { // if there is new arp data or if we are defaulting to all source IPs...
		// TODO: stop always notifying everyone when in managerModeMatchAllSourceIps mode.
		nw.sourceIpGroups = newMapIpGroups
		// Send IpGroups to all registered callbacks.
		for _, cb := range nw.callbacks {
			cb.UpdateSourceIpGroups(newMapIpGroups)
		}
	}
	nw.mutex.Unlock()
}

// scanNetwork performs an ARP scan and maps MAC addresses to IPs
func scanNetwork(logger *zap.SugaredLogger, arpCmd arpCommand) models.MapIpGroups {
	// Load YAML data
	gm, err := groupMacsLoaderFunc()
	if errors.Is(err, config.ErrorGroupMacFileNotFound) { // if there is an error loading the YAML data...
		// Log the error and configure all IPs subject to all groups.
		logger.Warnf("Source IPs will be tracked individually. MAC-Groups file not configured: %v", err)
		managerModeMatchAllSourceIps = true
	} else if err != nil {
		logger.Errorf("Source IPs will be tracked individually. Unexpected error loading MAC-Groups: %v", err)
		managerModeMatchAllSourceIps = true
	} else {
		managerModeMatchAllSourceIps = false
	}
	// TODO: add tests to check that managerModeMatchAllSourceIps is set correctly when the YAML file is missing or has an error.
	// TODO: add tests to check that managerModeMatchAllSourceIps is set correctly when the YAML file is added.

	// Initialize map
	mig := make(map[models.Ip][]models.Group)

	// Execute ARP scan
	output, err := arpCmd()
	if err != nil {
		logger.Errorf("Error running ARP command: %v", err)
		return nil
	}

	// Parse ARP output
	arpLines := strings.Split(output, "\n")
	for _, line := range arpLines {
		fields := strings.Fields(line)
		if len(fields) < 3 { // if the line can be skipped...
			continue
		}

		arpIp := strings.Trim(fields[1], "()") // field zero may be '?' as the hostnames haven't been looked up
		arpMAC := fields[3]

		if managerModeMatchAllSourceIps && gm.GroupMACs == nil { // if there are no groups of MACs found...
			// Set each source IP into the default group.
			mig[models.Ip(arpIp)] = []models.Group{defaultGroupName}
		} else {
			// Find group for MAC
			for group, macs := range gm.GroupMACs {
				for _, gmac := range macs {
					if gmac == arpMAC {
						groups := mig[models.Ip(arpIp)]                             // retrieve existing groups for the IP.
						mig[models.Ip(arpIp)] = append(groups, models.Group(group)) // append the new group to the existing groups.
					}
				}
			}
		}
	}

	return mig
}
