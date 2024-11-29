package tracker

import (
	"testing"
	"time"
)

func TestGetIndex(t *testing.T) {
	// Setup: Create a tracker with a 1-hour retention, 1-minute granularity.
	retention := 1 * time.Hour
	granularity := 1 * time.Minute
	tracker := NewTracker(retention, 0, granularity) // Threshold is not relevant here.

	// Example: 1-hour retention with 1-minute granularity => 60 slots.
	sampleSize := tracker.sampleSize // Should be 60.

	// Define test cases.
	tests := []struct {
		now      time.Time // Current time.
		expected int       // Expected index.
	}{
		// Basic cases within the first hour.
		{time.Date(2023, 1, 1, 10, 0, 0, 0, time.UTC), 0},   // Start of the buffer.
		{time.Date(2023, 1, 1, 10, 1, 0, 0, time.UTC), 1},   // 1 minute in.
		{time.Date(2023, 1, 1, 10, 59, 0, 0, time.UTC), 59}, // Last slot in the hour.

		// Wraparound cases: simulate the circular buffer.
		{time.Date(2023, 1, 1, 11, 0, 0, 0, time.UTC), 0}, // Wraps back to the start.
		{time.Date(2023, 1, 1, 11, 1, 0, 0, time.UTC), 1}, // Wraps back to index 1.

		// Custom granularity (5 minutes).
		{time.Date(2023, 1, 1, 12, 5, 0, 0, time.UTC), 5}, // 5 minutes in (1-minute granularity).
	}

	// Execute the test cases.
	for _, test := range tests {
		index := tracker.getIndex(test.now)
		if index != test.expected {
			t.Errorf("getIndex(%v) = %d; want %d", test.now, index, test.expected)
		}
	}

	// Edge case: Test an extremely large time difference to verify that the modulo operation handles it correctly.
	largeTime := time.Date(2023, 1, 2, 10, 0, 0, 0, time.UTC) // 1 day later.
	expectedIndex := 1440 % sampleSize                        // 1440 minutes in a day % 60 slots.
	if index := tracker.getIndex(largeTime); index != expectedIndex {
		t.Errorf("getIndex(%v) = %d; want %d", largeTime, index, expectedIndex)
	}
}

func TestHasExceededThreshold(t *testing.T) {
	// Setup - Create a tracker with a 1-hour retention, 10-minute threshold, and 1-minute granularity.
	retention := 1 * time.Hour
	threshold := 10 * time.Minute
	granularity := 1 * time.Minute
	tracker := NewTracker(retention, threshold, granularity)

	// Simulate a device data structure with preallocated samples.
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

	// Case 4: Samples exceed the threshold but include stale samples.
	// Mark some samples outside the retention window.
	deviceData.start = startTime.Add(-2 * granularity) // Simulate an old start time.
	for i := 0; i < tracker.sampleSize; i++ {
		deviceData.samples[i] = true
	}
	if tracker.HasExceededThreshold(deviceID) {
		t.Error("HasExceededThreshold included stale samples in the calculation")
	}

	// Reset to current start time and validate again.
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

	// Case 2: Partial elapsed time (clear first few slots).
	elapsedTime := startTime.Add(3 * granularity)
	tracker.syncWindow(deviceData, elapsedTime)
	if deviceData.samples[0] || deviceData.samples[1] || deviceData.samples[2] {
		t.Error("syncWindow did not clear the correct slots")
	}
	if deviceData.samples[3] || deviceData.samples[4] {
		t.Errorf("syncWindow incorrectly cleared untouched slots")
	}

	// Case 3: Elapsed time exceeds retention (expect the buffer to be reset).
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
