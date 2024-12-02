package config

import (
	"log"
	"os"
)

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

