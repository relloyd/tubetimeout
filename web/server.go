package web

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"relloyd/tubetimeout/config"
)

//go:embed static/* templates/*
var embeddedFiles embed.FS

type TemplateData struct {
	Config       config.AppConfig
	BuildTime    string
	UsageMinutes int
	NextReset    time.Time
	UsagePct     int
}

// TrackerSummariserI returns info from the usage tracker.
type TrackerSummariserI interface {
	GetSampleSummary() map[string]int
	CalculateWindow(now time.Time) (time.Time, time.Time)
}

type Handler struct {
	Usage TrackerSummariserI
}

func NewServer(s TrackerSummariserI) *http.Server {
	h := Handler{Usage: s}
	mux := http.NewServeMux()
	mux.HandleFunc("/", h.handler)
	mux.HandleFunc("/static/", h.staticHandler)

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

func (h *Handler) handler(w http.ResponseWriter, r *http.Request) {
	// Parse the HTML template from the embedded file system
	tmpl, err := template.ParseFS(embeddedFiles, "templates/index.html")
	if err != nil {
		http.Error(w, "Error loading template", http.StatusInternalServerError)
		return
	}

	_, nextResetTime := h.Usage.CalculateWindow(time.Now())

	sampleSummary := h.Usage.GetSampleSummary()
	usageMinutes := sampleSummary["youtube"]

	usagePct := int(float64(usageMinutes) / float64(config.AppCfg.TrackerConfig.Threshold.Minutes()) * 100)
	if usagePct > 100 {
		usagePct = 100
	}

	td := TemplateData{
		Config:       config.AppCfg,
		BuildTime:    config.BuildTime,
		NextReset:    nextResetTime,
		UsageMinutes: sampleSummary["youtube"],
		UsagePct:     usagePct,
	}

	// Execute the template with appCfg data
	tmpl.Option("missingkey=default")
	err = tmpl.Execute(w, td)
	if err != nil {
		http.Error(w, "Error rendering template", http.StatusInternalServerError)
	}
}

// File server handler for static files
func (h *Handler) staticHandler(w http.ResponseWriter, r *http.Request) {
	// Extract the requested file path
	path := strings.TrimPrefix(r.URL.Path, "/static/")
	data, err := embeddedFiles.ReadFile("static/" + path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Serve the content with proper headers
	http.ServeContent(w, r, path, fileModTime(), strings.NewReader(string(data)))
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
