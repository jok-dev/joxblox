package app

import (
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
)

func gzipTestPayload(t *testing.T, payload string) []byte {
	t.Helper()

	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	if _, err := gzipWriter.Write([]byte(payload)); err != nil {
		t.Fatalf("gzip write failed: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("gzip close failed: %v", err)
	}
	return compressed.Bytes()
}

func TestExtractAssetIDsWithRustyAssetToolFromTexturePackXML(t *testing.T) {
	payload := gzipTestPayload(t, `
<roblox>
  <texturepack_version>2</texturepack_version>
  <url>rbxassetid://72886069858230</url>
</roblox>
`)

	assetIDs, useCounts, references, rawOutput, err := extractAssetIDsWithRustyAssetToolFromFileWithCountsFromBytes(
		payload,
		63,
		rustyAssetToolDefaultLimit,
	)
	if err != nil {
		t.Fatalf("extractAssetIDsWithRustyAssetToolFromFileWithCountsFromBytes returned error: %v", err)
	}
	if len(assetIDs) != 1 || assetIDs[0] != 72886069858230 {
		t.Fatalf("expected one extracted asset id 72886069858230, got %v", assetIDs)
	}
	if got := useCounts[72886069858230]; got != 1 {
		t.Fatalf("expected use count 1 for 72886069858230, got %d", got)
	}
	if len(references) != 1 {
		t.Fatalf("expected one extracted reference, got %d", len(references))
	}
	if references[0].ID != 72886069858230 {
		t.Fatalf("expected reference asset id 72886069858230, got %d", references[0].ID)
	}
	if !strings.Contains(rawOutput, "72886069858230") {
		t.Fatalf("expected raw tool output to include extracted asset id, got %q", rawOutput)
	}
}

func TestExtractAssetIDsWithRustyAssetToolHandlesTexturePackXMLWithoutReferences(t *testing.T) {
	payload := gzipTestPayload(t, `
<roblox>
  <texturepack_version>2</texturepack_version>
  <usage>UI</usage>
</roblox>
`)

	assetIDs, useCounts, references, rawOutput, err := extractAssetIDsWithRustyAssetToolFromFileWithCountsFromBytes(
		payload,
		63,
		rustyAssetToolDefaultLimit,
	)
	if err != nil {
		t.Fatalf("extractAssetIDsWithRustyAssetToolFromFileWithCountsFromBytes returned error: %v", err)
	}
	if len(assetIDs) != 0 {
		t.Fatalf("expected no extracted asset ids, got %v", assetIDs)
	}
	if len(useCounts) != 0 {
		t.Fatalf("expected no use counts, got %v", useCounts)
	}
	if len(references) != 0 {
		t.Fatalf("expected no extracted references, got %d", len(references))
	}
	if strings.TrimSpace(rawOutput) != "[]" {
		t.Fatalf("expected raw tool output to be empty JSON array, got %q", rawOutput)
	}
}
