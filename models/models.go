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

type UsageTrackerConfig struct {
	// Retention is the period for samples to be kept and evaluated.
	Retention time.Duration `yaml:"retention"`
	// Threshold is duration for exceeding conditions.
	Threshold time.Duration `yaml:"threshold"`
	// StartDay is the day of the week to start the window.
	StartDay int `yaml:"startDay"`
	// StartTime is the duration past midnight to start the window.
	StartTime time.Duration `yaml:"startTime"`
}

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
