package netwatcher

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ConfigMACGroup represents the YAML structure
type ConfigMACGroup struct {
	Groups map[string][]string `yaml:"groups"` // group: [mac1, mac2, ...]
}

// LoadMACGroups parses the YAML file
func LoadMACGroups(filepath string) (ConfigMACGroup, error) {
	yamlFile, err := os.ReadFile(filepath)
	if err != nil {
		return ConfigMACGroup{}, fmt.Errorf("error reading YAML file: %w", err)
	}

	var macGroup ConfigMACGroup
	err = yaml.Unmarshal(yamlFile, &macGroup)
	if err != nil {
		return ConfigMACGroup{}, fmt.Errorf("error unmarshalling YAML: %w", err)
	}

	return macGroup, nil
}
