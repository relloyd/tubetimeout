package group

import (
	"fmt"

	"go.uber.org/zap"
	"relloyd/tubetimeout/models"
)

var (
	// managerModeMatchAllSourceIps is a flag to determine if the manager should match all source IPs (true) or aim for the intersection of source dest groups determined by source desk IPs (false).
	managerModeMatchAllSourceIps = false
	// rollingWindowSize is the size of the rolling window for the traffic monitor.
	rollingWindowSize = 5
)

type ManagerI interface {
	IsSrcDestIpKnown(srcIp, dstIp models.Ip) ([]models.Group, bool)
}

type Manager struct {
	logger           *zap.SugaredLogger
	sourceIpGroups   models.IpGroups
	destIpGroups     models.IpGroups
	destIpDomains    models.IpDomains
	destDomainGroups models.DomainGroups
}

func NewManager(logger *zap.SugaredLogger) *Manager {
	m := &Manager{
		logger:           logger,
		sourceIpGroups:   models.IpGroups{Data: make(models.MapIpGroups)},
		destIpGroups:     models.IpGroups{Data: make(models.MapIpGroups)},
		destIpDomains:    models.IpDomains{Data: make(models.MapIpDomain)},
		destDomainGroups: models.DomainGroups{Data: make(models.MapDomainGroups)},
	}
	return m
}

// UpdateSourceIpGroups implements the SourceIpGroupsReceiver interface.
func (m *Manager) UpdateSourceIpGroups(newData models.MapIpGroups) {
	m.sourceIpGroups.Mu.Lock()
	defer m.sourceIpGroups.Mu.Unlock()
	m.sourceIpGroups.Data = newData
	m.logger.Debugf("Manager callback updated source IP groups: %v", newData)
}

// UpdateDestIpGroups implements the DestIpGroupsReceiver interface.
func (m *Manager) UpdateDestIpGroups(newData models.MapIpGroups) {
	m.destIpGroups.Mu.Lock()
	defer m.destIpGroups.Mu.Unlock()
	m.destIpGroups.Data = newData
	m.logger.Debugf("Manager callback updated destination IP groups: %v", newData)
}

// UpdateDestIpDomains implements the DestIpDomainReceiver interface.
func (m *Manager) UpdateDestIpDomains(newData models.MapIpDomain) {
	m.destIpDomains.Mu.Lock()
	defer m.destIpDomains.Mu.Unlock()
	m.destIpDomains.Data = newData
}

// UpdateDestDomainGroups implements the DestDomainGroupsReceiver interface.
func (m *Manager) UpdateDestDomainGroups(newData models.MapDomainGroups) {
	m.destDomainGroups.Mu.Lock()
	defer m.destDomainGroups.Mu.Unlock()
	m.destDomainGroups.Data = newData
}

// isSrcIpGroupKnown checks if the source IP is known and returns the groups it belongs to.
func (m *Manager) isSrcIpGroupKnown(ip models.Ip) ([]models.Group, bool) {
	m.sourceIpGroups.Mu.RLock()
	defer m.sourceIpGroups.Mu.RUnlock()
	groups, ok := m.sourceIpGroups.Data[ip]
	if !ok {
		return []models.Group{}, false
	}
	return groups, true
}

// isDstIpGroupKnown checks if the destination IP is known and returns the groups it belongs to.
func (m *Manager) isDstIpGroupKnown(ip models.Ip) ([]models.Group, bool) {
	m.destIpGroups.Mu.RLock()
	defer m.destIpGroups.Mu.RUnlock()
	groups, ok := m.destIpGroups.Data[ip]
	if !ok {
		return []models.Group{}, false
	}
	return groups, true
}

// isDstIpDomainKnown checks if the destination IP is known and returns the domain it belongs to.
func (m *Manager) isDstIpDomainKnown(ip string) (models.Domain, bool) {
	m.destIpDomains.Mu.RLock()
	defer m.destIpDomains.Mu.RUnlock()
	domain, ok := m.destIpDomains.Data[models.Ip(ip)]
	return domain, ok
}

// isDstDomainGroupKnown checks if the destination domain is known and returns the groups it belongs to.
func (m *Manager) isDstDomainGroupKnown(domain models.Domain) ([]models.Group, bool) {
	m.destDomainGroups.Mu.RLock()
	defer m.destDomainGroups.Mu.RUnlock()
	groups, ok := m.destDomainGroups.Data[domain]
	if !ok {
		return []models.Group{}, false
	}
	return groups, true
}

// IsSrcDestIpKnown checks if the source and destination IPs are known and returns the src groups.
func (m *Manager) IsSrcDestIpKnown(srcIp, dstIp models.Ip) ([]models.Group, bool) {
	// If the manager should match all source IPs as if they're in their own group...
	if managerModeMatchAllSourceIps {
		// Create a return set of groups using metadata.
		var retval []models.Group
		dstGroup, dstOk := m.isDstIpGroupKnown(dstIp)
		if dstOk {
			for _, dg := range dstGroup {
				retval = append(retval, getMetaSrcIpDestGroup(srcIp, dg))
			}
			return retval, true
		}
		return retval, false
	}

	// Check if the source IP and destination domain are known.
	srcGroup, srcOk := m.isSrcIpGroupKnown(srcIp)
	_, dstOk := m.isDstIpGroupKnown(dstIp)
	if !srcOk || !dstOk {
		return []models.Group{}, false
	}

	// Return the list of source groups.
	return srcGroup, true
}

// IsSrcIpDestDomainKnown checks if the source IP and destination domain are known and returns the intersection of groups.
// TODO: only make public the functions in the interface and those used by rules and queue.
func (m *Manager) IsSrcIpDestDomainKnown(srcIp models.Ip, dstDomain models.Domain) ([]models.Group, bool) {
	// If the manager should match all source IPs as if they're in their own group...
	if managerModeMatchAllSourceIps {
		// Create a return set of groups using metadata.
		var retval []models.Group
		dstGroup, dstOk := m.isDstDomainGroupKnown(dstDomain)
		if dstOk {
			for _, dg := range dstGroup {
				retval = append(retval, getMetaSrcIpDestGroup(srcIp, dg))
			}
			return retval, true
		}
		return retval, false
	}

	// Check if the source IP and destination domain are known.
	srcGroup, srcOK := m.isSrcIpGroupKnown(srcIp)
	_, dstOK := m.isDstDomainGroupKnown(dstDomain)
	if !srcOK || !dstOK {
		return []models.Group{}, false
	}
	// Return the list of source groups.
	return srcGroup, true
}

func getMetaSrcIpDestGroup(srcIp models.Ip, dstGroup models.Group) models.Group {
	return models.Group(fmt.Sprintf("%v/%v", srcIp, dstGroup))
}
