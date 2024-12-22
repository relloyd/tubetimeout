package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"example.com/tubetimeout/models"
	"gopkg.in/yaml.v3"
)

var (
	// TODO: supply config values from main instead of being implicit.
	defaultGroupDomainsFilePath = "group-domains.yaml"
	defaultGroupDomains         = models.MapGroupDomains{"youtube": {"www.youtube.com", "youtube.com", "googlevideo.com"}}
)

// GroupDomainsConfig represents the YAML structure
type GroupDomainsConfig struct {
	GroupDomains models.MapGroupDomains `yaml:"groups"` // group: [domain1, domain2, ...]
}

// LoadGroupDomains parses the YAML file
func LoadGroupDomains() (models.MapGroupDomains, error) {
	_, err := os.Stat(defaultGroupDomainsFilePath)
	if err != nil && errors.Is(err, fs.ErrNotExist) {
		return defaultGroupDomains, nil
	}

	yamlFile, err := os.ReadFile(defaultGroupDomainsFilePath)
	if err != nil {
		return models.MapGroupDomains{}, fmt.Errorf("error reading YAML file: %w", err)
	}

	var groupDomains GroupDomainsConfig
	err = yaml.Unmarshal(yamlFile, &groupDomains)
	if err != nil {
		return models.MapGroupDomains{}, fmt.Errorf("error unmarshalling YAML: %w", err)
	}

	return groupDomains.GroupDomains, nil
}
