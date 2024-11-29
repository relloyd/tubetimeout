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
	devices       sync.Map       // Map of device IDs (string) to *deviceData
	retention     time.Duration  // The retention period for samples
	granularity   time.Duration  // The time granularity for sampling
	threshold     time.Duration  // The threshold duration for exceeding conditions
	sampleSize    int            // The number of slots in the circular buffer
	nowFunc       func() time.Time // Function to get the current time (defaults to time.Now)
}

// NewTracker initializes a Tracker with preallocated slices for each device.
func NewTracker(retention, threshold, granularity time.Duration) *Tracker {
	sampleSize := int(retention / granularity)
	return &Tracker{
		retention:   retention,
		granularity: granularity,
		threshold:   threshold,
		sampleSize:  sampleSize,
		nowFunc:     time.Now, // Default to time.Now
	}
}

// AddSample records a sample for a given device at the current time.
func (t *Tracker) AddSample(deviceID string) {
	now := t.nowFunc() // Use nowFunc instead of time.Now

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
	index := t.getIndex(now, dd.start)
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
func (t *Tracker) getIndex(now time.Time, bufferStart time.Time) int {
	elapsed := int(now.Sub(bufferStart) / t.granularity)
	return (elapsed%t.sampleSize + t.sampleSize) % t.sampleSize // Ensure positive modulo.
}

// syncWindow ensures the slice is synchronized with the current time.
func (t *Tracker) syncWindow(dd *deviceData, now time.Time) {
	elapsed := int(now.Sub(dd.start) / t.granularity)

	if elapsed >= t.sampleSize {
		// If elapsed time exceeds the buffer size, reset the entire window.
		for i := range dd.samples {
			dd.samples[i] = false
		}
		dd.start = now.Truncate(t.granularity) // Reset start only for large time jumps.
	} else if elapsed < 0 {
		// Backward movement: Reset the entire window (for simplicity).
		for i := range dd.samples {
			dd.samples[i] = false
		}
		dd.start = now.Truncate(t.granularity) // Reset start for backward jumps.
	}
	// If 0 < elapsed < t.sampleSize, do nothing. The circular buffer handles overwriting naturally.
}
