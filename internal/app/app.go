package app

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	previewWidth  = 360
	previewHeight = 240
	megabyte      = 1024 * 1024
)

//go:embed app_icon.svg
var appIconSVG []byte

func Run() {
	guiApp := app.New()
	appIcon := fyne.NewStaticResource("app_icon.svg", appIconSVG)
	guiApp.SetIcon(appIcon)
	window := guiApp.NewWindow("Roblox Asset Explorer")
	window.SetIcon(appIcon)
	window.Resize(fyne.NewSize(1100, 700))

	singleAssetTab := container.NewTabItem("Single Asset", createSingleAssetTab(guiApp))
	folderScanTab := container.NewTabItem("Folder Scan", createFolderScanTab(window))
	tabs := container.NewAppTabs(singleAssetTab, folderScanTab)
	authPanel := createAuthPanel(window)
	window.SetContent(container.NewBorder(nil, authPanel, nil, nil, tabs))
	window.ShowAndRun()
}

func createAuthPanel(window fyne.Window) fyne.CanvasObject {
	statusLabel := widget.NewLabel("Auth: Disabled")
	statusDot := canvas.NewCircle(theme.Color(theme.ColorNameError))
	statusDotWrapper := container.NewCenter(container.NewGridWrap(fyne.NewSize(10, 10), statusDot))
	cookieEntry := widget.NewPasswordEntry()
	cookieEntry.SetPlaceHolder("Optional .ROBLOSECURITY cookie value")
	helpButton := widget.NewButton("?", func() {
		dialog.ShowInformation(
			".ROBLOSECURITY Help",
			"How to get it:\n"+
				"1) Sign in at https://www.roblox.com in your browser.\n"+
				"2) Open browser developer tools.\n"+
				"3) Go to Storage/Application -> Cookies -> .roblox.com.\n"+
				"4) Copy the .ROBLOSECURITY cookie value.\n"+
				"5) Paste it here and click Apply Auth.\n\n"+
				"Security note: This cookie grants account access. Treat it like a password and do not share it.",
			window,
		)
	})
	helpButton.Resize(fyne.NewSize(32, 32))

	applyButton := widget.NewButton("Apply Auth", func() {
		SetRoblosecurityCookie(cookieEntry.Text)
		if IsAuthenticationEnabled() {
			statusLabel.SetText("Auth: Enabled")
			statusDot.FillColor = theme.Color(theme.ColorNameSuccess)
		} else {
			statusLabel.SetText("Auth: Disabled")
			statusDot.FillColor = theme.Color(theme.ColorNameError)
		}
		statusDot.Refresh()
	})
	clearButton := widget.NewButton("Clear Auth", func() {
		ClearRoblosecurityCookie()
		cookieEntry.SetText("")
		statusLabel.SetText("Auth: Disabled")
		statusDot.FillColor = theme.Color(theme.ColorNameError)
		statusDot.Refresh()
	})

	labeledEntry := container.NewBorder(nil, nil, widget.NewLabel("Auth Cookie:"), nil, cookieEntry)
	leftControls := container.NewBorder(
		nil,
		nil,
		nil,
		container.NewHBox(helpButton, applyButton, clearButton),
		labeledEntry,
	)
	rightStatus := container.NewHBox(statusDotWrapper, statusLabel)
	footerRow := container.NewBorder(nil, nil, nil, rightStatus, leftControls)

	return container.NewVBox(
		widget.NewSeparator(),
		widget.NewCard("", "", footerRow),
	)
}

func createSingleAssetTab(guiApp fyne.App) fyne.CanvasObject {
	var currentImageInfo *imageInfo
	var currentAssetID int64

	assetInput := widget.NewEntry()
	assetInput.SetPlaceHolder("Paste Roblox asset ID (e.g. 138155379338302 or rbxassetid://138155379338302)")

	statusLabel := widget.NewLabel("Enter an asset ID and click Go.")
	assetIDValue := widget.NewLabel("-")
	dimensionsValue := widget.NewLabel("-")
	sizeValue := widget.NewLabel("-")
	formatValue := widget.NewLabel("-")
	contentTypeValue := widget.NewLabel("-")
	stateValue := widget.NewLabel("-")
	sourceValue := widget.NewLabel("-")
	failureReasonValue := widget.NewLabel("-")
	failureReasonValue.Wrapping = fyne.TextWrapWord
	assetDeliveryJSONValue := widget.NewMultiLineEntry()
	assetDeliveryJSONValue.SetText("-")
	assetDeliveryJSONValue.Disable()
	assetDeliveryJSONValue.SetMinRowsVisible(6)
	thumbnailJSONValue := widget.NewMultiLineEntry()
	thumbnailJSONValue.SetText("-")
	thumbnailJSONValue.Disable()
	thumbnailJSONValue.SetMinRowsVisible(6)
	jsonAccordion := widget.NewAccordion(
		widget.NewAccordionItem(
			"API JSON Responses",
			container.NewVBox(
				widget.NewLabel("AssetDelivery JSON:"),
				assetDeliveryJSONValue,
				widget.NewLabel("Thumbnail JSON:"),
				thumbnailJSONValue,
			),
		),
	)
	fallbackNoteLabel := widget.NewLabelWithStyle(
		"",
		fyne.TextAlignLeading,
		fyne.TextStyle{Italic: true},
	)
	fallbackNoteLabel.Importance = widget.DangerImportance
	fallbackNoteLabel.Wrapping = fyne.TextWrapWord
	fallbackNoteLabel.Hide()
	loadingSpinner := widget.NewProgressBarInfinite()
	loadingSpinner.Hide()

	previewImage := canvas.NewImageFromImage(nil)
	previewImage.FillMode = canvas.ImageFillContain
	previewImage.SetMinSize(fyne.NewSize(previewWidth, previewHeight))
	previewPlaceholder := widget.NewLabel("No image loaded")
	previewContainer := container.NewMax(
		container.NewCenter(previewPlaceholder),
		container.NewCenter(previewImage),
		container.NewVBox(loadingSpinner),
	)

	expandImageButton := widget.NewButtonWithIcon("", theme.ViewFullScreenIcon(), func() {
		if currentImageInfo == nil {
			return
		}

		imageWindow := guiApp.NewWindow(fmt.Sprintf("Asset %d", currentAssetID))
		imageCanvas := canvas.NewImageFromResource(currentImageInfo.Resource)
		imageCanvas.FillMode = canvas.ImageFillContain
		imageWindow.SetContent(container.NewPadded(imageCanvas))
		imageWindow.Resize(fyne.NewSize(900, 700))
		imageWindow.Show()
	})
	expandImageButton.Disable()
	expandImageButton.Resize(fyne.NewSize(36, 36))
	expandButtonRow := container.NewHBox(layout.NewSpacer(), expandImageButton)
	previewBox := container.NewBorder(nil, expandButtonRow, nil, nil, previewContainer)

	var goButton *widget.Button
	loadAsset := func() {
		if goButton != nil && goButton.Disabled() {
			return
		}

		assetID, err := parseAssetID(assetInput.Text)
		if err != nil {
			statusLabel.SetText(err.Error())
			return
		}

		statusLabel.SetText("Loading image...")
		goButton.Disable()
		loadingSpinner.Show()
		loadingSpinner.Start()
		previewPlaceholder.SetText("Loading image...")
		previewPlaceholder.Show()
		stateValue.SetText("-")
		stateValue.Importance = widget.MediumImportance
		stateValue.Refresh()
		sourceValue.SetText("-")
		sourceValue.Importance = widget.MediumImportance
		sourceValue.Refresh()
		failureReasonValue.SetText("-")
		assetDeliveryJSONValue.SetText("-")
		thumbnailJSONValue.SetText("-")
		fallbackNoteLabel.Hide()
		fallbackNoteLabel.SetText("")
		previewBox.Refresh()

		loadedImageInfo, sourceDescription, stateDescription, warningMessage, assetDeliveryRawJSON, thumbnailRawJSON, loadErr := loadBestImageInfo(assetID)
		if loadErr != nil {
			loadingSpinner.Stop()
			loadingSpinner.Hide()
			goButton.Enable()
			statusLabel.SetText(loadErr.Error())
			return
		}

		currentImageInfo = loadedImageInfo
		currentAssetID = assetID
		assetIDValue.SetText(strconv.FormatInt(assetID, 10))
		dimensionsValue.SetText(fmt.Sprintf("%dx%d", loadedImageInfo.Width, loadedImageInfo.Height))
		sizeValue.SetText(fmt.Sprintf("%.2f MB", float64(loadedImageInfo.BytesSize)/megabyte))
		formatValue.SetText(loadedImageInfo.Format)
		contentTypeValue.SetText(loadedImageInfo.ContentType)
		if warningMessage != "" {
			failureReasonValue.SetText(warningMessage)
		} else {
			failureReasonValue.SetText("-")
		}
		if assetDeliveryRawJSON != "" {
			assetDeliveryJSONValue.SetText(assetDeliveryRawJSON)
		}
		if thumbnailRawJSON != "" {
			thumbnailJSONValue.SetText(thumbnailRawJSON)
		}
		stateValue.SetText(stateDescription)
		stateValue.Importance = widget.MediumImportance
		sourceValue.SetText(sourceDescription)
		sourceValue.Importance = widget.MediumImportance
		isThumbnailFallback := strings.EqualFold(sourceDescription, "Thumbnails API (Fallback)")
		thumbnailStateNotCompleted := strings.EqualFold(sourceDescription, "Thumbnails API (Fallback)") &&
			!strings.EqualFold(stateDescription, "Completed")
		if isThumbnailFallback {
			sourceValue.SetText(fmt.Sprintf("⚠ %s", sourceDescription))
			sourceValue.Importance = widget.DangerImportance
		}
		if thumbnailStateNotCompleted {
			stateValue.SetText(fmt.Sprintf("⚠ %s", stateDescription))
			stateValue.Importance = widget.DangerImportance
		}
		if warningMessage != "" {
			fallbackNoteLabel.SetText(buildFallbackWarningText(warningMessage))
			fallbackNoteLabel.Show()
		}
		stateValue.Refresh()
		sourceValue.Refresh()
		previewImage.Resource = loadedImageInfo.Resource
		previewImage.Refresh()
		previewPlaceholder.Hide()
		loadingSpinner.Stop()
		loadingSpinner.Hide()
		expandImageButton.Enable()
		goButton.Enable()
		previewBox.Refresh()
		if strings.EqualFold(sourceDescription, "AssetDelivery (In-Game)") {
			statusLabel.SetText("Image loaded.")
		} else {
			statusLabel.SetText(fmt.Sprintf("Loaded fallback thumbnail (state: %s).", stateDescription))
		}
	}

	goButton = widget.NewButton("Go", loadAsset)
	assetInput.OnSubmitted = func(_ string) {
		loadAsset()
	}

	inputRow := container.NewBorder(nil, nil, nil, goButton, assetInput)
	infoGrid := container.New(layout.NewFormLayout(),
		widget.NewLabel("Asset ID:"), assetIDValue,
		widget.NewLabel("Dimensions:"), dimensionsValue,
		widget.NewLabel("Size:"), sizeValue,
		widget.NewLabel("Format:"), formatValue,
		widget.NewLabel("Content-Type:"), contentTypeValue,
		widget.NewLabel("State:"), stateValue,
		widget.NewLabel("Source:"), sourceValue,
		widget.NewLabel("Failure Reason:"), failureReasonValue,
	)

	tabContent := container.NewVBox(
		widget.NewLabel("Roblox Asset Image Preview"),
		inputRow,
		statusLabel,
		previewBox,
		infoGrid,
		jsonAccordion,
		fallbackNoteLabel,
	)

	return container.NewVScroll(tabContent)
}
