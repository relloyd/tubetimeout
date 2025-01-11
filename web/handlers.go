package web

import (
	"encoding/json"
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
	if r.Method == http.MethodGet {
		gm, err := h.deviceGroups.GetAllGroupMACs(h.logger)
		if err != nil {
			h.logger.Errorf("Error getting device group data: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(gm)
		if err != nil {
			h.logger.Errorf("Error encoding device group response: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	if r.Method == http.MethodPost {
		// Handle POST request
		var flatGroupMACs []config.FlatGroupMAC
		if err := json.NewDecoder(r.Body).Decode(&flatGroupMACs); err != nil {
			h.logger.Errorf("Invalid request device group payload: %v", err)
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}
		err := h.deviceGroups.SaveGroupMACs(h.logger, flatGroupMACs)
		if err != nil {
			h.logger.Errorf("Error saving device group data: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Respond with success
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "configuration saved successfully"})
		return
	}

	http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
}

func (h *Handler) usageSummaryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	summary := h.usage.GetSampleSummary()
	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(summary)
	if err != nil {
		h.logger.Errorf("Error encoding sample summary response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
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
	h.usage.DeletePause()

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Reset successful"))
}
