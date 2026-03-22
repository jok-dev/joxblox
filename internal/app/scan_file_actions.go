package app

import "fyne.io/fyne/v2"

type scanTabFileActions struct {
	ContextKey     string
	SaveJSON       func()
	LoadJSON       func()
	HandleDrop     func([]fyne.URI)
	ExportMarkdown func()
	RecentFiles    func() []string
	LoadRecent     func(string)
	GetResults     func() []scanResult
	SetResults     func([]scanResult)
	AddRecentFile  func(string)
}

type scanTabFileActionsProvider func() *scanTabFileActions
