package webapp

import (
	"context"
	"fmt"
	"strings"

	"joxblox/internal/roblox"
)

// AuthStatus is the serializable auth state surfaced to the header indicator.
type AuthStatus struct {
	Enabled bool   `json:"enabled"`
	Saved   bool   `json:"saved"`
	Message string `json:"message"`
}

// AuthService exposes the .ROBLOSECURITY cookie lifecycle to the frontend,
// reusing the Fyne-free keyring/validation helpers in internal/roblox.
type AuthService struct {
	ctx    context.Context
	cookie string
	saved  bool
}

func NewAuthService() *AuthService { return &AuthService{} }

func (s *AuthService) startup(ctx context.Context) {
	s.ctx = ctx
	stored, err := roblox.LoadRoblosecurityCookieFromKeyring()
	if err == nil && strings.TrimSpace(stored) != "" {
		roblox.SetRoblosecurityCookie(stored)
		s.cookie = stored
		s.saved = true
	}
}

func (s *AuthService) status(message string) AuthStatus {
	return AuthStatus{
		Enabled: roblox.IsAuthenticationEnabled(),
		Saved:   s.saved,
		Message: message,
	}
}

// GetStatus reports the current auth state.
func (s *AuthService) GetStatus() AuthStatus {
	return s.status("")
}

// GetCookie returns the in-memory cookie so the Settings form can prefill it.
func (s *AuthService) GetCookie() string {
	return s.cookie
}

// ApplyCookie validates and applies a cookie, optionally persisting it to the
// OS keychain. An empty cookie clears auth.
func (s *AuthService) ApplyCookie(raw string, remember bool) (AuthStatus, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		roblox.ClearRoblosecurityCookie()
		_ = roblox.DeleteRoblosecurityCookieFromKeyring()
		s.cookie = ""
		s.saved = false
		return s.status(""), nil
	}

	if err := roblox.ValidateRoblosecurityCookie(raw); err != nil {
		roblox.ClearRoblosecurityCookie()
		s.saved = false
		return s.status("Auth failed"), fmt.Errorf("%s", roblox.SanitizeAuthErrorMessage(err))
	}

	roblox.SetRoblosecurityCookie(raw)
	s.cookie = raw
	s.saved = false
	if remember {
		if err := roblox.SaveRoblosecurityCookieToKeyring(raw); err != nil {
			return s.status("Enabled (keychain save failed)"), fmt.Errorf("keychain save failed: %s", err.Error())
		}
		s.saved = true
	} else {
		_ = roblox.DeleteRoblosecurityCookieFromKeyring()
	}
	return s.status(""), nil
}

// ClearCookie removes the in-memory cookie and any saved keychain credential.
func (s *AuthService) ClearCookie() AuthStatus {
	roblox.ClearRoblosecurityCookie()
	_ = roblox.DeleteRoblosecurityCookieFromKeyring()
	s.cookie = ""
	s.saved = false
	return s.status("")
}
