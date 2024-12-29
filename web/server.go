package web

import (
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func NewServer() *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", getHandler())

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

func getHandler() func(w http.ResponseWriter, r *http.Request) {
	staticDir := "dist"

	// Create a file server to serve static files
	fs := http.FileServer(http.Dir(staticDir))

	// Create a custom handler to handle Vue.js SPA fallback
	return func(w http.ResponseWriter, r *http.Request) {
		// Only allow GET requests for static files
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		
		path := filepath.Join(staticDir, r.URL.Path)
		// Check if the requested file exists
		if _, err := os.Stat(path); os.IsNotExist(err) || r.URL.Path == "/" || !isFile(path) {
			http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
			return
		}
		fs.ServeHTTP(w, r)
	}
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
