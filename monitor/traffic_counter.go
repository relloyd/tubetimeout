package monitor

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"
)

type TrafficCounter interface {
	CountTraffic(group models.Group, ip models.Ip, count int, packetLen int, direction models.Direction) bool
}

type TrafficMap struct {
	mu                sync.Mutex
	logger            *zap.SugaredLogger
	rollingWindowSize int
	trafficMap        *sync.Map
	trafficMapLen     int
	ipMACs            models.IpMACs
}

func NewTrafficMap(logger *zap.SugaredLogger, rollingWindowSize int) *TrafficMap {
	return &TrafficMap{
		logger:            logger,
		rollingWindowSize: rollingWindowSize,
		trafficMap:        &sync.Map{},
		ipMACs:            models.IpMACs{Data: make(models.MapIpMACs), Mu: sync.RWMutex{}}, // TODO test that the map is not nil.
	}
}

func (t *TrafficMap) CountTraffic(group models.Group, ip models.Ip, count int, packetLen int, direction models.Direction) bool {
	t.ipMACs.Mu.RLock()
	defer t.ipMACs.Mu.RUnlock()
	m, ok := t.ipMACs.Data[ip]
	if !ok {
		t.logger.Warnf("CountTraffic found no MAC found for IP %v in group %v. Returning active for now.", ip, group)
		return true
	}

	key := getTrafficMapKey(group, m)
	tm, loaded := t.trafficMap.LoadOrStore(key, newTrafficStats(t.logger, key, t.rollingWindowSize))
	if !loaded { // if the trafficMap was stored as new...
		t.mu.Lock()
		t.trafficMapLen++ // track of the number of trafficMap values.
		t.mu.Unlock()
	}
	return tm.(*trafficStats).countTraffic(count, packetLen, direction)
}

// UpdateSourceIpMACs implements SourceIpGroupsReceiver and is used to remove old data from the trafficMap.
func (t *TrafficMap) UpdateSourceIpMACs(newData models.MapIpMACs) {
	// Save the given data.
	t.ipMACs.Mu.Lock()
	defer t.ipMACs.Mu.Unlock()
	t.ipMACs.Data = newData

	// Remove old data from the trafficMap.
	minAllowedTime := time.Now().Add(-config.AppCfg.MonitorConfig.PurgeStatsAfterDuration) // remove trafficMaps older than this.
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(newData) != t.trafficMapLen { // if there are trafficMaps to clean up...
		t.trafficMap.Range(func(key any, value any) bool { // for each trafficMap key...
			for ip, groups := range newData { // check all group-ip input newData...
				foundKey := false
				for _, g := range groups { // for each group...
					k := getTrafficMapKey(g, ip) // generate the group-ip key (see also nfq code that creates this).
					v := value.(*trafficStats)
					// TODO: look up the MAC in config and remove it if it's not in the new data.
					if key.(string) == k {
						foundKey = true
					}
				}
				if !foundKey { // if the group-ip key was not found...
					t.trafficMap.Delete(key) // remove the key.
					t.trafficMapLen--        // decrement the trafficMap counter.
				}
			}
			return true
		})
	}
}

func getTrafficMapKey(group models.Group, ip models.MAC) string {
	return fmt.Sprintf("%v-%v", group, ip)
}
