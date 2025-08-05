package led

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func createTestLEDConfig(t *testing.T, basePath string, config Config) {
	t.Helper()

	ledPath := filepath.Join(basePath, config.Name)
	require.NoError(t, os.MkdirAll(ledPath, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(ledPath, "trigger"), []byte(""), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(ledPath, "brightness"), []byte(""), 0644))
}

func readFileContent(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(content)
}

func TestEnableWarning(t *testing.T) {
	tmpDir := t.TempDir()

	// Override sysfsPath for the test
	originalSysfsPath := sysfsPath
	sysfsPath = tmpDir
	defer func() { sysfsPath = originalSysfsPath }()

	cfg := Config{
		Name:              "test-led",
		EnableTrigger:     "heartbeat",
		EnableBrightness:  "1",
		DisableTrigger:    "none",
		DisableBrightness: "0",
	}
	createTestLEDConfig(t, tmpDir, cfg)
	knownLEDs = []Config{cfg}

	logger := zaptest.NewLogger(t).Sugar()
	ctrl := NewController(logger)

	require.True(t, ctrl.exists)
	ctrl.EnableWarning()

	triggerPath := filepath.Join(tmpDir, cfg.Name, "trigger")
	brightnessPath := filepath.Join(tmpDir, cfg.Name, "brightness")

	require.Equal(t, "heartbeat", readFileContent(t, triggerPath))
	require.Equal(t, "1", readFileContent(t, brightnessPath))
}

func TestDisableWarning(t *testing.T) {
	tmpDir := t.TempDir()

	// Override sysfsPath for the test
	originalSysfsPath := sysfsPath
	sysfsPath = tmpDir
	defer func() { sysfsPath = originalSysfsPath }()

	cfg := Config{
		Name:              "test-led",
		EnableTrigger:     "heartbeat",
		EnableBrightness:  "1",
		DisableTrigger:    "none",
		DisableBrightness: "0",
	}
	createTestLEDConfig(t, tmpDir, cfg)
	knownLEDs = []Config{cfg}

	logger := zaptest.NewLogger(t).Sugar()
	ctrl := NewController(logger)

	require.True(t, ctrl.exists)
	ctrl.DisableWarning()

	triggerPath := filepath.Join(tmpDir, cfg.Name, "trigger")
	brightnessPath := filepath.Join(tmpDir, cfg.Name, "brightness")

	require.Equal(t, "none", readFileContent(t, triggerPath))
	require.Equal(t, "0", readFileContent(t, brightnessPath))
}
