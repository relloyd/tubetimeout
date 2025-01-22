package config

import (
	"fmt"
	"os"
	"time"

	"github.com/kelseyhightower/envconfig"
)

var (
	AppHomeDir = ".tubetimeout"
	// AppCfg is the application configuration.
	AppCfg AppConfig
	// BuildTime is set by the go build command - probably see the Makefile.
	BuildTime string
)

func init() {
	// Load app config from the environment.
	err := envconfig.Process("", &AppCfg)
	if err != nil {
		fmt.Println("failed to process app config:", err)
		os.Exit(1)
	}
}

type AppConfig struct {
	LogLevel      string        `envconfig:"LOG_LEVEL" default:"info"`
	DebugConfig   DebugConfig   `envconfig:"DEBUG"`
	TrackerConfig TrackerConfig `envconfig:"TRACKER"`
	FilterConfig  FilterConfig  `envconfig:"FILTER"`
	WebConfig     WebConfig     `envconfig:"WEB"`
	MonitorConfig MonitorConfig `envconfig:"MONITOR"`
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
	Threshold time.Duration `envconfig:"THRESHOLD" default:"180m"`
	// StartDay is the day of the week to start the window.
	StartDay int `envconfig:"START_DAY" default:"5"` // Friday
	// StartTime is the duration past midnight to start the window.
	StartTime time.Duration `envconfig:"START_TIME" default:"12h"` // 12 PM
	// SampleFilePath is the path to the file to save/read the device ID samples from.
	SampleFilePath string `envconfig:"FILE_PATH" default:"samples.json"`
	// SampleFileSaveInterval
	SampleFileSaveInterval time.Duration `envconfig:"SAVE_INTERVAL" default:"1m"`
}

type FilterConfig struct {
	// PacketDropPercentage is the percentage of packets to drop.
	PacketDropPercentage float32 `envconfig:"PACKET_DROP_PCT" default:"0.40"`
	// PacketDelayPercentage is the percentage of packets to delay evaluated after dropping.
	PacketDelayPercentage float32       `envconfig:"PACKET_DELAY_PCT" default:"0.90"`
	PacketDelayMs         time.Duration `envconfig:"PACKET_DELAY_MS" default:"100ms"`
	PacketJitterMs        time.Duration `envconfig:"PACKET_DELAY_JITTER_MS" default:"50ms"`
	PacketDropUDP         bool          `envconfig:"PACKET_DROP_UDP" default:"true"`
	OutboundQueueNumber   uint16        `envconfig:"OUTBOUND_QUEUE_NUMBER" default:"100"`
	InboundQueueNumber    uint16        `envconfig:"INBOUND_QUEUE_NUMBER" default:"101"`
}

type WebConfig struct {
	WebEnabled bool `envconfig:"ENABLED" default:"true"`
	WebPort    int  `envconfig:"PORT" default:"80"`
}

type MonitorConfig struct {
	PurgeStatsAfterDuration time.Duration `envconfig:"PURGE_DURATION" default:"7d"`
}
