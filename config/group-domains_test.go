package config

import (
	"os"
	"slices"
	"testing"

	"example.com/tubetimeout/models"
)

// TestLoadGroupDomains tests the LoadGroupDomains function
func TestLoadGroupDomains(t *testing.T) {
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
