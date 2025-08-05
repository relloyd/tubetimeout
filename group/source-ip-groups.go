package group

import (
	"context"
	"errors"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"
)

const (
	defaultGroupName = "default"
)

func init() {
	cmd := "arp"
	err := config.CheckCmdAvailability(cmd)
	if err != nil {
		config.MustGetLogger().Fatalf("Error: %v. Please ensure the '%v' command is installed and available on your PATH.", cmd, err)
	}
}

// arpCommand is a function type for executing the ARP command
type arpCommand func() (string, error)

var (
	ARPCmd              = config.ARPCmd // ARPCmd is the default ARP command
	groupMacsLoaderFunc = funcGroupMacsLoader(config.GroupMACs.GetConfig)
)

type funcGroupMacsLoader func(logger *zap.SugaredLogger) (config.GroupMACsConfig, error)

// NetWatcher manages ARP scanning and registered callbacks
type NetWatcher struct {
	logger               *zap.SugaredLogger
	sourceIpGroups       models.MapIpGroups
	callbacksForIpGroups []models.SourceIpGroupsReceiver
	callbacksForIpMACs   []models.SourceIpMACReceiver
	mu                   sync.Mutex
}

// NewNetWatcher creates a new NetWatcher instance
func NewNetWatcher(logger *zap.SugaredLogger) *NetWatcher {
	return &NetWatcher{
		logger:               logger,
		sourceIpGroups:       make(map[models.Ip][]models.Group),
		callbacksForIpGroups: []models.SourceIpGroupsReceiver{},
	}
}

// RegisterSourceIpGroupsReceivers registers a callback to be called on updates
func (nw *NetWatcher) RegisterSourceIpGroupsReceivers(receivers ...models.SourceIpGroupsReceiver) {
	nw.mu.Lock()
	defer nw.mu.Unlock()
	nw.callbacksForIpGroups = append(nw.callbacksForIpGroups, receivers...)
}

func (nw *NetWatcher) RegisterSourceIpMACReceivers(receivers ...models.SourceIpMACReceiver) {
	nw.mu.Lock()
	defer nw.mu.Unlock()
	nw.callbacksForIpMACs = append(nw.callbacksForIpMACs, receivers...)
}

// Start begins the periodic ARP scanning process and supports cancellation using context
// TODO: add a test to check that scanNetworkAndNotify is called immediately and repeatedly.
func (nw *NetWatcher) Start(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		scanNetworkAndNotify(nw)
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				scanNetworkAndNotify(nw)
			}
		}
	}()
}

// TODO: stop always notifying everyone when in managerModeMatchAllSourceIps mode.
func scanNetworkAndNotify(nw *NetWatcher) {
	// Perform ARP scan and get updated map
	newMapIpGroups, newMapIpMACs := scanNetwork(nw.logger, ARPCmd) // Empty map returned if no groups are set up.

	nw.logger.Debugf("ARP scan results: %v", newMapIpGroups)

	nw.mu.Lock()
	defer nw.mu.Unlock()

	// TODO: return all IPs if there is an error loading the YAML data.
	if managerModeMatchAllSourceIps || !maps.EqualFunc(nw.sourceIpGroups, newMapIpGroups, func(m1 []models.Group, m2 []models.Group) bool {
		return slices.Equal(m1, m2)
	}) { // if there is new arp data or if we are defaulting to all source IPs...
		// Send IpGroups to all registered callbacks.
		nw.logger.Infof("ARP scan detected changes in source IPs: %v", newMapIpGroups)
		nw.sourceIpGroups = newMapIpGroups
		for _, cb := range nw.callbacksForIpGroups { // for each callback...
			cb.UpdateSourceIpGroups(duplicateMap(newMapIpGroups)) // send a copy of the new IP groups.
		}
		nw.logger.Debugf("ARP scan notified %d callbacks", len(nw.callbacksForIpGroups))
	}

	if newMapIpMACs != nil && len(newMapIpMACs) > 0 { // if there are any IP-MACs to notify downstream...
		for _, cb := range nw.callbacksForIpMACs { // for each callback...
			cb.UpdateSourceIpMACs(duplicateMap(newMapIpMACs)) // send a copy of the new IP-MACs.
		}
		// TODO: add test for UpdateSourceIpMACs() being called after arp scan.
	} else {
		nw.logger.Errorf("no IP-MAC data found to send downstream (usage stats will not work)")
	}
}

// scanNetwork performs an ARP scan and maps MAC addresses to IPs
func scanNetwork(logger *zap.SugaredLogger, arpCmd arpCommand) (models.MapIpGroups, models.MapIpMACs) {
	// Load YAML data each time.
	gm, err := groupMacsLoaderFunc(logger)
	if errors.Is(err, config.ErrorGroupMacFileNotFound) { // if there is an error loading the YAML data...
		// Log the error and configure all IPs subject to all groups.
		logger.Warnf("Source IPs will be tracked individually. MAC-Groups file not configured: %v", err)
		managerModeMatchAllSourceIps = true
	} else if err != nil {
		logger.Errorf("Source IPs will be tracked individually. Unexpected error loading MAC-Groups: %v", err)
		managerModeMatchAllSourceIps = true // TODO: turn off auto match all IPs when none are registered as we don't want things not explicitly included to be impacted by NFT rules.
	} else {
		managerModeMatchAllSourceIps = false
	}

	// TODO: add tests to check that managerModeMatchAllSourceIps is set correctly when the YAML file is missing or has an error.
	// TODO: add tests to check that managerModeMatchAllSourceIps is set correctly when the YAML file is added.

	// Initialize maps
	mig := make(map[models.Ip][]models.Group)
	mim := make(map[models.Ip]models.MAC)

	// Execute ARP scan
	output, err := arpCmd()
	if err != nil {
		logger.Errorf("Error running ARP command: %v", err)
		return nil, nil
	}

	var macRegex = regexp.MustCompile(`(?i)^(?:[0-9A-F]{2}[:-]){5}[0-9A-F]{2}$`)

	// Parse ARP output
	arpLines := strings.Split(output, "\n")
	for _, line := range arpLines {
		fields := strings.Fields(line)
		if len(fields) < 3 { // if the line can be skipped...
			continue
		}

		arpIp := strings.Trim(fields[1], "()") // field zero may be '?' as the hostnames haven't been looked up.
		arpMAC := fields[3]

		if !macRegex.Match([]byte(arpMAC)) { // if the MAC is no use...
			// TODO: test for regexp checks in MAC scan
			continue
		}

		arpMAC = models.NewMAC(arpMAC) // sanitise the MAC. // TODO: test that MACs are sanitised here

		mim[models.Ip(arpIp)] = models.MAC(arpMAC) // save the MAC address for the IP.

		if managerModeMatchAllSourceIps && gm.Groups == nil { // if there are no groups of MACs found...
			// Set each source IP into the default group.
			mig[models.Ip(arpIp)] = []models.Group{defaultGroupName}
		} else {
			// Find group for MAC
			for group, macs := range gm.Groups {
				for _, gmac := range macs {
					if gmac.MAC == arpMAC {
						existingGroups := mig[models.Ip(arpIp)] // retrieve existing groups for the IP.
						exists := false
						// Check if we saved the group already.
						for _, existingGroup := range existingGroups {
							if existingGroup == group {
								exists = true
							}
						}
						if !exists { // if the group has not yet been saved...
							mig[models.Ip(arpIp)] = append(existingGroups, group) // append the new group to the existing groups.
						}
					}
				}
			}
		}
	}

	return mig, mim
}

// duplicateMap creates a shallow copy of the original map.
func duplicateMap[K comparable, V any](original map[K]V) map[K]V {
	// Create a new map with the same type and capacity as the original.
	newCopy := make(map[K]V, len(original))

	// Iterate over the original map and copy each key-value pair
	for key, value := range original {
		newCopy[key] = value
	}

	return newCopy
}
