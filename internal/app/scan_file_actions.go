package app

type scanTabFileActions struct {
	SaveJSON       func()
	LoadJSON       func()
	ExportMarkdown func()
	RecentFiles    func() []string
	LoadRecent     func(string)
}

type scanTabFileActionsProvider func() *scanTabFileActions
