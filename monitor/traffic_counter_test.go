package monitor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"
)

// TODO test that old data is removed from the trafficMap by UpdateSourceIpGroups

func mockNowFunc(testTime time.Time) time.Time {
	now := time.Now()
	if !testTime.IsZero() {
		now = testTime
	}
	nowFunc = func() time.Time { return now }
	return now
}

func TestTrafficMap(t *testing.T) {
	logger := config.MustGetLogger()
	testGroup := models.Group("test")
	testMac := models.MAC("00:00:00:00:00:00")
	testIp := models.Ip("1.1.1.1")
	windowSize := 5

	// Mock the time.
	now := mockNowFunc(time.Time{})

	tm := NewTrafficMap(logger, windowSize)
	assert.Equal(t, windowSize, tm.rollingWindowSize, "unexpected rolling window size")
	assert.Equal(t, tm.trafficMapLen, 0, "unexpected traffic map len initially")
	assert.Same(t, logger, tm.logger, "unexpected logger")

	tm.CountTraffic(testGroup, testIp, models.Ingress, 10, 100)
	assert.Equal(t, 1, tm.trafficMapLen, "unexpected traffic map len")

	expectedKey := getTrafficMapKey(testGroup, testMac)

	tm.trafficMap.Range(func(key, value any) bool {
		assert.Equal(t, expectedKey, key, "expected testKey in traffic map")
		v := value.(*trafficStats)
		assert.Equal(t, expectedKey, v.monitorName, "unexpected monitor name")
		assert.Equal(t, now.UTC(), v.lastActiveTimeUTC, "unexpected lastActivetime")
		return true
	})
}

func TestTrafficMap_UpdateSourceIpGroups(t *testing.T) {
	// TODO: set up MAC data so that keys are removed if they aren't in the new data.
	testMac := models.MAC("00:00:00:00:00:00")

	logger := config.MustGetLogger()
	windowSize := 5
	tm := NewTrafficMap(logger, windowSize)

	// Known group.
	testGroup := models.Group("test1")
	testIp := models.Ip("1.1.1.1")
	tm.CountTraffic(testGroup, testIp, models.Ingress, 10, 100)

	// Unknown group should be removed.
	tm.CountTraffic("groupA", "8.8.8.8", models.Ingress, 10, 100)

	tm.UpdateSourceIpMACs(models.MapIpMACs{
		testIp: testMac,
	})
	assert.Equal(t, 1, tm.trafficMapLen, "unexpected traffic map len")

}
