package models

type SourceIpGroupsReceiver interface {
	UpdateSourceIpGroups(newData MapIpGroups)
}

type SourceIpMACReceiver interface {
	UpdateSourceIpMACs(newData MapIpMACs)
}

type SourceIpMACsReceiver interface {
	UpdateSourceIpMACs(newData MapIpMACs)
}

type DestIpDomainReceiver interface {
	UpdateDestIpDomains(newIps MapIpDomain)
}

type DestIpGroupsReceiver interface {
	UpdateDestIpGroups(newGroups MapIpGroups)
}

type DestDomainGroupsReceiver interface {
	UpdateDestDomainGroups(newGroups MapDomainGroups)
}

type ManagerI interface {
	IsSrcIpDestDomainKnown(ip Ip, domain Domain) ([]Group, bool)
}

type TrackerI interface {
	AddSample(id string, active bool)
	HasExceededThreshold(id string) bool
}
