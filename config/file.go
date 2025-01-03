package config

import (
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
)

var (
	DefaultCreateAppHomeDirAndGetConfigFilePathFunc = getConfigFileFunc(createAppHomeDirAndGetConfigFile)
	homeDirExists                                   = false
)

type getConfigFileFunc func(string) (string, error)

// createAppHomeDirAndGetConfigFile creates a directory in the user's home directory for the app's configuration file.
// It returns the full path to the configuration file.
func createAppHomeDirAndGetConfigFile(fileName string) (string, error) {
	// Get the home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	// Construct the app-specific directory path
	appDir := filepath.Join(homeDir, AppHomeDir)

	// Ensure the directory exists
	if !homeDirExists {
		if err := os.MkdirAll(appDir, 0755); err != nil {
			return "", fmt.Errorf("failed to create app directory: %v", err)
		}
	}
	homeDirExists = true

	// Construct the full path for the file
	filePath := filepath.Join(appDir, fileName)
	return filePath, nil
}

func SafeWriteViaTemp(logger *zap.SugaredLogger, filePath string, data string) error {
	tempPath := filePath + ".tmp"

	// Create a temporary file.
	file, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}
	defer file.Close()

	_, err = file.WriteString(data)
	if err != nil {
		return fmt.Errorf("failed to write data: %v", err)
	}

	// Flush data to disk.
	err = file.Sync()
	if err != nil {
		return fmt.Errorf("failed to sync temp file: %v", err)
	}

	// Rename temporary file to target file
	err = os.Rename(tempPath, filePath)
	if err != nil {
		return fmt.Errorf("failed to rename file: %v", err)
	}

	return nil
}
