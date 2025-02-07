package usage

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"
)

func getGroupTrackerConfig(t *Tracker) (models.MapGroupTrackerConfig, error) {
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
		err = config.FnSafeWriteViaTemp(defaultGroupTrackerConfigFilePath, "")
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

func setGroupTrackerConfig(t *Tracker, m models.MapGroupTrackerConfig) error {
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

	// Filter and sanitise records.
	for k, v := range m {
		if k == "" || v == nil { // if the record is empty...
			delete(m, k) // don't save it.
		} else { // else populate missing value using defaults
			v.Granularity = config.AppCfg.TrackerConfig.Granularity // always keep the default granularity
			if v.Retention == 0 {
				v.Retention = config.AppCfg.TrackerConfig.Retention
			}
			if v.Threshold == 0 {
				v.Threshold = config.AppCfg.TrackerConfig.Threshold
			}
			if v.StartDay == 0 {
				v.StartDay = config.AppCfg.TrackerConfig.StartDay
			}
			if v.StartTime == 0 {
				v.StartTime = config.AppCfg.TrackerConfig.StartTime
			}
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
	err = config.FnSafeWriteViaTemp(defaultGroupTrackerConfigFilePath, string(b))
	if err != nil {
		return fmt.Errorf("failed to write group-macs to file: %w", err)
	}
	return nil
}
