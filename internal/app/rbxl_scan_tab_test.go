package app

import "testing"

func TestBuildScanHitsFromRustReferencesUsesExtractorUseCount(t *testing.T) {
	references := []rustyAssetToolResult{
		{
			ID:           101,
			RawContent:   "rbxassetid://101",
			InstanceType: "Decal",
			InstanceName: "Sign",
			InstancePath: "Workspace.Sign",
			PropertyName: "Texture",
			Used:         3,
		},
	}

	hits := buildScanHitsFromRustReferences(references, "place.rbxl", map[string]float64{}, 0)
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if hits[0].UseCount != 3 {
		t.Fatalf("expected use count 3, got %d", hits[0].UseCount)
	}
}

func TestBuildScanHitsFromRustReferencesDefaultsUseCountAndDeduplicatesPaths(t *testing.T) {
	references := []rustyAssetToolResult{
		{
			ID:               202,
			RawContent:       "rbxthumb://type=Asset&id=202&w=420&h=420",
			InstancePath:     "Workspace.Part.SurfaceGui.ImageLabel",
			Used:             0,
			AllInstancePaths: []string{"Workspace.Part.SurfaceGui.ImageLabel", "Workspace.Part.SurfaceGui.ImageLabel"},
		},
	}

	hits := buildScanHitsFromRustReferences(references, "place.rbxl", map[string]float64{}, 0)
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if hits[0].UseCount != 1 {
		t.Fatalf("expected default use count 1, got %d", hits[0].UseCount)
	}
	if len(hits[0].AllInstancePaths) != 1 {
		t.Fatalf("expected 1 deduplicated instance path, got %d", len(hits[0].AllInstancePaths))
	}
	if hits[0].AllInstancePaths[0] != "Workspace.Part.SurfaceGui.ImageLabel" {
		t.Fatalf("unexpected instance path %q", hits[0].AllInstancePaths[0])
	}
}

func TestBuildScanHitsFromRustReferencesUsesLargestSceneSurfaceArea(t *testing.T) {
	references := []rustyAssetToolResult{
		{
			ID:               303,
			RawContent:       "rbxassetid://303",
			InstancePath:     "Workspace.Building.Wall.Decal",
			AllInstancePaths: []string{"Workspace.Building.Wall.Decal", "Workspace.Building.Window.Texture"},
			Used:             2,
		},
	}

	areaByPath := map[string]float64{
		"Workspace.Building.Wall":   24,
		"Workspace.Building.Window": 6,
	}

	hits := buildScanHitsFromRustReferences(references, "place.rbxl", areaByPath, 0)
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if hits[0].SceneSurfaceArea != 24 {
		t.Fatalf("expected largest scene surface area 24, got %v", hits[0].SceneSurfaceArea)
	}
	if hits[0].LargestSurfacePath != "Workspace.Building.Wall.Decal" {
		t.Fatalf("expected largest surface path Workspace.Building.Wall.Decal, got %q", hits[0].LargestSurfacePath)
	}
}
