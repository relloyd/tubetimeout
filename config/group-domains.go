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
	defaultGroupDomainsFilePath = "group-domains.yaml"
	defaultGroupDomains         = models.MapGroupDomains{"youtube": {"www.youtube.com", "youtube.com", "googlevideo.com", "youtu.be"}}
	groupDomainsFileUpdated     = false
)

// GroupDomainsConfig represents the YAML structure
type GroupDomainsConfig struct {
	GroupDomains models.MapGroupDomains `yaml:"groups"` // group: [domain1, domain2, ...]
}

// LoadGroupDomains parses the YAML file
func LoadGroupDomains() (models.MapGroupDomains, error) {
	if !groupMACsFileUpdated { // if we should update the file path with the app home dir...
		var err error
		defaultGroupDomainsFilePath, err = DefaultCreateAppHomeDirAndGetConfigFilePathFunc(defaultGroupDomainsFilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to create home directory for group-domains config file: %w", err)
		} else {
			groupDomainsFileUpdated = true
		}
	}

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
