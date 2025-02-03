package usage

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"
)

func GetGroupTrackerConfig(t *Tracker) (models.MapGroupTrackerConfig, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !groupTrackerConfigFileUpdated {
		var err error
		defaultGroupTrackerConfigFilePath, err = config.DefaultCreateAppHomeDirAndGetConfigFilePathFunc(defaultGroupTrackerConfigFilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to create home directory for usage tracker config file: %w", err)
		} else {
			groupTrackerConfigFileUpdated = true
		}
	}

	yamlFile, err := os.ReadFile(defaultGroupTrackerConfigFilePath)
	if err != nil && os.IsNotExist(err) { // if the file needs creating...
		// Create the file with zero data.
		err = config.SafeWriteViaTemp(defaultGroupTrackerConfigFilePath, "")
		if err != nil {
			return nil, fmt.Errorf("failed to create usager tracker config file: %w", err)
		}
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("%w: %v: %v", ErrorGroupTrackerConfigFileNotFound, err, defaultGroupTrackerConfigFilePath)
	}

	cfg := make(models.MapGroupTrackerConfig)
	err = yaml.Unmarshal(yamlFile, &cfg)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling YAML: %w", err)
	}

	return cfg, nil
}

func SetGroupTrackerConfig(t *Tracker, m models.MapGroupTrackerConfig) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !groupTrackerConfigFileUpdated {
		var err error
		defaultGroupTrackerConfigFilePath, err = config.DefaultCreateAppHomeDirAndGetConfigFilePathFunc(defaultGroupTrackerConfigFilePath)
		if err != nil {
			return fmt.Errorf("failed to create home directory for usage tracker config file: %w", err)
		} else {
			groupTrackerConfigFileUpdated = true
		}
	}

	for k, v := range m { // filter out bad records: empty key or nil values
		if k == "" || v == nil {
			delete(m, k)
		}
	}
	if len(m) == 0 {
		return fmt.Errorf("group tracker config not set: no valid groups or tracker config was found")
	}

	b, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("error marshalling YAML: %w", err)
	}
	t.cfgGroups = m
	err = config.SafeWriteViaTemp(defaultGroupTrackerConfigFilePath, string(b))
	if err != nil {
		return fmt.Errorf("failed to write group-macs to file: %w", err)
	}
	return nil
}

func SaveGroupTrackerConfig(t *Tracker, m string) error {
	mgt := make(models.MapGroupTrackerConfig)
	err := yaml.Unmarshal([]byte(m), &mgt)
	if err != nil {
		return fmt.Errorf("error unmarshalling YAML: %w", err)
	}
	return SetGroupTrackerConfig(t, mgt)
}