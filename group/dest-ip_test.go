package group

import (
	"sync"
	"testing"

	"example.com/youtube-nfqueue/models"
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
	mockLoaderFunc := func() (models.MapGroupDomains, error) {
		return models.MapGroupDomains{
			"GroupA": {"domain1.com", "domain2.com"},
			"GroupB": {"domain2.com", "domain3.com"},
		}, nil
	}

	// Set the loader function to the mock
	originalLoaderFunc := groupDomainLoaderFunc
	defer func() { groupDomainLoaderFunc = originalLoaderFunc }()
	groupDomainLoaderFunc = mockLoaderFunc

	// Initialize a DomainWatcher instance
	dw := &DomainWatcher{
		groupDomains: make(models.MapGroupDomains),
		destDomainGroups: models.DomainGroups{
			Data: make(models.MapDomainGroups),
			Mu:   sync.RWMutex{},
		},
		destDomainGroupsReceivers: []DestDomainGroupsReceiver{},
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