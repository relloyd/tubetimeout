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

type DomainGroups struct {
	Data MapDomainGroups
	Mu   sync.RWMutex
}

type Ip string
type Domain string
type Group string

type MapGroupDomains map[Group][]Domain
type MapIpDomain map[Ip]Domain
type MapIpGroups map[Ip][]Group
type MapDomainGroups map[Domain][]Group

type NamedMAC struct {
	MAC string `yaml:"mac"`
	Name string `yaml:"name"`
}