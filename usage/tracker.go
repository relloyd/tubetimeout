package usage

import (
	"context"
	"fmt"
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
	fnGetGroupTrackerConfig             = config.GetConfig[models.MapGroupTrackerConfig]
	fnGetTrackerSamplesFile             = config.FnDefaultCreateAppHomeDirAndGetConfigFilePath
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
	t.cfgGroups, err = fnGetGroupTrackerConfig(t.mu, defaultGroupTrackerConfigFilePath, func() models.MapGroupTrackerConfig { return make(models.MapGroupTrackerConfig) })
	if err != nil {
		return nil, err
	}
	if t.cfgGroups == nil {
		t.cfgGroups = make(models.MapGroupTrackerConfig)
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

func getDefaultGroupTrackerConfig(t *models.TrackerConfig) *models.TrackerConfig {
	return &models.TrackerConfig{
		Granularity:   t.Granularity,
		Retention:     t.Retention,
		Threshold:     t.Threshold,
		StartDayInt:   t.StartDayInt,
		StartDuration: t.StartDuration,
		SampleSize:    getSampleSize(t),
		Mode:          models.ModeMonitor,
		ModeEndTime:   time.Time{},
	}
}

func newDeviceData(now time.Time, cfg *models.TrackerConfig) *deviceData {
	// TODO: support more that 7*24h retention windows!
	if cfg.Retention > 7*24*time.Hour {
		cfg.Retention = 7 * 24 * time.Hour
	}

	if cfg.Retention < 24*time.Hour {
		cfg.StartDayInt = 0
	}

	if cfg.Threshold == 0 {
		cfg.Threshold = 1 * time.Minute
	}

	if cfg.Granularity == 0 {
		cfg.Granularity = 1 * time.Minute
	}

	cfg.SampleSize = getSampleSize(cfg)

	cfgCopy := *cfg

	dd := &deviceData{
		config:  &cfgCopy,
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
	now := t.nowFunc() // Use nowFunc instead of time.Now

	// Load the config for the group/id or use defaults.
	t.mu.Lock()
	defer t.mu.Unlock()
	cfg, ok := t.cfgGroups[models.Group(id)]
	if !ok {
		t.logger.Errorf("Unable to load config for group %v, using defaults", id)
		cfg = getDefaultGroupTrackerConfig(t.cfgTrackerDefaults)
		t.cfgGroups[models.Group(id)] = cfg // save the config, so we don't have to set this again until data is overridden by global group tracker config
	}

	// Get or initialize the device data.
	data, loaded := t.devices.LoadOrStore(id, newDeviceData(now, cfg))
	dd := data.(*deviceData)
	dd.mu.Lock()
	defer dd.mu.Unlock()

	t.logger.Debugf("Usage tracker for group %v: retention=%v, threshold=%v, mode=%v, modeEndTime=%v", id, cfg.Retention, cfg.Threshold, cfg.Mode, cfg.ModeEndTime)

	if loaded {
		// Ensure the config is up to date.
		if dd.config.SampleSize != cfg.SampleSize || dd.config.Threshold != cfg.Threshold { // if the tracker size or threshold has changed...
			// Reset the samples to zero usage.
			t.logger.Info("Tracker sample size changed for group %v, resetting now", id)
			mode := dd.config.Mode // preserve values
			modeEnd := dd.config.ModeEndTime
			dd = newDeviceData(now, cfg)
			dd.config.Mode = mode
			dd.config.ModeEndTime = modeEnd
			t.devices.Store(id, dd)
			dd.mu.Lock()
			defer dd.mu.Unlock()
		}
		// Update other attributes that don't affect retention or thresholds.
		// TODO: test that latest config is set.
		if cfg.StartDuration != dd.config.StartDuration || cfg.StartDayInt != dd.config.StartDayInt {
			dd.config.StartDuration = cfg.StartDuration
			dd.config.StartDayInt = cfg.StartDayInt
		}
	}

	if active && dd.config.Mode == models.ModeMonitor { // if the group is active and the tracker is not paused...
		// Ensure the time window is synchronized.
		dd.syncWindow(t.logger, now)
		// Mark the sample as seen.
		index := dd.getIndex(now, dd.windowStartTime)
		dd.samples[index] = true
		t.logger.Debugf("Usage tracker %v in monitor mode (counting the sample)", id)
	}

	// Reset the mode.
	if (dd.config.Mode == models.ModeAllow || dd.config.Mode == models.ModeBlock) &&
		dd.config.ModeEndTime.Before(now) { // if the tracker block/allow time has expired...
		t.logger.Infof("Usage tracker %v is active again (monitor mode set)", id)
		dd.config.Mode = models.ModeMonitor // TODO: add test for mode being reset in addSample
	}
}

// HasExceededThreshold checks if a device has exceeded the threshold duration.
// TODO: add test for HasExceededThreshold() when tracker is paused
func (t *Tracker) HasExceededThreshold(id string) bool {
	data, ok := t.devices.Load(id)
	if !ok {
		t.logger.Errorf("Unable to load config for group %v, returning false has-not-exceeded-threshold", id)
		return false
	}

	dd := data.(*deviceData)
	dd.mu.Lock()
	defer dd.mu.Unlock()

	if dd.config.Mode == models.ModeAllow && time.Now().Before(dd.config.ModeEndTime) { // if the tracker is paused...
		t.logger.Debugf("Usage tracker %s is allowed until %v", id, dd.config.ModeEndTime)
		return false
	} else if dd.config.Mode == models.ModeBlock && time.Now().Before(dd.config.ModeEndTime) { // if the tracker is paused...
		t.logger.Debugf("Usage tracker %s is blocked until %v", id, dd.config.ModeEndTime)
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
		startOfWeek := now.Truncate(7*24*time.Hour).AddDate(0, 0, d.config.StartDayInt-int(now.Weekday()))
		lastWindowStart = startOfWeek.Add(d.config.StartDuration).Truncate(d.config.Granularity)
		if now.Before(lastWindowStart) {
			lastWindowStart = lastWindowStart.Add(-7 * 24 * time.Hour).Truncate(d.config.Granularity)
		}
		nextWindowStart = lastWindowStart.Add(7 * 24 * time.Hour).Truncate(d.config.Granularity)
	} else if d.config.Retention >= 24*time.Hour {
		// Daily retention logic
		startOfDay := now.Truncate(24 * time.Hour)
		lastWindowStart = startOfDay.Add(d.config.StartDuration)
		if now.Before(lastWindowStart) {
			lastWindowStart = lastWindowStart.Add(-24 * time.Hour).Truncate(d.config.Granularity)
		}
		nextWindowStart = lastWindowStart.Add(24 * time.Hour).Truncate(d.config.Granularity)
	} else {
		// Sub-daily retention logic
		baseWindowStart := now.Truncate(d.config.Retention)
		lastWindowStart = baseWindowStart.Add(d.config.StartDuration).Truncate(d.config.Granularity)
		if now.Before(lastWindowStart) {
			baseWindowStart = baseWindowStart.Add(-d.config.Retention)
			lastWindowStart = baseWindowStart.Add(d.config.StartDuration).Truncate(d.config.Granularity)
		}
		nextWindowStart = lastWindowStart.Add(d.config.Retention).Truncate(d.config.Granularity)
	}

	return lastWindowStart, nextWindowStart

}

// GetSummary returns a map of device IDs to the number of samples seen.
// Used by package web for reporting.
func (t *Tracker) GetSummary() map[string]*models.TrackerSummary {
	samples := make(map[string]*models.TrackerSummary)

	t.devices.Range(func(k, v interface{}) bool {
		dd := v.(*deviceData)
		dd.mu.Lock()
		defer dd.mu.Unlock()
		count := 0
		total := 0
		for _, seen := range dd.samples {
			if seen {
				count++
			}
			total++
		}

		t.logger.Debugf("Usage tracker summary for %v: %v samples seen (threshold %v)", k, count, dd.config.Threshold.Minutes())

		usagePercent := int(float64(count) / dd.config.Threshold.Minutes() * 100) // TODO: test that summary data uses the local device data config not global config.AppCfg.
		if usagePercent > 100 {
			usagePercent = 100
		}

		samples[k.(string)] = &models.TrackerSummary{
			Used:       count,
			Total:      total,
			Percentage: usagePercent,
		}

		return true
	})

	return samples
}

// Reset resets the tracker sample data for the given device.
func (t *Tracker) Reset(id string) {
	t.devices.Delete(id)
}

// SetMode pauses the tracker for the given device for the specified duration.
func (t *Tracker) SetMode(id string, d time.Duration, mode models.UsageTrackerMode) error {
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

	// Load the global usage tracker data for the group, and save the new tracker mode to the config file.
	grp, ok := t.cfgGroups[models.Group(id)]
	if !ok {
		t.logger.Errorf("group %v not found while setting a allow/block mode", id)
		return fmt.Errorf("group %v not found while setting a allow/block mode", id)
	}
	grp.Mode = dd.config.Mode
	grp.ModeEndTime = dd.config.ModeEndTime
	return t.SetConfig(t.cfgGroups)
}

// GetModeEndTime returns the end time of the pause for the given device.
func (t *Tracker) GetModeEndTime(id string) (models.TrackerMode, error) {
	data, ok := t.devices.Load(id)
	if !ok {
		return models.TrackerMode{}, models.ErrGroupNotFound
	}
	dd := data.(*deviceData)

	dd.mu.Lock()
	defer dd.mu.Unlock()

	return models.TrackerMode{Mode: dd.config.Mode, ModeEndTime: dd.config.ModeEndTime}, nil
}

// validateGroupTrackerConfig contains the validation and sanitization logic.
func validateGroupTrackerConfig(cfg models.MapGroupTrackerConfig) error {
	for k, v := range cfg {
		if k == "" || v == nil { // if there is a bad key...
			delete(cfg, k)
		} else {
			v.Granularity = config.AppCfg.TrackerConfig.Granularity // always keep the default granularity
			if v.Retention == 0 {
				v.Retention = config.AppCfg.TrackerConfig.Retention
			}
			if v.Threshold < 0 {
				v.Threshold = 0
			}
			if v.StartDayInt == 0 {
				v.StartDayInt = config.AppCfg.TrackerConfig.StartDayInt
			}
			if v.StartDuration == 0 {
				v.StartDuration = config.AppCfg.TrackerConfig.StartDuration
			}
			if v.ModeEndTime.Before(time.Now().UTC()) { // if the input mode has expired...
				// Reset it to monitoring.
				// The usage tracker will ignore expired modes anyway.
				v.Mode = models.ModeMonitor
				v.ModeEndTime = time.Time{}.UTC()
			}
			v.SampleSize = getSampleSize(v)
		}
		// Remove bad characters from the map by replacing the keys.
		cleanGroup := models.Group(models.NewGroup(string(k))) // sanitise the group name
		if k != cleanGroup {                                   // if the sane group name doesn't match the input group...
			cfg[cleanGroup] = v // create the new clean group with the same data and delete the badly named key.
			delete(cfg, k)
		}
	}
	if len(cfg) == 0 {
		return fmt.Errorf("group tracker config is empty")
	}
	return nil
}

// GetConfig returns the group tracker config for all groups.
func (t *Tracker) GetConfig() (models.MapGroupTrackerConfig, error) {
	return config.GetConfig[models.MapGroupTrackerConfig](
		t.mu,
		defaultGroupTrackerConfigFilePath,
		models.NewMapGroupTrackerConfig,
	)
}

// SetConfig saves the supplied map of group tracker config data to disk and in the struct.
func (t *Tracker) SetConfig(m models.MapGroupTrackerConfig) error {
	return config.SetConfig[models.MapGroupTrackerConfig](
		t.mu,
		defaultGroupTrackerConfigFilePath,
		validateGroupTrackerConfig,
		func(v models.MapGroupTrackerConfig) { t.cfgGroups = v },
		m,
	)
}
