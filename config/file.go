package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

var (
	FnDefaultCreateAppHomeDirAndGetConfigFilePath = createAppHomeDirAndGetConfigFile
	FnDefaultSafeWriteViaTemp                     = SafeWriteViaTemp
	homeDirExists                                 = false
	configFileCreated                             = make(map[string]bool) // configFileCreated tracks whether the home directory for a given config file path has already been ensured.
	configFileCreatedMu                           sync.Mutex
)

// TODO: genericise the config file get/set functions for group-macs, group-domains and group-tracker-config
//   so that they can be used in the same way across the different packages.
//   note that group-domains may not really be used!

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

func SafeWriteViaTemp(filePath string, data string) error {
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

// GetConfig reads a configuration of any type T from a file. It ensures the home
// directory exists (by calling createHomeDirFunc), and if the file does not exist,
// it creates it using safeWriteFunc.
func GetConfig[T any](mu *sync.Mutex, configPath string, newInstance func() T) (T, error) {
	mu.Lock()
	defer mu.Unlock()

	// Ensure the home/app directory exists for this config file.
	configFileCreatedMu.Lock()
	if !configFileCreated[configPath] {
		var err error
		// createHomeDirFunc may update the config file path (e.g. by joining with the home dir).
		configPath, err = FnDefaultCreateAppHomeDirAndGetConfigFilePath(configPath)
		if err != nil {
			configFileCreatedMu.Unlock()
			var zero T
			return zero, fmt.Errorf("failed to create home directory: %w", err)
		}
		configFileCreated[configPath] = true // TODO: the config file isn't actually created here so link it to actual creation
	}
	configFileCreatedMu.Unlock()

	// Read the config file.
	data, err := os.ReadFile(configPath)
	if err != nil {
		// If the file doesn't exist, create an empty file.
		if os.IsNotExist(err) {
			if err := FnDefaultSafeWriteViaTemp(configPath, ""); err != nil {
				var zero T
				return zero, fmt.Errorf("failed to create config file: %w", err)
			}
			var zero T
			return zero, nil
		}
		var zero T
		return zero, fmt.Errorf("failed to read config file: %w", err)
	}

	// Unmarshal the file into our config struct.
	cfg := newInstance()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		var zero T
		return zero, fmt.Errorf("error unmarshalling config: %w", err)
	}
	return cfg, nil
}

// SetConfig validates, marshals, and writes a configuration of any type T to a file.
// In addition to ensuring the home directory exists, it calls a validate function (supplied
// by the caller) to check/adjust the configuration and a callback to update in‑memory state.
func SetConfig[T any](
	mu *sync.Mutex,
	configPath string,
	validate func(v T) error, // caller-supplied validation logic
	updateInMemory func(v T), // callback to update in-memory state
	configValue T,
) error {
	mu.Lock()
	defer mu.Unlock()

	// Ensure the home/app directory exists.
	configFileCreatedMu.Lock()
	if !configFileCreated[configPath] {
		var err error
		configPath, err = FnDefaultCreateAppHomeDirAndGetConfigFilePath(configPath)
		if err != nil {
			configFileCreatedMu.Unlock()
			return fmt.Errorf("failed to create home directory: %w", err)
		}
		configFileCreated[configPath] = true
	}
	configFileCreatedMu.Unlock()

	// Validate and adjust the configuration as needed.
	if validate != nil {
		if err := validate(configValue); err != nil {
			return err
		}
	}

	// Marshal the configuration.
	data, err := yaml.Marshal(configValue)
	if err != nil {
		return fmt.Errorf("error marshalling config: %w", err)
	}

	// Update in-memory state if needed.
	if updateInMemory != nil {
		updateInMemory(configValue)
	}

	// Write the new config to file safely.
	if err := FnDefaultSafeWriteViaTemp(configPath, string(data)); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}
