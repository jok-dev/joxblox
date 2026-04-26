package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// debugLogState writes timestamped log lines to latest.log in the repo root.
// The on-screen debug console was removed; this is now strictly a file sink.
type debugLogState struct {
	mutex       sync.RWMutex
	logFilePath string
}

var debugLog = &debugLogState{}

// LogDebugf is wired into internal/debug.Logf at startup so any caller using
// debug.Logf flows through here and into latest.log.
func LogDebugf(format string, args ...any) {
	messageText := fmt.Sprintf(format, args...)
	debugLog.appendLine(messageText)
}

func (state *debugLogState) appendLine(messageText string) {
	timestampedLine := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), strings.TrimSpace(messageText))
	state.mutex.RLock()
	logFilePath := state.logFilePath
	state.mutex.RUnlock()
	if strings.TrimSpace(logFilePath) != "" {
		appendLineToLogFile(logFilePath, timestampedLine)
	}
}

func InitializeDebugLogFile() {
	if GetRepositoryRootPath == nil {
		return
	}
	repositoryRootPath, rootErr := GetRepositoryRootPath()
	if rootErr != nil {
		return
	}
	logFilePath := filepath.Join(repositoryRootPath, "latest.log")
	_ = os.WriteFile(logFilePath, []byte(""), 0644)
	debugLog.mutex.Lock()
	debugLog.logFilePath = logFilePath
	debugLog.mutex.Unlock()
}

func appendLineToLogFile(logFilePath string, line string) {
	logFileHandle, openErr := os.OpenFile(logFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if openErr != nil {
		return
	}
	defer logFileHandle.Close()
	_, _ = logFileHandle.WriteString(line + "\n")
}
