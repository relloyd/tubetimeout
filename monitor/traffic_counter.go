package monitor

import (
	"sync"

	"go.uber.org/zap"
	"relloyd/tubetimeout/models"
)

type TrafficCounter interface {
	CountTraffic(key string, count int, packetLen int, direction models.Direction) bool
}

func NewTrafficCounter(logger *zap.SugaredLogger, rollingWindowSize int) *TrafficMap {
	return &TrafficMap{
		logger: logger,
		rollingWindowSize: rollingWindowSize,
		trafficMap: &sync.Map{},
	}
}

// TODO: implement deletion of traffic monitor entries when keys expire.

type TrafficMap struct {
	logger            *zap.SugaredLogger
	rollingWindowSize int
	trafficMap        *sync.Map
}

func (t *TrafficMap) CountTraffic(key string, count int, packetLen int, direction models.Direction) bool {
	tm, _ := t.trafficMap.LoadOrStore(key, newTrafficStats(t.logger, key, t.rollingWindowSize))
	return tm.(*trafficStats).countTraffic(count, packetLen, direction)
}
