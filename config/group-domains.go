package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// GroupDomains represents the YAML structure
type GroupDomains struct {
	Groups map[string][]string `yaml:"groups"` // group: [domain1, domain2, ...]
}

// LoadGroupDomains parses the YAML file
func LoadGroupDomains(filepath string) (GroupDomains, error) {
	yamlFile, err := os.ReadFile(filepath)
	if err != nil {
		return GroupDomains{}, fmt.Errorf("error reading YAML file: %w", err)
	}

	var groupDomains GroupDomains
	err = yaml.Unmarshal(yamlFile, &groupDomains)
	if err != nil {
		return GroupDomains{}, fmt.Errorf("error unmarshalling YAML: %w", err)
	}

	return groupDomains, nil
}
