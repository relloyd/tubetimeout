package tracker

import (
	"fmt"
	"sync"
	"time"
)

type deviceData struct {
	mu      sync.Mutex
	samples []bool    // Slice of fixed size to represent the rotating window
	start   time.Time // Start time of the slice window
}

type Tracker struct {
	devices     sync.Map         // Map of device IDs (string) to *deviceData
	retention   time.Duration    // The retention period for samples
	granularity time.Duration    // The time granularity for sampling
	threshold   time.Duration    // The threshold duration for exceeding conditions
	sampleSize  int              // The number of slots in the circular buffer
	nowFunc     func() time.Time // Function to get the current time (defaults to time.Now)
}

// NewTracker initializes a Tracker with preallocated slices for each device.
func NewTracker(retention, granularity, threshold time.Duration) *Tracker {
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
			fmt.Printf("seen count: %v\n", count)
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
	// Calculate number of time slices that have elapsed since the start of the window.
	elapsed := int(now.Sub(dd.start) / t.granularity)
	if elapsed >= t.sampleSize || elapsed < 0 {
		// If elapsed time exceeds the buffer size, reset the entire window.
		for i := range dd.samples {
			dd.samples[i] = false
		}
		dd.start = now.Truncate(t.granularity) // Reset start only for large time jumps.
	}
	// If 0 < elapsed < t.sampleSize, do nothing. The circular buffer handles overwriting naturally.
}

type trackerWindow struct {
	lastWindowStart time.Time
	nextWindowStart time.Time
	durationToNext  time.Duration
}

func calculateWindow(now time.Time, WindowStartDay int, WindowStartTime time.Duration) trackerWindow {
	// Find the last occurrence of the desired day of the week
	daysSinceStartDay := (int(now.Weekday()) - int(WindowStartDay) + 7) % 7
	lastWindowStart := now.AddDate(0, 0, -daysSinceStartDay).Truncate(24 * time.Hour).Add(WindowStartTime)

	// Calculate the duration to the next occurrence of WindowStartDay
	nextWindowStart := lastWindowStart.AddDate(0, 0, 7)
	durationToNextStart := nextWindowStart.Sub(now)

	// Output results
	fmt.Printf("Current time: %v\n", now)
	fmt.Printf("Last %v: %v\n", WindowStartDay, lastWindowStart)
	fmt.Printf("Next %v: %v\n", WindowStartDay, nextWindowStart)
	fmt.Printf("Duration to next %v: %v\n", WindowStartDay, durationToNextStart)

	return trackerWindow{
			lastWindowStart: lastWindowStart,
			nextWindowStart: nextWindowStart,
			durationToNext: durationToNextStart,
	}
}
