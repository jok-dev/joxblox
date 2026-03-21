package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	robloxAuthenticatedUserURL = "https://users.roblox.com/v1/users/authenticated"
	authValidationTimeout      = 10 * time.Second
)

type authenticatedUserResponse struct {
	ID int64 `json:"id"`
}

func validateRoblosecurityCookie(rawValue string) error {
	normalizedValue := normalizeRoblosecurityCookie(rawValue)
	if normalizedValue == "" {
		return fmt.Errorf("cookie is empty")
	}

	httpClient := &http.Client{Timeout: authValidationTimeout}
	request, err := http.NewRequest(http.MethodGet, robloxAuthenticatedUserURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Cookie", fmt.Sprintf(".ROBLOSECURITY=%s", normalizedValue))

	response, err := httpClient.Do(request)
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

func sanitizeAuthErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	errorMessage := strings.TrimSpace(err.Error())
	if errorMessage == "" {
		return "auth validation failed"
	}
	return errorMessage
}
