package models

import (
	"sync"
)

type IpSet struct {
	Ips        map[string]struct{}
	Mu         sync.RWMutex
	FnCallBack func(newIps map[string]struct{})
}

type IPListReceiver interface {
	Notify(newIps map[string]struct{})
}

func (i *IpSet) Notify(newIps map[string]struct{}) {
	// TODO: consider implementing IPListReceiver interface in each module that needs to be notified instead.
	// TODO: don't trust the supplied map is good to just take as we want our own copy.
	i.Mu.Lock()
	defer i.Mu.Unlock()
	i.Ips = newIps

	if i.FnCallBack != nil {
		i.FnCallBack(newIps)
	}
}
