package ui

import "joxblox/internal/app/loader"

func BuildExplorerSelectionReferenceContext(state *AssetExplorerState, selectedAssetID int64) loader.AssetReferenceContext {
	if state == nil {
		return loader.AssetReferenceContext{}
	}
	selectedRow, found := state.getRow(selectedAssetID)
	if !found {
		return loader.AssetReferenceContext{}
	}
	referenceInstancePath := state.getInstancePath(selectedAssetID)
	if referenceInstancePath == "" {
		referenceInstancePath = selectedRow.InstancePath
	}
	if referenceInstancePath == "" {
		referenceInstancePath = selectedRow.InstanceName
	}
	return loader.AssetReferenceContext{
		ReferenceInstanceType: selectedRow.InstanceType,
		ReferencePropertyName: selectedRow.PropertyName,
		ReferenceInstancePath: referenceInstancePath,
	}
}
