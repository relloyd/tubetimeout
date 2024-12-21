package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

func CreateAppHomeDirForConfigFile(fileName string) (string, error) {
	// Get the home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	// Construct the app-specific directory path
	appDir := filepath.Join(homeDir, AppHomeDir)

	// Ensure the directory exists
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create app directory: %v", err)
	}

	// Construct the full path for the file
	filePath := filepath.Join(appDir, fileName)
	return filePath, nil
}


func safeWriteViaTemp(filePath string, data string) {
	tempPath := filePath + ".tmp"

	// Create a temporary file
	file, err := os.Create(tempPath)
	if err != nil {
		log.Fatalf("Failed to create temp file: %v", err)
	}
	defer file.Close()

	_, err = file.WriteString(data)
	if err != nil {
		log.Fatalf("Failed to write data: %v", err)
	}

	// Flush data to disk
	err = file.Sync()
	if err != nil {
		log.Fatalf("Failed to sync temp file: %v", err)
	}

	// Rename temporary file to target file
	err = os.Rename(tempPath, filePath)
	if err != nil {
		log.Fatalf("Failed to rename file: %v", err)
	}
}

