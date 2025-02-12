package group

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"relloyd/tubetimeout/models"
)

func TestIsSrcIpDestDomainKnown(t *testing.T) {
	tests := []struct {
		name                string
		srcIp               models.Ip
		dstDomain           models.Domain
		managerModeMatchAll bool
		sourceIpGroups      models.IpGroups
		destDomainGroups    models.DomainGroups
		expectedGroups      []models.Group
		expectedOk          bool
	}{
		{
			name:                "Match all mode - domain known",
			srcIp:               "192.168.0.1",
			dstDomain:           "example.com",
			managerModeMatchAll: true,
			sourceIpGroups:      models.IpGroups{},
			destDomainGroups:    models.DomainGroups{Data: models.MapDomainGroups{"example.com": {"group1"}}},
			expectedGroups:      []models.Group{"192.168.0.1/group1"},
			expectedOk:          true,
		},
		{
			name:                "Match all mode - domain unknown",
			srcIp:               "192.168.0.1",
			dstDomain:           "unknown.com",
			managerModeMatchAll: true,
			sourceIpGroups:      models.IpGroups{},
			destDomainGroups:    models.DomainGroups{},
			expectedGroups:      nil,
			expectedOk:          false,
		},
		{
			name:                "All source groups",
			srcIp:               "192.168.0.1",
			dstDomain:           "example.com",
			managerModeMatchAll: false,
			sourceIpGroups:      models.IpGroups{Data: models.MapIpGroups{"192.168.0.1": {"group1", "group2"}}},
			destDomainGroups:    models.DomainGroups{Data: models.MapDomainGroups{"example.com": {"group2", "group3"}}},
			expectedGroups:      []models.Group{"group1", "group2"},
			expectedOk:          true,
		},
		{
			name:                "Either srcIp or dstDomain unknown",
			srcIp:               "192.168.0.1",
			dstDomain:           "example.com",
			managerModeMatchAll: false,
			sourceIpGroups:      models.IpGroups{}, // srcIp not known
			destDomainGroups:    models.DomainGroups{},
			expectedGroups:      []models.Group{},
			expectedOk:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Configure the manager with the test's specific attributes
			mgr := &Manager{
				sourceIpGroups:   tt.sourceIpGroups,
				destDomainGroups: tt.destDomainGroups,
			}

			// Override the global flag for match-all mode
			managerModeMatchAllSourceIps = tt.managerModeMatchAll

			// Run the method under test
			actualGroups, actualOk := mgr.IsSrcIpDestDomainKnown(tt.srcIp, tt.dstDomain)

			// Assert the results
			assert.Equal(t, tt.expectedGroups, actualGroups)
			assert.Equal(t, tt.expectedOk, actualOk)
		})
	}
}
