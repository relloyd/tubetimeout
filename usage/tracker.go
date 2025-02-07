package usage

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"
)

// type saveSamplesFunc func(*zap.SugaredLogger, string, *sync.Map) error

var (
	fnLoadSamples                       = loadSamples
	fnSaveSamples                       = saveSamples
	fnGetGroupTrackerConfig             = getGroupTrackerConfig
	fnGetTrackerSamplesFile             = config.DefaultCreateAppHomeDirAndGetConfigFilePathFunc
	fnSaveSamplesPeriodically           = saveSamplesPeriodically
	defaultGroupTrackerConfigFilePath   = "usage-tracker-config.yaml"
	groupTrackerConfigFileUpdated       = false
	ErrorGroupTrackerConfigFileNotFound = fmt.Errorf("usage-tracker config file not found")
)

type Tracker struct {
	logger             *zap.SugaredLogger
	cfgTrackerDefaults *models.TrackerConfig
	cfgGroups          models.MapGroupTrackerConfig
	mu                 *sync.Mutex
	devices            *sync.Map        // Map of device IDs (string) to *deviceData
	nowFunc            func() time.Time // Function to get the current time (defaults to time.Now)
}

// NewTracker initializes a Tracker with pre-allocated slices for each device.
func NewTracker(ctx context.Context, logger *zap.SugaredLogger, cfg *models.TrackerConfig) (*Tracker, error) {
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
	t.cfgGroups, err = fnGetGroupTrackerConfig(t)
	if err != nil {
		return nil, err
	}

	// Load & save existing sample data.
	if cfg.SampleFilePath != "" { // TODO: test when SampleFilePath is empty that no files are saved
		samplesFile, err := fnGetTrackerSamplesFile(cfg.SampleFilePath)
		if err != nil {
			return nil, err
		}
		s, err := fnLoadSamples(samplesFile)
		if err != nil {
			logger.Errorf("Failed to load samples from file: %v", err)
		} else {
			// Load the samples into the devices map.
			logger.Infof("Samples loaded from file: %q", samplesFile)
			t.devices = s
		}
		// Save samples to the file on context cancellation.
		if cfg.SampleFileSaveInterval > 0 {
			go fnSaveSamplesPeriodically(ctx, t.logger, t.devices, samplesFile, cfg.SampleFileSaveInterval)
		}
	}

	return t, nil
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
	config          *models.TrackerConfig
	samples         []bool    // Slice of fixed size to represent the rotating window
	windowStartTime time.Time // Start time of the slice window
}

// deviceDataDTO is used to save/load deviceData{}. It is a DTO to avoid saving the mutex.
type deviceDataDTO struct {
	Config          *models.TrackerConfig `json:"config"`
	Samples         []bool                `json:"samples"`
	WindowStartTime time.Time             `json:"windowStartTime"`
}

func newDeviceData(now time.Time, cfg *models.TrackerConfig) *deviceData {
	if cfg.Retention > 7*24*time.Hour {
		cfg.Retention = 7 * 24 * time.Hour
	}

	if cfg.Retention < 24*time.Hour {
		cfg.StartDay = 0
	}

	if cfg.Threshold == 0 {
		cfg.Threshold = 1 * time.Minute
	}

	if cfg.Granularity == 0 {
		cfg.Granularity = 1 * time.Minute
	}

	cfg.SampleSize = getSampleSize(cfg)

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

func getSampleSize(cfg *models.TrackerConfig) int {
	return int(cfg.Retention / cfg.Granularity)
}

// AddSample records a sample for a given identifier at the current time.
// TODO: add test for AddSample() when tracker is paused
func (t *Tracker) AddSample(id string, active bool) {
	id = strings.ToLower(id)

	now := t.nowFunc() // Use nowFunc instead of time.Now

	// Load the config for the group/id or use defaults.
	t.mu.Lock()
	defer t.mu.Unlock()
	cfg, ok := t.cfgGroups[models.Group(id)]
	if !ok {
		t.logger.Errorf("unable to load config for group %v, using defaults", id)
		cfg = &models.TrackerConfig{
			Granularity: t.cfgTrackerDefaults.Granularity,
			Retention:   t.cfgTrackerDefaults.Retention,
			Threshold:   t.cfgTrackerDefaults.Threshold,
			StartDay:    t.cfgTrackerDefaults.StartDay,
			StartTime:   t.cfgTrackerDefaults.StartTime,
			SampleSize:  getSampleSize(t.cfgTrackerDefaults),
			Mode:        models.ModeMonitor,
			ModeEndTime: time.Time{},
		}
		t.cfgGroups[models.Group(id)] = cfg // save the config, so we don't have to set this again until data is overridden by global group tracker config
	}

	// Get or initialize the device data.
	data, loaded := t.devices.LoadOrStore(id, newDeviceData(now, cfg))
	dd := data.(*deviceData)
	dd.mu.Lock()
	defer dd.mu.Unlock()

	if loaded {
		// Ensure the config is up to date.
		dd.config = cfg
		// TODO test that latest config is set
		// TODO: when the sample size changes then we risk going out of bounds in the samples slice so we need to remake this!
	}

	if active && dd.config.Mode == models.ModeMonitor { // if the group is active and the tracker is not paused...
		// Ensure the time window is synchronized.
		dd.syncWindow(t.logger, now)
		// Mark the sample as seen.
		index := dd.getIndex(now, dd.windowStartTime)
		dd.samples[index] = true
	}

	// Reset the mode.
	if (dd.config.Mode == models.ModeAllow || dd.config.Mode == models.ModeBlock) &&
		dd.config.ModeEndTime.Before(now) { // if the tracker block/allow time has expired...
		dd.config.Mode = models.ModeMonitor
	}
}

// HasExceededThreshold checks if a device has exceeded the threshold duration.
// TODO: add test for HasExceededThreshold() when tracker is paused
func (t *Tracker) HasExceededThreshold(id string) bool {
	id = strings.ToLower(id) // force lower case for deviceID

	data, ok := t.devices.Load(id)
	if !ok {
		return false
	}

	dd := data.(*deviceData)
	dd.mu.Lock()
	defer dd.mu.Unlock()

	if dd.config.Mode == models.ModeAllow && dd.config.ModeEndTime.Before(time.Now()) { // if the tracker is paused...
		return false
	} else if dd.config.Mode == models.ModeBlock && dd.config.ModeEndTime.Before(time.Now()) { // if the tracker is paused...
		return true
	} // else the tracker is in monitor mode

	// Ensure the time window is synchronized.
	dd.syncWindow(t.logger, time.Now())

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
func (d *deviceData) syncWindow(logger *zap.SugaredLogger, now time.Time) {
	// Calculate number of time slices that have elapsed since the start of the window.
	elapsed := int(now.Sub(d.windowStartTime) / d.config.Granularity)
	if elapsed >= d.config.SampleSize || elapsed < 0 {
		// If elapsed time exceeds the buffer size, reset the entire window.
		for i := range d.samples {
			d.samples[i] = false
		}
		lastWindowStart, _ := d.calculateWindow(now)
		d.windowStartTime = lastWindowStart // Reset the start as we roll into a new window.
		logger.Infof("Renew retention window (%v) for device %s", now, d.config.Retention)
	}
}

// CalculateWindow determines the start times for the last and next windows.
// Return the start time of the last window and the start time of the next window respectively.
// it uses t.retention to determine the duration of the window
// it uses StartDay and StartTime to determine the start time of the window as follows
// it uses StartDay to determine the day of the week the window starts on if t.retention is 7 days
// it uses StartTime to determine the time of day the window starts on if t.retention is 7 days
// it uses StartTime to determine the time of day the window starts on if t.retention is 24 hours
// if t.retention is 7 days, the window starts on StartDay at StartTime
// if t.retention is 24 hours, the window starts StartTime after midnight and StartDay is ignored
// if t.retention is less than 24 hours, the window starts StartTime after the current time and StartDay is ignored
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

// GetSummary returns a map of device IDs to the number of samples seen.
// Used by package web for reporting.
func (t *Tracker) GetSummary() map[string]*models.GroupSummary {
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

// ResetGroup resets the tracker sample data for the given device.
func (t *Tracker) Reset(id string) {
	id = strings.ToLower(id)
	t.devices.Delete(id)
}

// SetMode pauses the tracker for the given device for the specified duration.
func (t *Tracker) SetMode(id string, d time.Duration, mode models.UsageTrackerMode) error {
	id = strings.ToLower(id)

	data, ok := t.devices.Load(id)
	if !ok {
		return fmt.Errorf("usage tracker group %v not found", id)
	}
	dd := data.(*deviceData)

	dd.mu.Lock()
	defer dd.mu.Unlock()

	// Save the mode requested.
	dd.config.Mode = mode
	dd.config.ModeEndTime = t.nowFunc().Add(d)

	return nil
}

// GetModeEndTime returns the end time of the pause for the given device.
func (t *Tracker) GetModeEndTime(id string) (models.GroupMode, error) {
	id = strings.ToLower(id)

	data, ok := t.devices.Load(id)
	if !ok {
		return models.GroupMode{}, fmt.Errorf("group %v not found", id)
	}
	dd := data.(*deviceData)

	dd.mu.Lock()
	defer dd.mu.Unlock()

	return models.GroupMode{Mode: dd.config.Mode, ModeEndTime: dd.config.ModeEndTime}, nil
}

// Resume resumes the tracker for the given device.
func (t *Tracker) Resume(id string) error {
	id = strings.ToLower(id)

	data, ok := t.devices.Load(id)
	if !ok {
		return fmt.Errorf("device %v not found", id)
	}
	dd := data.(*deviceData)

	dd.mu.Lock()
	defer dd.mu.Unlock()

	dd.config.ModeEndTime = time.Time{}
	return nil
}

// GetConfig returns the group tracker config for all groups.
func (t *Tracker) GetConfig() (models.MapGroupTrackerConfig, error) {
	return getGroupTrackerConfig(t)
}

// SetConfig saves the supplied map of group tracker config data to disk and in the struct.
func (t *Tracker) SetConfig(m models.MapGroupTrackerConfig) error {
	return setGroupTrackerConfig(t, m)
}
