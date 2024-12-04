package netwatch

import (
	"context"
	"maps"
	"sync"
	"time"

	"example.com/youtube-nfqueue/models"
)

type NetWatcherReceiver interface {
	UpdateIpMacGroups(newData models.MapIpMacGroup)
}

// NetWatcher manages ARP scanning and registered callbacks
type NetWatcher struct {
	ipMacGroups models.MapIpMacGroup
	callbacks   []NetWatcherReceiver
	mutex       sync.RWMutex
}

// NewNetWatcher creates a new NetWatcher instance
func NewNetWatcher() *NetWatcher {
	return &NetWatcher{
		ipMacGroups: make(map[models.IP]models.MACGroup),
		callbacks:   []NetWatcherReceiver{},
	}
}

// RegisterIpMacGroupReceivers registers a callback to be called on updates
func (nw *NetWatcher) RegisterIpMacGroupReceivers(callback ...NetWatcherReceiver) {
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
				newMap := ScanNetwork(yamlPath, ARPCmd)

				// Compare with existing data
				nw.mutex.Lock()
				if !maps.Equal(nw.ipMacGroups, newMap) { // if there is new arp data...
					nw.ipMacGroups = newMap

					// UpdateIPDomains all registered callbacks
					for _, cb := range nw.callbacks {
						cb.UpdateIpMacGroups(newMap)
					}
				}
				nw.mutex.Unlock()
			}
		}
	}()
}
