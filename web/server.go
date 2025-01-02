package web

import (
	"embed"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
	"relloyd/tubetimeout/config"
)

//go:embed static/* templates/*
var embeddedFiles embed.FS

type TemplateData struct {
	BuildTime      string
	UsagePeriod    string
	UsageThreshold string
	UsageMinutes   int
	UsageNextReset time.Time
	UsagePercent   int
	PausedUntil    string
}

// UsageTracker returns info from the usage tracker.
type UsageTracker interface {
	GetSampleSummary() map[string]int
	CalculateWindow(now time.Time) (time.Time, time.Time)
	RemovePause()
	SetPause(d time.Duration)
	GetPauseEndTime() time.Time
}

type DeviceGroupGetterSetter interface {
	GetAllGroupMACs(logger *zap.SugaredLogger) ([]config.FlatGroupMAC, error)
	SaveGroupMACs(logger *zap.SugaredLogger, flatGroupMACs []config.FlatGroupMAC) error
}

type Handler struct {
	logger *zap.SugaredLogger
	usage  UsageTracker
	deviceGroups DeviceGroupGetterSetter
}

func NewServer(logger *zap.SugaredLogger, s UsageTracker, d DeviceGroupGetterSetter) *http.Server {
	h := Handler{logger: logger, usage: s, deviceGroups: d}
	mux := http.NewServeMux()
	mux.HandleFunc("/", h.rootHandler)
	mux.HandleFunc("/static/", h.staticHandler)
	mux.HandleFunc("/pause", h.pauseHandler)
	mux.HandleFunc("/reset", h.resetHandler)
	mux.HandleFunc("/groupMACs", h.groupMACHandler)

	return &http.Server{
		Addr:                         ":8081",
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
