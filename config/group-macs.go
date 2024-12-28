package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// TODO: allow naming in group mac file or comments?

var (
	ErrorGroupMacFileNotFound = fmt.Errorf("group-macs file not found")
	defaultGroupMacFilePath   = "group-macs.yaml"
	groupMACsFileUpdated      = false
)

type getConfigFileFunc func(string) (string, error)

// GroupConfig represents the YAML structure
type GroupConfig struct {
	GroupMACs map[string][]string `yaml:"groups"` // group: [mac1, mac2, ...]
}

// LoadGroupMACs parses the YAML file
func LoadGroupMACs() (GroupConfig, error) {
	if !groupMACsFileUpdated {
		var err error
		defaultGroupMacFilePath, err = DefaultCreateAppHomeDirAndGetConfigFilePathFunc(defaultGroupMacFilePath)
		if err != nil {
			return GroupConfig{}, fmt.Errorf("failed to create home directory for group-macs config file: %w", err)
		} else {
			groupMACsFileUpdated = true
		}
	}

	yamlFile, err := os.ReadFile(defaultGroupMacFilePath)
	if err != nil {
		return GroupConfig{}, fmt.Errorf("%w: %v: %v", ErrorGroupMacFileNotFound, err, defaultGroupMacFilePath)
	}

	var gc GroupConfig
	err = yaml.Unmarshal(yamlFile, &gc)
	if err != nil {
		return GroupConfig{}, fmt.Errorf("error unmarshalling YAML: %w", err)
	}

	return gc, nil
}
