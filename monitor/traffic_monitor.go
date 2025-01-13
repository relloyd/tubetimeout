package monitor

import (
	"sync"
	"time"

	"go.uber.org/zap"
	"gonum.org/v1/gonum/stat"
)

var nowFunc = time.Now

type AverageTrafficMonitor struct {
	logger             *zap.SugaredLogger
	monitorName        string // arbitrary monitorName for the monitor
	rollingWindowSize  int
	rollingCounts      []int
	rollingAverages    []float64 // use float64 for gonum/stat functions
	totalCount         int
	lastMinuteIdx      int
	isLastMinuteActive bool
	mu                 *sync.Mutex
}

func NewAverageTrafficMonitor(logger *zap.SugaredLogger, name string, rollingWindowSize int) *AverageTrafficMonitor {
	return &AverageTrafficMonitor{
		logger:            logger,
		monitorName:       name,
		rollingWindowSize: rollingWindowSize,
		rollingCounts:     make([]int, rollingWindowSize),
		rollingAverages:   make([]float64, rollingWindowSize),
		mu:                &sync.Mutex{},
	}
}

// CountTraffic increments the count of packets for the current minute.
// It returns true if the rate for the previous minute is deemed "active" based on the rolling average.
func (a *AverageTrafficMonitor) CountTraffic(count int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	currentMinuteIdx := nowFunc().Minute() % a.rollingWindowSize
	// If we've moved to a new minute
	if currentMinuteIdx != a.lastMinuteIdx {
		// Compute the average for the completed minute
		a.rollingAverages[a.lastMinuteIdx] = float64(a.rollingCounts[a.lastMinuteIdx]) / 60 // Assuming 60 seconds per minute
		// Subtract the completed minute's count from the total count
		a.totalCount -= a.rollingCounts[currentMinuteIdx]
		// Clear the count for the new minute
		a.rollingCounts[currentMinuteIdx] = 0
		// Determine if the rate for the previous minute is "active"
		a.isLastMinuteActive = a.isActive(a.rollingAverages[a.lastMinuteIdx], 1)
		// Update the last minute index
		a.lastMinuteIdx = currentMinuteIdx
	}
	// Add the packet count to the current minute's count
	a.rollingCounts[currentMinuteIdx] += count
	a.totalCount += count
	return a.isLastMinuteActive
}

// isActive determines if the traffic rate is deemed "active" i.e. true, based on the current rate.
// k is the number of standard deviations above the mean to consider as the threshold for "active".
// currentRate is in packets per second.
func (a *AverageTrafficMonitor) isActive(currentRate float64, k float64) bool {
	// Calculate mean and standard deviation.
	mean := stat.Mean(a.rollingAverages, nil)
	stdDev := stat.StdDev(a.rollingAverages, nil)
	// Define active threshold.
	activeThreshold := mean + k*stdDev
	// Determine if the current rate exceeds the threshold.
	isActive := currentRate > activeThreshold

	a.logger.With(
		"monitorName", a.monitorName,
		"rollingCounts", a.rollingCounts,
		"rollingAverages", a.rollingAverages,
		"mean", mean,
		"stdDev", stdDev,
		"activeThreshold", activeThreshold,
		"currentRate", currentRate,
		"isActive", isActive,
	).Debugf("monitor isActive() called")

	return isActive
}

// // getLastMinuteIdx returns the index of the last minute in the rolling window.
// func getLastMinuteIdx(currentIndex int, moduloSize int) int {
// 	if currentIndex == 0 {
// 		return moduloSize - 1
// 	}
// 	return currentIndex - 1
// }
