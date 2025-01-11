package usage

import (
	"time"

	"gonum.org/v1/gonum/stat"
)

var nowFunc = time.Now

type AverageTrafficMonitor struct {
	rollingWindowSize int
	rollingCounts     []int
	rollingAverages   []float64 // use float64 for gonum/stat functions
	totalCount        int
	lastMinuteIdx     int
}

func NewAverageTrafficMonitor(rollingWindowSize int) *AverageTrafficMonitor {
	return &AverageTrafficMonitor{
		rollingWindowSize: rollingWindowSize,
		rollingCounts:     make([]int, rollingWindowSize),
		rollingAverages:   make([]float64, rollingWindowSize),
	}
}

func (a *AverageTrafficMonitor) CountTraffic(count int) {
	currentMinuteIdx := nowFunc().Minute() % a.rollingWindowSize

	// If we've moved to a new minute
	if currentMinuteIdx != a.lastMinuteIdx {
		// Compute the average for the completed minute
		a.rollingAverages[a.lastMinuteIdx] = float64(a.rollingCounts[a.lastMinuteIdx]) / 60 // Assuming 60 seconds per minute

		// Subtract the completed minute's count from the total count
		a.totalCount -= a.rollingCounts[currentMinuteIdx]

		// Clear the count for the new minute
		a.rollingCounts[currentMinuteIdx] = 0

		// Update the last minute index
		a.lastMinuteIdx = currentMinuteIdx
	}

	// Add the packet count to the current minute's count
	a.rollingCounts[currentMinuteIdx] += count
	a.totalCount += count
}

func (a *AverageTrafficMonitor) GetRollingAverages() []float64 {
	return a.rollingAverages
}

// IsActive determines if the traffic rate is deemed "active" i.e. true, based on the current rate.
// k is the number of standard deviations above the mean to consider as the threshold for "active".
// currentRate is in packets per second.
func (a *AverageTrafficMonitor) IsActive(currentRate int, k float64) bool {
	// Calculate mean and standard deviation.
	mean := stat.Mean(a.rollingAverages, nil)
	stdDev := stat.StdDev(a.rollingAverages, nil)

	// Define active threshold.
	activeThreshold := mean + k*stdDev

	// Determine if the current rate exceeds the threshold.
	return float64(currentRate) > activeThreshold
}
