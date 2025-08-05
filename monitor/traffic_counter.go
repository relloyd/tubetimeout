package monitor

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"
)

var (
	defaultTrafficMapKeySeparator = "/"
)

type TrafficCounter interface {
	CountTraffic(group models.Group, ip models.Ip, direction models.Direction, count int, packetLen int) bool
}

type TrafficMap struct {
	logger            *zap.SugaredLogger
	rollingWindowSize int
	trafficMap        *sync.Map
	trafficMapLen     int
	muTrafficMapLen   sync.Mutex
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

func (t *TrafficMap) CountTraffic(group models.Group, ip models.Ip, direction models.Direction, count int, packetLen int) bool {
	t.ipMACs.Mu.RLock()
	defer t.ipMACs.Mu.RUnlock()
	mac, ok := t.ipMACs.Data[ip]
	if !ok {
		t.logger.Warnf("CountTraffic: no MAC found for %v in group %v. Returning active for now.", ip, group)
		return true
	}

	key := getTrafficMapKey(group, mac)
	tm, loaded := t.trafficMap.LoadOrStore(key, newTrafficStats(t.logger, key, t.rollingWindowSize))
	if !loaded { // if the trafficMap was stored as new...
		t.muTrafficMapLen.Lock()
		t.trafficMapLen++ // track of the number of trafficMap values.
		t.muTrafficMapLen.Unlock()
	}
	return tm.(*trafficStats).countTraffic(count, packetLen, direction)
}

// UpdateSourceIpMACs implements SourceIpGroupsReceiver and is used to remove old data from the trafficMap.
func (t *TrafficMap) UpdateSourceIpMACs(newData models.MapIpMACs) {
	// Save the given data.
	t.ipMACs.Mu.Lock()
	defer t.ipMACs.Mu.Unlock()
	t.ipMACs.Data = newData

	t.logger.Debugf("TrafficMap received new IP MAC data: %v", newData)

	// Remove old data from the trafficMap.
	minAllowedTime := time.Now().Add(-config.AppCfg.MonitorConfig.PurgeStatsAfterDuration) // remove trafficMaps older than this.

	t.muTrafficMapLen.Lock()
	defer t.muTrafficMapLen.Unlock()

	if len(newData) != t.trafficMapLen { // if there are trafficMaps to clean up...
		t.trafficMap.Range(func(key any, value any) bool { // for each trafficMap key...
			keyMac := getTrafficMapMACFromKey(key.(string))
			macExists := false
			for _, mac := range newData { // check all ip-mac input newData...
				if keyMac == mac { // if the mac is found in the ip-mac newData...
					macExists = true
				}
			}
			if !macExists { // if the MAC was not found...
				v := value.(*trafficStats)
				if v.lastActiveTimeUTC.Before(minAllowedTime) { // if the last active time for the MAC is old...
					t.trafficMap.Delete(key) // remove the key.
					t.trafficMapLen--        // decrement the trafficMap counter.
				}
			}
			return true
		})
	}
}

func getTrafficMapKey(group models.Group, mac models.MAC) string {
	return fmt.Sprintf("%v%v%v", group, defaultTrafficMapKeySeparator, mac)
}

func getTrafficMapMACFromKey(key string) models.MAC {
	s := strings.Split(key, defaultTrafficMapKeySeparator)
	return models.MAC(s[1])
}

// GetTrafficLastActiveTimes gets the traffic last active times (UTC) in a map where the key is the group
// and the value is a map[models.MAC]<last active time>
// See also getTrafficMapKey().
func (t *TrafficMap) GetTrafficLastActiveTimes() map[models.Group]map[models.MAC]time.Time {
	retval := make(map[models.Group]map[models.MAC]time.Time)
	t.trafficMap.Range(func(key any, value any) bool {
		k := key.(string) // key is "group/mac"
		v := value.(*trafficStats)
		gm := strings.Split(k, defaultTrafficMapKeySeparator)
		group := models.Group(gm[0])
		mac := models.MAC(gm[1])
		if retval[group] == nil {
			retval[group] = make(map[models.MAC]time.Time)
		}
		retval[group][mac] = v.lastActiveTimeUTC
		return true
	})
	return retval
}
