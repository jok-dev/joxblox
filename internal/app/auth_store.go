package app

import (
	"github.com/zalando/go-keyring"
)

const (
	authKeyringService = "roblox-asset-explorer"
	authKeyringUser    = "roblosecurity"
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
