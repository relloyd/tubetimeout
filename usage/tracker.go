package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"
)

type saveSamplesFunc func(*zap.SugaredLogger, string, *sync.Map) error

var (
	fnSaveSamples                       = saveSamplesFunc(saveSamples)
	fnGetTrackerSamplesFile             = config.DefaultCreateAppHomeDirAndGetConfigFilePathFunc
	defaultGroupTrackerConfigFilePath   = "usage-tracker-config.yaml"
	groupTrackerConfigFileUpdated       = false
	ErrorGroupTrackerConfigFileNotFound = fmt.Errorf("usage-tracker config file not found")
)

type Tracker struct {
	logger             *zap.SugaredLogger
	cfgTrackerDefaults *config.TrackerConfig
	cfgGroups          models.MapGroupUsageTrackerConfig
	mu                 *sync.Mutex
	devices            *sync.Map        // Map of device IDs (string) to *deviceData
	nowFunc            func() time.Time // Function to get the current time (defaults to time.Now)
}

// NewTracker initializes a Tracker with pre-allocated slices for each device.
func NewTracker(ctx context.Context, logger *zap.SugaredLogger, cfg *config.TrackerConfig) (*Tracker, error) {
	if logger == nil || cfg == nil {
		return nil, fmt.Errorf("logger and config must be provided")
	}

	t := &Tracker{
		logger:             logger,
		mu:                 &sync.Mutex{},
		devices:            &sync.Map{},
		nowFunc:            time.Now, // Default to time.Now
		cfgTrackerDefaults: cfg,
	}

	// Load groups config from file.
	var err error
	t.cfgGroups, err = t.GetGroupConfig()
	if err != nil {
		return nil, err
	}

	// Load & save existing sample data.
	if cfg.SampleFilePath != "" { // TODO: test when SampleFilePath is empty that no files are saved
		samplesFile, err := fnGetTrackerSamplesFile(cfg.SampleFilePath)
		if err != nil {
			return nil, err
		}
		s, err := loadSamples(samplesFile)
		if err != nil {
			logger.Errorf("Failed to load samples from file: %v", err)
		} else {
			// Load the samples into the devices map.
			logger.Infof("Samples loaded from file: %q", samplesFile)
			t.devices = s
		}
		// Save samples to the file on context cancellation.
		go saveSamplesPeriodically(ctx, t.logger, t.devices, samplesFile, cfg.SampleFileSaveInterval)
	}

	return t, nil
}

func (t *Tracker) GetGroupConfig() (models.MapGroupUsageTrackerConfig, error) {
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
		err = config.SafeWriteViaTemp(t.logger, defaultGroupTrackerConfigFilePath, "")
		if err != nil {
			return nil, fmt.Errorf("failed to create usager tracker config file: %w", err)
		}
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("%w: %v: %v", ErrorGroupTrackerConfigFileNotFound, err, defaultGroupTrackerConfigFilePath)
	}

	cfg := make(models.MapGroupUsageTrackerConfig)
	err = yaml.Unmarshal(yamlFile, &cfg)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling YAML: %w", err)
	}

	return cfg, nil
}

func (t *Tracker) SetGroupConfig(m models.MapGroupUsageTrackerConfig) error {
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

	b, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("error marshalling YAML: %w", err)
	}
	t.cfgGroups = m
	err = config.SafeWriteViaTemp(t.logger, defaultGroupTrackerConfigFilePath, string(b))
	if err != nil {
		return fmt.Errorf("failed to write group-macs to file: %w", err)
	}
	return nil
}

// TODO: only save samples if there are changes to the samples.
func saveSamplesPeriodically(ctx context.Context, logger *zap.SugaredLogger, devicesToSave *sync.Map, filePath string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	fn := func() {
		if err := fnSaveSamples(logger, filePath, devicesToSave); err != nil {
			logger.Errorf("Failed to save samples to file: %v", err)
		} else {
			logger.Infof("Saved samples to file %q", filePath)
		}
	}
	for {
		select {
		case <-ctx.Done(): // Exit if the context is cancelled.
			ticker.Stop()
			return
		case <-ticker.C:
			fn()
		}
	}
}

type deviceData struct {
	mu              *sync.Mutex
	config          models.UsageTrackerConfig
	samples         []bool    // Slice of fixed size to represent the rotating window
	windowStartTime time.Time // Start time of the slice window
}

// deviceDataDTO is used to save/load deviceData{}. It is a DTO to avoid saving the mutex.
type deviceDataDTO struct {
	Config          models.UsageTrackerConfig `json:"config"`
	Samples         []bool                    `json:"samples"`
	SampleSize      int                       `json:"sampleSize"`
	WindowStartTime time.Time                 `json:"windowStartTime"`
}

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
		samples[k.(string)] = deviceDataDTO{
			Config:          data.config,
			Samples:         data.samples,
			SampleSize:      len(data.samples),
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
	return config.SafeWriteViaTemp(logger, path, string(b))
}

func newDeviceData(now time.Time, cfg models.UsageTrackerConfig) *deviceData {
	if cfg.Retention > 7*24*time.Hour {
		cfg.Retention = 7 * 24 * time.Hour
	}

	if cfg.Retention < 24*time.Hour || cfg.StartTime > cfg.Retention {
		cfg.StartDay = 0
		cfg.StartTime = 0
	}

	if cfg.Threshold == 0 {
		cfg.Threshold = 1 * time.Minute
	}

	dd := &deviceData{
		config:  cfg,
		mu:      &sync.Mutex{},
		samples: make([]bool, cfg.SampleSize),
		// windowStartTime is set below
	}

	start, _ := dd.calculateWindow(now)
	dd.windowStartTime = start

	return dd
}

// AddSample records a sample for a given identifier at the current time.
func (t *Tracker) AddSample(id string) {
	id = strings.ToLower(id)

	now := t.nowFunc() // Use nowFunc instead of time.Now

	// Load the config for the group/id or use defaults.
	cfg, ok := t.cfgGroups[models.Group(id)]
	if !ok {
		t.logger.Errorf("unable to load config for group %v", id)
		cfg = models.UsageTrackerConfig{
			Granularity:  t.cfgTrackerDefaults.Granularity,
			Retention:    t.cfgTrackerDefaults.Retention,
			Threshold:    t.cfgTrackerDefaults.Threshold,
			StartDay:     t.cfgTrackerDefaults.StartDay,
			StartTime:    t.cfgTrackerDefaults.StartTime,
			SampleSize:   int(t.cfgTrackerDefaults.Retention / t.cfgTrackerDefaults.Granularity),
			PauseEndTime: time.Time{},
			Mode:         models.ModeMonitor,
		}
	}

	// Get or initialize the device data.
	data, _ := t.devices.LoadOrStore(id, newDeviceData(now, cfg))
	dd := data.(*deviceData)

	// Update the sample at the calculated index.
	dd.mu.Lock()
	defer dd.mu.Unlock()

	if dd.config.PauseEndTime.After(now) { // if the tracker is paused...
		// 	TODO: add test for AddSample() when tracker is paused
		return
	}

	// Ensure the time window is synchronized.
	dd.syncWindow(t.logger, dd, now)

	// Mark the sample as seen.
	index := dd.getIndex(now, dd.windowStartTime)
	dd.samples[index] = true
}

// HasExceededThreshold checks if a device has exceeded the threshold duration.
func (t *Tracker) HasExceededThreshold(id string) bool {
	id = strings.ToLower(id) // force lower case for deviceID

	data, ok := t.devices.Load(id)
	if !ok {
		return false
	}

	dd := data.(*deviceData)
	dd.mu.Lock()
	defer dd.mu.Unlock()

	if dd.config.PauseEndTime.After(time.Now()) { // if the tracker is paused...
		// TODO: add test for HasExceededThreshold() when tracker is paused
		return false
	}

	// Ensure the time window is synchronized.
	dd.syncWindow(t.logger, dd, time.Now())

	// Count the number of true samples in the window.
	count := 0
	for _, seen := range dd.samples {
		if seen {
			count++
		}
	}

	t.logger.Debugf("Usage tracker has seen %v %vx", id, count)

	return time.Duration(count)*dd.config.Granularity >= dd.config.Threshold
}

// getIndex calculates the index in the slice for the current time.
func (d *deviceData) getIndex(now time.Time, bufferStart time.Time) int {
	elapsed := int(now.Sub(bufferStart) / d.config.Granularity)
	return (elapsed%d.config.SampleSize + d.config.SampleSize) % d.config.SampleSize // Ensure positive modulo.
}

// syncWindow ensures the slice is synchronized with the current time.
// If 0 < elapsed < t.sampleSize, do nothing. The circular buffer handles overwriting naturally.
func (d *deviceData) syncWindow(logger *zap.SugaredLogger, dd *deviceData, now time.Time) {
	// Calculate number of time slices that have elapsed since the start of the window.
	elapsed := int(now.Sub(dd.windowStartTime) / d.config.Granularity)
	if elapsed >= d.config.SampleSize || elapsed < 0 {
		// If elapsed time exceeds the buffer size, reset the entire window.
		for i := range dd.samples {
			dd.samples[i] = false
		}
		lastWindowStart, _ := d.calculateWindow(now)
		dd.windowStartTime = lastWindowStart // Reset the start as we roll into a new window.
		logger.Infof("Renew retention window (%v) for device %s", now, d.config.Retention)
	}
}

// CalculateWindow determines the start times for the last and next windows.
// Return the start time of the last window and the start time of the next window respectively.
// it uses t.retention to determine the duration of the window
// it uses t.windowStartDay and t.windowStartTime to determine the start time of the window as follows
// it uses t.windowStartDay to determine the day of the week the window starts on if t.retention is 7 days
// it uses t.windowStartTime to determine the time of day the window starts on if t.retention is 7 days
// it uses t.windowStartTime to determine the time of day the window starts on if t.retention is 24 hours
// if t.retention is 7 days, the window starts on t.windowStartDay at t.windowStartTime
// if t.retention is 24 hours, the window starts t.windowStartTime after midnight and windowStartDay is ignored
// if t.retention is less than 24 hours, the window starts t.windowStartTime after the current time and windowStartDay is ignored
// TODO: make CalculateWindow work for monthly
func (d *deviceData) calculateWindow(now time.Time) (time.Time, time.Time) {
	var lastWindowStart, nextWindowStart time.Time

	if d.config.Retention >= 7*24*time.Hour {
		// Weekly retention logic
		startOfWeek := now.Truncate(7*24*time.Hour).AddDate(0, 0, d.config.StartDay-int(now.Weekday()))
		lastWindowStart = startOfWeek.Add(d.config.StartTime).Truncate(d.config.Granularity)
		if now.Before(lastWindowStart) {
			lastWindowStart = lastWindowStart.Add(-7 * 24 * time.Hour).Truncate(d.config.Granularity)
		}
		nextWindowStart = lastWindowStart.Add(7 * 24 * time.Hour).Truncate(d.config.Granularity)
	} else if d.config.Retention >= 24*time.Hour {
		// Daily retention logic
		startOfDay := now.Truncate(24 * time.Hour)
		lastWindowStart = startOfDay.Add(d.config.StartTime)
		if now.Before(lastWindowStart) {
			lastWindowStart = lastWindowStart.Add(-24 * time.Hour).Truncate(d.config.Granularity)
		}
		nextWindowStart = lastWindowStart.Add(24 * time.Hour).Truncate(d.config.Granularity)
	} else {
		// Sub-daily retention logic
		baseWindowStart := now.Truncate(d.config.Retention)
		lastWindowStart = baseWindowStart.Add(d.config.StartTime).Truncate(d.config.Granularity)
		if now.Before(lastWindowStart) {
			baseWindowStart = baseWindowStart.Add(-d.config.Retention)
			lastWindowStart = baseWindowStart.Add(d.config.StartTime).Truncate(d.config.Granularity)
		}
		nextWindowStart = lastWindowStart.Add(d.config.Retention).Truncate(d.config.Granularity)
	}

	return lastWindowStart, nextWindowStart

}

// GetSampleSummary returns a map of device IDs to the number of samples seen.
// Used by package web for reporting.
func (t *Tracker) GetSampleSummary() map[string]*models.GroupSummary {
	samples := make(map[string]*models.GroupSummary)

	t.devices.Range(func(k, v interface{}) bool {
		data := v.(*deviceData)
		data.mu.Lock()
		defer data.mu.Unlock()
		count := 0
		total := 0
		for _, seen := range data.samples {
			if seen {
				count++
			}
			total++
		}

		usagePercent := int(float64(count) / config.AppCfg.TrackerConfig.Threshold.Minutes() * 100)
		if usagePercent > 100 {
			usagePercent = 100
		}

		samples[k.(string)] = &models.GroupSummary{
			Used:       count,
			Total:      total,
			Percentage: usagePercent,
		}

		return true
	})

	return samples
}

func (t *Tracker) SetPause(id string, d time.Duration) error {
	id = strings.ToLower(id)

	data, ok := t.devices.Load(id)
	if !ok {
		return fmt.Errorf("device %v not found", id)
	}
	dd := data.(*deviceData)

	dd.mu.Lock()
	defer dd.mu.Unlock()

	if dd.config.PauseEndTime.IsZero() {
		dd.config.PauseEndTime = time.Now().Add(d)
		return nil
	}

	dd.config.PauseEndTime = dd.config.PauseEndTime.Add(d)
	return nil
}

func (t *Tracker) GetPauseEndTime(id string) (time.Time, error) {
	id = strings.ToLower(id)

	data, ok := t.devices.Load(id)
	if !ok {
		return time.Time{}, fmt.Errorf("device %v not found", id)
	}
	dd := data.(*deviceData)

	dd.mu.Lock()
	defer dd.mu.Unlock()

	return dd.config.PauseEndTime, nil
}

func (t *Tracker) DeletePause(id string) error {
	id = strings.ToLower(id)

	data, ok := t.devices.Load(id)
	if !ok {
		return fmt.Errorf("device %v not found", id)
	}
	dd := data.(*deviceData)

	dd.mu.Lock()
	defer dd.mu.Unlock()

	dd.config.PauseEndTime = time.Time{}
	return nil
}

func (t *Tracker) ResetSamples(id string) {
	id = strings.ToLower(id)
	t.devices.Delete(id)
}
