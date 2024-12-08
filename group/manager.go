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
	return &Manager{
		sourceIpGroups:   models.IpGroups{Data: make(models.MapIpGroups)},
		destIpGroups:     models.IpGroups{Data: make(models.MapIpGroups)},
		destIpDomains:    models.IpDomains{Data: make(models.MapIpDomain)},
		destDomainGroups: models.DomainGroups{Data: make(models.MapDomainGroups)},
	}
}

func (m *Manager) UpdateSourceIpGroups(newData models.MapIpGroups) {
	m.sourceIpGroups.Mu.Lock()
	defer m.sourceIpGroups.Mu.Unlock()
	m.sourceIpGroups.Data = newData
}

func (m *Manager) UpdateDestIpGroups(newData models.MapIpGroups) {
	m.destIpGroups.Mu.Lock()
	defer m.destIpGroups.Mu.Unlock()
	m.destIpGroups.Data = newData
}

func (m *Manager) UpdateDestIpDomains(newData models.MapIpDomain) {
	m.destIpDomains.Mu.Lock()
	defer m.destIpDomains.Mu.Unlock()
	m.destIpDomains.Data = newData
}

func (m *Manager) IsSrcIpKnown(ip string) (models.Group, bool) {
	m.sourceIpGroups.Mu.RLock()
	defer m.sourceIpGroups.Mu.RUnlock()
	groups, ok := m.sourceIpGroups.Data[models.Ip(ip)]
	if !ok {
		return "", false
	}
	return groups[0], true
}

func (m *Manager) IsDstIpKnown(ip string) (models.Domain, bool) {
	m.destIpDomains.Mu.RLock()
	defer m.destIpDomains.Mu.RUnlock()
	domain, ok := m.destIpDomains.Data[models.Ip(ip)]
	return domain, ok
}

func (m *Manager) IsDstIpGroupKnown(ip string) (models.Group, bool) {
	m.destIpGroups.Mu.RLock()
	defer m.destIpGroups.Mu.RUnlock()
	groups, ok := m.destIpGroups.Data[models.Ip(ip)]
	if !ok {
		return "", false
	}
	return groups[0], true
}
