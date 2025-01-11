package usage

import (
	"time"
)

var nowFunc = time.Now

type AverageTrafficMonitor struct {
	rollingWindowSize int
	rollingCounts     []int
	rollingAverages   []int
	totalCount        int
	lastMinuteIdx     int
}

func NewAverageTrafficMonitor(rollingWindowSize int) *AverageTrafficMonitor {
	return &AverageTrafficMonitor{
		rollingWindowSize: rollingWindowSize,
		rollingCounts:     make([]int, rollingWindowSize),
		rollingAverages:   make([]int, rollingWindowSize),
	}
}

func (a *AverageTrafficMonitor) CountTraffic(count int) {
	currentMinuteIdx := nowFunc().Minute() % a.rollingWindowSize

	// If we've moved to a new minute
	if currentMinuteIdx != a.lastMinuteIdx {
		// Compute the average for the completed minute
		a.rollingAverages[a.lastMinuteIdx] = a.rollingCounts[a.lastMinuteIdx] / 60 // Assuming 60 seconds per minute

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

func (a *AverageTrafficMonitor) GetRollingAverages() []int {
	return a.rollingAverages
}
