package main

import (
	"strings"
	"testing"
)

func TestParseRenderArgsDefaultsViewmodeWhenMissing(t *testing.T) {
	parts := strings.Fields("RENDER 800 600 0 0 5 -1 0 0 1 1 222222")
	args, err := parseRenderArgs(parts)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if args.Viewmode != ViewmodeVertexColor {
		t.Errorf("default viewmode: got %d, want %d (vertex color)", args.Viewmode, ViewmodeVertexColor)
	}
	if args.Wireframe {
		t.Errorf("wireframe should default to false")
	}
}

func TestParseRenderArgsAcceptsViewmodeArg(t *testing.T) {
	parts := strings.Fields("RENDER 800 600 0 0 5 -1 0 0 1 1 222222 0 2")
	args, err := parseRenderArgs(parts)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if args.Viewmode != ViewmodeNormals {
		t.Errorf("viewmode: got %d, want %d (Normals)", args.Viewmode, ViewmodeNormals)
	}
}

func TestParseRenderArgsClampsUnknownViewmode(t *testing.T) {
	parts := strings.Fields("RENDER 800 600 0 0 5 -1 0 0 1 1 222222 0 99")
	args, _ := parseRenderArgs(parts)
	if args.Viewmode != ViewmodeVertexColor {
		t.Errorf("unknown viewmode should fall back to vertex color, got %d", args.Viewmode)
	}
}
