package usage

import (
	"testing"
	"time"
)

func TestAdaptiveTrafficMonitor(t *testing.T) {
	// Define a mock nowFunc to control time in tests
	var mockTime time.Time
	nowFunc = func() time.Time {
		return mockTime
	}

	// Initialize the AverageTrafficMonitor with a rolling window size of 5
	rollingWindowSize := 5
	monitor := NewAverageTrafficMonitor(rollingWindowSize)

	// Simulate traffic counting over a 6-minute period to test wrap-around
	startTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	mockTime = startTime

	trafficCounts := []int{60, 120, 180, 240, 300, 360} // Traffic per minute
	for i, count := range trafficCounts {
		mockTime = startTime.Add(time.Duration(i) * time.Minute)
		for j := 0; j < count; j++ {
			monitor.CountTraffic(1) // Count traffic incrementally
		}
	}

	// Expected rolling averages after wrap-around
	expectedAverages := []int{
		1, // 300 packets / 60 seconds (minute 5 wraps to index 0)
		2, // 120 packets / 60 seconds (index 1)
		3, // 180 packets / 60 seconds (index 2)
		4, // 240 packets / 60 seconds (index 3)
		5, // 300 packets / 60 seconds (index 4)
	}

	// Expected rolling counts after wrap-around
	expectedCounts := []int{
		360, // Count for minute 5 (wraps to index 0)
		120, // Count for minute 1
		180, // Count for minute 2
		240, // Count for minute 3
		300, // Count for minute 4
	}

	// Verify the rolling averages after wrap-around
	actualAverages := monitor.GetRollingAverages()
	for i, expected := range expectedAverages {
		if actualAverages[i] != expected {
			t.Errorf("Minute %d: expected average %d, got %d", i, expected, actualAverages[i])
		}
	}

	// Verify the rolling counts after wrap-around
	actualCounts := monitor.rollingCounts
	for i, expected := range expectedCounts {
		if actualCounts[i] != expected {
			t.Errorf("Minute %d: expected count %d, got %d", i, expected, actualCounts[i])
		}
	}
}