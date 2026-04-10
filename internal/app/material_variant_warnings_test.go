package app

import (
	"strings"
	"testing"
)

func TestMaterialVariantWarningPathPrefixesIncludesMaterialService(t *testing.T) {
	prefixes := materialVariantWarningPathPrefixes([]string{"Workspace"})
	if len(prefixes) != 2 {
		t.Fatalf("expected 2 prefixes, got %d", len(prefixes))
	}
	if prefixes[1] != "MaterialService" {
		t.Fatalf("expected MaterialService to be appended, got %q", prefixes[1])
	}
}

func TestBuildMissingMaterialVariantWarning(t *testing.T) {
	warningData := buildMissingMaterialVariantWarningData("test.rbxl", []missingMaterialVariantRustyAssetToolResult{
		{VariantName: "Mud"},
		{VariantName: "Snow"},
	})
	if !strings.Contains(warningData.Summary, "missing in MaterialService") {
		t.Fatalf("expected missing MaterialService warning, got %q", warningData.Summary)
	}
	if !strings.Contains(warningData.DetailText, "Mud") {
		t.Fatalf("expected example variant name in warning details, got %q", warningData.DetailText)
	}
}
