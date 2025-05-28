package applogger

import (
	"github.com/whatap/golib/logger"
	"github.com/whatap/golib/logger/logfile"
)

var appLogger logger.Logger = &logger.EmptyLogger{}

// SetLogger sets the application logger
func SetLogger(logger logger.Logger) {
	appLogger = logger
}

// GetLogger returns the application logger
func GetLogger() logger.Logger {
	return appLogger
}

// NewFileLogger creates a new file logger and sets it as the application logger
func NewFileLogger(opts ...logfile.FileLoggerOption) *logfile.FileLogger {
	logger := logfile.NewFileLogger(opts...)
	SetLogger(logger)
	return logger
}