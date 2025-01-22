package monitor

import (
	"sync"
	"time"

	"go.uber.org/zap"
	"relloyd/tubetimeout/models"
)

var (
	nowFunc = time.Now
)

type trafficStats struct {
	mu                    *sync.Mutex
	logger                *zap.SugaredLogger
	monitorName           string // arbitrary monitorName for the monitor
	windowSize            int
	totalCount            map[models.Direction]int
	rollingCounts         map[models.Direction][]int // TODO: maybe remove rollingCounts of packets if packet len is good enough to determine activity.
	rollingPacketLenTotal map[models.Direction][]int
	lastMinuteIdx         map[models.Direction]int
	isLastMinuteActive    bool
	lastActiveTimeUTC     time.Time // the time at which stats were last counted
}

func newTrafficStats(logger *zap.SugaredLogger, name string, rollingWindowSize int) *trafficStats {
	a := &trafficStats{
		logger:                logger,
		monitorName:           name,
		windowSize:            rollingWindowSize,
		rollingCounts:         make(map[models.Direction][]int),
		rollingPacketLenTotal: make(map[models.Direction][]int),
		totalCount:            make(map[models.Direction]int),
		lastMinuteIdx:         make(map[models.Direction]int),
		lastActiveTimeUTC:     nowFunc().UTC(),
		isLastMinuteActive:    true, // assume the status is active until we get stats for the first minute
		mu:                    &sync.Mutex{},
	}
	a.rollingCounts[models.Ingress] = make([]int, rollingWindowSize)
	a.rollingCounts[models.Egress] = make([]int, rollingWindowSize)
	a.rollingPacketLenTotal[models.Ingress] = make([]int, rollingWindowSize)
	a.rollingPacketLenTotal[models.Egress] = make([]int, rollingWindowSize)
	return a
}

// countTraffic increments the count of packets for the current minute.
// It returns true if the rate for the previous minute is deemed "active" based on the rolling average.
func (a *trafficStats) countTraffic(count int, packetLen int, trafficDirection models.Direction) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	currentMinuteIdx := nowFunc().Minute() % a.windowSize
	lastMinuteIndex := a.lastMinuteIdx[trafficDirection]

	// Remember activity status for each call.
	a.lastActiveTimeUTC = nowFunc().UTC()

	// If we've moved to a new minute
	if currentMinuteIdx != lastMinuteIndex+1 { // if we have moved to the next minute...
		// Determine if the rate for the previous minute is "active".
		a.isLastMinuteActive = a.isActive(lastMinuteIndex)
		// Subtract the completed minute's count from the total count.
		a.totalCount[trafficDirection] -= a.rollingCounts[trafficDirection][currentMinuteIdx]
		// Clear the counts for the new minute.
		a.rollingCounts[trafficDirection][currentMinuteIdx] = 0
		a.rollingPacketLenTotal[trafficDirection][currentMinuteIdx] = 0
		// Update the last minute index.
		a.lastMinuteIdx[trafficDirection] = getLastMinuteIdx(currentMinuteIdx, a.windowSize)
	}

	// Add the packet count to the current minute's count
	a.rollingCounts[trafficDirection][currentMinuteIdx] += count
	a.rollingPacketLenTotal[trafficDirection][currentMinuteIdx] += packetLen
	a.totalCount[trafficDirection] += count

	return a.isLastMinuteActive
}

// isActive determines if the traffic rate is deemed "active" i.e. true, based on the current rate.
func (a *trafficStats) isActive(lastMinuteIndex int) bool {
	deltasPacketLen := make([]int, a.windowSize)

	for i := range a.rollingPacketLenTotal[models.Ingress] {
		deltasPacketLen[i] = a.rollingPacketLenTotal[models.Egress][i] - a.rollingPacketLenTotal[models.Ingress][i]
	}

	active := false                                                                                                         // assume inactive; give the benefit of doubt to start with.
	if a.rollingPacketLenTotal[models.Ingress][lastMinuteIndex] > a.rollingPacketLenTotal[models.Egress][lastMinuteIndex] { // if there is more data coming in than going out...
		active = true
	}

	a.logger.With(
		"monitorName", a.monitorName,
		"packetLenTotals", a.rollingPacketLenTotal,
		"deltas", deltasPacketLen,
		"lastMinuteIndex", lastMinuteIndex,
		"active", active,
	).Infof("monitor stats")

	return active
}

// getLastMinuteIdx returns the index of the last minute in the rolling window.
func getLastMinuteIdx(currentIndex int, moduloSize int) int {
	if currentIndex == 0 {
		return moduloSize - 1
	}
	return currentIndex - 1
}
