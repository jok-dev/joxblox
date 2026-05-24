package app

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// navItemDef describes a single navigable tool in the sidebar.
type navItemDef struct {
	key     string
	title   string
	content fyne.CanvasObject
}

// navGroupDef is a labeled section of the sidebar (e.g. "Core Analytics").
type navGroupDef struct {
	title string
	items []navItemDef
}

// navigator renders a grouped left sidebar and swaps the active tool content
// in a single content area. It replaces the flat AppTabs bar while preserving
// the simple "select by key" semantics the rest of the app relies on.
type navigator struct {
	contentArea *fyne.Container
	root        fyne.CanvasObject
	buttons     map[string]*widget.Button
	views       map[string]fyne.CanvasObject
	current     string
}

func newNavigator(groups []navGroupDef) *navigator {
	nav := &navigator{
		contentArea: container.NewStack(),
		buttons:     map[string]*widget.Button{},
		views:       map[string]fyne.CanvasObject{},
	}

	sidebarItems := []fyne.CanvasObject{}
	for groupIndex, group := range groups {
		if groupIndex > 0 {
			sidebarItems = append(sidebarItems, widget.NewSeparator())
		}
		header := widget.NewLabelWithStyle(strings.ToUpper(group.title), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		sidebarItems = append(sidebarItems, header)
		for _, item := range group.items {
			itemKey := item.key
			nav.views[itemKey] = item.content
			button := widget.NewButton(item.title, func() {
				nav.Select(itemKey)
			})
			button.Alignment = widget.ButtonAlignLeading
			nav.buttons[itemKey] = button
			sidebarItems = append(sidebarItems, button)
		}
	}

	sidebar := container.NewVScroll(container.NewVBox(sidebarItems...))
	sidebar.SetMinSize(fyne.NewSize(180, 0))
	nav.root = container.NewBorder(
		nil,
		nil,
		container.NewBorder(nil, nil, nil, widget.NewSeparator(), sidebar),
		nil,
		nav.contentArea,
	)
	return nav
}

// Select shows the content for the given key and highlights its sidebar button.
func (n *navigator) Select(key string) {
	view, ok := n.views[key]
	if !ok {
		return
	}
	n.contentArea.Objects = []fyne.CanvasObject{view}
	n.contentArea.Refresh()
	for buttonKey, button := range n.buttons {
		if buttonKey == key {
			button.Importance = widget.HighImportance
		} else {
			button.Importance = widget.MediumImportance
		}
		button.Refresh()
	}
	n.current = key
}

// Root returns the sidebar + content layout to place in the window.
func (n *navigator) Root() fyne.CanvasObject {
	return n.root
}
