package monitor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"relloyd/tubetimeout/models"
)

func TestGetTrafficMapKey(t *testing.T) {
	group := "GroupMixedCase"
	mac := "dummy:mac:23:ef:00"
	expectedKey := strings.ToLower(fmt.Sprintf("%v-%v", group, mac))

	key := getTrafficMapKey(models.Group(group), models.MAC(mac))
	assert.Equal(t, expectedKey, key, "expected lower case key")
}
