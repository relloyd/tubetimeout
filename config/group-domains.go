package config

import (
	"bufio"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	"relloyd/tubetimeout/models"
)

var (
	defaultGroupDomainsFilePath            = "group-domains.yaml"
	defaultYouTubeGroupName                = models.Group("youtube")
	defaultGroupDomains                    = models.MapGroupDomains{defaultYouTubeGroupName: {"www.youtube.com", "youtube.com", "googlevideo.com", "youtu.be"}}
	groupDomainsFileUpdated                = false
	youtubeDomainsURL                      = "https://raw.githubusercontent.com/nickspaargaren/no-google/master/categories/youtubeparsed"
	youtubeDomainsFile                     = "youtube-domains.txt"
	httpClient                  HTTPClient = &http.Client{} // Default HTTP client, can be replaced for testing
)

// HTTPClient interface for mocking
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Embed the local file into the binary at compile time
//
//go:embed youtube-domains.txt
var embeddedFile embed.FS

// GroupDomainsConfig represents the YAML structure
type GroupDomainsConfig struct {
	GroupDomains models.MapGroupDomains `yaml:"groups"` // group: [domain1, domain2, ...]
}

// LoadGroupDomains parses the default group domains YAML file and returns the map of group domains.
// Superseded by FetchYouTubeDomains for now to fetch the latest list of YouTube domains.
func LoadGroupDomains() (models.MapGroupDomains, error) {
	if !groupMACsFileUpdated { // if we should update the file path with the app home dir...
		var err error
		defaultGroupDomainsFilePath, err = FnDefaultCreateAppHomeDirAndGetConfigFilePath(defaultGroupDomainsFilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to create home directory for group-domains config file: %w", err)
		} else {
			groupDomainsFileUpdated = true
		}
	}

	_, err := os.Stat(defaultGroupDomainsFilePath)
	if err != nil && errors.Is(err, fs.ErrNotExist) {
		return defaultGroupDomains, nil
	}

	yamlFile, err := os.ReadFile(defaultGroupDomainsFilePath)
	if err != nil {
		return models.MapGroupDomains{}, fmt.Errorf("error reading YAML file: %w", err)
	}

	var groupDomains GroupDomainsConfig
	err = yaml.Unmarshal(yamlFile, &groupDomains)
	if err != nil {
		return models.MapGroupDomains{}, fmt.Errorf("error unmarshalling YAML: %w", err)
	}

	return groupDomains.GroupDomains, nil
}

// FetchYouTubeDomains retrieves the list of domains from the specified URL.
func FetchYouTubeDomains(logger *zap.SugaredLogger) (models.MapGroupDomains, error) {
	// Create HTTP request
	req, err := http.NewRequest(http.MethodGet, youtubeDomainsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Perform HTTP request using the package-level client
	resp, err := httpClient.Do(req)
	if err != nil {
		logger.Errorf("Failed to fetch domains from URL: %v. Falling back to embedded file.", err)
		return fetchDomainsFromEmbeddedFile()
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	return parseDomains(resp.Body, defaultYouTubeGroupName)
}

// fetchDomainsFromEmbeddedFile reads the embedded file contents
func fetchDomainsFromEmbeddedFile() (models.MapGroupDomains, error) {
	file, err := embeddedFile.Open(youtubeDomainsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open embedded file: %w", err)
	}
	defer func(file fs.File) {
		_ = file.Close()
	}(file)

	return parseDomains(file, defaultYouTubeGroupName)
}

// parseDomains parses domain names from an io.Reader source
// TODO: consider rejecting invalid domains/urls
func parseDomains(reader io.Reader, groupName models.Group) (models.MapGroupDomains, error) {
	var domains []models.Domain
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") || strings.Contains(line, " ") {
			continue
		}
		domains = append(domains, models.Domain(line))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading domains: %w", err)
	}
	return models.MapGroupDomains{groupName: domains}, nil
}
