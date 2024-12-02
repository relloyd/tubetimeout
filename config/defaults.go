package config

import (
	"time"
)

const (
// DefaultConfigFile is the default configuration file path.
// DefaultConfigFile = ".tubetimeout/config.yaml"
)

type DebugConfig struct {
	DebugEnabled bool          `envconfig:"DEBUG_ENABLED" default:"false"`
	DebugTime    time.Duration `envconfig:"DEBUG_TIME_SECONDS" default:"30s"`
}

type AppConfig struct {
	TrackerConfig TrackerConfig `envconfig:"TRACKER_CONFIG"`
}

type TrackerConfig struct {
	// Retention is the period for samples to be kept and evaluated.
	Retention time.Duration `envconfig:"RETENTION" default:"168h"` // 1 week
	// Granularity is the sampling resolution.
	Granularity time.Duration `envconfig:"GRANULARITY" default:"1m"`
	// Threshold is duration for exceeding conditions.
	Threshold time.Duration `envconfig:"THRESHOLD" default:"2m"`
	// StartDay is the day of the week to start the window.
	StartDay int `envconfig:"START_DAY" default:"5"` // Friday
	// StartTime is the duration past midnight to start the window.
	StartTime time.Duration `envconfig:"START_TIME" default:"12h"` // 12 PM
}
