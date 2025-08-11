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
	c.DisableStacktrace = true // Disable stack traces

	if AppCfg.LogLevel == "debug" {
		c.Level = zap.NewAtomicLevelAt(zap.DebugLevel) // Set log level to DEBUG
	} else if AppCfg.LogLevel == "info" {
		c.Level = zap.NewAtomicLevelAt(zap.InfoLevel) // Set log level to INFO
	} else if AppCfg.LogLevel == "warn" {
		c.Level = zap.NewAtomicLevelAt(zap.WarnLevel) // Set log level to WARN
	} else if AppCfg.LogLevel == "error" {
		c.Level = zap.NewAtomicLevelAt(zap.ErrorLevel) // Set log level to ERROR
	} else if AppCfg.LogLevel == "dpanic" {
		c.Level = zap.NewAtomicLevelAt(zap.DPanicLevel) // Set log level to DPANIC
	} else if AppCfg.LogLevel == "panic" {
		c.Level = zap.NewAtomicLevelAt(zap.PanicLevel) // Set log level to PANIC
	} else if AppCfg.LogLevel == "fatal" {
		c.Level = zap.NewAtomicLevelAt(zap.FatalLevel) // Set log level to FATAL
	}

	logger, err := c.Build()
	if err != nil {
		fmt.Printf("Failed to create logger: %v", err)
		os.Exit(1)
	}

	defaultLogger = logger.Sugar()
	return defaultLogger
}
