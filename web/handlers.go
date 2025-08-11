package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/dhcp"
	"relloyd/tubetimeout/models"
)

func (h *Handler) rootHandler(w http.ResponseWriter, r *http.Request) {
	// Parse the HTML template from the embedded file system
	tmpl, err := template.ParseFS(embeddedFiles, "templates/index.html")
	if err != nil {
		http.Error(w, "Error loading template", http.StatusInternalServerError)
		return
	}

	// Execute the template with config data.
	td := TemplateData{
		BuildTime:    config.BuildTime,
		BuildVersion: config.BuildVersion,
		StartTime:    h.startTime.Format(time.RFC822),
	}

	tmpl.Option("missingkey=default") // TODO: fix the error when keys are missing.
	err = tmpl.Execute(w, td)
	if err != nil {
		http.Error(w, "Error rendering template", http.StatusInternalServerError)
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

// groupMACHandler
func (h *Handler) groupMACHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		gm, err := h.groupMACsGetterSetter.GetAllGroupMACs(h.logger)
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
		err := h.groupMACsGetterSetter.SaveGroupMACs(h.logger, flatGroupMACs)
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

func (h *Handler) activityHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	if r.Method == http.MethodGet {
		lastActiveTimes := h.monitor.GetTrafficLastActiveTimes() //  map[models.Group]map[models.MAC]time.Time, where the string is the group

		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(lastActiveTimes)
		if err != nil {
			h.logger.Errorf("Error encoding monitor response: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}
}

func (h *Handler) usageHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: test the methods for usageHandler as we borked them before!
	if r.Method == http.MethodGet {
		summary := h.usageTracker.GetSummary()                   // map[string]models.TrackerSummary, where string is the device ID, which is a group
		lastActiveTimes := h.monitor.GetTrafficLastActiveTimes() //  map[models.Group]map[models.MAC]time.Time, where the string is the group

		for group, v := range lastActiveTimes {
			s, ok := summary[string(group)]
			if ok { // if the group exists in the usage data...
				s.LastActiveTimes = v // save the MAC last active time map.
			} else {
				h.logger.Errorf("monitor: group %v not found with last active data: %v", group, v)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(summary)
		if err != nil {
			h.logger.Errorf("Error encoding sample summary response: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	} else if r.Method == http.MethodDelete {
		deviceID := r.URL.Query().Get("deviceID")
		if deviceID != "" {
			h.usageTracker.Reset(deviceID)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "samples reset for deviceID"})
		}
		return
	} else {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
}

func (h *Handler) trackerConfigHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		gtc, err := h.usageTracker.GetConfig()
		if err != nil {
			h.logger.Errorf("Failed to get tracker config: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Flatten the tracker config.
		flatConfig := make([]models.FlatTrackerConfig, 0) // make empty slice so we marshall at least something below
		for k, v := range gtc {
			flatConfig = append(flatConfig, models.FlatTrackerConfig{
				Group:         k,
				Retention:     v.Retention,
				Threshold:     v.Threshold,
				StartDayInt:   v.StartDayInt,
				StartDuration: v.StartDuration,
				Mode:          v.Mode,
				ModeEndTime:   v.ModeEndTime,
			})
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(&flatConfig)
	} else if r.Method == http.MethodPost {
		// Save Usage Tracker Config.
		var flatConfig []models.FlatTrackerConfig
		if err := json.NewDecoder(r.Body).Decode(&flatConfig); err != nil {
			h.logger.Errorf("Failed to unmarshall tracker config: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}

		// Convert flat config to map.
		gtc := make(models.MapGroupTrackerConfig)
		for _, v := range flatConfig {
			if v.Group == "" {
				continue
			}
			gtc[v.Group] = &models.TrackerConfig{
				Retention:     v.Retention,
				Threshold:     v.Threshold,
				StartDayInt:   v.StartDayInt,
				StartDuration: v.StartDuration,
				Mode:          v.Mode,
				ModeEndTime:   v.ModeEndTime,
			}
		}

		// Save the config.
		err := h.usageTracker.SetConfig(gtc)
		if err != nil {
			h.logger.Errorf("Failed to set tracker config: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	} else {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
}

// modeHandler is an API endpoint for /pause where the usage tracker can be set into a mode or resumed.
func (h *Handler) modeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet { // GET will fetch the mode end time for the given group...
		// Parse the group ID.
		group := r.URL.Query().Get("group")
		if group == "" {
			h.logger.Errorf("Error fetching mode timer for group: empty group supplied")
			http.Error(w, "Invalid group", http.StatusBadRequest)
			return
		}

		modeData, err := h.usageTracker.GetModeEndTime(group)
		h.logger.Infof("Fetched mode data for group %v", group)
		if err != nil && errors.Is(err, models.ErrGroupNotFound) {
			h.logger.Errorf("Error fetching mode timer for group %v: %v", group, err)
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(err.Error()))
			return
		} else if err != nil {
			h.logger.Errorf("Error getting group mode end time: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Respond.
		w.WriteHeader(http.StatusOK)
		err = json.NewEncoder(w).Encode(&modeData)
		if err != nil {
			h.logger.Errorf("Error getting group mode data: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	} else if r.Method == http.MethodPut { // PUT will pause the group tracker...
		err := r.ParseForm()
		if err != nil {
			h.logger.Errorf("Error parsing mode form: %v", err)
			http.Error(w, "Unable to parse form", http.StatusBadRequest)
			return
		}
		// Parse the group ID.

		group := r.FormValue("group")
		if group == "" {
			h.logger.Errorf("Error pausing group: empty group supplied")
			http.Error(w, "Invalid group", http.StatusBadRequest)
			return
		}

		// Parse duration parameter.
		minutes := r.FormValue("minutes")
		duration, err := strconv.Atoi(minutes)
		if err != nil || duration <= 0 {
			h.logger.Errorf("Error pausing group: invalid duration: %v", err)
			http.Error(w, "Invalid duration", http.StatusBadRequest)
			return
		}

		// Parse the mode.
		strMode := r.FormValue("mode")
		intMode, err := strconv.Atoi(strMode)
		if err != nil || intMode < 0 || intMode > 2 {
			h.logger.Errorf("Error pausing group: invalid mode: %v", err)
			http.Error(w, "Invalid mode", http.StatusBadRequest)
			return
		}
		mode := models.UsageTrackerMode(intMode)

		// Log stuff.
		if mode == models.ModeAllow {
			strMode = fmt.Sprintf("paused for %d minutes", duration)
		} else if mode == models.ModeBlock {
			strMode = fmt.Sprintf("blocked for %d minutes", duration)
		} else {
			strMode = "resumed"
		}
		logMsg := fmt.Sprintf("Usage tracker for group %v %v", group, strMode)
		h.logger.Infof(logMsg)

		// Set the pause/allow/block.
		err = h.usageTracker.SetMode(group, time.Duration(duration)*time.Minute, models.UsageTrackerMode(intMode))
		if err != nil {
			h.logger.Errorf("Error setting block/allow timer: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Respond.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(logMsg))
	} else if r.Method == http.MethodDelete { // DELETE will resume the group tracker...
		// Parse the group ID.
		group := r.URL.Query().Get("group")
		if group == "" {
			h.logger.Errorf("Error resuming group: empty group supplied")
			http.Error(w, "Invalid group", http.StatusBadRequest)
			return
		}

		// Resume the usage tracker.
		err := h.usageTracker.SetMode(group, 0, models.ModeMonitor)
		if err != nil {
			h.logger.Errorf("Error resetting group block/allow timer: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		h.logger.Infof("Pause timer reset triggered for group %v", group)

		// Respond.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fmt.Sprintf("Group %v resumed successfully", group)))
	} else {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
}

func (h *Handler) resetGroupHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Parse the group ID.
	group := r.URL.Query().Get("group")
	if group == "" {
		h.logger.Errorf("Error resetting group usage: no group supplied")
		http.Error(w, "Invalid group", http.StatusBadRequest)
		return
	}

	// Reset the group sample data.
	h.usageTracker.Reset(group)
	h.logger.Infof("Reset usage for group: %v", group)

	// Respond.
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(fmt.Sprintf("Reset group %v successfully", group)))
}

func (h *Handler) dhcpHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// Handle GET request: Retrieve DHCP configuration
		dhcpConfig, err := h.dhcpConfigGetterSetter.GetConfig(h.logger)
		if err != nil {
			h.logger.Errorf("Error retrieving DHCP configuration: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Return DHCP configuration as JSON
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(dhcpConfig); err != nil {
			h.logger.Errorf("Error encoding DHCP configuration response: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	} else if r.Method == http.MethodPost {
		// Handle POST request: Update DHCP configuration
		var dhcpConfig dhcp.DNSMasqConfig
		if err := json.NewDecoder(r.Body).Decode(&dhcpConfig); err != nil {
			h.logger.Errorf("Failed to parse DHCP configuration payload: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest) // return the decoder error
			return
		}

		// Save DHCP configuration
		err := h.dhcpConfigGetterSetter.SetConfig(h.logger, &dhcpConfig)
		if err != nil {
			h.logger.Errorf("Error saving DHCP configuration: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Respond with success message
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message": "DHCP configuration updated successfully"}`))
	} else {
		// Invalid request method
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) ipv6Handler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		status := h.ipv6Checker.IsEnabled()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(status); err != nil {
			h.logger.Errorf("Error getting IPv6 status: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	} else {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
	}
}
