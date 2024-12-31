package config

import (
	"fmt"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
	"relloyd/tubetimeout/models"
)

// TODO: allow naming in group mac file or comments?

var (
	GroupMACs                 = &groupMACs{}
	ErrorGroupMacFileNotFound = fmt.Errorf("group-macs file not found")
	defaultGroupMacFilePath   = "group-macs.yaml"
	groupMACsFileUpdated      = false
)

// GroupMACsConfig represents the YAML structure
type GroupMACsConfig struct {
	Groups map[models.Group][]models.NamedMAC `yaml:"groups"` // group: [mac1, mac2, ...]
}

type groupMACs struct {
	mu sync.RWMutex
}

// LoadGroupMACs parses the YAML file
func (g *groupMACs) LoadGroupMACs() (GroupMACsConfig, error) {
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
