package netwatch

import (
	"context"
	"maps"
	"sync"
	"time"

	"example.com/youtube-nfqueue/models"
)

// MACGroup contains MAC and group information
type MACGroup struct {
	MAC   string
	Group string
}

type NetWatcherReceiver interface {
	Notify(map[models.IP]MACGroup)
}

// NetWatcher manages ARP scanning and registered callbacks
type NetWatcher struct {
	ipMap     map[models.IP]MACGroup
	callbacks []NetWatcherReceiver
	mutex     sync.RWMutex
}

// NewNetWatcher creates a new NetWatcher instance
func NewNetWatcher() *NetWatcher {
	return &NetWatcher{
		ipMap:     make(map[models.IP]MACGroup),
		callbacks: []NetWatcherReceiver{},
	}
}

// RegisterCallback registers a callback to be called on updates
func (nw *NetWatcher) RegisterCallback(callback []NetWatcherReceiver) {
	nw.mutex.Lock()
	defer nw.mutex.Unlock()
	nw.callbacks = append(nw.callbacks, callback...)
}

// Start begins the periodic ARP scanning process and supports cancellation using context
func (nw *NetWatcher) Start(ctx context.Context, yamlPath string) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				// Context was canceled, exit the loop
				return
			default:
				// Perform ARP scan and get updated map
				newMap := ScanNetwork(yamlPath, ARPCmd)

				// Compare with existing data
				nw.mutex.Lock()
				if !maps.Equal(nw.ipMap, newMap) {
					nw.ipMap = newMap

					// Notify all registered callbacks
					for _, cb := range nw.callbacks {
						cb.Notify(newMap)
					}
				}
				nw.mutex.Unlock()

				// Sleep for 1 minute before the next scan
				time.Sleep(1 * time.Minute)
			}
		}
	}()
}

// mapsEqual compares two maps for equality
func mapsEqual(m1, m2 map[string]MACGroup) bool {
	if len(m1) != len(m2) {
		return false
	}
	for k, v := range m1 {
		if v != m2[k] {
			return false
		}
	}
	return true
}
