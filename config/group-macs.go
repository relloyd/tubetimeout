package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// GroupConfig represents the YAML structure
type GroupConfig struct {
	GroupMACs map[string][]string `yaml:"groups"` // group: [mac1, mac2, ...]
}

// LoadGroupMACs parses the YAML file
func LoadGroupMACs(filepath string) (GroupConfig, error) {
	yamlFile, err := os.ReadFile(filepath)
	if err != nil {
		return GroupConfig{}, fmt.Errorf("error reading YAML file: %w", err)
	}

	var gc GroupConfig
	err = yaml.Unmarshal(yamlFile, &gc)
	if err != nil {
		return GroupConfig{}, fmt.Errorf("error unmarshalling YAML: %w", err)
	}

	return gc, nil
}
