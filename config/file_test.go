package config

import (
	"io/ioutil"
	"os"
	"testing"
)

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
	safeWriteViaTemp(testFilePath, testData)

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
