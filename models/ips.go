package models

import (
	"sync"
)

type IpSet struct {
	Ips map[string]struct{}
	Mu  sync.RWMutex
}
