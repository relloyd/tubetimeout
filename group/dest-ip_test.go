package group

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"
)

// Mock Receiver for Testing
type MockDestDomainGroupReceiver struct {
	updatedGroups models.MapDomainGroups
	mu            sync.Mutex
}

func (m *MockDestDomainGroupReceiver) UpdateDestDomainGroups(newGroups models.MapDomainGroups) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updatedGroups = newGroups
}

func TestLoadGroupDomains(t *testing.T) {
	// Mock loader function
	mockLoaderFunc := func(logger *zap.SugaredLogger) (models.MapGroupDomains, error) {
		return models.MapGroupDomains{
			"GroupA": {"domain1.com", "domain2.com"},
			"GroupB": {"domain2.com", "domain3.com"},
		}, nil
	}

	// Set the loader function to the mock
	originalLoaderFunc := fnGroupDomainLoader
	defer func() { fnGroupDomainLoader = originalLoaderFunc }()
	fnGroupDomainLoader = mockLoaderFunc

	// Initialize a DomainWatcher instance
	dw := &DomainWatcher{
		groupDomains: make(models.MapGroupDomains),
		destDomainGroups: models.DomainGroups{
			Data: make(models.MapDomainGroups),
			Mu:   sync.RWMutex{},
		},
		destDomainGroupsReceivers: []models.DestDomainGroupsReceiver{},
	}

	// Add a mock receiver to observe notifications
	mockReceiver := &MockDestDomainGroupReceiver{}
	dw.destDomainGroupsReceivers = append(dw.destDomainGroupsReceivers, mockReceiver)

	// Call the method under test
	dw.loadGroupDomains()

	// Assertions
	// 1. Validate that groupDomains was populated
	if len(dw.groupDomains) != 2 {
		t.Errorf("Expected groupDomains to have 2 groups, got %d", len(dw.groupDomains))
	}

	// 2. Validate that destDomainGroups was updated
	dw.destDomainGroups.Mu.RLock()
	expectedDomainCount := 3 // Total unique domains
	if len(dw.destDomainGroups.Data) != expectedDomainCount {
		t.Errorf("Expected destDomainGroups to have %d domains, got %d", expectedDomainCount, len(dw.destDomainGroups.Data))
	}
	dw.destDomainGroups.Mu.RUnlock()

	// 3. Verify that the receiver was notified
	mockReceiver.mu.Lock()
	if len(mockReceiver.updatedGroups) != expectedDomainCount {
		t.Errorf("Receiver should have been notified with %d domains, got %d", expectedDomainCount, len(mockReceiver.updatedGroups))
	}
	mockReceiver.mu.Unlock()
}

// TestNewDomainWatcher tests the NewDomainWatcher function created by AI overlords.
func TestNewDomainWatcher(t *testing.T) {
	// Call the function to create a new instance
	dw := NewDomainWatcher(config.MustGetLogger())

	// Assert each field is set up correctly
	assert.NotNil(t, dw, "DomainWatcher instance should not be nil")
	assert.IsType(t, &sync.RWMutex{}, &dw.mu, "mu should be a sync.RWMutex")
	assert.Equal(t, defaultInterval, dw.interval, "interval should be set to defaultInterval")
	assert.NotNil(t, dw.resolver, "resolver should not be nil")
	assert.IsType(t, models.MapGroupDomains{}, dw.groupDomains, "groupDomains should be initialized as MapGroupDomains")
	assert.NotNil(t, dw.groupDomains, "groupDomains should not be nil")

	assert.IsType(t, &models.IpDomains{}, &dw.destIpDomains, "destIpDomains should be of type IpDomains")
	assert.NotNil(t, dw.destIpDomains.Data, "destIpDomains.Data should not be nil")
	assert.IsType(t, models.MapIpDomain{}, dw.destIpDomains.Data, "destIpDomains.Data should be of type MapIpDomain")

	assert.IsType(t, &models.IpGroups{}, &dw.destIpGroups, "destIpGroups should be of type IpGroups")
	assert.NotNil(t, dw.destIpGroups.Data, "destIpGroups.Data should not be nil")
	assert.IsType(t, models.MapIpGroups{}, dw.destIpGroups.Data, "destIpGroups.Data should be of type MapIpGroups")

	assert.IsType(t, &models.DomainGroups{}, &dw.destDomainGroups, "destDomainGroups should be of type DomainGroups")
	assert.NotNil(t, dw.destDomainGroups.Data, "destDomainGroups.Data should not be nil")
	assert.IsType(t, models.MapDomainGroups{}, dw.destDomainGroups.Data, "destDomainGroups.Data should be of type MapDomainGroups")

	assert.Nil(t, dw.destIpDomainReceivers, "destIpDomainReceivers should be nil")
	assert.Nil(t, dw.destIpGroupReceivers, "destIpGroupReceivers should be nil")
	assert.Nil(t, dw.destDomainGroupsReceivers, "destDomainGroupsReceivers should be nil")
}
