package webapp

import (
	"testing"
)

// withTempConfig points os.UserConfigDir() at a throwaway dir so the tag store
// reads/writes a temp tags.json instead of the real user config.
func withTempConfig(t *testing.T) {
	t.Helper()
	// os.UserConfigDir resolves from these env vars per-platform; setting both
	// covers macOS (HOME/Library) and Linux (XDG_CONFIG_HOME).
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
}

func TestListTagDefinitionsSeedsPresets(t *testing.T) {
	withTempConfig(t)
	s := NewTagService()

	defs, err := s.ListTagDefinitions()
	if err != nil {
		t.Fatalf("ListTagDefinitions: %v", err)
	}
	if len(defs) == 0 {
		t.Fatal("expected presets to be seeded, got none")
	}
	if defs[0].Name != "Decimate" {
		t.Fatalf("expected first preset Decimate, got %q", defs[0].Name)
	}
}

func TestSetAndGetAssignmentsRoundTrip(t *testing.T) {
	withTempConfig(t)
	s := NewTagService()

	if err := s.SetAssetTags(123, []string{"Decimate", "Reduce"}); err != nil {
		t.Fatalf("SetAssetTags: %v", err)
	}
	got, err := s.GetAssignments()
	if err != nil {
		t.Fatalf("GetAssignments: %v", err)
	}
	if len(got["123"]) != 2 {
		t.Fatalf("expected 2 tags on asset 123, got %v", got["123"])
	}

	// Empty list removes the entry.
	if err := s.SetAssetTags(123, nil); err != nil {
		t.Fatalf("SetAssetTags clear: %v", err)
	}
	got, _ = s.GetAssignments()
	if _, present := got["123"]; present {
		t.Fatalf("expected asset 123 removed, still present: %v", got)
	}
}

func TestSaveTagDefinitionsPrunesAssignments(t *testing.T) {
	withTempConfig(t)
	s := NewTagService()
	_, _ = s.ListTagDefinitions() // seed
	if err := s.SetAssetTags(7, []string{"Decimate", "Reduce"}); err != nil {
		t.Fatalf("SetAssetTags: %v", err)
	}

	// Library now only has Decimate — Reduce must be pruned from asset 7.
	if err := s.SaveTagDefinitions([]TagDefinition{{Name: "Decimate", Color: "#fff"}}); err != nil {
		t.Fatalf("SaveTagDefinitions: %v", err)
	}
	got, _ := s.GetAssignments()
	if len(got["7"]) != 1 || got["7"][0] != "Decimate" {
		t.Fatalf("expected only Decimate to survive prune, got %v", got["7"])
	}
}

func TestRenameTagMigratesAssignments(t *testing.T) {
	withTempConfig(t)
	s := NewTagService()
	_, _ = s.ListTagDefinitions()
	if err := s.SetAssetTags(9, []string{"Decimate"}); err != nil {
		t.Fatalf("SetAssetTags: %v", err)
	}

	if err := s.RenameTag("Decimate", "Slim"); err != nil {
		t.Fatalf("RenameTag: %v", err)
	}
	got, _ := s.GetAssignments()
	if len(got["9"]) != 1 || got["9"][0] != "Slim" {
		t.Fatalf("expected assignment migrated to Slim, got %v", got["9"])
	}
}
