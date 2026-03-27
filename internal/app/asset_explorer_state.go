package app

import "strings"

type assetExplorerRow struct {
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

type assetExplorerState struct {
	rootAssetID int64
	selectedID  int64
	rows        []assetExplorerRow
	knownByID   map[int64]*assetPreviewResult
	indexByID   map[int64]int
}

func newAssetExplorerState(rootAssetID int64, rootPreview *assetPreviewResult) *assetExplorerState {
	state := &assetExplorerState{
		rootAssetID: rootAssetID,
		selectedID:  rootAssetID,
		rows:        []assetExplorerRow{},
		knownByID:   map[int64]*assetPreviewResult{},
		indexByID:   map[int64]int{},
	}
	state.knownByID[rootAssetID] = rootPreview
	selfBytesSize := rootPreview.Stats.BytesSize
	state.addRow(assetExplorerRow{
		AssetID:       rootAssetID,
		Depth:         0,
		SelfBytesSize: selfBytesSize,
		AssetTypeID:   rootPreview.AssetTypeID,
		Resolved:      true,
	})
	state.addChildren(rootAssetID, 1, rootPreview.ChildAssets)
	return state
}

func (state *assetExplorerState) addRow(row assetExplorerRow) {
	if _, exists := state.indexByID[row.AssetID]; exists {
		return
	}
	state.rows = append(state.rows, row)
	state.indexByID[row.AssetID] = len(state.rows) - 1
}

func (state *assetExplorerState) addChildren(parentAssetID int64, childDepth int, childAssets []childAssetInfo) {
	for _, childAsset := range childAssets {
		state.addRow(assetExplorerRow{
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

func (state *assetExplorerState) getRows() []assetExplorerRow {
	return state.rows
}

func (state *assetExplorerState) getSelectedID() int64 {
	return state.selectedID
}

func (state *assetExplorerState) getRow(assetID int64) (assetExplorerRow, bool) {
	rowIndex, exists := state.indexByID[assetID]
	if !exists || rowIndex < 0 || rowIndex >= len(state.rows) {
		return assetExplorerRow{}, false
	}
	return state.rows[rowIndex], true
}

func (state *assetExplorerState) getInstancePath(assetID int64) string {
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

func (state *assetExplorerState) selectAsset(assetID int64) (*assetPreviewResult, error) {
	preview, err, _ := state.selectAssetWithRequestSource(assetID)
	return preview, err
}

func (state *assetExplorerState) selectAssetWithRequestSource(assetID int64) (*assetPreviewResult, error, heatmapAssetRequestSource) {
	state.selectedID = assetID
	if knownPreview, exists := state.knownByID[assetID]; exists {
		return knownPreview, nil, heatmapAssetRequestSourceMemory
	}

	trace := &assetRequestTrace{}
	loadedPreview, loadErr := loadAssetPreviewWithTrace(assetID, trace)
	if loadErr != nil {
		return nil, loadErr, trace.classifyRequestSource()
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
	return loadedPreview, nil, trace.classifyRequestSource()
}
