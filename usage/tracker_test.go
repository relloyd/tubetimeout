package usage

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"example.com/tubetimeout/config"
	"github.com/stretchr/testify/assert"
)

func TestNewTracker(t *testing.T) {
	ctx := context.Background()

	// Generate some sample data to load into the tracker.
	devices, tmpFile, err := saveSomeSamples(t)
	assert.NoError(t, err, "Failed to save samples")

	// Case 1: Create a tracker with a 1-hour retention, 10-minute threshold, and 1-minute granularity.
	cfg := &config.TrackerConfig{
		Retention:              1 * time.Hour,
		Threshold:              10 * time.Minute,
		Granularity:            1 * time.Minute,
		SampleFilePath:         tmpFile.Name(),
		SampleFileSaveInterval: 50 * time.Millisecond,
	}

	// Mock the file saver func.
	savedFileCount := 0
	fnSaveSamples = func(path string, devices *sync.Map) error {
		savedFileCount++
		return nil
	}

	// Mock the file path getter func.
	fnGetTrackerConfigFile = func(path string) (string, error) {
		return path, nil
	}

	tracker, err := NewTracker(ctx, config.MustGetLogger(), cfg)

	assert.NoError(t, err, "NewTracker failed")
	assert.NotNil(t, tracker, "NewTracker returned nil")
	assert.NotNil(t, tracker.devices, "NewTracker did not initialize devices map")
	assert.Equal(t, cfg.Retention, tracker.retention, "NewTracker did not set retention")
	assert.Equal(t, cfg.Granularity, tracker.granularity, "NewTracker did not set granularity")
	assert.Equal(t, cfg.Threshold, tracker.threshold, "NewTracker did not set threshold")
	assert.NotNil(t, tracker.nowFunc, "NewTracker did not set a default nowFunc")

	// Test that the tracker loads the same samples that we saved.
	devices.Range(func(key, value interface{}) bool {
		tdv, ok := tracker.devices.Load(key)
		assert.True(t, ok, "NewTracker did not load samples from file")
		assert.Equal(t, value, tdv, "NewTracker did not load samples from file")
		return true
	})

	// Test that the saveSamplesPeriodically goroutine was started.
	time.Sleep(50 * time.Millisecond)
	assert.GreaterOrEqual(t, savedFileCount, 1, "saveSamplesPeriodically goroutine was not started")
}

func TestHasExceededThreshold(t *testing.T) {
	ctx := context.Background()

	// Setup: Create a tracker with a 1-hour retention, 10-minute threshold, and 1-minute granularity.
	cfg := &config.TrackerConfig{
		Retention:   1 * time.Hour,
		Threshold:   10 * time.Minute,
		Granularity: 1 * time.Minute,
	}

	tracker, err := NewTracker(ctx, config.MustGetLogger(), cfg)
	assert.NoError(t, err, "NewTracker failed")

	// Simulate a device data structure with pre-allocated samples.
	startTime := time.Now().Truncate(cfg.Granularity)
	deviceID := "test-device"
	deviceData := &deviceData{
		samples: make([]bool, tracker.sampleSize),
		start:   startTime,
		mu:      &sync.Mutex{},
	}

	// Add device data to the tracker.
	tracker.devices.Store(deviceID, deviceData)

	// Case 1: No samples recorded.
	if tracker.HasExceededThreshold(deviceID) {
		t.Error("HasExceededThreshold returned true with no samples recorded")
	}

	// Case 2: Samples recorded but below threshold.
	for i := 0; i < 5; i++ {
		deviceData.samples[i] = true // Mark 5 minutes as seen.
	}
	if tracker.HasExceededThreshold(deviceID) {
		t.Error("HasExceededThreshold returned true with samples below the threshold")
	}

	// Case 3: Samples meet the threshold.
	for i := 0; i < 10; i++ {
		deviceData.samples[i] = true // Mark 10 minutes as seen.
	}
	if !tracker.HasExceededThreshold(deviceID) {
		t.Error("HasExceededThreshold returned false with samples meeting the threshold")
	}

	// Case 4: Simulate backward time adjustment.
	// Step 4a: Mark all slots as seen.
	for i := 0; i < tracker.sampleSize; i++ {
		deviceData.samples[i] = true
	}
	// Step 4b: Move `start` time backward by 2 hours.
	// This simulates an old start time being used.
	deviceData.start = startTime.Add(-2 * time.Hour)
	// Step 4c: Test that stale samples are ignored and the buffer is reset.
	if tracker.HasExceededThreshold(deviceID) {
		t.Errorf("HasExceededThreshold incorrectly included stale samples or did not reset after backward time adjustment")
	}
	// Step 4d: Reset the device data with valid samples and verify behavior.
	deviceData.start = startTime
	for i := range deviceData.samples {
		deviceData.samples[i] = false
	}
	for i := 0; i < 10; i++ {
		deviceData.samples[i] = true
	}
	if !tracker.HasExceededThreshold(deviceID) {
		t.Error("HasExceededThreshold returned false with valid samples meeting the threshold")
	}
}

func TestAddSample(t *testing.T) {
	ctx := context.Background()

	// Setup: Create a tracker with 1-hour retention and 1-minute granularity.
	cfg := &config.TrackerConfig{
		Retention:   1 * time.Hour,
		Granularity: 1 * time.Minute,
		Threshold:   10 * time.Minute,
	}

	tracker, err := NewTracker(ctx, config.MustGetLogger(), cfg)
	assert.NoError(t, err, "NewTracker failed")

	deviceID := "test-device"

	// Mock current time to control time progression in tests.
	now := time.Now().Truncate(cfg.Granularity)

	// Override time.Now function in the tracker to use the mocked time.
	tracker.nowFunc = func() time.Time {
		return now
	}

	// Case 1: Add a sample at the start of the buffer.
	tracker.AddSample(deviceID)

	data, ok := tracker.devices.Load(deviceID)
	if !ok {
		t.Fatalf("AddSample did not initialize device data")
	}

	dd := data.(*deviceData)
	index := tracker.getIndex(now, dd.start)
	if !dd.samples[index] {
		t.Errorf("AddSample failed to mark the sample at index %d", index)
	}

	// Case 2: Add a sample at a later time within the same hour.
	now = now.Add(5 * cfg.Granularity) // Advance time by 5 minutes.
	tracker.AddSample(deviceID)

	index = tracker.getIndex(now, dd.start)
	if !dd.samples[index] {
		t.Errorf("AddSample failed to mark the sample at index %d for time %v", index, now)
	}

	// Case 3: Add a sample after the retention period has passed.
	now = now.Add(cfg.Retention) // Advance time by 1 hour.
	tracker.AddSample(deviceID)  // This should reset the whole buffer and record a new one.
	// Case 3a: Verify that the device data was reinitialized.
	data, ok = tracker.devices.Load(deviceID)
	if !ok {
		t.Fatalf("AddSample did not reinitialize device data after retention period")
	}
	// Case 3b: Verify that the sample was recorded.
	dd = data.(*deviceData)
	index = tracker.getIndex(now, dd.start)
	if !dd.samples[index] {
		t.Errorf("AddSample failed to mark the sample at index %d after retention period", index)
	}

	// Case 4: Add multiple samples in rapid succession.
	now = now.Add(2 * cfg.Granularity) // Advance time by 2 minutes.
	for i := 0; i < 3; i++ {
		tracker.AddSample(deviceID)
		index = tracker.getIndex(now, dd.start)
		if !dd.samples[index] {
			t.Errorf("AddSample failed to mark the sample at index %d on iteration %d", index, i)
		}
		now = now.Add(cfg.Granularity) // Advance time by 1 minute for next iteration.
	}

	// Case 5: Add a sample with a large time jump forward.
	now = now.Add(2 * cfg.Retention) // Advance time by 2 hours.
	tracker.AddSample(deviceID)
	// Case 5a: Verify that the device data was reinitialized.
	data, ok = tracker.devices.Load(deviceID)
	if !ok {
		t.Fatalf("AddSample did not reinitialize device data after large time jump")
	}
	// Case 5b: Verify that the sample was recorded.
	dd = data.(*deviceData)
	index = tracker.getIndex(now, dd.start)
	if !dd.samples[index] {
		t.Errorf("AddSample failed to mark the sample at index %d after large time jump", index)
	}
}

func TestGetIndex(t *testing.T) {
	ctx := context.Background()

	// Setup: Create a tracker with a 1-hour retention and 1-minute granularity.
	cfg := &config.TrackerConfig{
		Retention:   1 * time.Hour,
		Granularity: 1 * time.Minute,
	}
	tracker, err := NewTracker(ctx, config.MustGetLogger(), cfg)
	assert.NoError(t, err, "NewTracker failed")

	startTime := time.Date(2023, 1, 1, 10, 0, 0, 0, time.UTC)

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
		index := tracker.getIndex(test.now, startTime)
		if index != test.expected {
			t.Errorf("getIndex(%v, %v) = %d; want %d", test.now, startTime, index, test.expected)
		}
	}
}

func TestSyncWindow(t *testing.T) {
	ctx := context.Background()

	// Setup - Create a tracker with a 1-hour retention and 1-minute granularity.
	cfg := &config.TrackerConfig{
		Granularity: 1 * time.Minute,
		Retention:   1 * time.Hour,
	}

	tracker, err := NewTracker(ctx, config.MustGetLogger(), cfg) // No threshold needed for this test.
	assert.NoError(t, err, "NewTracker failed")

	// Simulate a device data structure.
	// startTime := time.Now().Truncate(granularity)
	startTime, _ := tracker.calculateWindow(time.Now())
	deviceData := &deviceData{
		samples: make([]bool, tracker.sampleSize),
		start:   startTime,
	}

	// Mark a few initial samples as true.
	deviceData.samples[0] = true
	deviceData.samples[1] = true
	deviceData.samples[2] = true

	// Case 1: No elapsed time.
	tracker.syncWindow(deviceData, startTime)
	if !deviceData.samples[0] || !deviceData.samples[1] || !deviceData.samples[2] {
		t.Error("syncWindow cleared samples when no time had elapsed")
	}

	// Case 2: Elapsed time exceeds retention (expect the buffer to be reset).
	exceedTime := startTime.Add(2 * cfg.Retention)
	tracker.syncWindow(deviceData, exceedTime)
	expectedNewTime, _ := tracker.calculateWindow(exceedTime)
	for i, v := range deviceData.samples {
		if v {
			t.Errorf("syncWindow failed to reset the buffer at index %d", i)
		}
	}
	if !deviceData.start.Equal(exceedTime.Truncate(cfg.Granularity)) {
		t.Errorf("syncWindow did not update the start time correctly. Got %v, want %v", deviceData.start, expectedNewTime)
	}
}

func TestCalculateWindow(t *testing.T) {
	tests := []struct {
		name         string
		tracker      Tracker
		now          time.Time
		expectedLast time.Time
		expectedNext time.Time
	}{
		{
			name: "Weekly Retention - Current Week",
			tracker: Tracker{
				retention:       7 * 24 * time.Hour,
				windowStartDay:  int(time.Monday),
				windowStartTime: 10 * time.Hour,
			},
			now:          time.Date(2024, 12, 2, 15, 0, 0, 0, time.UTC), // Monday
			expectedLast: time.Date(2024, 12, 2, 10, 0, 0, 0, time.UTC),
			expectedNext: time.Date(2024, 12, 9, 10, 0, 0, 0, time.UTC),
		},
		{
			name: "Daily Retention - Current Day",
			tracker: Tracker{
				retention:       24 * time.Hour,
				windowStartDay:  int(time.Sunday), // Ignored for daily retention
				windowStartTime: 6 * time.Hour,
			},
			now:          time.Date(2024, 12, 2, 15, 0, 0, 0, time.UTC), // Monday
			expectedLast: time.Date(2024, 12, 2, 6, 0, 0, 0, time.UTC),
			expectedNext: time.Date(2024, 12, 3, 6, 0, 0, 0, time.UTC),
		},
		{
			name: "Sub-Daily Retention",
			tracker: Tracker{
				retention:       12 * time.Hour,
				windowStartDay:  0, // Ignored for sub-daily retention
				windowStartTime: 3 * time.Hour,
			},
			now:          time.Date(2024, 12, 2, 15, 0, 0, 0, time.UTC), // Monday
			expectedLast: time.Date(2024, 12, 2, 15, 0, 0, 0, time.UTC),
			expectedNext: time.Date(2024, 12, 3, 3, 0, 0, 0, time.UTC),
		},
		{
			name: "Sub-Daily Retention 2",
			tracker: Tracker{
				retention:       10 * time.Minute,
				windowStartDay:  0, // Ignored for sub-daily retention
				windowStartTime: 3 * time.Minute,
			},
			now:          time.Date(2024, 12, 2, 13, 50, 0, 0, time.UTC), // Monday
			expectedLast: time.Date(2024, 12, 2, 13, 43, 0, 0, time.UTC),
			expectedNext: time.Date(2024, 12, 2, 13, 53, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lastWindowStart, nextWindowStart := tt.tracker.calculateWindow(tt.now)

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
		samples: []bool{true, false, true, false},
		start:   time.Now().UTC(),
		mu:      &sync.Mutex{},
	})
	devices.Store("device2", &deviceData{
		samples: []bool{false, true, false, true},
		start:   time.Now().Add(-time.Hour).UTC(),
		mu:      &sync.Mutex{},
	})

	err = saveSamples(tmpFile.Name(), devices)

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
	assert.WithinDuration(t, ld1.start, time.Now(), time.Minute, "Device1 start time mismatch")

	// Validate device2
	assert.Equal(t, []bool{false, true, false, true}, ld2.samples, "Device2 samples mismatch")
	assert.WithinDuration(t, ld2.start, time.Now().Add(-time.Hour), time.Minute, "Device2 start time mismatch")
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
