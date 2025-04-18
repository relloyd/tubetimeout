package config

import (
	"fmt"
	"os"
	"time"

	"github.com/kelseyhightower/envconfig"
	"relloyd/tubetimeout/models"
)

var (
	AppHomeDir = ".tubetimeout"
	// AppCfg is the application configuration.
	AppCfg AppConfig
	// BuildTime is set by the go build command - probably see the Makefile.
	BuildTime string
	// BuildVersion is set by the go build command - probably see the Makefile.
	BuildVersion string
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
	LogLevel              string                `envconfig:"LOG_LEVEL" default:"info"`
	DelayStart            bool                  `envconfig:"DELAY_START" default:"true"`
	DebugConfig           DebugConfig           `envconfig:"DEBUG"`
	DHCPServerDisabled    bool                  `envconfig:"DHCP_SERVER_DISABLED" default:"false"` // DHCPServerDisabled is a hack to indicate whether we attempt to start DHCP server functionality at all, aiming to help debugging which needs a stable eth0 IP.
	FilterConfig          FilterConfig          `envconfig:"FILTER"`
	WebConfig             WebConfig             `envconfig:"WEB"`
	MonitorConfig         MonitorConfig         `envconfig:"MONITOR"`
	TrackerConfig         models.TrackerConfig  `envconfig:"TRACKER"`
	ActivityMonitorConfig ActivityMonitorConfig `envconfig:"ACTIVITY_MONITOR"`
}

type DebugConfig struct {
	// DebugEnabled when set true allows time for a dlv debug session to be started before continuing main.
	DebugEnabled bool `envconfig:"ENABLED" default:"false"`
	// DebugTime is the delay before starting main in which time you should connect a dlv debugging session.
	DebugTime time.Duration `envconfig:"TIME_SECONDS" default:"30s"`
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
	PurgeStatsAfterDuration time.Duration `envconfig:"PURGE_DURATION" default:"168h"` // 168h = 7 * 24h = 7days
}

type ActivityMonitorConfig struct {
	// ThresholdIngressEgressKB is the difference between ingress and egress that makes the activity monitor to assume traffic is active when EnableThresholdLogic is set.
	ThresholdIngressEgressKB int `envconfig:"THRESHOLD_INGRESS_EGRESS_KB" default:"0"`
	// EnableThresholdLogic true causes monitor.isActive() to require ingress to be higher than egress to consider traffic as active.
	EnableThresholdLogic bool `envconfig:"ENABLE_THRESHOLD_LOGIC" default:"false"`
}
