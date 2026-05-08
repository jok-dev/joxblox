package scan

import (
	"joxblox/internal/app/loader"

	"fyne.io/fyne/v2"
)

type ScanTabFileActions struct {
	ContextKey    string
	SaveJSON      func()
	LoadJSON      func()
	HandleDrop    func([]fyne.URI)
	LoadSource    func(string)
	RecentFiles   func() []string
	LoadRecent    func(string)
	GetResults    func() []loader.ScanResult
	SetResults    func([]loader.ScanResult)
	AddRecentFile func(string)
	// SetSourcePath sets the displayed source path without triggering a
	// scan. Used by the report → scan handoff so the file is shown as
	// the source for the imported rows.
	SetSourcePath              func(string)
	SetPathFilter              func(enabled bool, text string)
	SetLargeTextureThreshold   func(threshold float64)
	SetBannedTextureSizeMB     func(limitMB float64)
	// GenerateTagHTMLReport opens a save dialog and writes a self-contained
	// HTML page that groups every tagged result under its tag heading. Nil
	// when no rows have been tagged yet.
	GenerateTagHTMLReport func()
}

type ScanTabFileActionsProvider func() *ScanTabFileActions
