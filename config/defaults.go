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
	TrackerConfig TrackerConfig
}

type TrackerConfig struct {
	WindowStartDay  int           `envconfig:"WINDOW_START_DAY" default:"5"`    // Friday
	WindowStartTime time.Duration `envconfig:"WINDOW_START_TIME" default:"12h"` // 12 PM
	// The default time granularity for sampling.
	Granularity time.Duration `envconfig:"GRANULARITY" default:"1m"`
	// The default retention period for samples.
	Retention time.Duration `envconfig:"RETENTION" default:"168h"` // 1 week
	// The default threshold duration for exceeding conditions.
	Threshold time.Duration `envconfig:"THRESHOLD" default:"2h"`
}

