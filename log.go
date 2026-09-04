package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type logLevel int

const (
	logQuiet logLevel = iota
	logError
	logWarn
	logInfo
	logDebug
)

func parseLogLevel(s string) (logLevel, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "quiet", "none", "off":
		return logQuiet, nil
	case "error":
		return logError, nil
	case "warn", "warning":
		return logWarn, nil
	case "info", "":
		return logInfo, nil
	case "debug", "verbose", "trace":
		return logDebug, nil
	}
	return logInfo, fmt.Errorf("unknown log level %q (use quiet|error|warn|info|debug)", s)
}

var (
	logMu    sync.Mutex
	curLevel = logInfo
	logStamp = false
)

func setLogLevel(l logLevel) {
	logMu.Lock()
	curLevel = l
	logMu.Unlock()
}

func logAt(l logLevel, tag, format string, args ...interface{}) {
	logMu.Lock()
	defer logMu.Unlock()
	if l > curLevel {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if logStamp {
		fmt.Fprintf(os.Stderr, "%s [%s] %s\n", time.Now().Format("15:04:05.000"), tag, msg)
	} else {
		fmt.Fprintf(os.Stderr, "[%s] %s\n", tag, msg)
	}
}

func logErrorf(format string, args ...interface{}) { logAt(logError, "error", format, args...) }
func logWarnf(format string, args ...interface{})  { logAt(logWarn, "warn", format, args...) }
func logInfof(format string, args ...interface{})  { logAt(logInfo, "info", format, args...) }
func logDebugf(format string, args ...interface{}) { logAt(logDebug, "debug", format, args...) }

func errf(format string, args ...interface{}) error {
	return fmt.Errorf(format, args...)
}
