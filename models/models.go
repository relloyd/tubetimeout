package models

import (
	"sync"
)

type Domain string
type IP string
type MapIpDomain map[IP]Domain
type MapIpMacGroup map[IP]MACGroup

type IpDomains struct {
	Data MapIpDomain
	Mu   sync.RWMutex
}

// MACGroup contains MAC and group information
type MACGroup struct {
	MAC   string
	Group string
}

type IpMacGroups struct {
	Data MapIpMacGroup
	Mu   sync.RWMutex
}
