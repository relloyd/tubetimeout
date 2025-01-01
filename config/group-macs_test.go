package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"relloyd/tubetimeout/models"
)

func setupConfig(t *testing.T) {
	// Create a sample YAML file content
	yamlContent := `
groups:
  group1:
  - mac: "00:11:22:33:44:55"
    name: "my-device"
  - mac: "66:77:88:99:AA:BB"
    name: ""
  group2:
  - mac: "CC:DD:EE:FF:00:11"
    name: ""
  - mac: "22:33:44:55:66:77"
    name: ""
`
	// Create a temporary file to hold the YAML content
	tempFile, err := os.CreateTemp("", "test-mac-groups-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(tempFile.Name())
	})

	// Write the YAML content to the file
	if _, err := tempFile.WriteString(yamlContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tempFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	// Hack functions so the temp file is returned to GetConfig().
	defaultGroupMacFilePath = tempFile.Name()                                                          // override the default file path with temp file above.
	DefaultCreateAppHomeDirAndGetConfigFilePathFunc = func(f string) (string, error) { return f, nil } // override the function that uses the home dir for config files.
}

func TestGetGroupMACs(t *testing.T) {
	setupConfig(t)

	// Call the function under test.
	gm, err := GroupMACs.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig returned an error: %v", err)
	}

	// Validate the result.
	expectedGroups := map[models.Group][]models.NamedMAC{
		"group1": {{MAC: "00:11:22:33:44:55", Name: "my-device"}, {MAC: "66:77:88:99:AA:BB", Name: ""}},
		"group2": {{MAC: "CC:DD:EE:FF:00:11", Name: ""}, {MAC: "22:33:44:55:66:77", Name: ""}},
	}

	if len(gm.Groups) != len(expectedGroups) {
		t.Fatalf("Expected %d groups, got %d", len(expectedGroups), len(gm.Groups))
	}

	for group, namedMacs := range expectedGroups {
		parsedMacs, ok := gm.Groups[group]
		if !ok {
			t.Errorf("Group %q not found in parsed result", group)
			continue
		}
		if len(parsedMacs) != len(namedMacs) {
			t.Errorf("Group %q: expected %d MACs, got %d", group, len(namedMacs), len(parsedMacs))
			continue
		}
		for i, v := range namedMacs {
			if parsedMacs[i].MAC != v.MAC {
				t.Errorf("Group %q: expected MAC %q, got %q", group, v.MAC, parsedMacs[i])
			}
			if parsedMacs[i].Name != v.Name {
				t.Errorf("Group %q: expected Name %q, got %q", group, v.Name, parsedMacs[i].Name)
			}
		}
	}
}

// Test cases
func TestGetAllGroupMACs(t *testing.T) {
	setupConfig(t)

	// Define a mock ARP command that returns a fixed output
	ARPCmd = func() (string, error) {
		return `
? (192.168.1.10) at 00:11:22:33:44:55
? (192.168.1.11) at 66:77:88:99:AA:BB
? (192.168.1.12) at CC:DD:EE:FF:00:11
? (192.168.1.12) at CC:DD:EE:FF:00:22
`, nil
	}

	// Call the function under test.
	allGroupMACs, err := GroupMACs.GetAllGroupMACs(MustGetLogger())
	// Validate the result.
	assert.NoError(t, err, "GetAllGroupMACs returned an error")
	// Expect 5 MACs in the result: 4 from the fake config file and 1 extra from the ARP scan.
	assert.Equal(t, 5, len(allGroupMACs), "Number of MACs in the result")
}
