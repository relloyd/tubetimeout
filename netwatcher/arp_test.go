package netwatcher

import (
	"os"
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
	defer os.Remove(tempFile.Name()) // Clean up the temp file after the test

	// Write the YAML content to the file
	if _, err := tempFile.WriteString(yamlContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tempFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	// Call the function under test
	ipMap := ScanNetwork(tempFile.Name(), mockARPCommand)

	// Validate the result
	expectedMap := map[models.IP]models.MACGroup{
		"192.168.1.10": {MAC: "00:11:22:33:44:55", Group: "group1"},
		"192.168.1.11": {MAC: "66:77:88:99:AA:BB", Group: "group1"},
		"192.168.1.12": {MAC: "CC:DD:EE:FF:00:11", Group: "group2"},
	}

	if len(ipMap) != len(expectedMap) {
		t.Fatalf("Expected %d entries, got %d", len(expectedMap), len(ipMap))
	}

	for ip, expectedMapping := range expectedMap {
		mapping, exists := ipMap[ip]
		if !exists {
			t.Errorf("IP %s not found in result", ip)
			continue
		}
		if mapping != expectedMapping {
			t.Errorf("IP %s: expected %v, got %v", ip, expectedMapping, mapping)
		}
	}
}
