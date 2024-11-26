package models

import (
	"sync"
)

type IpSet struct {
	Ips map[string]struct{}
	Mu  sync.RWMutex
}

type IPListReceiver interface {
	Notify(newIps map[string]struct{})
}

func (i *IpSet) Notify(newIps map[string]struct{}) {
	// TODO: don't trust the supplied map is good to just take as we want our own copy.
	i.Mu.Lock()
	defer i.Mu.Unlock()
	i.Ips = newIps
}
