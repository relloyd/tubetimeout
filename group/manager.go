package group

import (
	"example.com/youtube-nfqueue/models"
)

type Manager struct {
	sourceIpGroups   models.IpGroups
	destIpGroups     models.IpGroups
	destIpDomains    models.IpDomains
	destDomainGroups models.DomainGroups
}

func NewManager() *Manager {
	m := &Manager{
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
}

// UpdateDestIpGroups implements the DestIpGroupsReceiver interface.
func (m *Manager) UpdateDestIpGroups(newData models.MapIpGroups) {
	m.destIpGroups.Mu.Lock()
	defer m.destIpGroups.Mu.Unlock()
	m.destIpGroups.Data = newData
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

// IsSrcIpGroupKnown checks if the source IP is known and returns the groups it belongs to.
func (m *Manager) IsSrcIpGroupKnown(ip string) ([]models.Group, bool) {
	m.sourceIpGroups.Mu.RLock()
	defer m.sourceIpGroups.Mu.RUnlock()
	groups, ok := m.sourceIpGroups.Data[models.Ip(ip)]
	if !ok {
		return []models.Group{}, false
	}
	return groups, true
}

// IsDstIpGroupKnown checks if the destination IP is known and returns the groups it belongs to.
func (m *Manager) IsDstIpGroupKnown(ip string) ([]models.Group, bool) {
	m.destIpGroups.Mu.RLock()
	defer m.destIpGroups.Mu.RUnlock()
	groups, ok := m.destIpGroups.Data[models.Ip(ip)]
	if !ok {
		return []models.Group{}, false
	}
	return groups, true
}

// IsDstIpDomainKnown checks if the destination IP is known and returns the domain it belongs to.
func (m *Manager) IsDstIpDomainKnown(ip string) (models.Domain, bool) {
	m.destIpDomains.Mu.RLock()
	defer m.destIpDomains.Mu.RUnlock()
	domain, ok := m.destIpDomains.Data[models.Ip(ip)]
	return domain, ok
}

// IsDstDomainGroupKnown checks if the destination domain is known and returns the groups it belongs to.
func (m *Manager) IsDstDomainGroupKnown(domain string) ([]models.Group, bool) {
	m.destDomainGroups.Mu.RLock()
	defer m.destDomainGroups.Mu.RUnlock()
	groups, ok := m.destDomainGroups.Data[models.Domain(domain)]
	if !ok {
		return []models.Group{}, false
	}
	return groups, true
}

// IsSrcDestIpKnown checks if the source and destination IPs are known and returns the groups they intersect.
func (m *Manager) IsSrcDestIpKnown(srcIp, dstIp string) ([]models.Group, bool) {
	srcGroup, srcOk := m.IsSrcIpGroupKnown(srcIp)
	dstGroup, dstOk := m.IsDstIpGroupKnown(dstIp)
	if !srcOk && !dstOk {
		return []models.Group{}, false
	}
	// Return a list of groups where they intersect.
	var intersection []models.Group
	for _, src := range srcGroup {
		for _, dst := range dstGroup {
			if src == dst {
				intersection = append(intersection, src)
			}
		}
	}
	if len(intersection) == 0 {
		return []models.Group{}, false
	}
	return intersection, true
}

// IsSrcIpDestDomainKnown checks if the source IP and destination domain are known and returns the groups they intersect.
func (m *Manager) IsSrcIpDestDomainKnown(srcIp, dstDomain string) ([]models.Group, bool) {
	srcGroup, srcOK := m.IsSrcIpGroupKnown(srcIp)
	dstGroup, dstOK := m.IsDstDomainGroupKnown(dstDomain)
	if !srcOK || !dstOK {
		return []models.Group{}, false
	}
	// Return a list of groups where they intersect.
	var intersection []models.Group
	for _, src := range srcGroup {
		for _, dst := range dstGroup {
			if src == dst {
				intersection = append(intersection, src)
			}
		}
	}
	if len(intersection) == 0 {
		return []models.Group{}, false
	}
	return intersection, true
}