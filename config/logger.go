package config

import (
	"fmt"
	"os"

	"go.uber.org/zap"
)

var defaultLogger *zap.SugaredLogger

func MustGetLogger() *zap.SugaredLogger {
	if defaultLogger != nil {
		return defaultLogger
	}

	c := zap.NewDevelopmentConfig()
	c.Level = zap.NewAtomicLevelAt(zap.InfoLevel) // Set log level to INFO
	c.DisableStacktrace = true                    // Disable stack traces

	logger, err := c.Build()
	if err != nil {
		fmt.Printf("Failed to create logger: %v", err)
		os.Exit(1)
	}

	defaultLogger = logger.Sugar()
	return defaultLogger
}
