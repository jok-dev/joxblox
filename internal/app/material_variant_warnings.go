package app

import (
	"image/color"
	"path/filepath"
	"sort"
	"strings"

	"joxblox/internal/format"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

type missingMaterialVariantRustyAssetToolResult struct {
	VariantName  string `json:"variantName"`
	InstanceType string `json:"instanceType"`
	InstanceName string `json:"instanceName"`
	InstancePath string `json:"instancePath"`
}

type materialVariantWarningData struct {
	Summary     string
	DetailTitle string
	DetailText  string
}

type materialVariantWarningBanner struct {
	window     fyne.Window
	root       *fyne.Container
	content    *fyne.Container
	label      *widget.Label
	viewButton *widget.Button
	detail     materialVariantWarningData
}

func newMaterialVariantWarningBanner(window fyne.Window) *materialVariantWarningBanner {
	background := canvas.NewRectangle(color.NRGBA{R: 98, G: 74, B: 0, A: 235})
	label := widget.NewLabel("")
	label.Wrapping = fyne.TextWrapWord
	label.TextStyle = fyne.TextStyle{Bold: true}
	viewButton := widget.NewButton("View Missing Materials", nil)
	viewButton.Importance = widget.HighImportance
	content := container.NewMax(
		background,
		container.NewPadded(container.NewBorder(nil, nil, nil, viewButton, label)),
	)
	root := container.NewVBox()
	banner := &materialVariantWarningBanner{
		window:     window,
		root:       root,
		content:    content,
		label:      label,
		viewButton: viewButton,
	}
	viewButton.OnTapped = func() {
		if banner.window == nil || strings.TrimSpace(banner.detail.DetailText) == "" {
			return
		}
		detailLabel := widget.NewLabel(banner.detail.DetailText)
		detailLabel.Wrapping = fyne.TextWrapWord
		scroll := container.NewVScroll(detailLabel)
		scroll.SetMinSize(fyne.NewSize(720, 420))
		dialog.ShowCustom(
			strings.TrimSpace(banner.detail.DetailTitle),
			"Close",
			container.NewPadded(scroll),
			banner.window,
		)
	}
	return banner
}

func (banner *materialVariantWarningBanner) SetWarning(data materialVariantWarningData) {
	if banner == nil || banner.root == nil || banner.content == nil || banner.label == nil || banner.viewButton == nil {
		return
	}
	banner.detail = data
	trimmedText := strings.TrimSpace(data.Summary)
	banner.root.RemoveAll()
	if trimmedText == "" {
		banner.label.SetText("")
		banner.viewButton.Hide()
		banner.detail = materialVariantWarningData{}
		banner.root.Refresh()
		return
	}
	banner.label.SetText(trimmedText)
	if strings.TrimSpace(data.DetailText) != "" {
		banner.viewButton.Show()
	} else {
		banner.viewButton.Hide()
	}
	banner.root.Add(banner.content)
	banner.root.Refresh()
}

func materialVariantWarningPathPrefixes(pathPrefixes []string) []string {
	if len(pathPrefixes) == 0 {
		return nil
	}
	filtered := make([]string, 0, len(pathPrefixes)+1)
	hasMaterialService := false
	for _, prefix := range pathPrefixes {
		trimmedPrefix := strings.TrimSpace(prefix)
		if trimmedPrefix == "" {
			continue
		}
		filtered = append(filtered, trimmedPrefix)
		if strings.EqualFold(trimmedPrefix, "MaterialService") {
			hasMaterialService = true
		}
	}
	if !hasMaterialService {
		filtered = append(filtered, "MaterialService")
	}
	return filtered
}

func buildMissingMaterialVariantWarningData(fileLabel string, missing []missingMaterialVariantRustyAssetToolResult) materialVariantWarningData {
	if len(missing) == 0 {
		return materialVariantWarningData{}
	}

	uniqueNames := map[string]struct{}{}
	sampleNames := make([]string, 0, len(missing))
	for _, entry := range missing {
		name := strings.TrimSpace(entry.VariantName)
		if name == "" {
			continue
		}
		nameKey := strings.ToLower(name)
		if _, exists := uniqueNames[nameKey]; exists {
			continue
		}
		uniqueNames[nameKey] = struct{}{}
		sampleNames = append(sampleNames, name)
	}
	sort.Strings(sampleNames)

	variantCount := len(sampleNames)
	if variantCount == 0 {
		variantCount = len(missing)
	}
	if variantCount == 0 {
		return materialVariantWarningData{}
	}

	label := strings.TrimSpace(fileLabel)
	if label == "" {
		label = "this file"
	}

	variantWord := "MaterialVariants"
	if variantCount == 1 {
		variantWord = "MaterialVariant"
	}
	detailLines := make([]string, 0, len(sampleNames)+3)
	detailLines = append(detailLines,
		"Referenced by "+label+":",
		"",
		"Missing MaterialVariants in MaterialService:",
	)
	for _, name := range sampleNames {
		detailLines = append(detailLines, "- "+name)
	}
	return materialVariantWarningData{
		Summary:     "Warning: " + format.FormatIntCommas(int64(variantCount)) + " " + variantWord + " missing in MaterialService for " + label + ".",
		DetailTitle: "Missing MaterialVariants",
		DetailText: strings.Join(detailLines, "\n") +
			"\n\nYou won't get a perfect look at all textures since some will be in missing materials.",
	}
}

func buildRBXLMissingMaterialVariantWarning(filePath string, pathPrefixes []string, stopChannel <-chan struct{}) (materialVariantWarningData, error) {
	missingVariants, extractErr := extractMissingMaterialVariantsWithRustyAssetTool(filePath, materialVariantWarningPathPrefixes(pathPrefixes), stopChannel)
	if extractErr != nil {
		return materialVariantWarningData{}, extractErr
	}
	return buildMissingMaterialVariantWarningData(filepath.Base(strings.TrimSpace(filePath)), missingVariants), nil
}

func combineMaterialVariantWarnings(warnings ...materialVariantWarningData) materialVariantWarningData {
	filteredWarnings := make([]materialVariantWarningData, 0, len(warnings))
	for _, warning := range warnings {
		if strings.TrimSpace(warning.Summary) == "" {
			continue
		}
		filteredWarnings = append(filteredWarnings, warning)
	}
	if len(filteredWarnings) == 0 {
		return materialVariantWarningData{}
	}
	if len(filteredWarnings) == 1 {
		return filteredWarnings[0]
	}
	summaries := make([]string, 0, len(filteredWarnings))
	details := make([]string, 0, len(filteredWarnings))
	for _, warning := range filteredWarnings {
		summaries = append(summaries, warning.Summary)
		if strings.TrimSpace(warning.DetailText) != "" {
			details = append(details, warning.DetailText)
		}
	}
	return materialVariantWarningData{
		Summary:     strings.Join(summaries, " "),
		DetailTitle: "Missing MaterialVariants",
		DetailText:  strings.Join(details, "\n\n"),
	}
}
