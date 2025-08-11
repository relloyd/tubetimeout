package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"go.uber.org/zap"
	"relloyd/tubetimeout/config"
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
			v.Config = getDefaultGroupTrackerConfig(&config.AppCfg.TrackerConfig) // set starter values.
			// TODO: test for default TrackerConfig being set when loading samples files if it's not present. It's needed for window synchronisation.
			//   TrackerConfig data will be set by the web interface eventually and should come before or at the same time as groupMAC data.
			//   Remember that the web interface writes groupMAC data back to the API and tracker config data back to the API separately.
			//   Then we have samples being saved that contain the tracker config, so there is a lot of scope for things to get out of sync!
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
	return config.FnDefaultSafeWriteViaTemp(path, string(b))
}
