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
	// Mock loader function
	mockLoaderFunc := func(logger *zap.SugaredLogger) (config.GroupMACsConfig, error) {
		return config.GroupMACsConfig{
			Groups: map[models.Group][]models.NamedMAC{
				"group1": {{MAC: "00:11:22:33:44:55", Name: ""}, {MAC: "66:77:88:99:AA:BB", Name: ""}},
				"group2": {{MAC: "CC:DD:EE:FF:00:11", Name: ""}},
			},
		}, nil
	}

	// Set the loader function to the mock
	originalLoaderFunc := groupMacsLoaderFunc
	defer func() { groupMacsLoaderFunc = originalLoaderFunc }()
	groupMacsLoaderFunc = mockLoaderFunc

	// Define a mock ARP command that returns a fixed output
	mockARPCommand := func() (string, error) {
		return `
? (192.168.1.10) at 00:11:22:33:44:55
? (192.168.1.11) at 66:77:88:99:AA:BB
? (192.168.1.12) at CC:DD:EE:FF:00:11
`, nil
	}

	// Call the function under test
	mig := scanNetwork(config.MustGetLogger(), mockARPCommand)

	// Validate the result
	expectedMap := map[models.Ip][]models.Group{
		"192.168.1.10": {"group1"},
		"192.168.1.11": {"group1"},
		"192.168.1.12": {"group2"},
	}
	assert.Equal(t, len(expectedMap), len(mig), "Number of entries in the map")

	for ip, expectedGroups := range expectedMap {
		groups, exists := mig[ip]
		if !exists {
			t.Errorf("Ip %s not found in result", ip)
			continue
		}
		if !slices.Equal(groups, expectedGroups) {
			t.Errorf("Ip %s: expected %v, got %v", ip, expectedGroups, groups)
		}
	}

	// Test the case where the group-macs file is not found.
	// Expect all IPs to be in the default group.
	groupMacsLoaderFunc = func(logger *zap.SugaredLogger) (config.GroupMACsConfig, error) {
		return config.GroupMACsConfig{}, config.ErrorGroupMacFileNotFound
	}
	mig = scanNetwork(config.MustGetLogger(), mockARPCommand)
	expectedMap = map[models.Ip][]models.Group{
		"192.168.1.10": {defaultGroupName},
		"192.168.1.11": {defaultGroupName},
		"192.168.1.12": {defaultGroupName},
	}
	assert.Equal(t, len(expectedMap), len(mig), "Number of entries in the map")
	for ip, expectedGroups := range expectedMap {
		groups, exists := mig[ip]
		if !exists {
			t.Errorf("Ip %s not found in result", ip)
			continue
		}
		if !slices.Equal(groups, expectedGroups) {
			t.Errorf("Ip %s: expected %v, got %v", ip, expectedGroups, groups)
		}
	}
}

// TODO: test that the callback is called when the ARP scan is triggered
//  and for when the MAC-Group mapping is empty and we default to every IP
