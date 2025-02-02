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
	return config.SafeWriteViaTemp(path, string(b))
}
