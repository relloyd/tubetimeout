package group

import (
	"os"
	"slices"
	"testing"

	"example.com/youtube-nfqueue/models"
)

func TestScanNetwork(t *testing.T) {
	// Create a sample YAML file content
	yamlContent := `
groups:
  group1:
    - 00:11:22:33:44:55
    - 66:77:88:99:AA:BB
  group2:
    - CC:DD:EE:FF:00:11
`

	// Define a mock ARP command that returns a fixed output
	mockARPCommand := func() (string, error) {
		return `
? (192.168.1.10) at 00:11:22:33:44:55
? (192.168.1.11) at 66:77:88:99:AA:BB
? (192.168.1.12) at CC:DD:EE:FF:00:11
`, nil
	}

	// Create a temporary file to hold the YAML content
	tempFile, err := os.CreateTemp("", "test_mac_groups_*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func(name string) {
		_ = os.Remove(name)
	}(tempFile.Name()) // Clean up the temp file after the test

	// Write the YAML content to the file
	if _, err := tempFile.WriteString(yamlContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tempFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	// Call the function under test
	mig := ScanNetwork(tempFile.Name(), mockARPCommand)

	// Validate the result
	expectedMap := map[models.IP]models.Groups{
		"192.168.1.10": {"group1"},
		"192.168.1.11": {"group1"},
		"192.168.1.12": {"group2"},
	}

	if len(mig) != len(expectedMap) {
		t.Fatalf("Expected %d entries, got %d", len(expectedMap), len(mig))
	}

	for ip, expectedGroups := range expectedMap {
		groups, exists := mig[ip]
		if !exists {
			t.Errorf("IP %s not found in result", ip)
			continue
		}
		if !slices.Equal(groups, expectedGroups) {
			t.Errorf("IP %s: expected %v, got %v", ip, expectedGroups, groups)
		}
	}
}
