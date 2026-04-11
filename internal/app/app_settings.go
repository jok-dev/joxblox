package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	nativeDialog "github.com/sqweek/dialog"
)

const (
	preferenceKeyAssetDownloadCacheEnabled  = "settings.asset_download_cache.enabled"
	preferenceKeyAssetDownloadCacheFolder   = "settings.asset_download_cache.folder"
	preferenceKeyMeshPreviewLookSensitivity = "settings.mesh_preview.look_sensitivity"
)

type assetDownloadCacheSettings struct {
	Enabled bool
	Folder  string
}

func loadAssetDownloadCacheSettings() assetDownloadCacheSettings {
	currentApp := fyne.CurrentApp()
	if currentApp == nil {
		return assetDownloadCacheSettings{}
	}

	return assetDownloadCacheSettings{
		Enabled: currentApp.Preferences().Bool(preferenceKeyAssetDownloadCacheEnabled),
		Folder:  strings.TrimSpace(currentApp.Preferences().String(preferenceKeyAssetDownloadCacheFolder)),
	}
}

func saveAssetDownloadCacheSettings(settings assetDownloadCacheSettings) error {
	normalizedSettings, err := validateAssetDownloadCacheSettings(settings)
	if err != nil {
		return err
	}

	currentApp := fyne.CurrentApp()
	if currentApp == nil {
		return fmt.Errorf("application preferences are unavailable")
	}

	currentApp.Preferences().SetBool(preferenceKeyAssetDownloadCacheEnabled, normalizedSettings.Enabled)
	currentApp.Preferences().SetString(preferenceKeyAssetDownloadCacheFolder, normalizedSettings.Folder)
	logDebugf("Settings saved (asset_download_cache_enabled=%t, asset_download_cache_folder=%q)", normalizedSettings.Enabled, normalizedSettings.Folder)
	return nil
}

func validateAssetDownloadCacheSettings(settings assetDownloadCacheSettings) (assetDownloadCacheSettings, error) {
	normalizedSettings := assetDownloadCacheSettings{
		Enabled: settings.Enabled,
		Folder:  strings.TrimSpace(settings.Folder),
	}
	if normalizedSettings.Folder != "" {
		normalizedSettings.Folder = filepath.Clean(normalizedSettings.Folder)
	}

	if normalizedSettings.Enabled && normalizedSettings.Folder == "" {
		return assetDownloadCacheSettings{}, fmt.Errorf("select a cache folder before enabling asset download cache")
	}

	if normalizedSettings.Folder == "" {
		return normalizedSettings, nil
	}

	folderInfo, err := os.Stat(normalizedSettings.Folder)
	if err != nil {
		return assetDownloadCacheSettings{}, fmt.Errorf("cache folder is not accessible: %w", err)
	}
	if !folderInfo.IsDir() {
		return assetDownloadCacheSettings{}, fmt.Errorf("cache folder must be a directory")
	}

	return normalizedSettings, nil
}

func loadMeshPreviewMouseLookSensitivity() float64 {
	currentApp := fyne.CurrentApp()
	if currentApp == nil {
		return meshPreviewDefaultMouseLookSensitivity
	}
	stored := currentApp.Preferences().Float(preferenceKeyMeshPreviewLookSensitivity)
	if stored <= 0 {
		return meshPreviewDefaultMouseLookSensitivity
	}
	return clampFloat64(stored, meshPreviewMinimumMouseLookSensitivity, meshPreviewMaximumMouseLookSensitivity)
}

func saveMeshPreviewMouseLookSensitivity(value float64) error {
	currentApp := fyne.CurrentApp()
	if currentApp == nil {
		return fmt.Errorf("application preferences are unavailable")
	}
	normalized := clampFloat64(value, meshPreviewMinimumMouseLookSensitivity, meshPreviewMaximumMouseLookSensitivity)
	currentApp.Preferences().SetFloat(preferenceKeyMeshPreviewLookSensitivity, normalized)
	logDebugf("Settings saved (mesh_preview_look_sensitivity=%f)", normalized)
	return nil
}

func calculateDirectorySize(folderPath string) (int64, error) {
	trimmedPath := strings.TrimSpace(folderPath)
	if trimmedPath == "" {
		return 0, nil
	}

	var totalBytes int64
	err := filepath.WalkDir(trimmedPath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry == nil || entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			totalBytes += info.Size()
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	return totalBytes, nil
}

func showSettingsDialog(window fyne.Window) {
	currentSettings := loadAssetDownloadCacheSettings()
	currentLookSensitivity := loadMeshPreviewMouseLookSensitivity()

	cacheEnabledCheck := widget.NewCheck("Enable asset download cache", nil)
	cacheEnabledCheck.SetChecked(currentSettings.Enabled)

	cacheFolderEntry := widget.NewEntry()
	cacheFolderEntry.SetPlaceHolder("Select a folder to store cached asset bytes")
	cacheFolderEntry.SetText(currentSettings.Folder)

	cacheWarningLabel := widget.NewLabel(
		"Warning: cached asset and thumbnail downloads can grow very large depending on how much content you browse.",
	)
	cacheWarningLabel.Wrapping = fyne.TextWrapWord

	cacheFolderSizeLabel := widget.NewLabel("Current folder size: -")
	cacheFolderSizeLabel.Wrapping = fyne.TextWrapWord

	lookSensitivityValueLabel := widget.NewLabel(fmt.Sprintf("%.4f", currentLookSensitivity))
	lookSensitivitySlider := widget.NewSlider(meshPreviewMinimumMouseLookSensitivity, meshPreviewMaximumMouseLookSensitivity)
	lookSensitivitySlider.Step = meshPreviewMouseLookSensitivityStep
	lookSensitivitySlider.SetValue(currentLookSensitivity)
	lookSensitivitySlider.OnChanged = func(value float64) {
		lookSensitivityValueLabel.SetText(fmt.Sprintf("%.4f", clampFloat64(value, meshPreviewMinimumMouseLookSensitivity, meshPreviewMaximumMouseLookSensitivity)))
	}

	statusLabel := widget.NewLabel("")
	statusLabel.Wrapping = fyne.TextWrapWord

	var refreshToken atomic.Uint64
	refreshFolderSize := func() {
		folderPath := strings.TrimSpace(cacheFolderEntry.Text)
		if folderPath == "" {
			cacheFolderSizeLabel.SetText("Current folder size: -")
			return
		}

		requestToken := refreshToken.Add(1)
		cacheFolderSizeLabel.SetText("Current folder size: Refreshing...")
		go func(selectedPath string, expectedToken uint64) {
			totalBytes, err := calculateDirectorySize(selectedPath)
			fyne.Do(func() {
				if refreshToken.Load() != expectedToken || strings.TrimSpace(cacheFolderEntry.Text) != selectedPath {
					return
				}
				if err != nil {
					cacheFolderSizeLabel.SetText(fmt.Sprintf("Current folder size: unavailable (%s)", err.Error()))
					return
				}
				cacheFolderSizeLabel.SetText(fmt.Sprintf("Current folder size: %s", formatSizeAuto64(totalBytes)))
			})
		}(folderPath, requestToken)
	}

	cacheFolderEntry.OnChanged = func(string) {
		statusLabel.SetText("")
		cacheFolderSizeLabel.SetText("Current folder size: click Refresh.")
	}

	browseButton := widget.NewButton("Browse...", func() {
		selectedPath, pickerErr := nativeDialog.Directory().Title("Select cache folder").Browse()
		if pickerErr == nil {
			cacheFolderEntry.SetText(selectedPath)
			refreshFolderSize()
			return
		}
		if errors.Is(pickerErr, nativeDialog.Cancelled) {
			return
		}
		statusLabel.SetText(fmt.Sprintf("Folder picker failed: %s", pickerErr.Error()))
	})

	refreshButton := widget.NewButton("Refresh", func() {
		refreshFolderSize()
	})

	folderRow := container.NewBorder(
		nil,
		nil,
		nil,
		container.NewHBox(browseButton, refreshButton),
		cacheFolderEntry,
	)

	formContent := container.NewVBox(
		cacheEnabledCheck,
		widget.NewSeparator(),
		widget.NewLabel("3D Preview Mouse Look Sensitivity"),
		container.NewBorder(
			nil,
			nil,
			nil,
			lookSensitivityValueLabel,
			lookSensitivitySlider,
		),
		widget.NewSeparator(),
		widget.NewLabel("Cache Folder"),
		folderRow,
		cacheWarningLabel,
		cacheFolderSizeLabel,
		widget.NewSeparator(),
		statusLabel,
	)

	var settingsDialog dialog.Dialog
	cancelButton := widget.NewButton("Cancel", func() {
		if settingsDialog != nil {
			settingsDialog.Hide()
		}
	})
	saveButton := widget.NewButton("Save", func() {
		nextSettings := assetDownloadCacheSettings{
			Enabled: cacheEnabledCheck.Checked,
			Folder:  cacheFolderEntry.Text,
		}
		if err := saveAssetDownloadCacheSettings(nextSettings); err != nil {
			statusLabel.SetText(err.Error())
			return
		}
		if err := saveMeshPreviewMouseLookSensitivity(lookSensitivitySlider.Value); err != nil {
			statusLabel.SetText(err.Error())
			return
		}

		if settingsDialog != nil {
			settingsDialog.Hide()
		}
		dialog.ShowInformation("Settings", "Settings saved.", window)
	})

	content := container.NewVBox(
		formContent,
		widget.NewSeparator(),
		container.NewHBox(saveButton, cancelButton),
	)

	settingsDialog = dialog.NewCustomWithoutButtons("Settings", content, window)
	settingsDialog.Resize(fyne.NewSize(640, content.MinSize().Height))
	settingsDialog.Show()

	if strings.TrimSpace(currentSettings.Folder) != "" {
		refreshFolderSize()
	}
}
