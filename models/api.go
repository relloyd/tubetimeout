package models

import (
	"time"
)

// FlatTrackerConfig is used by the API.
type FlatTrackerConfig struct {
	Group         Group            `json:"name"`
	Retention     time.Duration    `json:"retention"`
	Threshold     time.Duration    `json:"threshold"`
	StartDayInt   int              `json:"startDay"`
	StartDuration time.Duration    `json:"startDuration"`
	Mode          UsageTrackerMode `json:"mode"`
	ModeEndTime   time.Time        `json:"modeEndTime"`
}

// TrackerMode is used by the API to return data to the web page.
type TrackerMode struct {
	Mode        UsageTrackerMode `json:"mode"`
	ModeEndTime time.Time        `json:"modeEndTime"`
}

// TrackerSummary contains the used and total count of a group used by the usage tracker and web for reporting.
type TrackerSummary struct {
	Used            int               `json:"used"`
	Total           int               `json:"total"`
	Percentage      int               `json:"percentage"`
	LastActiveTimes map[MAC]time.Time `json:"activity"`
}
