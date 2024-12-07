package group

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"example.com/youtube-nfqueue/models"
)

type Receiver interface {
	UpdateIpMacGroups(newData models.MapIpGroups)
}

// NetWatcher manages ARP scanning and registered callbacks
type NetWatcher struct {
	ipMacGroups models.MapIpGroups
	callbacks   []Receiver
	mutex       sync.RWMutex
}

// NewNetWatcher creates a new NetWatcher instance
func NewNetWatcher() *NetWatcher {
	return &NetWatcher{
		ipMacGroups: make(map[models.IP]models.Groups),
		callbacks:   []Receiver{},
	}
}

// RegisterIpMacGroupReceivers registers a callback to be called on updates
func (nw *NetWatcher) RegisterIpMacGroupReceivers(callback ...Receiver) {
	nw.mutex.Lock()
	defer nw.mutex.Unlock()
	nw.callbacks = append(nw.callbacks, callback...)
}

// Start begins the periodic ARP scanning process and supports cancellation using context
func (nw *NetWatcher) Start(ctx context.Context, yamlPath string) {
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		for {
			select {
			case <-ctx.Done():
				// Context was canceled, exit the loop
				return
			case <-ticker.C:
				// Perform ARP scan and get updated map
				newMapIpGroups := ScanNetwork(yamlPath, ARPCmd)

				// Compare with existing data
				nw.mutex.Lock()
				if !maps.EqualFunc(nw.ipMacGroups, newMapIpGroups, func(m1 models.Groups, m2 models.Groups) bool {
					return slices.Equal(m1, m2)
				}) { // if there is new arp data...
					nw.ipMacGroups = newMapIpGroups
					// UpdateIPDomains all registered callbacks
					for _, cb := range nw.callbacks {
						cb.UpdateIpMacGroups(newMapIpGroups)
					}
				}
				nw.mutex.Unlock()
			}
		}
	}()
}
