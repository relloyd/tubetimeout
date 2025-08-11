package config

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"relloyd/tubetimeout/models"
)

// TestLoadGroupDomains tests the LoadGroupDomains function
func TestLoadGroupDomains(t *testing.T) {
	// Override the default home dir to just return the tmp dir so that LoadGroupDomains doesn't try the app home dir.
	FnDefaultCreateAppHomeDirAndGetConfigFilePath = func(f string) (string, error) { return f, nil }

	// Define test cases
	tests := []struct {
		name        string
		yamlContent string
		expected    models.MapGroupDomains
		expectError bool
	}{
		{
			name: "Valid YAML file",
			yamlContent: `
groups:
  group1:
    - domain1.com
    - domain2.com
  group2:
    - domain3.com
  `,
			expected: models.MapGroupDomains{
				"group1": {"domain1.com", "domain2.com"},
				"group2": {"domain3.com"},
			},
			expectError: false,
		},
		{
			name:        "Invalid YAML file",
			yamlContent: `invalid YAML content`,
			expected:    models.MapGroupDomains{},
			expectError: true,
		},
		{
			name:        "Empty YAML file",
			yamlContent: ``,
			expected:    models.MapGroupDomains{},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary file
			tmpFile, err := os.CreateTemp("", "test-*.yaml")
			if err != nil {
				t.Fatalf("Failed to create temporary file: %v", err)
			}
			defer func(name string) {
				_ = os.Remove(name)
			}(tmpFile.Name()) // Clean up after test

			// Write the test YAML content to the file
			if _, err := tmpFile.Write([]byte(tt.yamlContent)); err != nil {
				t.Fatalf("Failed to write to temporary file: %v", err)
			}
			_ = tmpFile.Close()

			// Call the function under test
			defaultGroupDomainsFilePath = tmpFile.Name()
			result, err := LoadGroupDomains()

			// Check for expected errors
			if (err != nil) != tt.expectError {
				t.Errorf("Expected error: %v, got: %v", tt.expectError, err)
			}

			// Check for expected result
			if err == nil && !equalGroupDomains(result, tt.expected) {
				t.Errorf("Expected result: %+v, got: %+v", tt.expected, result)
			}
		})
	}
}

// Helper function to compare two GroupDomainsConfig structs
func equalGroupDomains(a, b models.MapGroupDomains) bool {
	if len(a) != len(b) {
		return false
	}

	for key, val := range a {
		if bVal, exists := b[key]; !exists || !slices.Equal(val, bVal) {
			return false
		}
	}
	return true
}

// Mock HTTP client for testing
type mockHTTPClient struct {
	responseBody string
	statusCode   int
	err          error
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &http.Response{
		StatusCode: m.statusCode,
		Body:       io.NopCloser(bytes.NewBufferString(m.responseBody)),
	}, nil
}

// Test for successful fetch from remote URL
func TestFetchYouTubeDomains_RemoteSuccess(t *testing.T) {
	// Override httpClient with mock client
	httpClient = &mockHTTPClient{
		responseBody: "youtube.com\ngooglevideo.com\n# comment\n\nytimg.com",
		statusCode:   http.StatusOK,
	}

	got, err := FetchYouTubeDomains(MustGetLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := models.MapGroupDomains{
		defaultYouTubeGroupName: []models.Domain{"youtube.com", "googlevideo.com", "ytimg.com"},
	}
	if !compareDomains(got, expected) {
		t.Errorf("expected %v, got %v", expected, got)
	}
}

// Test for fallback to embedded file
func TestFetchYouTubeDomains_FallbackToEmbedded(t *testing.T) {
	// Override httpClient with mock client simulating a network error
	httpClient = &mockHTTPClient{
		err: errors.New("network error"),
	}

	got, err := FetchYouTubeDomains(MustGetLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Ensure fallback uses the embedded file
	expected, err := fetchDomainsFromEmbeddedFile()
	assert.NoError(t, err, "fetchDomainsFromEmbeddedFile() error = %v", err)
	if !compareDomains(got, expected) {
		t.Errorf("expected fallback domains %v, got %v", expected, got)
	}
}

// Test parsing invalid domains
func TestParseDomains_InvalidData(t *testing.T) {
	data := "valid.com\n# comment\ninvalid domain\nanother-valid.com\n"
	reader := bytes.NewBufferString(data)

	got, err := parseDomains(reader, defaultYouTubeGroupName)
	assert.NoError(t, err, "parseDomains() error = %v", err)

	expected := models.MapGroupDomains{
		defaultYouTubeGroupName: []models.Domain{"valid.com", "another-valid.com"},
	}
	if !compareDomains(got, expected) {
		t.Errorf("expected %v, got %v", expected, got)
	}
}

// Helper to compare domain maps
func compareDomains(got, expected models.MapGroupDomains) bool {
	if len(got) != len(expected) {
		return false
	}
	for key, gotDomains := range got {
		expectedDomains, ok := expected[key]
		if !ok || len(gotDomains) != len(expectedDomains) {
			return false
		}
		for i := range gotDomains {
			if gotDomains[i] != expectedDomains[i] {
				return false
			}
		}
	}
	return true
}
