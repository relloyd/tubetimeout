package models

import (
	"sync"
)

type IpDomains struct {
	Data MapIpDomain
	Mu   sync.RWMutex
}

type IpGroups struct {
	Data MapIpGroups
	Mu   sync.RWMutex
}

type IP string
type Domain string
type Group string

type MapGroupDomains map[Group][]Domain
type MapIpDomain map[IP]Domain
type MapIpGroups map[IP][]Group
type MapDomainGroups map[Domain][]Group

