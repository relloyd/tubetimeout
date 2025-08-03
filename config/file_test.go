package config

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCreateAppHomeDirForConfigFile tests the createAppHomeDirAndGetConfigFile function.
func TestCreateAppHomeDirForConfigFile(t *testing.T) {
	// Setup
	AppHomeDir = ".myapp"
	fileName := "config.yaml"

	// Get the current user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home directory: %v", err)
	}

	// Expected output
	expectedDir := filepath.Join(homeDir, AppHomeDir)
	expectedPath := filepath.Join(expectedDir, fileName)

	// Cleanup after test
	defer func() {
		_ = os.RemoveAll(expectedDir) // Clean up the test directory
	}()

	// Call the function under test
	result, err := createAppHomeDirAndGetConfigFile(fileName)

	assert.NoError(t, err, "Unexpected error")
	assert.Equal(t, expectedPath, result, "Unexpected path")

	// Check if directory was created
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Errorf("Expected directory %s to exist, but it does not", expectedDir)
	}
}

func TestSafeWriteViaTemp(t *testing.T) {
	// Setup: Define the file paths and test data
	testFilePath := "test_example.txt"
	tempFilePath := testFilePath + ".tmp"
	testData := "data for testing"

	// Cleanup: Ensure no leftover files from previous tests
	defer func() {
		_ = os.Remove(testFilePath)
		_ = os.Remove(tempFilePath)
	}()

	// Run the function
	FnDefaultSafeWriteViaTemp(testFilePath, testData)

	// Verify the original file exists
	if _, err := os.Stat(testFilePath); os.IsNotExist(err) {
		t.Fatalf("Expected file %s to exist, but it does not", testFilePath)
	}

	// Verify the temporary file does not exist
	if _, err := os.Stat(tempFilePath); err == nil {
		t.Fatalf("Temporary file %s should have been renamed but still exists", tempFilePath)
	}

	// Read and verify the file contents
	content, err := ioutil.ReadFile(testFilePath)
	if err != nil {
		t.Fatalf("Failed to read the file %s: %v", testFilePath, err)
	}

	if string(content) != testData {
		t.Fatalf("Expected file contents '%s', got '%s'", testData, string(content))
	}
}
