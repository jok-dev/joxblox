package app

import (
	"strings"

	"github.com/zalando/go-keyring"
)

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
