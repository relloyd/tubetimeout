package models

import (
	"sync"
)

type IP string
type Domain string
type Groups []string

type MapIpDomain map[IP]Domain
type MapIpGroups map[IP]Groups
type MapDomainGroups map[Domain]Groups

type IpDomains struct {
	Data MapIpDomain
	Mu   sync.RWMutex
}

type IpGroups struct {
	Data MapIpGroups
	Mu   sync.RWMutex
}

// type Groups struct {
// 	Groups string
// 	MAC   string
// }
