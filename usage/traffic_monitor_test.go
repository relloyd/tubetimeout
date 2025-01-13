package usage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"relloyd/tubetimeout/config"
)

func TestAverageTrafficMonitor(t *testing.T) {
	// Define a mock nowFunc to control time in tests
	var mockTime time.Time
	nowFunc = func() time.Time {
		return mockTime
	}

	// Initialize the AverageTrafficMonitor with a rolling window size of 5
	rollingWindowSize := 5
	monitor := NewAverageTrafficMonitor(config.MustGetLogger(), rollingWindowSize)

	// Simulate traffic counting over a 6-minute period to test wrap-around
	startTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	mockTime = startTime

	trafficCounts := []int{60, 120, 180, 240, 300, 360} // Traffic per minute

	for i, count := range trafficCounts {
		mockTime = startTime.Add(time.Duration(i) * time.Minute) // cause the avg for the last minute to be evaluated
		monitor.CountTraffic(count)
	}

	// Expected rolling counts after wrap-around
	expectedCounts := []int{
		360, // Count for minute 5 (wraps to index 0)
		120, // Count for minute 1
		180, // Count for minute 2
		240, // Count for minute 3
		300, // Count for minute 4
	}

	// Verify the rolling counts after wrap-around
	for i, expected := range expectedCounts {
		if monitor.rollingCounts[i] != expected {
			t.Errorf("Minute %d: expected count %d, got %d", i, expected, monitor.rollingCounts[i])
		}
	}

	// Expected rolling averages after wrap-around
	expectedAverages := []float64{
		1, // 60  packets / 60 seconds (index 0)
		2, // 120 packets / 60 seconds (index 1)
		3, // 180 packets / 60 seconds (index 2)
		4, // 240 packets / 60 seconds (index 3)
		5, // 300 packets / 60 seconds (index 4) calculated on the 6th element.
	}

	// Verify the rolling averages after wrap-around
	for i, expected := range expectedAverages {
		if monitor.rollingAverages[i] != expected {
			t.Errorf("Minute %d: expected average %f, got %f", i, expected, monitor.rollingAverages[i])
		}
	}
}

func TestAverageTrafficMonitor_IsActive(t *testing.T) {
	// Define a mock nowFunc to control time in tests
	var mockTime time.Time
	nowFunc = func() time.Time {
		return mockTime
	}

	// Initialize the AverageTrafficMonitor with a rolling window size of 5
	rollingWindowSize := 5
	monitor := NewAverageTrafficMonitor(config.MustGetLogger(), rollingWindowSize)

	// Simulate traffic counting over a 6-minute period to test wrap-around
	startTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	mockTime = startTime

	trafficCounts := []int{60, 120, 180, 240, 300, 360} // Traffic per minute
	for i, count := range trafficCounts {
		mockTime = startTime.Add(time.Duration(i) * time.Minute)
		monitor.CountTraffic(count)
	}

	// Test isActive function with different rates and thresholds.
	assert.True(t, monitor.isActive(5, 1.0), "should be active")
	assert.False(t, monitor.isActive(3, 1.0), "should be inactive")
}

func TestAverageTrafficMonitor_CountTraffic_ActiveResults(t *testing.T) {
	config.AppCfg.LogLevel = "debug"
	logger := config.MustGetLogger()

	// Initialize the AverageTrafficMonitor with a rolling window size of 5
	rollingWindowSize := 5
	monitor := NewAverageTrafficMonitor(logger, rollingWindowSize)

	// Define a mock nowFunc to control time in tests
	var mockTime time.Time
	nowFunc = func() time.Time {
		return mockTime
	}

	// Simulate traffic counting over a 6-minute period to test wrap-around
	startTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	mockTime = startTime

	trafficCounts := []int{60, 60, 60, 60, 60, 60} // Flat traffic levels
	for i, count := range trafficCounts {
		mockTime = startTime.Add(time.Duration(i) * time.Minute) // cause the avg for the last minute to be evaluated
		monitor.CountTraffic(count)
	}

	trafficCounts = []int{120, 60, 60, 60, 60, 60} // Spike the traffic
	var activeStatuses []bool
	for i, count := range trafficCounts {
		mockTime = startTime.Add(time.Duration(i) * time.Minute) 
		activeStatuses = append(activeStatuses, monitor.CountTraffic(count))
	}

	// Expected active results
	expectedActiveResults := []bool{false, true, false, false, false, true}
	for i, expected := range expectedActiveResults {
		got := activeStatuses[i]
		assert.Equal(t, expected, got, "Minute %d: expected active %t, got %t", i, expected, got)
	}
}