package config

import (
	"time"
)

const (
// DefaultConfigFile is the default configuration file path.
// DefaultConfigFile = ".tubetimeout/config.yaml"
)

type AppConfig struct {
	DebugConfig   DebugConfig   `envconfig:"DEBUG"`
	TrackerConfig TrackerConfig `envconfig:"TRACKER"`
	FilterConfig  FilterConfig  `envconfig:"FILTER"`
	ProxyConfig   ProxyConfig   `envconfig:"PROXY"`
}

type DebugConfig struct {
	// DebugEnabled when set true allows time for a dlv debug session to be started before continuing main.
	DebugEnabled bool `envconfig:"ENABLED" default:"false"`
	// DebugTime is the delay before starting main in which time you should connect a dlv debugging session.
	DebugTime time.Duration `envconfig:"TIME_SECONDS" default:"30s"`
}

type TrackerConfig struct {
	// Retention is the period for samples to be kept and evaluated.
	Retention time.Duration `envconfig:"RETENTION" default:"168h"` // 168h == 1 week
	// Granularity is the sampling resolution.
	Granularity time.Duration `envconfig:"GRANULARITY" default:"1m"`
	// Threshold is duration for exceeding conditions.
	Threshold time.Duration `envconfig:"THRESHOLD" default:"1m"`
	// StartDay is the day of the week to start the window.
	StartDay int `envconfig:"START_DAY" default:"5"` // Friday
	// StartTime is the duration past midnight to start the window.
	StartTime time.Duration `envconfig:"START_TIME" default:"12h"` // 12 PM
	// SampleFilePath is the path to the file to save/read the device ID samples from.
	SampleFilePath string `envconfig:"FILE_PATH" default:"samples.json"`
}

type FilterConfig struct {
	PacketDelayMs        time.Duration `envconfig:"PACKET_DELAY_MS" default:"100ms"`
	PacketJitterMs       time.Duration `envconfig:"PACKET_DELAY_JITTER_MS" default:"50ms"`
	PacketDropPercentage float32       `envconfig:"PACKET_DROP_PCT" default:"0.4"`
	PacketDropUDP        bool          `envconfig:"PACKET_DROP_UDP" default:"true"`
	OutboundQueueNumber  uint16        `envconfig:"OUTBOUND_QUEUE_NUMBER" default:"100"`
	InboundQueueNumber   uint16        `envconfig:"INBOUND_QUEUE_NUMBER" default:"101"`
}

type ProxyConfig struct {
	ProxyEnabled bool `envconfig:"ENABLED" default:"false"`
}
