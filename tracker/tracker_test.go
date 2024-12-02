package tracker

import (
	"reflect"
	"testing"
	"time"
)

func TestHasExceededThreshold(t *testing.T) {
	// Setup: Create a tracker with a 1-hour retention, 10-minute threshold, and 1-minute granularity.
	retention := 1 * time.Hour
	threshold := 10 * time.Minute
	granularity := 1 * time.Minute
	tracker := NewTracker(retention, threshold, granularity)

	// Simulate a device data structure with pre-allocated samples.
	startTime := time.Now().Truncate(granularity)
	deviceID := "test-device"
	deviceData := &deviceData{
		samples: make([]bool, tracker.sampleSize),
		start:   startTime,
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
	// Setup: Create a tracker with 1-hour retention and 1-minute granularity.
	retention := 1 * time.Hour
	granularity := 1 * time.Minute
	threshold := 10 * time.Minute
	tracker := NewTracker(retention, threshold, granularity)

	deviceID := "test-device"

	// Mock current time to control time progression in tests.
	now := time.Now().Truncate(granularity)

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
	now = now.Add(5 * granularity) // Advance time by 5 minutes.
	tracker.AddSample(deviceID)

	index = tracker.getIndex(now, dd.start)
	if !dd.samples[index] {
		t.Errorf("AddSample failed to mark the sample at index %d for time %v", index, now)
	}

	// Case 3: Add a sample after the retention period has passed.
	now = now.Add(retention) // Advance time by 1 hour.
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
	now = now.Add(2 * granularity) // Advance time by 2 minutes.
	for i := 0; i < 3; i++ {
		tracker.AddSample(deviceID)
		index = tracker.getIndex(now, dd.start)
		if !dd.samples[index] {
			t.Errorf("AddSample failed to mark the sample at index %d on iteration %d", index, i)
		}
		now = now.Add(granularity) // Advance time by 1 minute for next iteration.
	}

	// Case 5: Add a sample with a large time jump forward.
	now = now.Add(2 * retention) // Advance time by 2 hours.
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
	// Setup: Create a tracker with a 1-hour retention and 1-minute granularity.
	retention := 1 * time.Hour
	granularity := 1 * time.Minute
	tracker := NewTracker(retention, 0, granularity)

	startTime := time.Date(2023, 1, 1, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		now      time.Time
		expected int
	}{
		{startTime.Add(0 * granularity), 0},
		{startTime.Add(1 * granularity), 1},
		{startTime.Add(59 * granularity), 59},
		{startTime.Add(60 * granularity), 0}, // Wraps around.
		{startTime.Add(-1 * granularity), 59}, // Negative time wraps to last index.
	}

	for _, test := range tests {
		index := tracker.getIndex(test.now, startTime)
		if index != test.expected {
			t.Errorf("getIndex(%v, %v) = %d; want %d", test.now, startTime, index, test.expected)
		}
	}
}

func TestSyncWindow(t *testing.T) {
	// Setup - Create a tracker with a 1-hour retention and 1-minute granularity.
	retention := 1 * time.Hour
	granularity := 1 * time.Minute
	tracker := NewTracker(retention, 0, granularity) // No threshold needed for this test.

	// Simulate a device data structure.
	startTime := time.Now().Truncate(granularity)
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
	exceedTime := startTime.Add(2 * retention)
	tracker.syncWindow(deviceData, exceedTime)
	for i, v := range deviceData.samples {
		if v {
			t.Errorf("syncWindow failed to reset the buffer at index %d", i)
		}
	}
	if !deviceData.start.Equal(exceedTime.Truncate(granularity)) {
		t.Errorf("syncWindow did not update the start time correctly. Got %v, want %v", deviceData.start, exceedTime.Truncate(granularity))
	}
}

func Test_calculateWindow(t *testing.T) {
	type args struct {
		now             time.Time
		WindowStartDay  int
		WindowStartTime time.Duration
	}
	tests := []struct {
		name string
		args args
		want trackerWindow
	}{
		{
			name: "",
			args: args{
				now:             time.Date(2024, 12, 2, 12, 0, 0, 0, time.UTC),
				WindowStartDay:  5,
				WindowStartTime: 12 * time.Hour,
			},
			want: trackerWindow{
				lastWindowStart: time.Date(2024, 11, 29, 12, 0, 0, 0, time.UTC),
				nextWindowStart: time.Date(2024, 12, 6, 12, 0, 0, 0, time.UTC),
				durationToNext:  96 * time.Hour,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := calculateWindow(tt.args.now, tt.args.WindowStartDay, tt.args.WindowStartTime); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("calculateWindow() = %v, want %v", got, tt.want)
			}
		})
	}
}