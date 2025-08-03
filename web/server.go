package web

import (
	"embed"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/dhcp"
	"relloyd/tubetimeout/ipv6"
	"relloyd/tubetimeout/models"
)

//go:embed static/* templates/*
var embeddedFiles embed.FS

type TemplateData struct {
	BuildTime    string
	BuildVersion string
	StartTime    string
}

type GroupMACsGroupGetterSetter interface {
	GetAllGroupMACs(logger *zap.SugaredLogger) ([]config.FlatGroupMAC, error)
	SaveGroupMACs(logger *zap.SugaredLogger, flatGroupMACs []config.FlatGroupMAC) error
}

// UsageTracker returns info from the usage tracker.
type UsageTracker interface {
	GetSummary() map[string]*models.TrackerSummary
	SetMode(id string, d time.Duration, mode models.UsageTrackerMode) error
	GetModeEndTime(id string) (models.TrackerMode, error)
	Reset(id string)
	GetConfig() (models.MapGroupTrackerConfig, error)
	SetConfig(m models.MapGroupTrackerConfig) error
}

type Monitor interface {
	GetTrafficLastActiveTimes() map[models.Group]map[models.MAC]time.Time
}

type DHCPConfigGetterSetter interface {
	GetConfig(logger *zap.SugaredLogger) (*dhcp.DNSMasqConfig, error)
	SetConfig(logger *zap.SugaredLogger, cfg *dhcp.DNSMasqConfig) error
}

type IPV6Checker interface {
	IsEnabled() ipv6.Status
}

type Handler struct {
	logger                 *zap.SugaredLogger
	startTime              time.Time
	groupMACsGetterSetter  GroupMACsGroupGetterSetter
	usageTracker           UsageTracker
	monitor                Monitor
	dhcpConfigGetterSetter DHCPConfigGetterSetter
	ipv6Checker            IPV6Checker
}

func NewServer(logger *zap.SugaredLogger, ut UsageTracker, gm GroupMACsGroupGetterSetter, m Monitor, d DHCPConfigGetterSetter, ipv6Checker IPV6Checker) *http.Server {
	h := Handler{logger: logger, startTime: time.Now(), usageTracker: ut, groupMACsGetterSetter: gm, monitor: m, dhcpConfigGetterSetter: d, ipv6Checker: ipv6Checker}
	mux := http.NewServeMux()
	mux.HandleFunc("/", h.rootHandler)
	mux.HandleFunc("/static/", h.staticHandler)
	mux.HandleFunc("/groups", h.groupMACHandler)
	mux.HandleFunc("/trackerConfig", h.trackerConfigHandler)
	mux.HandleFunc("/usage", h.usageHandler)       // TODO: probably convert this to /tracker/<group-id>/usage
	mux.HandleFunc("/activity", h.activityHandler) // TODO: rename either monitor or activity to be consistent
	mux.HandleFunc("/mode", h.modeHandler)         // TODO: move /pause to a sub context under group
	mux.HandleFunc("/reset", h.resetGroupHandler)
	mux.HandleFunc("/dhcp", h.dhcpHandler)
	mux.HandleFunc("/ipv6", h.ipv6Handler)

	return &http.Server{
		Addr:                         fmt.Sprintf(":%d", config.AppCfg.WebConfig.WebPort),
		Handler:                      mux,
		DisableGeneralOptionsHandler: false,
		TLSConfig:                    nil,
		ReadTimeout:                  30 * time.Second, // Maximum duration for reading the request body
		ReadHeaderTimeout:            5 * time.Second,  // Time to read headers before timing out
		WriteTimeout:                 30 * time.Second, // Maximum duration for writing the response
		IdleTimeout:                  30 * time.Second, // Maximum amount of time to keep idle connections alive
		MaxHeaderBytes:               1 << 20,          // Maximum size of request headers (1 MB)
	}
}

// Mock file modification time (for cache control)
func fileModTime() time.Time {
	t, err := time.Parse(time.RFC3339, config.BuildTime)
	if err != nil {
		// Fallback to the current time if parsing fails
		fmt.Println("Error parsing build time:", err)
		return time.Now()
	}
	return t
}

// formatDuration converts a time.Duration to a string showing days, hours, and minutes
func formatDuration(d time.Duration) string {
	days := d / (24 * time.Hour)
	d -= days * (24 * time.Hour)
	hours := d / time.Hour
	d -= hours * time.Hour
	minutes := d / time.Minute

	return fmt.Sprintf("%dd %02dh %02dm", days, hours, minutes)
}
