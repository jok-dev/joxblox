package app

import "testing"

func TestResolveRustyAssetToolSubcommandCommandWithPathsPrefersBundledBinary(t *testing.T) {
	commandName, commandArgs, usesCargo, err := resolveRustyAssetToolSubcommandCommandWithPaths(
		"map",
		"place.rbxl",
		"bundled-tool",
		"local-tool",
		"C:/repo/tools/rbxl-id-extractor/Cargo.toml",
		"Workspace.Part",
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if commandName != "bundled-tool" {
		t.Fatalf("expected bundled-tool, got %q", commandName)
	}
	if usesCargo {
		t.Fatalf("expected bundled binary path to avoid cargo")
	}
	if len(commandArgs) != 3 || commandArgs[0] != "map" || commandArgs[1] != "place.rbxl" || commandArgs[2] != "Workspace.Part" {
		t.Fatalf("unexpected command args: %#v", commandArgs)
	}
}

func TestResolveRustyAssetToolSubcommandCommandWithPathsPrefersLocalBinaryBeforeCargo(t *testing.T) {
	commandName, commandArgs, usesCargo, err := resolveRustyAssetToolSubcommandCommandWithPaths(
		"heatmap",
		"place.rbxl",
		"",
		"local-tool",
		"C:/repo/tools/rbxl-id-extractor/Cargo.toml",
		"Workspace.Part",
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if commandName != "local-tool" {
		t.Fatalf("expected local-tool, got %q", commandName)
	}
	if usesCargo {
		t.Fatalf("expected local binary path to avoid cargo")
	}
	if len(commandArgs) != 3 || commandArgs[0] != "heatmap" || commandArgs[1] != "place.rbxl" || commandArgs[2] != "Workspace.Part" {
		t.Fatalf("unexpected command args: %#v", commandArgs)
	}
}

func TestResolveRustyAssetToolSubcommandCommandWithPathsFallsBackToCargo(t *testing.T) {
	commandName, commandArgs, usesCargo, err := resolveRustyAssetToolSubcommandCommandWithPaths(
		"map",
		"place.rbxl",
		"",
		"",
		"C:/repo/tools/rbxl-id-extractor/Cargo.toml",
		"",
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if commandName != "cargo" {
		t.Fatalf("expected cargo, got %q", commandName)
	}
	if !usesCargo {
		t.Fatalf("expected cargo fallback to report usesCargo")
	}
	if len(commandArgs) < 8 || commandArgs[0] != "run" || commandArgs[6] != "map" || commandArgs[7] != "place.rbxl" {
		t.Fatalf("unexpected cargo args: %#v", commandArgs)
	}
}
