package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

var (
	ErrorGroupMacFileNotFound = fmt.Errorf("group-macs file not found")
	defaultGroupMacFilePath = "group-macs.yaml"
)

// GroupConfig represents the YAML structure
type GroupConfig struct {
	GroupMACs map[string][]string `yaml:"groups"` // group: [mac1, mac2, ...]
}

// LoadGroupMACs parses the YAML file
func LoadGroupMACs() (GroupConfig, error) {
	yamlFile, err := os.ReadFile(defaultGroupMacFilePath)
	if err != nil {
		return GroupConfig{}, fmt.Errorf("%w: %v", ErrorGroupMacFileNotFound, err)
	}

	var gc GroupConfig
	err = yaml.Unmarshal(yamlFile, &gc)
	if err != nil {
		return GroupConfig{}, fmt.Errorf("error unmarshalling YAML: %w", err)
	}

	return gc, nil
}
