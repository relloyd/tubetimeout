package config

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"example.com/tubetimeout/models"
	"gopkg.in/yaml.v3"
)

var (
	defaultGroupDomainsFilePath = "group-domains.yaml"
	defaultYouTubeGroupName     = models.Group("youtube")
	defaultGroupDomains         = models.MapGroupDomains{defaultYouTubeGroupName: {"www.youtube.com", "youtube.com", "googlevideo.com", "youtu.be"}}
	groupDomainsFileUpdated     = false
	youtubeDomainsURL           = "https://raw.githubusercontent.com/nickspaargaren/no-google/master/categories/youtubeparsed"
)

// GroupDomainsConfig represents the YAML structure
type GroupDomainsConfig struct {
	GroupDomains models.MapGroupDomains `yaml:"groups"` // group: [domain1, domain2, ...]
}

// LoadGroupDomains parses the YAML file
func LoadGroupDomains() (models.MapGroupDomains, error) {
	if !groupMACsFileUpdated { // if we should update the file path with the app home dir...
		var err error
		defaultGroupDomainsFilePath, err = DefaultCreateAppHomeDirAndGetConfigFilePathFunc(defaultGroupDomainsFilePath)
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
// TODO: test FetchYouTubeDomains()
// TODO: use a local copy of the domains file if we can't fetch it and log an error.
func FetchYouTubeDomains() (models.MapGroupDomains, error) {
	resp, err := http.Get(youtubeDomainsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch domains: %w", err)
	}
	defer resp.Body.Close()

	var domains []models.Domain
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		domains = append(domains, models.Domain(line))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading domains: %w", err)
	}
	return models.MapGroupDomains{defaultYouTubeGroupName: domains}, nil
}
