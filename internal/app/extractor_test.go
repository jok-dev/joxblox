package app

import (
	"bytes"
	"compress/gzip"
	"strings"
	"testing"

	"joxblox/internal/extractor"
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

func TestExtractAssetIDsFromTexturePackXML(t *testing.T) {
	payload := gzipTestPayload(t, `
<roblox>
  <texturepack_version>2</texturepack_version>
  <url>rbxassetid://72886069858230</url>
</roblox>
`)

	result, err := extractor.ExtractAssetIDsFromBytesWithCounts(payload, 63, extractor.DefaultLimit)
	if err != nil {
		t.Fatalf("ExtractAssetIDsFromBytesWithCounts returned error: %v", err)
	}
	if len(result.AssetIDs) != 1 || result.AssetIDs[0] != 72886069858230 {
		t.Fatalf("expected one extracted asset id 72886069858230, got %v", result.AssetIDs)
	}
	if got := result.UseCounts[72886069858230]; got != 1 {
		t.Fatalf("expected use count 1 for 72886069858230, got %d", got)
	}
	if len(result.References) != 1 {
		t.Fatalf("expected one extracted reference, got %d", len(result.References))
	}
	if result.References[0].ID != 72886069858230 {
		t.Fatalf("expected reference asset id 72886069858230, got %d", result.References[0].ID)
	}
	if !strings.Contains(result.CommandOutput, "72886069858230") {
		t.Fatalf("expected raw tool output to include extracted asset id, got %q", result.CommandOutput)
	}
}

func TestExtractAssetIDsFromTexturePackXMLWithoutReferences(t *testing.T) {
	payload := gzipTestPayload(t, `
<roblox>
  <texturepack_version>2</texturepack_version>
  <usage>UI</usage>
</roblox>
`)

	result, err := extractor.ExtractAssetIDsFromBytesWithCounts(payload, 63, extractor.DefaultLimit)
	if err != nil {
		t.Fatalf("ExtractAssetIDsFromBytesWithCounts returned error: %v", err)
	}
	if len(result.AssetIDs) != 0 {
		t.Fatalf("expected no extracted asset ids, got %v", result.AssetIDs)
	}
	if len(result.UseCounts) != 0 {
		t.Fatalf("expected no use counts, got %v", result.UseCounts)
	}
	if len(result.References) != 0 {
		t.Fatalf("expected no extracted references, got %d", len(result.References))
	}
	if strings.TrimSpace(result.CommandOutput) != "[]" {
		t.Fatalf("expected raw tool output to be empty JSON array, got %q", result.CommandOutput)
	}
}
