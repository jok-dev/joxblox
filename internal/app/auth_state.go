package app

import (
	"fmt"
	"strings"
	"sync"
)

var authState = struct {
	mutex         sync.RWMutex
	roblosecurity string
}{}

func SetRoblosecurityCookie(rawValue string) {
	normalizedValue := normalizeRoblosecurityCookie(rawValue)

	authState.mutex.Lock()
	authState.roblosecurity = normalizedValue
	authState.mutex.Unlock()
}

func ClearRoblosecurityCookie() {
	authState.mutex.Lock()
	authState.roblosecurity = ""
	authState.mutex.Unlock()
}

func IsAuthenticationEnabled() bool {
	authState.mutex.RLock()
	defer authState.mutex.RUnlock()
	return authState.roblosecurity != ""
}

func GetRoblosecurityCookieHeader() string {
	authState.mutex.RLock()
	defer authState.mutex.RUnlock()
	if authState.roblosecurity == "" {
		return ""
	}
	return fmt.Sprintf(".ROBLOSECURITY=%s", authState.roblosecurity)
}

func normalizeRoblosecurityCookie(rawValue string) string {
	trimmedValue := strings.TrimSpace(rawValue)
	if trimmedValue == "" {
		return ""
	}

	if strings.Contains(trimmedValue, ";") {
		trimmedValue = strings.Split(trimmedValue, ";")[0]
	}

	lowerValue := strings.ToLower(trimmedValue)
	if strings.HasPrefix(lowerValue, ".roblosecurity=") {
		parts := strings.SplitN(trimmedValue, "=", 2)
		if len(parts) == 2 {
			return strings.TrimSpace(parts[1])
		}
	}

	return trimmedValue
}
