package ui

import (
	"strings"

	"joxblox/internal/app/loader"
	"joxblox/internal/heatmap"
)

type AssetExplorerRow struct {
	AssetID       int64
	Depth         int
	SelfBytesSize int
	AssetTypeID   int
	Resolved      bool
	InstanceType  string
	InstanceName  string
	InstancePath  string
	PropertyName  string
}

type AssetExplorerState struct {
	RootAssetID int64
	selectedID  int64
	rows        []AssetExplorerRow
	knownByID   map[int64]*loader.AssetPreviewResult
	indexByID   map[int64]int
}

func NewAssetExplorerState(rootAssetID int64, rootPreview *loader.AssetPreviewResult) *AssetExplorerState {
	state := &AssetExplorerState{
		RootAssetID: rootAssetID,
		selectedID:  rootAssetID,
		rows:        []AssetExplorerRow{},
		knownByID:   map[int64]*loader.AssetPreviewResult{},
		indexByID:   map[int64]int{},
	}
	state.knownByID[rootAssetID] = rootPreview
	selfBytesSize := rootPreview.Stats.BytesSize
	state.addRow(AssetExplorerRow{
		AssetID:       rootAssetID,
		Depth:         0,
		SelfBytesSize: selfBytesSize,
		AssetTypeID:   rootPreview.AssetTypeID,
		Resolved:      true,
	})
	state.addChildren(rootAssetID, 1, rootPreview.ChildAssets)
	return state
}

func (state *AssetExplorerState) addRow(row AssetExplorerRow) {
	if _, exists := state.indexByID[row.AssetID]; exists {
		return
	}
	state.rows = append(state.rows, row)
	state.indexByID[row.AssetID] = len(state.rows) - 1
}

func (state *AssetExplorerState) addChildren(parentAssetID int64, childDepth int, childAssets []loader.ChildAssetInfo) {
	for _, childAsset := range childAssets {
		state.addRow(AssetExplorerRow{
			AssetID:       childAsset.AssetID,
			Depth:         childDepth,
			SelfBytesSize: childAsset.BytesSize,
			AssetTypeID:   childAsset.AssetTypeID,
			Resolved:      childAsset.Resolved,
			InstanceType:  childAsset.InstanceType,
			InstanceName:  childAsset.InstanceName,
			InstancePath:  childAsset.InstancePath,
			PropertyName:  childAsset.PropertyName,
		})
	}
}

func (state *AssetExplorerState) GetRows() []AssetExplorerRow {
	return state.rows
}

func (state *AssetExplorerState) getSelectedID() int64 {
	return state.selectedID
}

func (state *AssetExplorerState) getRow(assetID int64) (AssetExplorerRow, bool) {
	rowIndex, exists := state.indexByID[assetID]
	if !exists || rowIndex < 0 || rowIndex >= len(state.rows) {
		return AssetExplorerRow{}, false
	}
	return state.rows[rowIndex], true
}

func (state *AssetExplorerState) getInstancePath(assetID int64) string {
	if state == nil {
		return ""
	}
	rowIndex, exists := state.indexByID[assetID]
	if !exists || rowIndex < 0 || rowIndex >= len(state.rows) {
		return ""
	}
	if explicitPath := strings.TrimSpace(state.rows[rowIndex].InstancePath); explicitPath != "" {
		return explicitPath
	}
	pathSegments := []string{}
	currentDepth := state.rows[rowIndex].Depth
	for index := rowIndex; index >= 0; index-- {
		row := state.rows[index]
		if row.Depth != currentDepth {
			continue
		}
		segment := strings.TrimSpace(row.InstanceName)
		if segment == "" {
			segment = strings.TrimSpace(row.InstanceType)
		}
		if segment != "" {
			pathSegments = append([]string{segment}, pathSegments...)
		}
		if currentDepth == 0 {
			break
		}
		currentDepth--
	}
	return strings.Join(pathSegments, ".")
}

func (state *AssetExplorerState) selectAsset(assetID int64) (*loader.AssetPreviewResult, error) {
	preview, err, _ := state.SelectAssetWithRequestSource(assetID)
	return preview, err
}

func (state *AssetExplorerState) SelectAssetWithRequestSource(assetID int64) (*loader.AssetPreviewResult, error, heatmap.RequestSource) {
	state.selectedID = assetID
	if knownPreview, exists := state.knownByID[assetID]; exists {
		return knownPreview, nil, heatmap.SourceMemory
	}

	trace := &loader.AssetRequestTrace{}
	loadedPreview, loadErr := loader.LoadAssetPreviewWithTrace(assetID, trace)
	if loadErr != nil {
		return nil, loadErr, trace.ClassifyRequestSource()
	}
	state.knownByID[assetID] = loadedPreview
	if rowIndex, exists := state.indexByID[assetID]; exists {
		state.rows[rowIndex].Resolved = true
		state.rows[rowIndex].SelfBytesSize = loadedPreview.Stats.BytesSize
		state.rows[rowIndex].AssetTypeID = loadedPreview.AssetTypeID
	}
	parentDepth := 0
	if rowIndex, exists := state.indexByID[assetID]; exists {
		parentDepth = state.rows[rowIndex].Depth
	}
	state.addChildren(assetID, parentDepth+1, loadedPreview.ChildAssets)
	return loadedPreview, nil, trace.ClassifyRequestSource()
}
