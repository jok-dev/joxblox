package main

import (
	"embed"

	"joxblox/internal/webapp"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

// main is the Wails (web-rendered) entry point for joxblox. The legacy Fyne
// entry at cmd/joxblox is being retired as the UI migrates to this app.
func main() {
	app := webapp.NewApp()

	err := wails.Run(&options.App{
		Title:     "joxblox",
		Width:     1280,
		Height:    800,
		MinWidth:  900,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 24, G: 24, B: 27, A: 1},
		OnStartup:        app.Startup,
		OnShutdown:       app.Shutdown,
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
		},
		Bind: app.Bindings(),
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
