package monitor

import (
	"fmt"
	"sync"
	"time"

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

// UpdateSourceIpGroups implements SourceIpGroupsReceiver and is used to remove old data from the trafficMap.
func (t *TrafficMap) UpdateSourceIpGroups(newData models.MapIpGroups) {
	minAllowedTime := time.Now().Add(-5*time.Minute)
	t.trafficMap.Range(func(key any, value any) bool { // for each trafficMap key...
		for ip, groups := range newData { // process all ip groups in the newData...
			for _, g := range groups { // for each group...
				k := fmt.Sprintf("%v-%v", g, ip)
				v := value.(*trafficStats)
				if key.(string) == k &&  v.lastActiveTime.Before(minAllowedTime) { // if the key we generated matches the trafficMap key and the data is old...
					t.trafficMap.Delete(key) // remove the key.
				}
			}
		}
		return true
	})
}

// TODO test that old data is removed from the trafficMap by UpdateSourceIpGroups