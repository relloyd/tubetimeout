package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
	"relloyd/tubetimeout/config"
)

type saveSamplesFunc func(string, *sync.Map) error

var fnSaveSamples = saveSamplesFunc(saveSamples)

var fnGetTrackerConfigFile = config.DefaultCreateAppHomeDirAndGetConfigFilePathFunc

type deviceData struct {
	mu      *sync.Mutex
	samples []bool    // Slice of fixed size to represent the rotating window
	start   time.Time // Start time of the slice window
}

// deviceDataDTO is used to save/load deviceData{}. It is a DTO to avoid saving the mutex.
type deviceDataDTO struct {
	Samples []bool    `json:"samples"`
	Start   time.Time `json:"start"`
}

type TrackerI interface {
	AddSample(id string)
	HasExceededThreshold(deviceID string) bool
}

type Tracker struct {
	logger          *zap.SugaredLogger
	devices         *sync.Map     // Map of device IDs (string) to *deviceData
	retention       time.Duration // The retention period for samples
	granularity     time.Duration // The time granularity for sampling
	threshold       time.Duration // The threshold duration for exceeding conditions
	windowStartDay  int
	windowStartTime time.Duration
	sampleSize      int              // The number of slots in the circular buffer
	nowFunc         func() time.Time // Function to get the current time (defaults to time.Now)
}

// NewTracker initializes a Tracker with preallocated slices for each device.
func NewTracker(ctx context.Context, logger *zap.SugaredLogger, cfg *config.TrackerConfig) (*Tracker, error) {

	sampleSize := int(cfg.Retention / cfg.Granularity)

	if cfg.Retention > 7*24*time.Hour {
		cfg.Retention = 7 * 24 * time.Hour
	}

	if cfg.Retention < 24*time.Hour || cfg.StartTime > cfg.Retention {
		cfg.StartDay = 0
		cfg.StartTime = 0
	}

	t := &Tracker{
		logger:          logger,
		devices:         &sync.Map{},
		retention:       cfg.Retention,
		granularity:     cfg.Granularity,
		threshold:       cfg.Threshold,
		sampleSize:      sampleSize,
		windowStartDay:  cfg.StartDay,
		windowStartTime: cfg.StartTime,
		nowFunc:         time.Now, // Default to time.Now
	}

	// Load & save existing sample data.
	if cfg.SampleFilePath != "" { // TODO: test when SampleFilePath is empty that no files are saved
		configFile, err := fnGetTrackerConfigFile(cfg.SampleFilePath)
		if err != nil {
			return nil, err
		}
		s, err := loadSamples(configFile)
		if err != nil {
			logger.Errorf("Failed to load samples from file: %v", err)
		} else {
			// Load the samples into the devices map.
			logger.Infof("Samples loaded from file: %q", configFile)
			t.devices = s
		}
		// Save samples to the file on context cancellation.
		go saveSamplesPeriodically(ctx, t.logger, t.devices, configFile, cfg.SampleFileSaveInterval)
	}

	return t, nil
}

// TODO: only save samples if there are changes to the samples.
func saveSamplesPeriodically(ctx context.Context, logger *zap.SugaredLogger, devicesToSave *sync.Map, filePath string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	fn := func() {
		// TODO: save samples safely by using a temporary file and renaming it.
		if err := fnSaveSamples(filePath, devicesToSave); err != nil {
			logger.Errorf("Failed to save samples to file: %v", err)
		} else {
			logger.Infof("Saved samples to file %q", filePath)
		}
	}
	for {
		select {
		case <-ctx.Done(): // TODO: implement a "done" chan to save usage samples on exit safely.
		case <-ticker.C:
			fn()
		}
	}
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
			// mu:      &sync.Mutex{}, // Reinitialize the mutex
			samples: v.Samples,
			start:   v.Start,
			mu:      &sync.Mutex{},
		})
	}

	return m, nil
}

func saveSamples(path string, devices *sync.Map) error {
	// Prepare the DTO map.
	samples := make(map[string]deviceDataDTO)

	devices.Range(func(k, v interface{}) bool {
		data := v.(*deviceData)
		samples[k.(string)] = deviceDataDTO{
			Samples: data.samples,
			Start:   data.start,
		}
		return true
	})

	// Marshal the DTO map.
	b, err := json.Marshal(samples)
	if err != nil {
		return err
	}

	// Write the samples to the file.
	return os.WriteFile(path, b, 0644)
}

// AddSample records a sample for a given identifier at the current time.
func (t *Tracker) AddSample(id string) {
	now := t.nowFunc() // Use nowFunc instead of time.Now

	lastWindowStart, _ := t.calculateWindow(now)

	// Get or initialize the device data.
	data, _ := t.devices.LoadOrStore(id, &deviceData{
		samples: make([]bool, t.sampleSize),
		start:   lastWindowStart,
		mu:      &sync.Mutex{},
	})

	dd := data.(*deviceData)

	// Update the sample at the calculated index.
	dd.mu.Lock()
	defer dd.mu.Unlock()

	// Ensure the time window is synchronized.
	t.syncWindow(dd, now)

	// Mark the sample as seen.
	index := t.getIndex(now, dd.start)
	dd.samples[index] = true
}

// HasExceededThreshold checks if a device has exceeded the threshold duration.
func (t *Tracker) HasExceededThreshold(deviceID string) bool {
	data, ok := t.devices.Load(deviceID)
	if !ok {
		return false
	}

	dd := data.(*deviceData)

	dd.mu.Lock()
	defer dd.mu.Unlock()

	now := time.Now()

	// Ensure the time window is synchronized.
	t.syncWindow(dd, now)

	// Count the number of true samples in the window.
	count := 0
	for _, seen := range dd.samples {
		if seen {
			count++
		}
	}

	t.logger.Debugf("Usage tracker has seen %v %vx", deviceID, count)

	return time.Duration(count)*t.granularity >= t.threshold
}

// getIndex calculates the index in the slice for the current time.
func (t *Tracker) getIndex(now time.Time, bufferStart time.Time) int {
	elapsed := int(now.Sub(bufferStart) / t.granularity)
	return (elapsed%t.sampleSize + t.sampleSize) % t.sampleSize // Ensure positive modulo.
}

// syncWindow ensures the slice is synchronized with the current time.
func (t *Tracker) syncWindow(dd *deviceData, now time.Time) {
	// Calculate number of time slices that have elapsed since the start of the window.
	elapsed := int(now.Sub(dd.start) / t.granularity)
	if elapsed >= t.sampleSize || elapsed < 0 {
		// If elapsed time exceeds the buffer size, reset the entire window.
		for i := range dd.samples {
			dd.samples[i] = false
		}
		// dd.start = now.Truncate(t.granularity) // Reset start as we roll into a new window.
		lastWindowStart, _ := t.calculateWindow(now)
		dd.start = lastWindowStart // Reset the start as we roll into a new window.
		t.logger.Infof("Renew retention window (%v) for device %s", now, t.retention)
	}
	// If 0 < elapsed < t.sampleSize, do nothing. The circular buffer handles overwriting naturally.
}

// calculateWindow determines the start times for the last and next windows.
// Return the start time of the last window and the start time of the next window respectively.
// it uses t.retention to determine the duration of the window
// it uses t.windowStartDay and t.windowStartTime to determine the start time of the window as follows
// it uses t.windowStartDay to determine the day of the week the window starts on if t.retention is 7 days
// it uses t.windowStartTime to determine the time of day the window starts on if t.retention is 7 days
// it uses t.windowStartTime to determine the time of day the window starts on if t.retention is 24 hours
// if t.retention is 7 days, the window starts on t.windowStartDay at t.windowStartTime
// if t.retention is 24 hours, the window starts t.windowStartTime after midnight and windowStartDay is ignored
// if t.retention is less than 24 hours, the window starts t.windowStartTime after the current time and windowStartDay is ignored
// TODO: make calculateWindow work for monthly
func (t *Tracker) calculateWindow(now time.Time) (time.Time, time.Time) {
	var lastWindowStart, nextWindowStart time.Time

	if t.retention >= 7*24*time.Hour {
		// Weekly retention logic
		startOfWeek := now.Truncate(7*24*time.Hour).AddDate(0, 0, t.windowStartDay-int(now.Weekday()))
		lastWindowStart = startOfWeek.Add(t.windowStartTime).Truncate(t.granularity)
		if now.Before(lastWindowStart) {
			lastWindowStart = lastWindowStart.Add(-7 * 24 * time.Hour).Truncate(t.granularity)
		}
		nextWindowStart = lastWindowStart.Add(7 * 24 * time.Hour).Truncate(t.granularity)
	} else if t.retention >= 24*time.Hour {
		// Daily retention logic
		startOfDay := now.Truncate(24 * time.Hour)
		lastWindowStart = startOfDay.Add(t.windowStartTime)
		if now.Before(lastWindowStart) {
			lastWindowStart = lastWindowStart.Add(-24 * time.Hour).Truncate(t.granularity)
		}
		nextWindowStart = lastWindowStart.Add(24 * time.Hour).Truncate(t.granularity)
	} else {
		// Sub-daily retention logic
		baseWindowStart := now.Truncate(t.retention)
		lastWindowStart = baseWindowStart.Add(t.windowStartTime).Truncate(t.granularity)
		if now.Before(lastWindowStart) {
			baseWindowStart = baseWindowStart.Add(-t.retention)
			lastWindowStart = baseWindowStart.Add(t.windowStartTime).Truncate(t.granularity)
		}
		nextWindowStart = lastWindowStart.Add(t.retention).Truncate(t.granularity)
	}

	return lastWindowStart, nextWindowStart

}
