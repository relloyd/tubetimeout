package monitor

import (
	"sync"
	"time"

	"go.uber.org/zap"
	"relloyd/tubetimeout/models"
)

var nowFunc = time.Now

type AverageTrafficMonitor struct {
	mu                 *sync.Mutex
	logger             *zap.SugaredLogger
	monitorName        string // arbitrary monitorName for the monitor
	rollingWindowSize  int
	isLastMinuteActive bool
	rollingCounts      map[models.Direction][]int
	rollingAverages    map[models.Direction][]float64 // use float64 for gonum/stat functions
	totalCount         map[models.Direction]int
	lastMinuteIdx      map[models.Direction]int
}

func NewAverageTrafficMonitor(logger *zap.SugaredLogger, name string, rollingWindowSize int) *AverageTrafficMonitor {
	a := &AverageTrafficMonitor{
		logger:            logger,
		monitorName:       name,
		rollingWindowSize: rollingWindowSize,
		rollingCounts:     make(map[models.Direction][]int),
		rollingAverages:   make(map[models.Direction][]float64),
		totalCount:        make(map[models.Direction]int),
		lastMinuteIdx:     make(map[models.Direction]int),
		mu:                &sync.Mutex{},
	}
	a.rollingCounts[models.Ingress] = make([]int, rollingWindowSize)
	a.rollingCounts[models.Egress] = make([]int, rollingWindowSize)
	a.rollingAverages[models.Ingress] = make([]float64, rollingWindowSize)
	a.rollingAverages[models.Egress] = make([]float64, rollingWindowSize)
	return a
}

// CountTraffic increments the count of packets for the current minute.
// It returns true if the rate for the previous minute is deemed "active" based on the rolling average.
func (a *AverageTrafficMonitor) CountTraffic(count int, trafficDirection models.Direction) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	currentMinuteIdx := nowFunc().Minute() % a.rollingWindowSize
	lastMinuteIndex := a.lastMinuteIdx[trafficDirection]

	// If we've moved to a new minute
	if currentMinuteIdx != lastMinuteIndex {
		// Determine if the rate for the previous minute is "active"
		a.isLastMinuteActive = a.isActive(lastMinuteIndex)
		// Compute the average for the completed minute
		a.rollingAverages[trafficDirection][lastMinuteIndex] = float64(a.rollingCounts[trafficDirection][lastMinuteIndex]) / 60 // Assuming 60 seconds per minute
		// Subtract the completed minute's count from the total count
		a.totalCount[trafficDirection] -= a.rollingCounts[trafficDirection][currentMinuteIdx]
		// Clear the count for the new minute
		a.rollingCounts[trafficDirection][currentMinuteIdx] = 0
		// Update the last minute index
		a.lastMinuteIdx[trafficDirection] = currentMinuteIdx
	}

	// Add the packet count to the current minute's count
	a.rollingCounts[trafficDirection][currentMinuteIdx] += count
	a.totalCount[trafficDirection] += count

	return a.isLastMinuteActive
}

// isActive determines if the traffic rate is deemed "active" i.e. true, based on the current rate.
func (a *AverageTrafficMonitor) isActive(lastMinuteIndex int) bool {
	// ratios := make([]float64, a.rollingWindowSize)
	deltas := make([]float64, a.rollingWindowSize)
	winners := make([]models.Direction, a.rollingWindowSize)

	for i := range a.rollingCounts[models.Ingress] {
		ingress := float64(a.rollingCounts[models.Ingress][i])
		egress := float64(a.rollingCounts[models.Egress][i])
		// ratios[i] = nonZero(ingress) / nonZero(egress)
		deltas[i] = ingress - egress
		if ingress > egress {
			winners[i] = models.Ingress
		} else {
			winners[i] = models.Egress
		}
	}

	// meanIngress := stat.Mean(a.rollingAverages[models.Ingress], nil)
	// meanEgress := stat.Mean(a.rollingAverages[models.Egress], nil)
	// meanRatios := stat.Mean(ratios, nil)
	// meanDeltas := stat.Mean(deltas, nil)

	a.logger.With(
		"monitorName", a.monitorName,
		"rollingCounts", a.rollingCounts,
		"deltas", deltas,
		"winners", winners,
		"lastMinuteWinner", winners[lastMinuteIndex],
	).Debugf("monitor stats")

	return true
}

func nonZero(num float64) float64 {
	if num == 0 {
		return 1
	}
	return num
}

// // getLastMinuteIdx returns the index of the last minute in the rolling window.
// func getLastMinuteIdx(currentIndex int, moduloSize int) int {
// 	if currentIndex == 0 {
// 		return moduloSize - 1
// 	}
// 	return currentIndex - 1
// }
