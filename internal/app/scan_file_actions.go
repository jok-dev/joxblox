package app

import (
	"joxblox/internal/app/loader"

	"fyne.io/fyne/v2"
)

type scanTabFileActions struct {
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
}

type scanTabFileActionsProvider func() *scanTabFileActions
