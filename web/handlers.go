package web

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"relloyd/tubetimeout/config"
)

func (h *Handler) rootHandler(w http.ResponseWriter, r *http.Request) {
	// Parse the HTML template from the embedded file system
	tmpl, err := template.ParseFS(embeddedFiles, "templates/index.html")
	if err != nil {
		http.Error(w, "Error loading template", http.StatusInternalServerError)
		return
	}

	// Gather data to use in the template.
	_, nextResetTime := h.usage.CalculateWindow(time.Now())
	usageMinutes := h.usage.GetSampleSummary()["youtube"]
	usagePercent := int(float64(usageMinutes) / float64(config.AppCfg.TrackerConfig.Threshold.Minutes()) * 100)
	if usagePercent > 100 {
		usagePercent = 100
	}
	pe := h.usage.GetPauseEndTime()
	pausedUntil := pe.Format(time.RFC1123) // RFC1123 = "Mon, 02 Jan 2006 15:04:05 MST"
	if pe.IsZero() {
		pausedUntil = "-"
	}

	// Execute the template with config data.
	td := TemplateData{
		BuildTime:      config.BuildTime,
		UsagePeriod:    formatDuration(config.AppCfg.TrackerConfig.Retention),
		UsageNextReset: nextResetTime,
		UsageThreshold: formatDuration(config.AppCfg.TrackerConfig.Threshold),
		UsageMinutes:   usageMinutes,
		UsagePercent:   usagePercent,
		PausedUntil:    pausedUntil,
	}

	tmpl.Option("missingkey=default") // TODO: fix the error when keys are missing.
	err = tmpl.Execute(w, td)
	if err != nil {
		http.Error(w, "Error rendering template", http.StatusInternalServerError)
	}
}

// groupMACHandler
func (h *Handler) groupMACHandler(w http.ResponseWriter, r *http.Request) {

}

// File server rootHandler for static files
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

// pauseHandler is an API endpoint for /pause
func (h *Handler) pauseHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Parse duration parameter
	minutes := r.FormValue("minutes")
	duration, err := strconv.Atoi(minutes)
	if err != nil || duration <= 0 {
		http.Error(w, "Invalid duration", http.StatusBadRequest)
		return
	}

	// Add a pause to the usage tracker.
	h.logger.Info("Usage tracker paused for %d minutes\n", duration)
	h.usage.SetPause(time.Duration(duration) * time.Minute)

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(fmt.Sprintf("Paused for %d minutes", duration)))
}

// resetHandler is an API endpoint for /reset
func (h *Handler) resetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Reset the usage tracker pause timer.
	h.logger.Info("Pause timer reset triggered")
	h.usage.RemovePause()

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Reset successful"))
}