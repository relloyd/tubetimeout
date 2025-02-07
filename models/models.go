package models

import (
	"sync"
	"time"
)

type Ip string
type Domain string
type Group string
type MAC string

type MapGroupDomains map[Group][]Domain
type MapIpDomain map[Ip]Domain
type MapIpGroups map[Ip][]Group
type MapIpMACs map[Ip]MAC
type MapDomainGroups map[Domain][]Group

type IpDomains struct {
	Data MapIpDomain
	Mu   sync.RWMutex
}

type IpGroups struct {
	Data MapIpGroups
	Mu   sync.RWMutex
}

type IpMACs struct {
	Data MapIpMACs
	Mu   sync.RWMutex
}

type DomainGroups struct {
	Data MapDomainGroups
	Mu   sync.RWMutex
}

type NamedMAC struct {
	MAC  string `yaml:"mac"`
	Name string `yaml:"name"`
}

type UsageTrackerMode int

const (
	ModeMonitor UsageTrackerMode = iota
	ModeBlock
	ModeAllow
)

// TrackerConfig contains the configuration for the usage tracker of a specific group.
type TrackerConfig struct {
	// Granularity is the sampling resolution.
	Granularity time.Duration `envconfig:"GRANULARITY" default:"1m"`
	// Retention is the period for samples to be kept and evaluated.
	Retention time.Duration `yaml:"retention" envconfig:"RETENTION" default:"168h"` // 168h == 1 week
	// Threshold is duration for exceeding conditions.
	Threshold time.Duration `yaml:"threshold" envconfig:"THRESHOLD" default:"180m"`
	// StartDay is the day of the week to start the window.
	StartDay int `yaml:"startDay" envconfig:"START_DAY" default:"5"` // Friday
	// StartTime is the duration past midnight to start the window.
	StartTime time.Duration `yaml:"startTime" envconfig:"START_TIME" default:"12h"` // 12 PM
	// SampleFilePath is the path to the file to save/read the device ID samples from.
	SampleFilePath string `envconfig:"FILE_PATH" default:"samples.json"`
	// SampleFileSaveInterval is the interval at which the samples are saved to the file.
	SampleFileSaveInterval time.Duration `envconfig:"SAVE_INTERVAL" default:"1m"`
	// SampleSize is the number of slots in the circular buffer.
	SampleSize int `yaml:"sampleSize"`
	// Mode is the mode of the tracker.
	Mode UsageTrackerMode
	// ModeEndTime is the time at which explicit blocking or allowing ends.
	ModeEndTime time.Time
}

type MapGroupTrackerConfig map[Group]*TrackerConfig

// GroupSummary contains the used and total count of a group used by the usage tracker and web for reporting.
type GroupSummary struct {
	Used            int               `json:"used"`
	Total           int               `json:"total"`
	Percentage      int               `json:"percentage"`
	LastActiveTimes map[MAC]time.Time `json:"activity"`
}

type Direction string

const (
	Ingress Direction = "in"
	Egress  Direction = "out"
)

type GroupMode struct {
	Mode        UsageTrackerMode `json:"mode"`
	ModeEndTime time.Time        `json:"modeEndTime"`
}
