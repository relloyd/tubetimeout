package usage

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"
)

var (
	originalFnLoadSamples             = fnLoadSamples
	originalFnSaveSamples             = fnSaveSamples
	originalFnGetTrackerSamplesFile   = fnGetTrackerSamplesFile
	originalFnGetGroupTrackerConfig   = fnGetGroupTrackerConfig
	originalFnSaveSamplesPeriodically = fnSaveSamplesPeriodically
)

func restoreFunctions() {
	fnLoadSamples = originalFnLoadSamples
	fnSaveSamples = originalFnSaveSamples
	fnGetTrackerSamplesFile = originalFnGetTrackerSamplesFile
	fnGetGroupTrackerConfig = originalFnGetGroupTrackerConfig
	fnSaveSamplesPeriodically = originalFnSaveSamplesPeriodically
	config.FnDefaultSafeWriteViaTemp = config.SafeWriteViaTemp
}

type mockTrafficCounter struct {
	count     int
	packetLen int
	direction models.Direction
}

func (m *mockTrafficCounter) CountTraffic(count int, packetLen int, trafficDirection models.Direction) bool {
	return true
}

func TestNewTracker(t *testing.T) {
	ctx := context.Background()

	t.Cleanup(func() {
		restoreFunctions()
	})

	// Generate some sample data to load into the tracker.
	devices, tmpFile, err := saveSomeSamples(t)
	assert.NoError(t, err, "Failed to save samples")

	// Case 1: Create a tracker with a 1-hour retention, 10-minute threshold, and 1-minute granularity.
	cfgTrackerDefaults := &models.TrackerConfig{
		Retention:              1 * time.Hour,
		Threshold:              0 * time.Minute, // expect tracker to use at least 1 min.
		Granularity:            1 * time.Minute,
		SampleFilePath:         tmpFile.Name(),
		SampleFileSaveInterval: 50 * time.Millisecond,
	}

	fnGetGroupTrackerConfig = func(mu *sync.Mutex, configPath string, _ func() models.MapGroupTrackerConfig) (models.MapGroupTrackerConfig, error) {
		return models.MapGroupTrackerConfig{
			"GroupA": {
				Granularity:   1 * time.Minute,
				Retention:     30 * time.Minute,
				Threshold:     5 * time.Minute,
				StartDayInt:   0,
				StartDuration: 0,
				SampleSize:    1,
				ModeEndTime:   time.Time{},
				Mode:          models.ModeAllow,
			},
		}, nil
	}

	// Mock the file saver func.
	savedFileCount := 0
	fnSaveSamples = func(logger *zap.SugaredLogger, path string, devices *sync.Map) error {
		savedFileCount++
		return nil
	}

	// Mock the file path getter func.
	fnGetTrackerSamplesFile = func(path string) (string, error) {
		return path, nil
	}

	// Tracker returns error if not supplied with correct args.
	tracker, err := NewTracker(ctx, config.MustGetLogger(), nil)
	assert.Error(t, err, "NewTracker did not return error when not supplied with correct cfg")
	assert.Nil(t, tracker, "NewTracker did not return nil when not supplied with correct cfg")

	tracker, err = NewTracker(ctx, nil, cfgTrackerDefaults)
	assert.Error(t, err, "NewTracker did not return error when not supplied with correct logger")
	assert.Nil(t, tracker, "NewTracker did not return nil when not supplied with correct logger")

	// Restore the function that loads samples so it can load the samples we created above.
	fnLoadSamples = originalFnLoadSamples

	// Tracker with threshold 0 should default to 1 minute.
	tracker, err = NewTracker(ctx, config.MustGetLogger(), cfgTrackerDefaults)
	assert.NoError(t, err, "NewTracker failed")
	assert.NotNil(t, tracker, "NewTracker returned nil")
	assert.NotNil(t, tracker.devices, "NewTracker did not initialize devices map")
	assert.Equal(t, cfgTrackerDefaults.Retention, tracker.cfgTrackerDefaults.Retention, "NewTracker did not set retention")
	assert.Equal(t, cfgTrackerDefaults.Granularity, tracker.cfgTrackerDefaults.Granularity, "NewTracker did not set granularity")
	assert.Equal(t, cfgTrackerDefaults.Threshold, tracker.cfgTrackerDefaults.Threshold, "NewTracker did not set threshold")
	assert.NotNil(t, tracker.nowFunc, "NewTracker did not set a default nowFunc")
	assert.NotNil(t, tracker.mu, "NewTracker did not setup the mutex")

	// Test that the tracker loads the same samples that we saved.
	devices.Range(func(key, value interface{}) bool {
		tdv, ok := tracker.devices.Load(key)
		assert.True(t, ok, "NewTracker did not load samples from file")
		assert.Equal(t, value, tdv, "NewTracker did not load samples from file")
		return true
	})

	// Test that the saveSamplesPeriodically goroutine was started.
	time.Sleep(100 * time.Millisecond)
	assert.GreaterOrEqual(t, savedFileCount, 1, "saveSamplesPeriodically goroutine was not started")
}

func TestHasExceededThreshold(t *testing.T) {
	ctx := context.Background()

	// Setup: Create a tracker with a 1-hour retention, 10-minute threshold, and 1-minute granularity.
	cfg := &models.TrackerConfig{
		Granularity:            1 * time.Minute,
		Retention:              1 * time.Hour,
		Threshold:              10 * time.Minute,
		StartDayInt:            0,
		StartDuration:          0,
		SampleSize:             0,
		ModeEndTime:            time.Time{},
		Mode:                   models.ModeMonitor,
		SampleFileSaveInterval: 50 * time.Millisecond,
	}

	tracker, err := NewTracker(ctx, config.MustGetLogger(), cfg)
	assert.NoError(t, err, "NewTracker failed")

	// Simulate a device data structure with pre-allocated samples.
	startTime := time.Now().Truncate(cfg.Granularity)
	deviceID := "test-device"
	data := newDeviceData(time.Now(), cfg)

	// Add device data to the tracker.
	tracker.devices.Store(deviceID, data)

	// Case 1: No samples recorded.
	if tracker.HasExceededThreshold(deviceID) {
		t.Error("HasExceededThreshold returned true with no samples recorded")
	}

	// Case 2a: Samples recorded but below threshold.
	for i := 0; i < 5; i++ {
		data.samples[i] = true // Mark 5 minutes as seen.
	}
	if tracker.HasExceededThreshold(deviceID) {
		t.Error("HasExceededThreshold returned true with samples below the threshold")
	}

	// Case 2b: Test case insensitivity.
	if tracker.HasExceededThreshold(strings.ToUpper(deviceID)) {
		t.Error("HasExceededThreshold didn't use lower case for the device ID")
	}

	// Case 3: Samples meet the threshold.
	for i := 0; i < 10; i++ {
		data.samples[i] = true // Mark 10 minutes as seen.
	}
	if !tracker.HasExceededThreshold(deviceID) {
		t.Error("HasExceededThreshold returned false with samples meeting the threshold")
	}

	// Case 4: Simulate backward time adjustment.
	// Step 4a: Mark all slots as seen.
	for i, _ := range data.samples {
		data.samples[i] = true
	}
	// Step 4b: Move `start` time backward by 2 hours.
	// This simulates an old start time being used.
	data.windowStartTime = startTime.Add(-2 * time.Hour)
	// Step 4c: Test that stale samples are ignored and the buffer is reset.
	if tracker.HasExceededThreshold(deviceID) {
		t.Errorf("HasExceededThreshold incorrectly included stale samples or did not reset after backward time adjustment")
	}
	// Step 4d: Reset the device data with valid samples and verify behavior.
	data.windowStartTime = startTime
	for i := range data.samples {
		data.samples[i] = false
	}
	for i := 0; i < 10; i++ {
		data.samples[i] = true
	}
	if !tracker.HasExceededThreshold(deviceID) {
		t.Error("HasExceededThreshold returned false with valid samples meeting the threshold")
	}

	// Case 5a: Allow mode
	for i := 0; i < 10; i++ {
		data.samples[i] = true // expire the tracker so it blocks.
	}
	data.config.Mode = models.ModeAllow
	data.config.ModeEndTime = time.Now().Add(-time.Minute) // set an expired allow time.
	assert.True(t, tracker.HasExceededThreshold(deviceID), "HasExceededThreshold should return true for expired tracker and allow mode")
	data.config.ModeEndTime = time.Now().Add(time.Minute) // set an allow time in the future.
	assert.False(t, tracker.HasExceededThreshold(deviceID), "HasExceededThreshold should return false for expired tracker but valid allow mode")

	// Case 5b: block mode
	for i := 0; i < 10; i++ {
		data.samples[i] = false // open the tracker so it allows.
	}
	data.config.Mode = models.ModeBlock
	data.config.ModeEndTime = time.Now().Add(-time.Minute) // set an expired block time.
	assert.False(t, tracker.HasExceededThreshold(deviceID), "HasExceededThreshold should return false for open tracker and expired block mode")
	data.config.ModeEndTime = time.Now().Add(time.Minute) // set an expired block time.
	assert.True(t, tracker.HasExceededThreshold(deviceID), "HasExceededThreshold should return true for open tracker and valid block mode")
}

func TestAddSample_GroupDefaults(t *testing.T) {
	ctx := context.Background()
	logger := config.MustGetLogger()

	mockDeviceID := "test-device"
	mockDeviceID2 := "test-device2"

	// Setup defaults.
	cfg := &models.TrackerConfig{
		Retention:   2 * time.Minute, // use a different Retention value to cfgGroups so we can check that defaults are used.
		Granularity: 1 * time.Minute,
		Threshold:   1 * time.Minute,
		Mode:        models.ModeMonitor,
	}

	tracker, err := NewTracker(ctx, logger, cfg)
	assert.NoError(t, err, "NewTracker failed")
	// Setup groups so we can test the mode handling.
	tracker.cfgGroups = models.MapGroupTrackerConfig{
		models.Group(mockDeviceID): &models.TrackerConfig{
			Retention:   1 * time.Minute, // use a different Retention value to cfg defaults so we can check that this is used.
			Granularity: 1 * time.Minute,
			Threshold:   1 * time.Minute,
			Mode:        models.ModeMonitor,
		},
	}
	tracker.AddSample(mockDeviceID, true)

	// Get the deviceData
	d, ok := tracker.devices.Load(mockDeviceID)
	assert.True(t, ok, "AddSample found the deviceData")
	dd := d.(*deviceData)
	assert.Equal(t, getSampleSize(tracker.cfgGroups[models.Group(mockDeviceID)]), len(dd.samples), "AddSample did not create any samples")
	assert.Equal(t, true, dd.samples[0], "AddSample did not mark the first sample in monitor mode")

	// Try mode allow.
	dd.config.Mode = models.ModeAllow
	dd.config.ModeEndTime = time.Now().Add(-1 * time.Hour)
	dd.samples[0] = false
	tracker.AddSample(mockDeviceID, true)
	assert.Equal(t, models.ModeMonitor, dd.config.Mode, "AddSample did not set the mode correctly")
	assert.Equal(t, false, dd.samples[0], "AddSample should not mark the first sample in allow mode")

	// Try mode block.
	dd.config.Mode = models.ModeBlock
	dd.config.ModeEndTime = time.Now().Add(-1 * time.Hour)
	tracker.AddSample(mockDeviceID, true)
	assert.Equal(t, models.ModeMonitor, dd.config.Mode, "AddSample did not reset the mode correctly")
	assert.Equal(t, false, dd.samples[0], "AddSample should not mark the first sample in block mode")

	// Try mode monitor but with inactive bool value supplied.
	dd.config.Mode = models.ModeMonitor
	tracker.AddSample(mockDeviceID, false)
	assert.Equal(t, false, dd.samples[0], "AddSample should not mark the first sample in monitor mode with active=false")

	// Try mode monitor but with inactive bool value supplied.
	dd.config.Mode = models.ModeMonitor
	tracker.AddSample(mockDeviceID, true)
	assert.Equal(t, true, dd.samples[0], "AddSample should mark the first sample in monitor mode with active=true")

	// Check that defaults are used, well one of them anyway.
	tracker.AddSample(mockDeviceID2, true)
	assert.True(t, ok, "AddSample found the deviceData")
	d, ok = tracker.devices.Load(mockDeviceID2)
	assert.True(t, ok, "AddSample found the deviceData")
	dd = d.(*deviceData)
	assert.Equal(t, cfg.Retention, dd.config.Retention, "AddSample did not use the default retention")
}

func TestAddSample_ChangeSampleSize(t *testing.T) {
	ctx := context.Background()
	logger := config.MustGetLogger()

	fnGetGroupTrackerConfig = func(mu *sync.Mutex, configPath string, _ func() models.MapGroupTrackerConfig) (models.MapGroupTrackerConfig, error) {
		return models.MapGroupTrackerConfig{}, nil
	}

	mockDeviceID := "test-device"
	mockDeviceIDThreshold := "test-device2"
	savedTime := time.Now().Add(1 * time.Hour)

	// Setup defaults.
	cfg := &models.TrackerConfig{
		Retention:   2 * time.Minute, // use a different Retention value to cfgGroups so we can check that defaults are used.
		Granularity: 1 * time.Minute,
		Threshold:   1 * time.Minute,
		Mode:        models.ModeMonitor,
	}

	tracker, err := NewTracker(ctx, logger, cfg)
	assert.NoError(t, err, "NewTracker failed")

	// Setup groups so we can test regeneration of samples.
	tracker.cfgGroups = models.MapGroupTrackerConfig{
		models.Group(mockDeviceID): &models.TrackerConfig{
			Retention:   3 * time.Hour, // pick a large number that's easy to debug
			Granularity: 1 * time.Minute,
			Threshold:   1 * time.Minute,
			Mode:        models.ModeMonitor,
		},
	}

	// Add a sample to store newDeviceData.
	tracker.AddSample(mockDeviceID, true)

	// Get the sample size.
	d, ok := tracker.devices.Load(mockDeviceID)
	assert.True(t, ok, "AddSample found the deviceData")
	dd := d.(*deviceData)
	savedSampleSize := len(dd.samples)

	// Reduce the retention for mockDeviceID to check the device data and samples are remade.
	tracker.cfgGroups[models.Group(mockDeviceID)].Retention = 5 * time.Minute // pick a smaller number that's easy to debug
	dd.config.Mode = models.ModeAllow
	dd.config.ModeEndTime = savedTime

	// Add a sample and expect samples to be remade with correct size.
	tracker.AddSample(mockDeviceID, true)

	// Compare sample sizes.
	d, ok = tracker.devices.Load(mockDeviceID)
	assert.True(t, ok, "AddSample found the deviceData")
	dd = d.(*deviceData)
	assert.NotEqual(t, savedSampleSize, len(dd.samples), "AddSample did not regenerate samples")
	assert.Equal(t, getSampleSize(tracker.cfgGroups[models.Group(mockDeviceID)]), len(dd.samples), "AddSample did not recreate samples at the correct length")
	assert.Equal(t, models.ModeAllow, dd.config.Mode, "AddSample did not retain the mode correctly")
	assert.Equal(t, savedTime, dd.config.ModeEndTime, "AddSample did not retain the mode end time correctly")

	// Change the threshold to check the samples are remade.
	idx := 0
	now := time.Now()
	tracker.nowFunc = func() time.Time {
		idx++
		return now.Add(time.Duration(idx) * time.Minute)
	}
	// Setup groups so we can test regeneration of samples.
	tracker.cfgGroups = models.MapGroupTrackerConfig{
		models.Group(mockDeviceIDThreshold): &models.TrackerConfig{
			Retention:   3 * time.Hour, // pick a large number that's easy to debug
			Granularity: 1 * time.Minute,
			Threshold:   1 * time.Minute,
			Mode:        models.ModeMonitor,
		},
	}
	tracker.AddSample(mockDeviceIDThreshold, true)
	tracker.AddSample(mockDeviceIDThreshold, true)
	count := countSamples(t, tracker, mockDeviceIDThreshold)
	assert.Equal(t, 2, count, "AddSample did not regenerate samples")
	// Cause config to be remade.
	tracker.cfgGroups[models.Group(mockDeviceIDThreshold)].Threshold = 5 * time.Minute
	tracker.AddSample(mockDeviceIDThreshold, true)
	count = countSamples(t, tracker, mockDeviceIDThreshold)
	assert.Equal(t, 1, count, "AddSample did not regenerate samples")

}

func countSamples(t *testing.T, tracker *Tracker, deviceID string) int {
	d, ok := tracker.devices.Load(deviceID)
	assert.True(t, ok, "could not load the deviceData")
	dd := d.(*deviceData)
	count := 0
	for _, seen := range dd.samples {
		if seen {
			count++
		}
	}
	return count
}

func TestAddSample_SamplesAreSaved(t *testing.T) {
	ctx := context.Background()

	// Setup: Create a tracker with 1-hour retention and 1-minute granularity.
	cfg := &models.TrackerConfig{
		Retention:   1 * time.Hour,
		Granularity: 1 * time.Minute,
		Threshold:   10 * time.Minute,
	}

	tracker, err := NewTracker(ctx, config.MustGetLogger(), cfg)
	assert.NoError(t, err, "NewTracker failed")

	deviceID := "Test-Device"

	// Mock current time to control time progression in tests.
	now := time.Now().Truncate(cfg.Granularity)

	// Override time.Now function in the tracker to use the mocked time.
	tracker.nowFunc = func() time.Time {
		return now
	}

	// Case 1a: Add a sample at the start of the buffer and verify that we cannot find the mixed case device ID.
	tracker.AddSample(deviceID, true)
	data, ok := tracker.devices.Load(strings.ToLower(deviceID)) // use lower case to assert case sensitivity
	assert.False(t, ok, "AddSample should not find data by mixed case device ID")

	// Case 1b: Add a sample at the start of the buffer and verify that we can find the device ID.
	data, ok = tracker.devices.Load(deviceID)
	if !ok {
		t.Fatalf("AddSample did not initialize device data")
	}

	dd := data.(*deviceData)
	index := dd.getIndex(now, dd.windowStartTime)
	if !dd.samples[index] {
		t.Errorf("AddSample failed to mark the sample at index %d", index)
	}

	// Case 2: Add a sample at a later time within the same hour.
	now = now.Add(5 * cfg.Granularity) // Advance time by 5 minutes.
	tracker.AddSample(deviceID, true)

	index = dd.getIndex(now, dd.windowStartTime)
	if !dd.samples[index] {
		t.Errorf("AddSample failed to mark the sample at index %d for time %v", index, now)
	}

	// Case 3: Add a sample after the retention period has passed.
	now = now.Add(cfg.Retention)      // Advance time by 1 hour.
	tracker.AddSample(deviceID, true) // This should reset the whole buffer and record a new one.
	// Case 3a: Verify that the device data was reinitialized.
	data, ok = tracker.devices.Load(deviceID)
	if !ok {
		t.Fatalf("AddSample did not reinitialize device data after retention period")
	}
	// Case 3b: Verify that the sample was recorded.
	dd = data.(*deviceData)
	index = dd.getIndex(now, dd.windowStartTime)
	if !dd.samples[index] {
		t.Errorf("AddSample failed to mark the sample at index %d after retention period", index)
	}

	// Case 4: Add multiple samples in rapid succession.
	now = now.Add(2 * cfg.Granularity) // Advance time by 2 minutes.
	for i := 0; i < 3; i++ {
		tracker.AddSample(deviceID, true)
		index = dd.getIndex(now, dd.windowStartTime)
		if !dd.samples[index] {
			t.Errorf("AddSample failed to mark the sample at index %d on iteration %d", index, i)
		}
		now = now.Add(cfg.Granularity) // Advance time by 1 minute for next iteration.
	}

	// Case 5a: Add a sample with a large time jump forward.
	now = now.Add(2 * cfg.Retention) // Advance time by 2 hours.
	tracker.AddSample(deviceID, true)
	// Case 5a: Verify that the device data was reinitialized.
	data, ok = tracker.devices.Load(deviceID)
	if !ok {
		t.Fatalf("AddSample did not reinitialize device data after large time jump")
	}
	// Case 5b: Verify that the sample was recorded.
	dd = data.(*deviceData)
	index = dd.getIndex(now, dd.windowStartTime)
	if !dd.samples[index] {
		t.Errorf("AddSample failed to mark the sample at index %d after large time jump", index)
	}
}

func TestGetIndex(t *testing.T) {
	// Setup: Create a tracker with a 1-hour retention and 1-minute granularity.
	cfg := &models.TrackerConfig{
		Retention:   1 * time.Hour,
		Granularity: 1 * time.Minute,
	}

	startTime := time.Date(2023, 1, 1, 10, 0, 0, 0, time.UTC)

	data := newDeviceData(startTime, cfg)

	tests := []struct {
		now      time.Time
		expected int
	}{
		{startTime.Add(0 * cfg.Granularity), 0},
		{startTime.Add(1 * cfg.Granularity), 1},
		{startTime.Add(59 * cfg.Granularity), 59},
		{startTime.Add(60 * cfg.Granularity), 0},  // Wraps around.
		{startTime.Add(-1 * cfg.Granularity), 59}, // Negative time wraps to last index.
	}

	for _, test := range tests {
		index := data.getIndex(test.now, startTime)
		if index != test.expected {
			t.Errorf("getIndex(%v, %v) = %d; want %d", test.now, startTime, index, test.expected)
		}
	}
}

func TestSyncWindow(t *testing.T) {
	logger := config.MustGetLogger()

	// Setup - Create a tracker with a 1-hour retention and 1-minute granularity.
	cfg := &models.TrackerConfig{
		Granularity: 1 * time.Minute,
		Retention:   1 * time.Hour,
	}

	data := newDeviceData(time.Now(), cfg) // No threshold needed for this test.

	// Simulate a device data structure.
	// startTime := time.Now().Truncate(granularity)
	startTime, _ := data.calculateWindow(time.Now())

	// Mark a few initial samples as true.
	data.samples[0] = true
	data.samples[1] = true
	data.samples[2] = true

	// Case 1: No elapsed time.
	data.syncWindow(logger, startTime)
	if !data.samples[0] || !data.samples[1] || !data.samples[2] {
		t.Error("syncWindow cleared samples when no time had elapsed")
	}

	// Case 2: Elapsed time exceeds retention (expect the buffer to be reset).
	exceedTime := startTime.Add(2 * cfg.Retention)
	data.syncWindow(logger, exceedTime)
	expectedNewTime, _ := data.calculateWindow(exceedTime)
	for i, v := range data.samples {
		if v {
			t.Errorf("syncWindow failed to reset the buffer at index %d", i)
		}
	}
	if !data.windowStartTime.Equal(exceedTime.Truncate(cfg.Granularity)) {
		t.Errorf("syncWindow did not update the start time correctly. Got %v, want %v", data.windowStartTime, expectedNewTime)
	}
}

func TestCalculateWindow(t *testing.T) {
	tests := []struct {
		name         string
		config       *models.TrackerConfig
		now          time.Time
		expectedLast time.Time
		expectedNext time.Time
	}{
		{
			name: "Weekly Retention - Current Week",
			config: &models.TrackerConfig{
				Retention:     7 * 24 * time.Hour,
				StartDayInt:   int(time.Monday),
				StartDuration: 10 * time.Hour,
			},
			now:          time.Date(2024, 12, 2, 15, 0, 0, 0, time.UTC), // Monday
			expectedLast: time.Date(2024, 12, 2, 10, 0, 0, 0, time.UTC),
			expectedNext: time.Date(2024, 12, 9, 10, 0, 0, 0, time.UTC),
		},
		{
			name: "Daily Retention - Current Day",
			config: &models.TrackerConfig{
				Retention:     24 * time.Hour,
				StartDayInt:   int(time.Sunday), // Ignored for daily retention
				StartDuration: 6 * time.Hour,
			},
			now:          time.Date(2024, 12, 2, 15, 0, 0, 0, time.UTC), // Monday
			expectedLast: time.Date(2024, 12, 2, 6, 0, 0, 0, time.UTC),
			expectedNext: time.Date(2024, 12, 3, 6, 0, 0, 0, time.UTC),
		},
		{
			name: "Sub-Daily Retention",
			config: &models.TrackerConfig{
				Retention:     12 * time.Hour,
				StartDayInt:   0, // Ignored for sub-daily retention
				StartDuration: 3 * time.Hour,
			},
			now:          time.Date(2024, 12, 2, 15, 0, 0, 0, time.UTC), // Monday
			expectedLast: time.Date(2024, 12, 2, 15, 0, 0, 0, time.UTC),
			expectedNext: time.Date(2024, 12, 3, 3, 0, 0, 0, time.UTC),
		},
		{
			name: "Sub-Daily Retention 2",
			config: &models.TrackerConfig{
				Retention:     10 * time.Minute,
				StartDayInt:   0, // Ignored for sub-daily retention
				StartDuration: 3 * time.Minute,
			},
			now:          time.Date(2024, 12, 2, 13, 50, 0, 0, time.UTC), // Monday
			expectedLast: time.Date(2024, 12, 2, 13, 43, 0, 0, time.UTC),
			expectedNext: time.Date(2024, 12, 2, 13, 53, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := newDeviceData(tt.now, tt.config)
			lastWindowStart, nextWindowStart := data.calculateWindow(tt.now)

			if !lastWindowStart.Equal(tt.expectedLast) {
				t.Errorf("%v: lastWindowStart: got %v, want %v", tt.name, lastWindowStart, tt.expectedLast)
			}
			if !nextWindowStart.Equal(tt.expectedNext) {
				t.Errorf("%v: nextWindowStart: got %v, want %v", tt.name, nextWindowStart, tt.expectedNext)
			}
		})
	}
}

func saveSomeSamples(t *testing.T) (*sync.Map, *os.File, error) {
	// Create a temporary file for testing.
	tmpFile, err := os.CreateTemp("", "samples_test_*.json")
	assert.NoError(t, err, "Failed to create temp file")

	// Create sample data.
	devices := &sync.Map{}
	devices.Store("device1", &deviceData{
		config:          getDefaultGroupTrackerConfig(&config.AppCfg.TrackerConfig),
		samples:         []bool{true, false, true, false},
		windowStartTime: time.Now().UTC(),
		mu:              &sync.Mutex{},
	})
	devices.Store("device2", &deviceData{
		config:          getDefaultGroupTrackerConfig(&config.AppCfg.TrackerConfig),
		samples:         []bool{false, true, false, true},
		windowStartTime: time.Now().Add(-time.Hour).UTC(),
		mu:              &sync.Mutex{},
	})

	err = saveSamples(config.MustGetLogger(), tmpFile.Name(), devices)

	return devices, tmpFile, err
}

// TestSaveAndLoadSamples tests saving and loading samples to/from a file.
func TestSaveAndLoadSamples(t *testing.T) {
	// Test SaveSamples
	_, tmpFile, err := saveSomeSamples(t)
	assert.NoError(t, err, "Failed to save samples")

	// Test LoadSamples
	loadedDevices, err := loadSamples(tmpFile.Name())
	assert.NoError(t, err, "Failed to load samples")

	// Verify loaded data.
	loadedDevice1, _ := loadedDevices.Load("device1")
	loadedDevice2, _ := loadedDevices.Load("device2")

	// Type assertion
	ld1 := loadedDevice1.(*deviceData)
	ld2 := loadedDevice2.(*deviceData)

	// Validate device1
	assert.Equal(t, []bool{true, false, true, false}, ld1.samples, "Device1 samples mismatch")
	assert.WithinDuration(t, ld1.windowStartTime, time.Now(), time.Minute, "Device1 start time mismatch")

	// Validate device2
	assert.Equal(t, []bool{false, true, false, true}, ld2.samples, "Device2 samples mismatch")
	assert.WithinDuration(t, ld2.windowStartTime, time.Now().Add(-time.Hour), time.Minute, "Device2 start time mismatch")
}

// TestLoadNonExistentFile tests loading from a non-existent file.
func TestLoadNonExistentFile(t *testing.T) {
	_, err := loadSamples("nonexistent_file.json")
	assert.Error(t, err, "Expected error for non-existent file")
}

// TestCorruptFile tests loading from a corrupted file.
func TestCorruptFile(t *testing.T) {
	// Create a temporary corrupt file.
	tmpFile, err := os.CreateTemp("", "corrupt_test_*.json")
	assert.NoError(t, err, "Failed to create temp file")
	defer func(name string) {
		_ = os.Remove(name)
	}(tmpFile.Name()) // Clean up after test

	// Write invalid JSON content.
	_, err = tmpFile.WriteString("{invalid_json}")
	assert.NoError(t, err, "Failed to write corrupt data")
	_ = tmpFile.Close()

	// Try loading the corrupt file.
	_, err = loadSamples(tmpFile.Name())
	assert.Error(t, err, "Expected error for corrupt file")
}

// TestResetSamples tests resetting samples for a device.
func TestResetSamples(t *testing.T) {
	// Setup: Create a tracker with 1-hour retention and 1-minute granularity.
	cfg := &models.TrackerConfig{
		Retention:   1 * time.Hour,
		Granularity: 1 * time.Minute,
		Threshold:   10 * time.Minute,
	}

	testDevice := "test-device"

	tracker, err := NewTracker(context.Background(), config.MustGetLogger(), cfg)
	assert.NoError(t, err, "NewTracker failed")

	tracker.AddSample(testDevice, true)
	_, ok := tracker.devices.Load(testDevice)
	assert.True(t, ok, "Device should exist in tracker")

	tracker.Reset(testDevice)
	_, ok = tracker.devices.Load(testDevice)
	assert.False(t, ok, "Device should not be found in tracker")
}

func TestNewTracker_GetGroupConfig(t *testing.T) {
	ctx := context.Background()
	logger := config.MustGetLogger()

	t.Cleanup(func() {
		restoreFunctions()
	})

	cfg := &config.AppCfg.TrackerConfig
	if cfg.SampleFileSaveInterval == 0 {
		t.Fatal("SampleFileSaveInterval must be set by default to a positive value")
	}

	d := &sync.Map{}
	fnLoadSamples = func(path string) (*sync.Map, error) {
		return d, nil
	}

	saveSamplesPeriodicallyWasCalled := false
	done := make(chan struct{})
	fnSaveSamplesPeriodically = func(ctx context.Context, logger *zap.SugaredLogger, devicesToSave *sync.Map, filePath string, interval time.Duration) {
		saveSamplesPeriodicallyWasCalled = true
		done <- struct{}{}
	}

	// Mock getGroupTrackerConfig so it returns an error.
	fnGetGroupTrackerConfig = func(mu *sync.Mutex, configPath string, _ func() models.MapGroupTrackerConfig) (models.MapGroupTrackerConfig, error) {
		return nil, errors.New("mocked error for getGroupTrackerConfig")
	}
	tracker, err := NewTracker(ctx, logger, cfg)
	assert.Error(t, err, "NewTracker should fail")
	assert.Nil(t, tracker, "Tracker should be nil")

	// Mock the fnGetGroupTrackerConfig so it does not error.
	fnGetGroupTrackerConfig = func(mu *sync.Mutex, configPath string, _ func() models.MapGroupTrackerConfig) (models.MapGroupTrackerConfig, error) {
		return models.MapGroupTrackerConfig{}, nil
	}
	tracker, err = NewTracker(ctx, logger, cfg)
	assert.NoError(t, err, "NewTracker failed")
	assert.NotNil(t, tracker, "Tracker should not be nil")
	assert.False(t, saveSamplesPeriodicallyWasCalled, "saveSamples should not be called when SampleFilePath is empty")

	// Test that samples are loaded and periodically.
	cfg.SampleFilePath = "dummy-file.json"
	tracker, err = NewTracker(ctx, logger, cfg)
	assert.NoError(t, err, "NewTracker failed")
	assert.NotNil(t, tracker, "Tracker should not be nil")
	<-done
	assert.True(t, saveSamplesPeriodicallyWasCalled, "saveSamples should be called when SampleFilePath is not empty")
	assert.Equal(t, d, tracker.devices)
}

func TestNewTracker_SampleFilePathInvalid(t *testing.T) {
	ctx := context.Background()
	logger := config.MustGetLogger()

	t.Cleanup(func() {
		restoreFunctions()
	})

	cfg := &models.TrackerConfig{
		SampleFilePath: "nonexistent-path.json",
	}

	// Mock the fnGetGroupTrackerConfig to be a noop.
	fnGetGroupTrackerConfig = func(mu *sync.Mutex, configPath string, _ func() models.MapGroupTrackerConfig) (models.MapGroupTrackerConfig, error) {
		return nil, nil
	}

	// Mock fnGetTrackerSamplesFile to return an error.
	fnGetTrackerSamplesFile = func(sampleFilePath string) (string, error) {
		return "", errors.New("mocked error for missing sample file path")
	}

	tracker, err := NewTracker(ctx, logger, cfg)
	assert.Error(t, err, "expected error for missing sample file path")
	assert.Nil(t, tracker, "expected tracker to be nil, but got a valid instance")
}

func TestNewTracker_LoadSamples(t *testing.T) {
	ctx := context.Background()
	logger := config.MustGetLogger()

	t.Cleanup(func() {
		restoreFunctions()
	})

	cfg := &models.TrackerConfig{
		SampleFilePath: "dummy-sample-file.json",
	}

	// Mock the fnGetGroupTrackerConfig to be a noop.
	fnGetGroupTrackerConfig = func(mu *sync.Mutex, configPath string, _ func() models.MapGroupTrackerConfig) (models.MapGroupTrackerConfig, error) {
		return nil, nil
	}

	// Mock fnGetTrackerSamplesFile to return a valid path.
	fnGetTrackerSamplesFile = func(sampleFilePath string) (string, error) {
		return "mocked-sample-file-path.json", nil
	}

	// Mock loadSamples to return an error.
	fnLoadSamples = func(path string) (*sync.Map, error) {
		return nil, errors.New("mocked error for loadSamples")
	}

	tracker, err := NewTracker(ctx, logger, cfg)
	assert.NoError(t, err, "expected no error from unable to load samples, but got: %v", err)
	assert.NotNil(t, tracker, "expected tracker to be non-nil, but got nil")
}

func TestNewTracker_HandlesEmptySampleFilePath(t *testing.T) {
	logger := config.MustGetLogger()

	t.Cleanup(func() {
		restoreFunctions()
	})

	// Config with empty SampleFilePath
	cfg := &models.TrackerConfig{
		SampleFilePath:         "",
		SampleFileSaveInterval: time.Minute,
	}

	// Mock the fnGetGroupTrackerConfig to be a noop.
	fnGetGroupTrackerConfig = func(mu *sync.Mutex, configPath string, _ func() models.MapGroupTrackerConfig) (models.MapGroupTrackerConfig, error) {
		return nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tracker, err := NewTracker(ctx, logger, cfg)
	assert.NoError(t, err, "expected no error, but got: %v", err)
	assert.NotNil(t, tracker, "expected tracker to be non-nil, but got nil")
}

func TestTracker_SetMode(t *testing.T) {
	ctx := context.Background()
	// logger := config.MustGetLogger()

	// Setup: Create a tracker with a 1-hour retention, 10-minute threshold, and 1-minute granularity.
	cfg := &models.TrackerConfig{
		Granularity:            1 * time.Minute,
		Retention:              1 * time.Hour,
		Threshold:              10 * time.Minute,
		StartDayInt:            0,
		StartDuration:          0,
		SampleSize:             0,
		ModeEndTime:            time.Time{},
		Mode:                   models.ModeMonitor,
		SampleFileSaveInterval: 50 * time.Millisecond,
	}

	fnGetGroupTrackerConfig = func(mu *sync.Mutex, configPath string, _ func() models.MapGroupTrackerConfig) (models.MapGroupTrackerConfig, error) {
		return models.MapGroupTrackerConfig{
			"GroupA": {
				Granularity:   1 * time.Minute,
				Retention:     30 * time.Minute,
				Threshold:     5 * time.Minute,
				StartDayInt:   0,
				StartDuration: 0,
				SampleSize:    1,
				ModeEndTime:   time.Time{},
				Mode:          models.ModeAllow,
			},
		}, nil
	}

	configWasSaved := false
	config.FnDefaultSafeWriteViaTemp = func(filePath string, data string) error {
		configWasSaved = true
		return nil
	}

	t.Cleanup(func() {
		config.FnDefaultSafeWriteViaTemp = config.SafeWriteViaTemp // restore the file writer func
		fnGetGroupTrackerConfig = config.GetConfig
	})

	// Test Cases.

	tracker, err := NewTracker(ctx, config.MustGetLogger(), cfg)
	assert.NoError(t, err, "NewTracker failed")

	now := time.Now()
	tracker.nowFunc = func() time.Time {
		return now
	}

	deviceID := "test-device" // device not in group config faked above.
	tracker.AddSample(deviceID, true)

	data, ok := tracker.devices.Load(deviceID) // save data that doesn't already exist so it takes default values
	assert.True(t, ok, "expected device to be loaded")
	dd := data.(*deviceData)
	assert.Equal(t, models.ModeMonitor, dd.config.Mode, "expected default mode to be monitor")
	assert.Equal(t, time.Time{}, dd.config.ModeEndTime, "expected default mode to be default end-time")

	err = tracker.SetMode(deviceID, time.Minute, models.ModeAllow)
	assert.NoError(t, err, "expected no error setting mode")
	assert.Equal(t, models.ModeAllow, dd.config.Mode, "expected mode to be allow")
	assert.Equal(t, now.Add(time.Minute), dd.config.ModeEndTime, "expected mode end time to bet set")

	groupData, ok := tracker.cfgGroups[models.Group(deviceID)]
	assert.True(t, ok, "expected device group to be loaded")
	assert.Equal(t, dd.config.Mode, groupData.Mode, "expected central group data mode to match the mode we set")
	assert.Equal(t, dd.config.ModeEndTime, groupData.ModeEndTime, "expected mode end time to be set in the central group data")
	assert.True(t, configWasSaved, "expected central group config to be saved")
}

// TestValidateGroupTrackerConfig_SampleSize ensures that validateGroupTrackerConfig
// correctly sets the SampleSize value for each valid group.
func TestValidateGroupTrackerConfig_SampleSize(t *testing.T) {
	// Create a dummy TrackerConfig for the test group.
	dummyConfig := &models.TrackerConfig{
		Granularity:   5 * time.Minute, // this is overridden to 1min
		Retention:     60 * time.Minute,
		Threshold:     1 * time.Minute,
		StartDayInt:   1,
		StartDuration: 0,
		ModeEndTime:   time.Now().UTC().Add(time.Hour), // expires in one hour.
		Mode:          models.ModeMonitor,
		// SampleSize is expected to be set by validateGroupTrackerConfig.
	}

	// Create a map with one group entry.
	cfg := models.MapGroupTrackerConfig{
		"test-group": dummyConfig,
	}

	// Call validateGroupTrackerConfig.
	err := validateGroupTrackerConfig(cfg)
	assert.NoError(t, err)

	// Assert that SampleSize has been set to the value returned by the test stub.
	assert.Equal(t, 60, cfg["test-group"].SampleSize, "SampleSize should be set correctly by getSampleSize")

	// TODO: test more of the validateGroupTrackerConfig() mutations.
}
