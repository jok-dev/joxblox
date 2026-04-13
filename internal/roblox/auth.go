package roblox

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
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

const (
	authKeyringService  = "joxblox"
	authKeyringUser     = "roblosecurity"
	openCloudAPIKeyUser = "roblox_opencloud_api_key"
)

func SaveRoblosecurityCookieToKeyring(rawValue string) error {
	normalizedValue := normalizeRoblosecurityCookie(rawValue)
	if normalizedValue == "" {
		return DeleteRoblosecurityCookieFromKeyring()
	}

	return keyring.Set(authKeyringService, authKeyringUser, normalizedValue)
}

func LoadRoblosecurityCookieFromKeyring() (string, error) {
	storedValue, err := keyring.Get(authKeyringService, authKeyringUser)
	if err == keyring.ErrNotFound {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	return normalizeRoblosecurityCookie(storedValue), nil
}

func DeleteRoblosecurityCookieFromKeyring() error {
	err := keyring.Delete(authKeyringService, authKeyringUser)
	if err == keyring.ErrNotFound {
		return nil
	}
	return err
}

func SaveOpenCloudAPIKeyToKeyring(rawValue string) error {
	normalizedValue := strings.TrimSpace(rawValue)
	if normalizedValue == "" {
		return DeleteOpenCloudAPIKeyFromKeyring()
	}

	return keyring.Set(authKeyringService, openCloudAPIKeyUser, normalizedValue)
}

func LoadOpenCloudAPIKeyFromKeyring() (string, error) {
	storedValue, err := keyring.Get(authKeyringService, openCloudAPIKeyUser)
	if err == keyring.ErrNotFound {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(storedValue), nil
}

func DeleteOpenCloudAPIKeyFromKeyring() error {
	err := keyring.Delete(authKeyringService, openCloudAPIKeyUser)
	if err == keyring.ErrNotFound {
		return nil
	}
	return err
}

const authValidationTimeout = 10 * time.Second

type authenticatedUserResponse struct {
	ID int64 `json:"id"`
}

func ValidateRoblosecurityCookie(rawValue string) error {
	normalizedValue := normalizeRoblosecurityCookie(rawValue)
	if normalizedValue == "" {
		return fmt.Errorf("cookie is empty")
	}

	cookieHeader := fmt.Sprintf(".ROBLOSECURITY=%s", normalizedValue)
	response, err := DoAuthenticatedUserGet(cookieHeader, authValidationTimeout)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return fmt.Errorf("invalid or expired .ROBLOSECURITY cookie")
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("auth check returned HTTP %d", response.StatusCode)
	}

	var userResponse authenticatedUserResponse
	if err := json.NewDecoder(response.Body).Decode(&userResponse); err != nil {
		return fmt.Errorf("invalid auth response")
	}
	if userResponse.ID <= 0 {
		return fmt.Errorf("auth check returned no user")
	}

	return nil
}

func ValidateCurrentAuthCookie() error {
	if !IsAuthenticationEnabled() {
		return nil
	}
	cookieHeader := GetRoblosecurityCookieHeader()
	if cookieHeader == "" {
		return nil
	}
	response, err := DoAuthenticatedUserGet(cookieHeader, authValidationTimeout)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return fmt.Errorf("your .ROBLOSECURITY cookie is expired or invalid — please update it in the Auth panel")
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("auth check returned HTTP %d", response.StatusCode)
	}

	var userResponse authenticatedUserResponse
	if err := json.NewDecoder(response.Body).Decode(&userResponse); err != nil {
		return fmt.Errorf("invalid auth response")
	}
	if userResponse.ID <= 0 {
		return fmt.Errorf("auth check returned no user")
	}

	return nil
}

func SanitizeAuthErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	errorMessage := strings.TrimSpace(err.Error())
	if errorMessage == "" {
		return "auth validation failed"
	}
	return errorMessage
}
