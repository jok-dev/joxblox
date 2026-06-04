package webapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Settings holds the non-auth app preferences. Persistence is a plain JSON file
// under the OS config dir, replacing the Fyne preferences store the legacy UI
// used (which is unavailable in the Wails app).
type Settings struct {
	CacheEnabled        bool    `json:"cacheEnabled"`
	CacheFolder         string  `json:"cacheFolder"`
	StudioPath          string  `json:"studioPath"`
	MeshLookSensitivity float64 `json:"meshLookSensitivity"`
}

const defaultMeshLookSensitivity = 0.3

// SettingsService exposes load/save of app settings to the frontend.
type SettingsService struct {
	mu sync.Mutex
}

func NewSettingsService() *SettingsService { return &SettingsService{} }

func settingsFilePath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "joxblox")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// GetSettings returns the persisted settings, or sensible defaults if none.
func (s *SettingsService) GetSettings() (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	settings := Settings{MeshLookSensitivity: defaultMeshLookSensitivity}
	path, err := settingsFilePath()
	if err != nil {
		return settings, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return settings, nil
		}
		return settings, err
	}
	_ = json.Unmarshal(data, &settings)
	if settings.MeshLookSensitivity <= 0 {
		settings.MeshLookSensitivity = defaultMeshLookSensitivity
	}
	return settings, nil
}

// SaveSettings writes the given settings to disk.
func (s *SettingsService) SaveSettings(next Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := settingsFilePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
