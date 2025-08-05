package config

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	"relloyd/tubetimeout/models"
)

var (
	GroupMACs                 = &groupMACs{}
	ErrorGroupMacFileNotFound = fmt.Errorf("group-macs file not found")
	defaultGroupMacFilePath   = "group-macs.yaml"
	groupMACsFileUpdated      = false
)

var ARPCmd = func() (string, error) {
	output, err := exec.Command("arp", "-n", "-a").Output() // -n: show numerical addresses, -a: show all hosts
	return string(output), err
}

// GroupMACsConfig represents the YAML structure saved to disk.
type GroupMACsConfig struct {
	Groups     map[models.Group][]models.NamedMAC `yaml:"groups"`     // group: [mac1, mac2, ...]
	UnusedMACs []models.NamedMAC                  `yaml:"unusedMACs"` // MACs that are not in a group
}

// FlatGroupMAC represents the JSON structure used to get/set the group-macs from the web API.
type FlatGroupMAC struct {
	Group string `json:"group"`
	MAC   string `json:"mac"`
	Name  string `json:"name"`
}

// groupMACs is used as a package variable to load the group-macs from disk.
type groupMACs struct {
	mu sync.Mutex
}

// GetConfig parses the defaultGroupMacFilePath YAML file.
func (g *groupMACs) GetConfig(logger *zap.SugaredLogger) (GroupMACsConfig, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !groupMACsFileUpdated {
		var err error
		defaultGroupMacFilePath, err = FnDefaultCreateAppHomeDirAndGetConfigFilePath(defaultGroupMacFilePath)
		if err != nil {
			return GroupMACsConfig{}, fmt.Errorf("failed to create home directory for group-macs config file: %w", err)
		} else {
			groupMACsFileUpdated = true
		}
	}

	yamlFile, err := os.ReadFile(defaultGroupMacFilePath)
	if err != nil && os.IsNotExist(err) { // if the file needs creating...
		// Create the file with zero data.
		err = FnDefaultSafeWriteViaTemp(defaultGroupMacFilePath, "")
		if err != nil {
			return GroupMACsConfig{}, fmt.Errorf("failed to create group-macs file: %w", err)
		}
		return GroupMACsConfig{}, nil
	} else if err != nil {
		return GroupMACsConfig{}, fmt.Errorf("%w: %v: %v", ErrorGroupMacFileNotFound, err, defaultGroupMacFilePath)
	}

	var gc GroupMACsConfig
	err = yaml.Unmarshal(yamlFile, &gc)
	if err != nil {
		return GroupMACsConfig{}, fmt.Errorf("error unmarshalling YAML: %w", err)
	}

	return gc, nil
}

// GetAllGroupMACs returns all the group-macs from the config file and ARP scan.
func (g *groupMACs) GetAllGroupMACs(logger *zap.SugaredLogger) ([]FlatGroupMAC, error) {
	// Load the configured group-macs from disk.
	gm, err := g.GetConfig(logger)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	// Convert the group-macs to the JSON structure
	var allGroupMACs []FlatGroupMAC
	macs := make(map[string]bool)
	for group, namedMacs := range gm.Groups {
		for _, namedMAC := range namedMacs {
			allGroupMACs = append(allGroupMACs, FlatGroupMAC{
				Group: string(group),
				MAC:   namedMAC.MAC,
				Name:  namedMAC.Name,
			})
			macs[namedMAC.MAC] = true
		}
	}
	// Add the unused MACs with names to allGroupMACs.
	for _, namedMAC := range gm.UnusedMACs {
		if _, seen := macs[namedMAC.MAC]; !seen { // if we haven't already seen this MAC...
			allGroupMACs = append(allGroupMACs, FlatGroupMAC{
				Group: "",
				MAC:   namedMAC.MAC,
				Name:  namedMAC.Name,
			})
			macs[namedMAC.MAC] = true
		}
	}

	// Execute ARP scan to get all MACs.
	output, err := ARPCmd()
	if err != nil {
		return nil, fmt.Errorf("failed to run ARP command to get MAC addresses: %w", err)
	}

	re := regexp.MustCompile(`([0-9A-Fa-f]{2}:){5}([0-9A-Fa-f]{2})`)
	// Parse ARP output to get the MAC addresses.
	arpLines := strings.Split(output, "\n")
	for _, line := range arpLines {
		fields := strings.Fields(line)
		if len(fields) < 3 { // if the line can be skipped...
			continue
		}
		arpMAC := strings.Trim(fields[3], " ")
		if !re.MatchString(arpMAC) { // if the MAC address is invalid...
			continue
		}
		arpMAC = models.NewMAC(arpMAC)      // sanitise the MAC
		if _, seen := macs[arpMAC]; !seen { // if we don't already have config for this MAC...
			// Add the MAC to the list.
			allGroupMACs = append(allGroupMACs, FlatGroupMAC{
				Group: "",
				MAC:   arpMAC,
				Name:  "",
			})
			macs[arpMAC] = true // MACs may appear on multiple network adapters so remember that we have seen them.
		}
	}

	return allGroupMACs, nil
}

// SaveGroupMACs saves the group-macs to the config file.
func (g *groupMACs) SaveGroupMACs(logger *zap.SugaredLogger, flatGroupMACs []FlatGroupMAC) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Convert the JSON structure to the group-macs YAML structure.
	groups := make(map[models.Group][]models.NamedMAC)
	unusedMACs := make([]models.NamedMAC, 0)

	for _, flatGroupMAC := range flatGroupMACs {
		if flatGroupMAC.Group != "" && flatGroupMAC.MAC != "" { // if the group is worth saving...
			flatGroupMAC.Group = models.NewGroup(flatGroupMAC.Group)

			// Create the group if it doesn't already exist.
			group := models.Group(flatGroupMAC.Group)
			if _, ok := groups[group]; !ok { // if the group doesn't already exist...
				groups[group] = []models.NamedMAC{}
			}

			// Append the namedMAC to the group.
			groups[group] = append(groups[group], models.NamedMAC{
				MAC:  flatGroupMAC.MAC,
				Name: flatGroupMAC.Name, // Name may be blank.
			})
		} else if flatGroupMAC.MAC != "" { // else if the MAC has a name and is worth remembering...
			// Append the MAC to the unusedMACs.
			unusedMACs = append(unusedMACs, models.NamedMAC{
				MAC:  flatGroupMAC.MAC,
				Name: flatGroupMAC.Name, // Name may be blank.
			})
		}
	}

	// Marshal the group-macs to YAML.
	gc := GroupMACsConfig{Groups: groups}
	yamlBytes, err := yaml.Marshal(gc)
	if err != nil {
		return fmt.Errorf("failed to marshal group-macs to YAML: %w", err)
	}

	err = FnDefaultSafeWriteViaTemp(defaultGroupMacFilePath, string(yamlBytes))
	if err != nil {
		return fmt.Errorf("failed to write group-macs to file: %w", err)
	}

	return nil
}
