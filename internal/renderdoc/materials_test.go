package renderdoc

import (
	"reflect"
	"testing"
)

func makeTexture(id string, category TextureCategory, bytes int64) TextureInfo {
	return TextureInfo{ResourceID: id, Category: category, Bytes: bytes}
}

func TestBuildMaterialsGroupsFullPBRTriple(t *testing.T) {
	textures := &Report{
		Textures: []TextureInfo{
			makeTexture("100", CategoryAssetOpaque, 1024),
			makeTexture("200", CategoryNormalDXT5nm, 512),
			makeTexture("300", CategoryCustomMR, 256),
		},
	}
	meshes := &MeshReport{
		DrawCalls: []DrawCall{
			{PSTextureIDs: []string{"100", "200", "300"}},
		},
	}
	got := BuildMaterials(textures, meshes)
	if len(got) != 1 {
		t.Fatalf("materials: got %d, want 1", len(got))
	}
	mat := got[0]
	if mat.ColorTextureID != "100" || mat.NormalTextureID != "200" || mat.MRTextureID != "300" {
		t.Errorf("classification wrong: %+v", mat)
	}
	if mat.DrawCallCount != 1 {
		t.Errorf("DrawCallCount: got %d, want 1", mat.DrawCallCount)
	}
	if mat.TotalBytes != 1024+512+256 {
		t.Errorf("TotalBytes: got %d, want %d", mat.TotalBytes, 1024+512+256)
	}
}

func TestBuildMaterialsDedupesByTuple(t *testing.T) {
	textures := &Report{
		Textures: []TextureInfo{
			makeTexture("100", CategoryAssetOpaque, 1024),
			makeTexture("200", CategoryNormalDXT5nm, 512),
		},
	}
	meshes := &MeshReport{
		DrawCalls: []DrawCall{
			{PSTextureIDs: []string{"100", "200"}},
			{PSTextureIDs: []string{"100", "200"}},
			{PSTextureIDs: []string{"100", "200"}},
		},
	}
	got := BuildMaterials(textures, meshes)
	if len(got) != 1 {
		t.Fatalf("materials: got %d, want 1", len(got))
	}
	if got[0].DrawCallCount != 3 {
		t.Errorf("DrawCallCount: got %d, want 3", got[0].DrawCallCount)
	}
}

func TestBuildMaterialsSplitsBySharedColorDifferentNormal(t *testing.T) {
	textures := &Report{
		Textures: []TextureInfo{
			makeTexture("100", CategoryAssetOpaque, 1024),
			makeTexture("200", CategoryNormalDXT5nm, 512),
			makeTexture("201", CategoryNormalDXT5nm, 512),
		},
	}
	meshes := &MeshReport{
		DrawCalls: []DrawCall{
			{PSTextureIDs: []string{"100", "200"}},
			{PSTextureIDs: []string{"100", "201"}},
		},
	}
	got := BuildMaterials(textures, meshes)
	if len(got) != 2 {
		t.Fatalf("materials: got %d, want 2", len(got))
	}
}

func TestBuildMaterialsExcludesGlobalByCategory(t *testing.T) {
	textures := &Report{
		Textures: []TextureInfo{
			makeTexture("100", CategoryAssetOpaque, 1024),
			makeTexture("999", CategoryBuiltinBRDFLUT, 64),
		},
	}
	meshes := &MeshReport{
		DrawCalls: []DrawCall{
			{PSTextureIDs: []string{"100", "999"}},
		},
	}
	got := BuildMaterials(textures, meshes)
	if len(got) != 1 || got[0].ColorTextureID != "100" {
		t.Fatalf("expected single color-only material, got %+v", got)
	}
	if len(got[0].OtherTextureIDs) != 0 {
		t.Errorf("global texture leaked into Others: %v", got[0].OtherTextureIDs)
	}
}

func TestBuildMaterialsExcludesGlobalByFrequency(t *testing.T) {
	textures := &Report{
		Textures: []TextureInfo{
			makeTexture("100", CategoryAssetOpaque, 1024),
			makeTexture("101", CategoryAssetOpaque, 1024),
			makeTexture("102", CategoryAssetOpaque, 1024),
			makeTexture("103", CategoryAssetOpaque, 1024),
			makeTexture("shadow", CategoryAssetRaw, 64),
		},
	}
	meshes := &MeshReport{
		DrawCalls: []DrawCall{
			{PSTextureIDs: []string{"100", "shadow"}},
			{PSTextureIDs: []string{"101", "shadow"}},
			{PSTextureIDs: []string{"102", "shadow"}},
			{PSTextureIDs: []string{"103", "shadow"}},
		},
	}
	got := BuildMaterials(textures, meshes)
	if len(got) != 4 {
		t.Fatalf("materials: got %d, want 4", len(got))
	}
	for _, m := range got {
		if m.ColorTextureID == "shadow" {
			t.Errorf("shadow leaked as color: %+v", m)
		}
		if containsString(m.OtherTextureIDs, "shadow") {
			t.Errorf("shadow leaked into Others: %+v", m)
		}
	}
}

func TestBuildMaterialsMultipleColorsKeepsLowestSlotFirst(t *testing.T) {
	textures := &Report{
		Textures: []TextureInfo{
			makeTexture("100", CategoryAssetOpaque, 1024),
			makeTexture("101", CategoryAssetOpaque, 512),
		},
	}
	meshes := &MeshReport{
		DrawCalls: []DrawCall{
			{PSTextureIDs: []string{"100", "101"}},
		},
	}
	got := BuildMaterials(textures, meshes)
	if len(got) != 1 {
		t.Fatalf("materials: got %d, want 1", len(got))
	}
	if got[0].ColorTextureID != "100" {
		t.Errorf("ColorTextureID: got %q, want 100", got[0].ColorTextureID)
	}
	want := []string{"101"}
	if !reflect.DeepEqual(got[0].OtherTextureIDs, want) {
		t.Errorf("OtherTextureIDs: got %v, want %v", got[0].OtherTextureIDs, want)
	}
}

func TestBuildMaterialsSortsByTotalBytesDescending(t *testing.T) {
	textures := &Report{
		Textures: []TextureInfo{
			makeTexture("a", CategoryAssetOpaque, 100),
			makeTexture("b", CategoryAssetOpaque, 5000),
			makeTexture("c", CategoryAssetOpaque, 1000),
		},
	}
	meshes := &MeshReport{
		DrawCalls: []DrawCall{
			{PSTextureIDs: []string{"a"}},
			{PSTextureIDs: []string{"b"}},
			{PSTextureIDs: []string{"c"}},
		},
	}
	got := BuildMaterials(textures, meshes)
	var sizes []int64
	for _, m := range got {
		sizes = append(sizes, m.TotalBytes)
	}
	want := []int64{5000, 1000, 100}
	if !reflect.DeepEqual(sizes, want) {
		t.Errorf("sort order: got %v, want %v", sizes, want)
	}
}
