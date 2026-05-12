package diff

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// DiffFilterState captures the user's current filtering choices. The row
// list re-renders whenever any field changes.
type DiffFilterState struct {
	Query        string
	ShowAdded    bool
	ShowRemoved  bool
	ShowChanged  bool
}

// HasSearch reports whether a non-empty text query is active. Used by
// the row list to decide whether to auto-expand matching branches.
func (s DiffFilterState) HasSearch() bool {
	return strings.TrimSpace(s.Query) != ""
}

// Matches reports whether a row with the given status and identity fields
// passes the active filter.
func (s DiffFilterState) Matches(status DiffRowStatus, path, class, name string) bool {
	switch status {
	case DiffRowAdded:
		if !s.ShowAdded {
			return false
		}
	case DiffRowRemoved:
		if !s.ShowRemoved {
			return false
		}
	case DiffRowChanged:
		if !s.ShowChanged {
			return false
		}
	}
	query := strings.TrimSpace(strings.ToLower(s.Query))
	if query == "" {
		return true
	}
	if strings.Contains(strings.ToLower(path), query) {
		return true
	}
	if strings.Contains(strings.ToLower(class), query) {
		return true
	}
	if strings.Contains(strings.ToLower(name), query) {
		return true
	}
	return false
}

// DiffFilterBar is the search entry plus the three status toggles. It
// owns the state struct and calls onChange whenever any control changes.
type DiffFilterBar struct {
	State     DiffFilterState
	Container fyne.CanvasObject
}

// NewDiffFilterBar builds the filter UI. onChange is invoked on every
// edit so the parent tab can re-render its row list.
func NewDiffFilterBar(onChange func(DiffFilterState)) *DiffFilterBar {
	bar := &DiffFilterBar{
		State: DiffFilterState{
			Query:       "",
			ShowAdded:   true,
			ShowRemoved: true,
			ShowChanged: true,
		},
	}

	notify := func() {
		if onChange != nil {
			onChange(bar.State)
		}
	}

	searchEntry := widget.NewEntry()
	searchEntry.SetPlaceHolder("Filter by path / class / name...")
	searchEntry.OnChanged = func(text string) {
		bar.State.Query = text
		notify()
	}

	addedCheck := widget.NewCheck("Added", func(checked bool) {
		bar.State.ShowAdded = checked
		notify()
	})
	addedCheck.SetChecked(true)

	removedCheck := widget.NewCheck("Removed", func(checked bool) {
		bar.State.ShowRemoved = checked
		notify()
	})
	removedCheck.SetChecked(true)

	changedCheck := widget.NewCheck("Changed", func(checked bool) {
		bar.State.ShowChanged = checked
		notify()
	})
	changedCheck.SetChecked(true)

	bar.Container = container.NewBorder(
		nil,
		nil,
		nil,
		container.NewHBox(addedCheck, removedCheck, changedCheck),
		searchEntry,
	)
	return bar
}
