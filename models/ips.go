package models

import (
	"sync"
)

type Domain string
type IP string
type MapIpDomain map[IP]Domain

type IpSet struct {
	Ips MapIpDomain
	Mu  sync.RWMutex
}
