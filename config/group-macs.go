package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// GroupMACs represents the YAML structure
type GroupMACs struct {
	Groups map[string][]string `yaml:"groups"` // group: [mac1, mac2, ...]
}

// LoadGroupMACs parses the YAML file
func LoadGroupMACs(filepath string) (GroupMACs, error) {
	yamlFile, err := os.ReadFile(filepath)
	if err != nil {
		return GroupMACs{}, fmt.Errorf("error reading YAML file: %w", err)
	}

	var groupMACs GroupMACs
	err = yaml.Unmarshal(yamlFile, &groupMACs)
	if err != nil {
		return GroupMACs{}, fmt.Errorf("error unmarshalling YAML: %w", err)
	}

	return groupMACs, nil
}
