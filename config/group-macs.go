package config

import (
	"fmt"
	"os"
	"os/exec"
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
	Groups map[models.Group][]models.NamedMAC `yaml:"groups"` // group: [mac1, mac2, ...]
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
func (g *groupMACs) GetConfig() (GroupMACsConfig, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !groupMACsFileUpdated {
		var err error
		defaultGroupMacFilePath, err = DefaultCreateAppHomeDirAndGetConfigFilePathFunc(defaultGroupMacFilePath)
		if err != nil {
			return GroupMACsConfig{}, fmt.Errorf("failed to create home directory for group-macs config file: %w", err)
		} else {
			groupMACsFileUpdated = true
		}
	}

	yamlFile, err := os.ReadFile(defaultGroupMacFilePath)
	if err != nil {
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
	gm, err := g.GetConfig()
	if err != nil {
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

	// Execute ARP scan to get all MACs.
	output, err := ARPCmd()
	if err != nil {
		return nil, fmt.Errorf("failed to run ARP command to get MAC addresses: %w", err)
	}
	// Parse ARP output to get the MAC addresses.
	arpLines := strings.Split(output, "\n")
	for _, line := range arpLines {
		fields := strings.Fields(line)
		if len(fields) < 3 { // if the line can be skipped...
			continue
		}
		arpMAC := strings.Trim(fields[3], " ")
		if _, seen := macs[arpMAC]; !seen { // if we don't already have config for this MAC...
			// Add the MAC to the list.
			allGroupMACs = append(allGroupMACs, FlatGroupMAC{
				Group: "",
				MAC:   arpMAC,
				Name:  "",
			})
		}
	}

	return allGroupMACs, nil
}
