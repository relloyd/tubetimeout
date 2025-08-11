package monitor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"
)

var monitorNameForTesting = "test-monitor"

func TestAverageTrafficStats_RollingCounts(t *testing.T) {
	// Define a mock nowFunc to control time in tests
	var mockTime time.Time
	nowFunc = func() time.Time {
		return mockTime
	}

	// Initialize the trafficStats with a rolling window size of 5
	rollingWindowSize := 5
	monitor := newTrafficStats(config.MustGetLogger(), monitorNameForTesting, rollingWindowSize)

	// Simulate traffic counting over a 6-minute period to test wrap-around
	startTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	mockTime = startTime

	trafficCounts := []int{60, 120, 180, 240, 300, 360} // Traffic per minute

	for i, count := range trafficCounts {
		mockTime = startTime.Add(time.Duration(i) * time.Minute) // cause the avg for the last minute to be evaluated
		monitor.countTraffic(count, 0, models.Ingress)           // TODO: fix packet lengths in tests
		monitor.countTraffic(count, 0, models.Egress)
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
		if monitor.rollingCounts[models.Ingress][i] != expected {
			t.Errorf("Minute %d: expected ingress count %d, got %d", i, expected, monitor.rollingCounts[models.Ingress][i])
		}
		if monitor.rollingCounts[models.Egress][i] != expected {
			t.Errorf("Minute %d: expected egress count %d, got %d", i, expected, monitor.rollingCounts[models.Egress][i])
		}
	}
}

// TestAverageTrafficStats_IsActive contains a bunch of junk because packet counts are not considered to
// produce the active status in calls to isActive().
// TODO: test for bad packet sizes
func TestAverageTrafficStats(t *testing.T) {
	// Define a mock nowFunc to control time in tests
	var mockTime time.Time
	nowFunc = func() time.Time {
		return mockTime
	}

	windowSize := 5

	// Simulate traffic counting over a 6-minute period to test wrap-around.
	startTime := time.Date(2025, 1, 1, 12, 1, 0, 0, time.UTC)
	mockTime = startTime
	trafficCounts := []int{60, 120, 180, 240, 300, 360} // Traffic per minute
	monitor := newTrafficStats(config.MustGetLogger(), monitorNameForTesting, windowSize)
	for i, count := range trafficCounts {
		mockTime = startTime.Add(time.Duration(i) * time.Minute)
		monitor.countTraffic(count, 1, models.Ingress) // setting ingress packet len higher than egress on the first iteration causes active status to be true in the first minute.
		monitor.countTraffic(count, 2, models.Egress)
		expectedLastMinuteIdx := getLastMinuteIdx(mockTime.Minute()%windowSize, windowSize)
		assert.Equal(t, expectedLastMinuteIdx, monitor.lastMinuteIdx[models.Ingress], "lastMinuteIndex ingress should increment and wrap")
		assert.Equal(t, expectedLastMinuteIdx, monitor.lastMinuteIdx[models.Egress], "lastMinuteIndex egress should increment and wrap")
	}
	assert.True(t, monitor.isActive(0, true), "should be active when packet len is gt 0")

	// Assert packet lens are saved.
	expectedRollingPacketLetTotal := map[models.Direction][]int{
		models.Ingress: {1, 1, 1, 1, 1},
		models.Egress:  {2, 2, 2, 2, 2},
	}
	assert.Equal(t, monitor.rollingPacketLenTotal, expectedRollingPacketLetTotal, "unexpected rollingPacketLenTotal")

	// Misc assertions.
	assert.Equal(t, mockTime, monitor.lastActiveTimeUTC, "unexpected last active time")
	assert.Equal(t, monitor.monitorName, monitorNameForTesting, "bad monitor name")
	assert.Equal(t, monitor.windowSize, windowSize, "bad window size")
	assert.Equal(t, mockTime.Add(-1*time.Minute).Minute()%windowSize, monitor.lastMinuteIdx[models.Ingress], "unexpected last minute idx for ingress")
	assert.Equal(t, mockTime.Add(-1*time.Minute).Minute()%windowSize, monitor.lastMinuteIdx[models.Egress], "unexpected last minute idx for egress")

	// Test wrap around of lastMinuteIdx when current index is 0 the lastMinuteIndex should be at the end of mod range i.e. 4.
	startTime = time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC) // reset to the 0th minute minus 1
	mockTime = startTime
	monitor.lastMinuteIdx[models.Ingress] = startTime.Add(-1*time.Minute).Minute() % windowSize
	monitor.countTraffic(1, 1, models.Ingress)
	assert.Equal(t, 4, monitor.lastMinuteIdx[models.Ingress], "expected lastMinuteIdx to wrap back to the end of the slice based on window size")
}

func TestTrafficMap_IsActive(t *testing.T) {
	var mockTime time.Time
	nowFunc = func() time.Time {
		return mockTime
	}
	windowSize := 10 // match the number of tests

	// Assert active status.
	monitor := newTrafficStats(config.MustGetLogger(), monitorNameForTesting, windowSize)
	startTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	mockTime = startTime
	data := []struct {
		count                         int
		packetLenEgress               int
		packetLenIngress              int
		wantActive                    bool
		enableIngressEgressComparison bool
		test                          string
	}{
		{1, 1, 1, false, true, "equal ingress/egress is inactive"},
		{1, 10, 1, false, true, "bigger egress is inactive"},
		{1, 0, 0, false, true, "initial values should be inactive"},
		{1, 1, config.AppCfg.ActivityMonitorConfig.ThresholdIngressEgressKB - 1, false, true, "ingress must be gte to threshold for active"},
		{1, 2, config.AppCfg.ActivityMonitorConfig.ThresholdIngressEgressKB + 3, true, true, "ingress gte thresholdIngressEgressKB is active"},
		{1, 1, 2, true, true, "ingress must be xKB bigger than egress for active"},
		{1, 0, 0, false, false, "expected inactive for neither ingress nor egress"},
		{1, 1, 0, true, false, "expected active for egress"},
		{1, 0, 1, true, false, "expected active for ingress"},
		{1, 1, 1, true, false, "expected active for both ingress and egress"},
	}
	for i, d := range data {
		mockTime = startTime.Add(time.Duration(i) * time.Minute)
		monitor.countTraffic(d.count, d.packetLenIngress, models.Ingress)
		monitor.countTraffic(d.count, d.packetLenEgress, models.Egress)
		if d.enableIngressEgressComparison {
			config.AppCfg.ActivityMonitorConfig.EnableThresholdLogic = true
		} else {
			config.AppCfg.ActivityMonitorConfig.EnableThresholdLogic = false
		}
		if d.wantActive {
			assert.True(t, monitor.isActive(i, true), d.test)
		} else {
			assert.False(t, monitor.isActive(i, true), d.test)
		}
	}
}
