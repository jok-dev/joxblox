// Package webapp hosts the Wails (web-rendered) backend for joxblox. It exposes
// the existing Go core (asset loading, scanning, reporting, auth, …) to the
// React/TypeScript frontend through bound service structs.
package webapp

import (
	"context"

	"joxblox/internal/app/loader"
	"joxblox/internal/audio"
)

// App wires application lifecycle and owns the bound service structs.
type App struct {
	ctx context.Context

	Auth     *AuthService
	Settings *SettingsService
	Asset    *AssetService
	Scan     *ScanService
}

// NewApp constructs the app and its services.
func NewApp() *App {
	return &App{
		Auth:     NewAuthService(),
		Settings: NewSettingsService(),
		Asset:    NewAssetService(),
		Scan:     NewScanService(),
	}
}

// Startup is the Wails OnStartup hook; it captures the runtime context and
// initializes services. It is intentionally not bound to the frontend.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.Auth.startup(ctx)
	a.Asset.startup(ctx)
	a.Scan.startup(ctx)
	a.wireLoaderGlobals()
}

// Bindings returns the service structs exposed to the frontend. App itself is
// deliberately excluded so its lifecycle methods aren't surfaced as callable
// bindings.
func (a *App) Bindings() []interface{} {
	return []interface{}{
		a.Auth,
		a.Settings,
		a.Asset,
		a.Scan,
	}
}

// wireLoaderGlobals points the loader package's pluggable globals at the
// webapp's own implementations, replacing the wiring the Fyne app.Run did:
// the asset download cache plus the audio-metadata hooks, which now resolve to
// the Fyne-free internal/audio package so audio assets report a duration.
func (a *App) wireLoaderGlobals() {
	loader.LoadCacheSettings = func() loader.CacheSettings {
		settings, err := a.Settings.GetSettings()
		if err != nil {
			return loader.CacheSettings{}
		}
		return loader.CacheSettings{Enabled: settings.CacheEnabled, Folder: settings.CacheFolder}
	}
	loader.IsAudioContent = audio.IsAudioContent
	loader.ExtractAudio = func(fileName, contentType string, data []byte) (*loader.AudioMetadata, error) {
		meta, err := audio.ExtractAudioMetadata(fileName, contentType, data)
		if err != nil {
			return nil, err
		}
		return &loader.AudioMetadata{Duration: meta.Duration, Format: meta.Format}, nil
	}
}
