package group

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"
)

func TestScanNetwork(t *testing.T) {
	// Set the loader function to the mock
	originalLoaderFunc := groupMacsLoaderFunc
	defer func() { groupMacsLoaderFunc = originalLoaderFunc }()

	// Mock loader function
	groupMacsLoaderFunc = func(logger *zap.SugaredLogger) (config.GroupMACsConfig, error) {
		return config.GroupMACsConfig{
			Groups: map[models.Group][]models.NamedMAC{
				"group1": {{MAC: "00-11-22-33-44-55", Name: ""}, {MAC: "66-77-88-99-AA-BB", Name: ""}},
				"group2": {{MAC: "CC-DD-EE-FF-00-11", Name: ""}},
			},
		}, nil
	}

	// Define a mock ARP command that returns a fixed output.
	// Include duplicates across multiple adapters.
	mockARPCommand := func() (string, error) {
		return `
? (192.168.1.10) at 00:11:22:33:44:55
? (192.168.1.11) at 66:77:88:99:AA:BB
? (192.168.1.12) at CC:DD:EE:FF:00:11 on wlan0
? (192.168.1.12) at CC:DD:EE:FF:00:11 on eth0
`, nil
	}

	// Call the function under test.
	mig, mim := scanNetwork(config.MustGetLogger(), mockARPCommand)
	// Validate the IP MACs.
	expectedMig := map[models.Ip][]models.Group{
		"192.168.1.10": {"group1"},
		"192.168.1.11": {"group1"},
		"192.168.1.12": {"group2"},
	}
	assert.Equal(t, len(expectedMig), len(mig), "Number of entries in the map Ip Groups")
	for ip, expectedGroups := range expectedMig {
		groups, exists := mig[ip]
		if !exists {
			t.Errorf("Ip %s not found in result", ip)
			continue
		}
		if !slices.Equal(groups, expectedGroups) {
			t.Errorf("Ip %s: expected %v, got %v", ip, expectedGroups, groups)
		}
	}

	// Validate the IP MACs.
	expectedMim := models.MapIpMACs{
		"192.168.1.10": "00-11-22-33-44-55",
		"192.168.1.11": "66-77-88-99-AA-BB",
		"192.168.1.12": "CC-DD-EE-FF-00-11",
	}
	assert.Equal(t, len(expectedMim), len(mim), "Number of entries in the map Ip MACs")
	for expectedIp, expectedMAC := range expectedMim {
		_, exists := mim[expectedIp]
		if !exists {
			t.Errorf("MAC %s not found in map", expectedMAC)
		}
	}

	// Test the case where the group-macs file is not found.
	// Expect all IPs to be in the default group.
	// Expect the IP-MACs to be returned anyway.
	groupMacsLoaderFunc = func(logger *zap.SugaredLogger) (config.GroupMACsConfig, error) {
		return config.GroupMACsConfig{}, config.ErrorGroupMacFileNotFound
	}
	// Call the function under test.
	mig, mim = scanNetwork(config.MustGetLogger(), mockARPCommand)
	// Validate the IP Groups.
	expectedMig = map[models.Ip][]models.Group{
		"192.168.1.10": {defaultGroupName},
		"192.168.1.11": {defaultGroupName},
		"192.168.1.12": {defaultGroupName},
	}
	assert.Equal(t, len(expectedMig), len(mig), "Number of entries in the map")
	for ip, expectedGroups := range expectedMig {
		groups, exists := mig[ip]
		if !exists {
			t.Errorf("Ip %s not found in result", ip)
			continue
		}
		if !slices.Equal(groups, expectedGroups) {
			t.Errorf("Ip %s: expected %v, got %v", ip, expectedGroups, groups)
		}
	}
	// Validate the IP MACs.
	assert.Equal(t, len(expectedMim), len(mim), "bad number of IP MACs returned from scanNetwork")
	assert.Equal(t, expectedMim, mim, "unexpected IP MACs returned from scanNetwork")
}

// TODO: test that the source IPs and MACs callbacks are called when the ARP scan is triggered
//  and when the MAC-Group mapping is empty and we default to every IP
//  and in what cases we get zero macs
//  and that the IP-MACs callbacks are executed when we have data for them
