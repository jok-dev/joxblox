// Package diff implements the Diff tab — a side-by-side comparison of
// two .rbxl/.rbxm files. It wraps the Rust `rbxl-id-extractor diff`
// subcommand and renders the added / removed / changed instance buckets
// in a filterable indented list with a property-detail pane on the
// right.
package diff

import (
	"fmt"
	"path/filepath"
	"strings"

	"joxblox/internal/app/common"
	"joxblox/internal/clipboard"
	"joxblox/internal/debug"
	"joxblox/internal/extractor"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// NewDiffTab is the package entry point — returns the canvas object the
// app embeds in its tab container.
func NewDiffTab(window fyne.Window) fyne.CanvasObject {
	pathALabel := widget.NewLabel("File A: (none)")
	pathALabel.Truncation = fyne.TextTruncateEllipsis
	pathBLabel := widget.NewLabel("File B: (none)")
	pathBLabel.Truncation = fyne.TextTruncateEllipsis

	var fileAPath, fileBPath string
	// session is the live Rust process holding both DOMs in memory.
	// Right-click copies talk to this session; a new Compare tears it
	// down and starts a fresh one. Holds nil until the first Compare
	// succeeds.
	var session *extractor.DiffSession
	var lastNameA, lastNameB string

	selectAButton := widget.NewButton("Select File A...", func() {
		common.PickRBXLBaselineSource(window, func(path string) {
			fileAPath = path
			pathALabel.SetText("File A: " + filepath.Base(path))
		}, func(err error) {
			dialog.ShowError(err, window)
		})
	})
	selectBButton := widget.NewButton("Select File B...", func() {
		common.PickRBXLTargetSource(window, func(path string) {
			fileBPath = path
			pathBLabel.SetText("File B: " + filepath.Base(path))
		}, func(err error) {
			dialog.ShowError(err, window)
		})
	})

	ignoreScriptsCheck := widget.NewCheck("Ignore script files", nil)
	ignoreScriptsCheck.SetChecked(true)

	summaryLabel := widget.NewLabel("No diff yet")
	summaryLabel.TextStyle = fyne.TextStyle{Bold: true}

	progressBar := widget.NewProgressBarInfinite()
	progressBar.Hide()

	propertyTable := NewDiffPropertyTable()
	rowList := NewDiffRowList(func(row DiffRow) {
		propertyTable.ShowRow(row)
	})
	filterBar := NewDiffFilterBar(func(state DiffFilterState) {
		rowList.SetFilter(state)
	})

	compareButton := widget.NewButton("Compare", nil)
	compareButton.OnTapped = func() {
		if strings.TrimSpace(fileAPath) == "" || strings.TrimSpace(fileBPath) == "" {
			dialog.ShowInformation("Diff", "Select both File A and File B first.", window)
			return
		}
		fileA := fileAPath
		fileB := fileBPath
		ignoreScripts := ignoreScriptsCheck.Checked

		compareButton.Disable()
		progressBar.Show()
		summaryLabel.SetText("Diffing...")
		propertyTable.Clear()
		rowList.SetRows(nil)

		// Tear down the previous session before starting a new one —
		// the old Rust process holds GB of parsed DOMs we no longer
		// need, and stale references would be misleading.
		if session != nil {
			session.Close()
			session = nil
		}

		go func() {
			newSession, err := extractor.StartDiffSession(fileA, fileB, ignoreScripts)
			fyne.Do(func() {
				progressBar.Hide()
				compareButton.Enable()
				if err != nil {
					debug.Logf("Diff tab: extractor failed: %s", err.Error())
					summaryLabel.SetText("Diff failed.")
					dialog.ShowError(err, window)
					return
				}
				session = newSession
				lastNameA = filepath.Base(fileA)
				lastNameB = filepath.Base(fileB)
				propertyTable.SetFileNames(lastNameA, lastNameB)
				result := session.Result()
				summaryLabel.SetText(fmt.Sprintf(
					"+%d added · -%d removed · %d changed",
					len(result.Added), len(result.Removed), len(result.Changed),
				))
				rowList.SetRows(BuildDiffRows(result))
			})
		}()
	}

	// Right-click on any row pops a context menu. Diff entries
	// (Added/Removed/Changed) get one item per side that's actually
	// present; intermediate folder nodes — only in the tree because
	// some descendant differs — exist in both files so we offer both.
	// The copy is dispatched into the live diff session and the bytes
	// go straight onto the Windows clipboard under Roblox Studio's
	// custom format.
	rowList.SetOnSecondary(func(target DiffRowTarget, pos fyne.Position) {
		if session == nil {
			return
		}
		var items []*fyne.MenuItem
		if !target.HasRow {
			// Intermediate node — the path exists in both DOMs by
			// virtue of being an ancestor of differing descendants.
			items = append(items, copyMenuItem(window, session, "a", lastNameA, target.Path))
			items = append(items, copyMenuItem(window, session, "b", lastNameB, target.Path))
		} else {
			switch target.Row.Status {
			case DiffRowAdded:
				items = append(items, copyMenuItem(window, session, "b", lastNameB, target.Path))
			case DiffRowRemoved:
				items = append(items, copyMenuItem(window, session, "a", lastNameA, target.Path))
			case DiffRowChanged:
				items = append(items, copyMenuItem(window, session, "a", lastNameA, target.Path))
				items = append(items, copyMenuItem(window, session, "b", lastNameB, target.Path))
			}
		}
		if len(items) == 0 {
			return
		}
		menu := fyne.NewMenu("", items...)
		widget.ShowPopUpMenuAtPosition(menu, window.Canvas(), pos)
	})

	pickerA := container.NewBorder(nil, nil, selectAButton, nil, pathALabel)
	pickerB := container.NewBorder(nil, nil, selectBButton, nil, pathBLabel)
	pickerRow := container.NewGridWithColumns(2, pickerA, pickerB)

	topBar := container.NewVBox(
		pickerRow,
		container.NewBorder(nil, nil, ignoreScriptsCheck, compareButton, summaryLabel),
		progressBar,
		filterBar.Container,
	)

	split := container.NewHSplit(rowList.CanvasObject(), propertyTable.CanvasObject())
	split.Offset = 0.45

	return container.NewBorder(topBar, nil, nil, nil, split)
}

// copyMenuItem builds one "Copy from <basename>" menu entry that
// dispatches into the supplied diff session. The Rust subprocess +
// clipboard write run off the UI thread. Success is silent now that
// the session protocol makes copies near-instant; only failure pops
// a dialog.
func copyMenuItem(window fyne.Window, session *extractor.DiffSession, side, fileLabel, instancePath string) *fyne.MenuItem {
	return fyne.NewMenuItem("Copy from "+fileLabel, func() {
		go func() {
			payload, err := session.CopyInstance(side, instancePath)
			if err != nil {
				debug.Logf("Diff tab: copy failed: %s", err.Error())
				fyne.Do(func() { dialog.ShowError(err, window) })
				return
			}
			if err := clipboard.SetRobloxModel(payload); err != nil {
				debug.Logf("Diff tab: clipboard write failed: %s", err.Error())
				fyne.Do(func() { dialog.ShowError(err, window) })
				return
			}
			debug.Logf("Diff tab: copied %d bytes of %s to clipboard", len(payload), instancePath)
		}()
	})
}
