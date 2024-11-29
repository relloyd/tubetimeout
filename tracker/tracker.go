package tracker

import (
	"sync"
	"time"
)

type deviceData struct {
	mu      sync.Mutex
	samples []bool    // Slice of fixed size to represent the rotating window
	start   time.Time // Start time of the slice window
}

type Tracker struct {
	devices     sync.Map
	retention   time.Duration
	granularity time.Duration
	threshold   time.Duration
	sampleSize  int
}

// NewTracker initializes a Tracker with preallocated slices for each device.
func NewTracker(retention, threshold, granularity time.Duration) *Tracker {
	sampleSize := int(retention / granularity)
	return &Tracker{
		retention:   retention,
		threshold:   threshold,
		granularity: granularity,
		sampleSize:  sampleSize,
	}
}

// AddSample records a sample for a given device at the current time.
func (t *Tracker) AddSample(deviceID string) {
	now := time.Now()
	index := t.getIndex(now)

	// Get or initialize the device data.
	data, _ := t.devices.LoadOrStore(deviceID, &deviceData{
		samples: make([]bool, t.sampleSize),
		start:   now.Truncate(t.granularity),
	})

	dd := data.(*deviceData)

	// Update the sample at the calculated index.
	dd.mu.Lock()
	defer dd.mu.Unlock()

	// Ensure the time window is synchronized.
	t.syncWindow(dd, now)

	// Mark the sample as seen.
	dd.samples[index] = true
}

// HasExceededThreshold checks if a device has exceeded the threshold duration.
func (t *Tracker) HasExceededThreshold(deviceID string) bool {
	data, ok := t.devices.Load(deviceID)
	if !ok {
		return false
	}

	dd := data.(*deviceData)

	dd.mu.Lock()
	defer dd.mu.Unlock()

	now := time.Now()

	// Ensure the time window is synchronized.
	t.syncWindow(dd, now)

	// Count the number of true samples in the window.
	count := 0
	for _, seen := range dd.samples {
		if seen {
			count++
		}
	}

	return time.Duration(count)*t.granularity >= t.threshold
}

// getIndex calculates the index in the slice for the current time.
func (t *Tracker) getIndex(now time.Time) int {
	elapsed := now.Sub(now.Truncate(t.granularity))
	return int(elapsed/t.granularity) % t.sampleSize
}

// syncWindow ensures the slice is synchronized with the current time.
func (t *Tracker) syncWindow(dd *deviceData, now time.Time) {
	elapsed := int(now.Sub(dd.start) / t.granularity)

	if elapsed >= t.sampleSize {
		// Reset the entire slice if elapsed exceeds the window size.
		for i := range dd.samples {
			dd.samples[i] = false
		}
		dd.start = now.Truncate(t.granularity)
	} else if elapsed > 0 {
		// Clear only the elapsed slots.
		for i := 0; i < elapsed; i++ {
			dd.samples[(int(dd.start.Sub(dd.start.Truncate(t.granularity))/t.granularity)+i)%t.sampleSize] = false
		}
		dd.start = now.Truncate(t.granularity)
	} else if elapsed < 0 {
		// Backward movement: Reset the entire window (for simplicity).
		for i := range dd.samples {
			dd.samples[i] = false
		}
		dd.start = now.Truncate(t.granularity)
	}
}
