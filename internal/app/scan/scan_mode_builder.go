package scan

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type scanModeTabConfig struct {
	Window        fyne.Window
	SingleLabel   string
	DiffLabel     string
	SingleContext string
	DiffContext   string
	SingleOptions assetScanTabOptions
	DiffOptions   assetScanTabOptions
}

func buildScanModeTab(cfg scanModeTabConfig) (fyne.CanvasObject, ScanTabFileActionsProvider, []ScanTabFileActionsProvider, func(string)) {
	singleScan, singleActions := newAssetScanTab(cfg.Window, cfg.SingleOptions)
	diffScan, diffActions := newAssetScanTab(cfg.Window, cfg.DiffOptions)

	modeLabel := widget.NewLabel("Mode:")
	modeSwitch := widget.NewRadioGroup([]string{cfg.SingleLabel, cfg.DiffLabel}, nil)
	modeSwitch.Horizontal = true
	modeSwitch.SetSelected(cfg.SingleLabel)
	contentStack := container.NewStack(singleScan, diffScan)
	contentStack.Objects[1].Hide()
	currentActions := singleActions
	singleProvider := func() *ScanTabFileActions { return singleActions }
	diffProvider := func() *ScanTabFileActions { return diffActions }
	modeSwitch.OnChanged = func(selectedMode string) {
		if strings.EqualFold(selectedMode, cfg.DiffLabel) {
			contentStack.Objects[0].Hide()
			contentStack.Objects[1].Show()
			currentActions = diffActions
			contentStack.Refresh()
			return
		}
		contentStack.Objects[1].Hide()
		contentStack.Objects[0].Show()
		currentActions = singleActions
		contentStack.Refresh()
	}
	selectContext := func(contextKey string) {
		switch strings.TrimSpace(contextKey) {
		case cfg.DiffContext:
			modeSwitch.SetSelected(cfg.DiffLabel)
		default:
			modeSwitch.SetSelected(cfg.SingleLabel)
		}
	}
	content := container.NewBorder(
		container.NewHBox(modeLabel, modeSwitch),
		nil,
		nil,
		nil,
		contentStack,
	)
	return content, func() *ScanTabFileActions {
		return currentActions
	}, []ScanTabFileActionsProvider{singleProvider, diffProvider}, selectContext
}
