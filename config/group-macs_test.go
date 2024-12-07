package config

import (
	"os"
	"testing"
)

func TestLoadMACGroups(t *testing.T) {
	// Create a sample YAML file content
	yamlContent := `
groups:
  group1:
    - 00:11:22:33:44:55
    - 66:77:88:99:AA:BB
  group2:
    - CC:DD:EE:FF:00:11
    - 22:33:44:55:66:77
`

	// Create a temporary file to hold the YAML content
	tempFile, err := os.CreateTemp("", "test-mac-groups-*.yaml")
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
	macGroup, err := LoadGroupMACs(tempFile.Name())
	if err != nil {
		t.Fatalf("LoadGroupMACs returned an error: %v", err)
	}

	// Validate the result
	expectedGroups := map[string][]string{
		"group1": {"00:11:22:33:44:55", "66:77:88:99:AA:BB"},
		"group2": {"CC:DD:EE:FF:00:11", "22:33:44:55:66:77"},
	}

	if len(macGroup.Groups) != len(expectedGroups) {
		t.Fatalf("Expected %d groups, got %d", len(expectedGroups), len(macGroup.Groups))
	}

	for group, macs := range expectedGroups {
		parsedMacs, ok := macGroup.Groups[group]
		if !ok {
			t.Errorf("Group %s not found in parsed result", group)
			continue
		}
		if len(parsedMacs) != len(macs) {
			t.Errorf("Group %s: expected %d MACs, got %d", group, len(macs), len(parsedMacs))
			continue
		}
		for i, mac := range macs {
			if parsedMacs[i] != mac {
				t.Errorf("Group %s: expected MAC %s, got %s", group, mac, parsedMacs[i])
			}
		}
	}
}
