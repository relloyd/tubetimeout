package usage

// func getGroupTrackerConfig(t *Tracker) (models.MapGroupTrackerConfig, error) {
// 	t.mu.Lock()
// 	defer t.mu.Unlock()
//
// 	if !groupTrackerConfigFileUpdated {
// 		var err error
// 		defaultGroupTrackerConfigFilePath, err = config.FnDefaultCreateAppHomeDirAndGetConfigFilePath(defaultGroupTrackerConfigFilePath)
// 		if err != nil {
// 			return nil, fmt.Errorf("failed to create home directory for usage tracker config file: %w", err)
// 		} else {
// 			groupTrackerConfigFileUpdated = true
// 		}
// 	}
//
// 	yamlFile, err := os.ReadFile(defaultGroupTrackerConfigFilePath)
// 	if err != nil && os.IsNotExist(err) { // if the file needs creating...
// 		// Create the file with zero data.
// 		err = config.FnDefaultSafeWriteViaTemp(defaultGroupTrackerConfigFilePath, "")
// 		if err != nil {
// 			return nil, fmt.Errorf("failed to create usager tracker config file: %w", err)
// 		}
// 		return nil, nil
// 	} else if err != nil {
// 		return nil, fmt.Errorf("%w: %v: %v", ErrorGroupTrackerConfigFileNotFound, err, defaultGroupTrackerConfigFilePath)
// 	}
//
// 	cfg := make(models.MapGroupTrackerConfig)
// 	err = yaml.Unmarshal(yamlFile, &cfg)
// 	if err != nil {
// 		return nil, fmt.Errorf("error unmarshalling YAML: %w", err)
// 	}
//
// 	return cfg, nil
// }
//
// func setGroupTrackerConfig(t *Tracker, m models.MapGroupTrackerConfig) error {
// 	t.mu.Lock()
// 	defer t.mu.Unlock()
//
// 	if !groupTrackerConfigFileUpdated {
// 		var err error
// 		defaultGroupTrackerConfigFilePath, err = config.FnDefaultCreateAppHomeDirAndGetConfigFilePath(defaultGroupTrackerConfigFilePath)
// 		if err != nil {
// 			return fmt.Errorf("failed to create home directory for usage tracker config file: %w", err)
// 		} else {
// 			groupTrackerConfigFileUpdated = true
// 		}
// 	}
//
// 	// Filter and sanitise records.
// 	for k, v := range m { // for each input usage tracker...
// 		if k == "" || v == nil { // if the record is empty...
// 			delete(m, k) // discard it.
// 		} else { // else populate any missing value using defaults...
// 			v.Granularity = config.AppCfg.TrackerConfig.Granularity // always keep the default granularity
// 			if v.Retention == 0 {
// 				v.Retention = config.AppCfg.TrackerConfig.Retention
// 			}
// 			if v.Threshold < 0 {
// 				v.Threshold = 0
// 			}
// 			if v.StartDayInt == 0 {
// 				v.StartDayInt = config.AppCfg.TrackerConfig.StartDayInt
// 			}
// 			if v.StartDuration == 0 {
// 				v.StartDuration = config.AppCfg.TrackerConfig.StartDuration
// 			}
// 			if v.ModeEndTime.Before(time.Now().UTC()) { // if the input mode has expired...
// 				// Reset it to monitoring.
// 				// The usage tracker will ignore expired modes anyway.
// 				v.Mode = models.ModeMonitor
// 				v.ModeEndTime = time.Time{}.UTC()
// 			}
// 		}
// 		// Remove bad characters from the map by replacing the keys.
// 		cleanGroup := models.Group(models.NewGroup(string(k))) // sanitise the group name
// 		if k != cleanGroup {                                   // if the sane group name doesn't match the input group...
// 			m[cleanGroup] = v // create the new clean group with the same data and delete the badly named key.
// 			delete(m, k)
// 		}
// 	}
// 	if len(m) == 0 {
// 		return fmt.Errorf("group tracker config not set: no valid groups or tracker config was found")
// 	}
//
// 	b, err := yaml.Marshal(m)
// 	if err != nil {
// 		return fmt.Errorf("error marshalling YAML: %w", err)
// 	}
// 	// Save the usage tracker data in memory under lock.
// 	t.cfgGroups = m
// 	// Save to file.
// 	err = config.FnDefaultSafeWriteViaTemp(defaultGroupTrackerConfigFilePath, string(b))
// 	if err != nil {
// 		return fmt.Errorf("failed to write group-macs to file: %w", err)
// 	}
// 	return nil
// }
