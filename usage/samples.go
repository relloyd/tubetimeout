package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"
)

func loadSamples(path string) (*sync.Map, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("usage samples file %q does not exist", path)
	}

	// Read file contents.
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read samples from file: %v", err)
	}

	// Unmarshal into DTO.
	loadedData := make(map[string]deviceDataDTO)
	err = json.Unmarshal(b, &loadedData)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal samples: %v", err)
	}

	// Convert DTO to sync.Map.
	m := &sync.Map{}
	for k, v := range loadedData {
		if v.Config == nil { // if the samples file doesn't have tracker config persisted...
			v.Config = &models.TrackerConfig{
				Granularity:            config.AppCfg.TrackerConfig.Granularity,
				Retention:              config.AppCfg.TrackerConfig.Retention,
				Threshold:              config.AppCfg.TrackerConfig.Threshold,
				StartDay:               config.AppCfg.TrackerConfig.StartDay,
				StartTime:              config.AppCfg.TrackerConfig.StartTime,
				Mode:                   models.ModeMonitor,
				ModeEndTime:            time.Time{},
			} // set a starter values.
		}
		m.Store(k, &deviceData{
			mu:              &sync.Mutex{}, // Reinitialize the mutex
			config:          v.Config,
			samples:         v.Samples,
			windowStartTime: v.WindowStartTime,
		})
	}

	return m, nil
}

func saveSamples(logger *zap.SugaredLogger, path string, devices *sync.Map) error {
	// Prepare the DTO map.
	samples := make(map[string]deviceDataDTO)

	devices.Range(func(k, v interface{}) bool {
		data := v.(*deviceData)
		data.mu.Lock()
		defer data.mu.Unlock()
		samples[k.(string)] = deviceDataDTO{
			Config:          data.config,
			Samples:         data.samples,
			WindowStartTime: data.windowStartTime,
		}
		return true
	})

	// Marshal the DTO map.
	b, err := json.Marshal(samples)
	if err != nil {
		return err
	}

	// Write the samples to the file.
	return config.FnSafeWriteViaTemp(path, string(b))
}
