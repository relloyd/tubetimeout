package monitor

import (
	"sync"
	"time"

	"go.uber.org/zap"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"
)

var (
	nowFunc                  = time.Now
)

// TODO: maybe remove rollingCounts of packets if packet len is good enough to determine activity.
// TODO: remove arrays and looping windows once we know how to track active status reliably, as we should only need to track the last minute of data!
type trafficStats struct {
	mu                    *sync.Mutex
	logger                *zap.SugaredLogger
	monitorName           string // arbitrary monitorName for the monitor
	windowSize            int
	totalCount            map[models.Direction]int
	rollingCounts         map[models.Direction][]int
	rollingPacketLenTotal map[models.Direction][]int
	rollingMinPacketLen   map[models.Direction][]int
	rollingMaxPacketLen   map[models.Direction][]int
	rollingAvgPacketLen   map[models.Direction][]float64
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
		rollingMaxPacketLen:   make(map[models.Direction][]int),
		rollingMinPacketLen:   make(map[models.Direction][]int),
		rollingAvgPacketLen:   make(map[models.Direction][]float64),
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
	a.rollingMinPacketLen[models.Ingress] = make([]int, rollingWindowSize)
	a.rollingMinPacketLen[models.Egress] = make([]int, rollingWindowSize)
	a.rollingMaxPacketLen[models.Ingress] = make([]int, rollingWindowSize)
	a.rollingMaxPacketLen[models.Egress] = make([]int, rollingWindowSize)
	a.rollingAvgPacketLen[models.Ingress] = make([]float64, rollingWindowSize)
	a.rollingAvgPacketLen[models.Egress] = make([]float64, rollingWindowSize)
	return a
}

// countTraffic increments the count of packets for the current minute.
// It returns true if the rate for the previous minute is deemed "active" based on the rolling average.
func (a *trafficStats) countTraffic(count int, packetLen int, trafficDirection models.Direction) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	currentMinuteIdx := nowFunc().Minute() % a.windowSize
	lastMinuteIndex := a.lastMinuteIdx[trafficDirection]

	// If we've moved to a new minute
	if currentMinuteIdx != (lastMinuteIndex+1)%a.windowSize { // if we have moved to the next minute...
		// Calculate avg
		a.rollingAvgPacketLen[trafficDirection][lastMinuteIndex] = float64(a.rollingPacketLenTotal[trafficDirection][lastMinuteIndex]) / float64(a.rollingCounts[trafficDirection][lastMinuteIndex])
		// Determine if the rate for the previous minute is "active".
		logStats := false
		if trafficDirection == models.Ingress {
			logStats = true
		}
		a.isLastMinuteActive = a.isActive(lastMinuteIndex, logStats)
		// Update last active time
		if a.isLastMinuteActive {
			a.lastActiveTimeUTC = nowFunc().UTC().Truncate(time.Minute)
		}
		// Subtract the completed minute's count from the total count.
		a.totalCount[trafficDirection] -= a.rollingCounts[trafficDirection][currentMinuteIdx]
		// Reset Min/Max
		a.rollingMinPacketLen[trafficDirection][currentMinuteIdx] = packetLen
		a.rollingMaxPacketLen[trafficDirection][currentMinuteIdx] = packetLen
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

	if packetLen > a.rollingMaxPacketLen[trafficDirection][currentMinuteIdx] {
		a.rollingMaxPacketLen[trafficDirection][currentMinuteIdx] = packetLen
	}

	if packetLen < a.rollingMinPacketLen[trafficDirection][currentMinuteIdx] {
		a.rollingMinPacketLen[trafficDirection][currentMinuteIdx] = packetLen
	}

	return a.isLastMinuteActive
}

// isActive determines if the traffic rate is deemed "active" i.e. true, based on the current rate.
func (a *trafficStats) isActive(lastMinuteIndex int, logStats bool) bool {
	activeStatus := false // assume inactive; give the benefit of doubt to start with.
	if config.AppCfg.ActivityMonitorConfig.EnableThresholdLogic { // if ingress should be compared to egress...
		if a.rollingPacketLenTotal[models.Ingress][lastMinuteIndex] >= config.AppCfg.ActivityMonitorConfig.ThresholdIngressEgressKB &&
			a.rollingPacketLenTotal[models.Ingress][lastMinuteIndex] > a.rollingPacketLenTotal[models.Egress][lastMinuteIndex] { // // if ingress is xKB more than egress...
			activeStatus = true
		}
	} else { // else if there is ANY traffic...
		if a.rollingPacketLenTotal[models.Ingress][lastMinuteIndex] > 0 || a.rollingPacketLenTotal[models.Egress][lastMinuteIndex] > 0 {
			activeStatus = true
		}
	}

	if logStats {
		deltas := make([]int, a.windowSize)
		for i := range a.rollingPacketLenTotal[models.Ingress] {
			deltas[i] = a.rollingPacketLenTotal[models.Egress][i] - a.rollingPacketLenTotal[models.Ingress][i]
		}
		a.logger.With(
			"monitorName", a.monitorName,
			"count-in", a.rollingCounts[models.Ingress][lastMinuteIndex],
			"count-out", a.rollingCounts[models.Egress][lastMinuteIndex],
			"size-in", a.rollingPacketLenTotal[models.Ingress][lastMinuteIndex],
			"size-out", a.rollingPacketLenTotal[models.Egress][lastMinuteIndex],
			"maxSize-in", a.rollingMaxPacketLen[models.Ingress][lastMinuteIndex],
			"maxSize-out", a.rollingMaxPacketLen[models.Egress][lastMinuteIndex],
			"avgSize-in", a.rollingAvgPacketLen[models.Ingress][lastMinuteIndex],
			"avgSize-out", a.rollingAvgPacketLen[models.Egress][lastMinuteIndex],
			"delta", deltas[lastMinuteIndex],
			"lastMinuteIndex", lastMinuteIndex,
			"active", activeStatus,
		).Infof("monitor stats")
	}

	return activeStatus
}

// getLastMinuteIdx returns the index of the last minute in the rolling window.
func getLastMinuteIdx(currentIndex int, moduloSize int) int {
	if currentIndex == 0 {
		return moduloSize - 1
	}
	return currentIndex - 1
}
