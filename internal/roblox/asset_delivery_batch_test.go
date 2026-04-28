package roblox

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestBuildKTX2ContentRepresentationPriorityListMatchesStudioCapture(t *testing.T) {
	// Captured from a live Studio /v1/assets/batch POST for an Image asset
	// at fidelity 64 — used as the canonical reference for what the engine
	// sends. If this test fails the server may stop matching the request.
	const expected = "W3siZm9ybWF0Ijoia3R4MiIsIm1ham9yVmVyc2lvbiI6IjZyZG8iLCJmaWRlbGl0eSI6IlFBQT0ifSx7ImZvcm1hdCI6Imt0eDIiLCJtYWpvclZlcnNpb24iOiI2IiwiZmlkZWxpdHkiOiJRQUE9In1d"

	got := BuildKTX2ContentRepresentationPriorityList(KTX2FidelityDefault)
	if got != expected {
		t.Fatalf("CRPL mismatch\nexpected %q\ngot      %q", expected, got)
	}
}

func TestBuildKTX2ContentRepresentationPriorityListEncodesFidelityLittleEndian(t *testing.T) {
	for _, tc := range []struct {
		fidelity     uint16
		expectedB64  string
		expectedDesc string
	}{
		{64, "QAA=", "low byte 0x40"},
		{65, "QQA=", "low byte 0x41"},
		{66, "QgA=", "low byte 0x42"},
		{256, "AAE=", "high byte 0x01 — confirms LE order"},
	} {
		raw := BuildKTX2ContentRepresentationPriorityList(tc.fidelity)
		decoded, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			t.Fatalf("fidelity %d: outer base64 decode failed: %v", tc.fidelity, err)
		}
		var specs []ContentRepresentationSpecifier
		if err := json.Unmarshal(decoded, &specs); err != nil {
			t.Fatalf("fidelity %d: inner JSON decode failed: %v", tc.fidelity, err)
		}
		if len(specs) != 2 {
			t.Fatalf("fidelity %d: expected 2 specifiers, got %d", tc.fidelity, len(specs))
		}
		if specs[0].Fidelity != tc.expectedB64 {
			t.Errorf("fidelity %d (%s): expected encoded fidelity %q, got %q",
				tc.fidelity, tc.expectedDesc, tc.expectedB64, specs[0].Fidelity)
		}
		if specs[0].MajorVersion != "6rdo" || specs[1].MajorVersion != "6" {
			t.Errorf("fidelity %d: expected priority order 6rdo then 6, got %q then %q",
				tc.fidelity, specs[0].MajorVersion, specs[1].MajorVersion)
		}
	}
}
