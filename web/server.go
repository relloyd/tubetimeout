package web

import (
	"embed"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"
)

//go:embed static/* templates/*
var embeddedFiles embed.FS

type TemplateData struct {
	BuildTime      string
	BuildVersion      string
}

// UsageTracker returns info from the usage tracker.
type UsageTracker interface {
	GetGroupSummary() map[string]*models.GroupSummary
	SetGroupPause(id string, d time.Duration, mode models.UsageTrackerMode) error
	DeleteGroupPause(id string) error
	GetGroupPauseEndTime(id string) (time.Time, error)
	ResetGroup(id string)
}

type Monitor interface {
	GetTrafficLastActiveTimes() map[models.Group]map[models.MAC]time.Time
}

type DeviceGroupGetterSetter interface {
	GetAllGroupMACs(logger *zap.SugaredLogger) ([]config.FlatGroupMAC, error)
	SaveGroupMACs(logger *zap.SugaredLogger, flatGroupMACs []config.FlatGroupMAC) error
}

type Handler struct {
	logger       *zap.SugaredLogger
	usage        UsageTracker
	deviceGroups DeviceGroupGetterSetter
	monitor      Monitor
}

func NewServer(logger *zap.SugaredLogger, s UsageTracker, d DeviceGroupGetterSetter, m Monitor) *http.Server {
	h := Handler{logger: logger, usage: s, deviceGroups: d, monitor: m}
	mux := http.NewServeMux()
	mux.HandleFunc("/", h.rootHandler)
	mux.HandleFunc("/static/", h.staticHandler)
	mux.HandleFunc("/groups", h.groupMACHandler)
	mux.HandleFunc("/configure", h.groupUsage)
	mux.HandleFunc("/usage", h.groupUsageHandler)
	mux.HandleFunc("/activity", h.activityHandler)
	mux.HandleFunc("/block", h.pauseGroupHandler)
	mux.HandleFunc("/allow", h.pauseGroupHandler)
	mux.HandleFunc("/resume", h.resumeGroupHandler)
	mux.HandleFunc("/reset", h.resetGroupHandler)

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
