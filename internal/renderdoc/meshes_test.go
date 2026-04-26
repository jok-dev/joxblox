package renderdoc

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

const sampleCreateBufferXML = `<root>
<chunk name="ID3D11Device::CreateBuffer">
  <struct name="pDesc">
    <uint name="ByteWidth">240104</uint>
    <enum name="Usage" string="D3D11_USAGE_DEFAULT">0</enum>
    <enum name="BindFlags" string="D3D11_BIND_VERTEX_BUFFER">1</enum>
  </struct>
  <ResourceId name="pBuffer">14419</ResourceId>
  <buffer name="InitialData" byteLength="240104">42</buffer>
</chunk>
</root>`

func TestParseCreateBufferChunk(t *testing.T) {
	report, err := parseMeshXML(strings.NewReader(sampleCreateBufferXML))
	if err != nil {
		t.Fatalf("parseMeshXML: %v", err)
	}
	buf, ok := report.Buffers["14419"]
	if !ok {
		t.Fatalf("buffer 14419 not parsed; got %v", report.Buffers)
	}
	if buf.ByteWidth != 240104 {
		t.Errorf("ByteWidth = %d, want 240104", buf.ByteWidth)
	}
	if !buf.IsVertexBuffer() {
		t.Errorf("IsVertexBuffer = false, want true (bind flags: %q)", buf.BindFlags)
	}
	if buf.InitialDataBufferID != "42" {
		t.Errorf("InitialDataBufferID = %q, want %q", buf.InitialDataBufferID, "42")
	}
}

const sampleInputLayoutXML = `<root>
<chunk name="ID3D11Device::CreateInputLayout">
  <array name="pInputElementDescs">
    <struct>
      <string name="SemanticName">POSITION</string>
      <uint name="SemanticIndex">0</uint>
      <enum name="Format" string="DXGI_FORMAT_R32G32B32_FLOAT">6</enum>
      <uint name="InputSlot">0</uint>
      <uint name="AlignedByteOffset">0</uint>
    </struct>
    <struct>
      <string name="SemanticName">TEXCOORD</string>
      <uint name="SemanticIndex">0</uint>
      <enum name="Format" string="DXGI_FORMAT_R32G32_FLOAT">16</enum>
      <uint name="InputSlot">0</uint>
      <uint name="AlignedByteOffset">16</uint>
    </struct>
  </array>
  <ResourceId name="pInputLayout">9999</ResourceId>
</chunk>
</root>`

func TestParseCreateInputLayoutChunk(t *testing.T) {
	report, err := parseMeshXML(strings.NewReader(sampleInputLayoutXML))
	if err != nil {
		t.Fatalf("parseMeshXML: %v", err)
	}
	layout, ok := report.InputLayouts["9999"]
	if !ok {
		t.Fatalf("input layout 9999 not parsed; got %v", report.InputLayouts)
	}
	if len(layout.Elements) != 2 {
		t.Fatalf("Elements len = %d, want 2", len(layout.Elements))
	}
	pos := layout.Elements[0]
	if pos.SemanticName != "POSITION" || pos.Format != "DXGI_FORMAT_R32G32B32_FLOAT" {
		t.Errorf("first element = %+v, want POSITION R32G32B32_FLOAT", pos)
	}
	if pos.AlignedByteOffset != 0 || pos.InputSlot != 0 {
		t.Errorf("first element slot/offset = (%d, %d), want (0, 0)", pos.InputSlot, pos.AlignedByteOffset)
	}
}

const sampleDrawCallXML = `<root>
<chunk name="ID3D11Device::CreateBuffer">
  <struct><uint name="ByteWidth">1024</uint>
  <enum name="BindFlags" string="D3D11_BIND_VERTEX_BUFFER">1</enum></struct>
  <ResourceId name="pBuffer">100</ResourceId>
  <buffer name="InitialData" byteLength="1024">1</buffer>
</chunk>
<chunk name="ID3D11Device::CreateBuffer">
  <struct><uint name="ByteWidth">512</uint>
  <enum name="BindFlags" string="D3D11_BIND_INDEX_BUFFER">2</enum></struct>
  <ResourceId name="pBuffer">200</ResourceId>
  <buffer name="InitialData" byteLength="512">2</buffer>
</chunk>
<chunk name="ID3D11DeviceContext::IASetVertexBuffers">
  <uint name="StartSlot">0</uint>
  <array name="ppVertexBuffers"><ResourceId>100</ResourceId></array>
  <array name="pStrides"><uint>32</uint></array>
  <array name="pOffsets"><uint>0</uint></array>
</chunk>
<chunk name="ID3D11DeviceContext::IASetIndexBuffer">
  <ResourceId name="pIndexBuffer">200</ResourceId>
  <enum name="Format" string="DXGI_FORMAT_R16_UINT">57</enum>
  <uint name="Offset">0</uint>
</chunk>
<chunk name="ID3D11DeviceContext::IASetInputLayout">
  <ResourceId name="pInputLayout">9999</ResourceId>
</chunk>
<chunk name="ID3D11DeviceContext::DrawIndexed">
  <uint name="IndexCount">300</uint>
  <uint name="StartIndexLocation">0</uint>
  <int name="BaseVertexLocation">0</int>
</chunk>
</root>`

func TestParseDrawCallPairing(t *testing.T) {
	report, err := parseMeshXML(strings.NewReader(sampleDrawCallXML))
	if err != nil {
		t.Fatalf("parseMeshXML: %v", err)
	}
	if len(report.DrawCalls) != 1 {
		t.Fatalf("DrawCalls len = %d, want 1", len(report.DrawCalls))
	}
	dc := report.DrawCalls[0]
	if dc.IndexCount != 300 {
		t.Errorf("IndexCount = %d, want 300", dc.IndexCount)
	}
	if dc.IndexBufferID != "200" || dc.IndexBufferFormat != "DXGI_FORMAT_R16_UINT" {
		t.Errorf("index buffer = (%q, %q)", dc.IndexBufferID, dc.IndexBufferFormat)
	}
	if len(dc.VertexBuffers) != 1 || dc.VertexBuffers[0].BufferID != "100" {
		t.Errorf("vertex buffers = %+v", dc.VertexBuffers)
	}
	if dc.VertexBuffers[0].Stride != 32 {
		t.Errorf("vb stride = %d, want 32", dc.VertexBuffers[0].Stride)
	}
	if dc.InputLayoutID != "9999" {
		t.Errorf("InputLayoutID = %q, want 9999", dc.InputLayoutID)
	}
}

// fakeBufferReader implements the minimal surface needed by BuildMeshes:
// ReadBuffer(id) returning bytes for a buffer ID.
type fakeBufferReader struct{ bytesByID map[string][]byte }

func (f *fakeBufferReader) ReadBuffer(id string) ([]byte, error) {
	return f.bytesByID[id], nil
}

func TestBuildMeshesDedupesByHash(t *testing.T) {
	vbBytes := []byte("vertex-data-aaaa")
	ibBytes := []byte("index-data-bbbb")
	report := &MeshReport{
		Buffers: map[string]BufferInfo{
			"100": {ResourceID: "100", ByteWidth: len(vbBytes), BindFlags: "D3D11_BIND_VERTEX_BUFFER", InitialDataBufferID: "vb-blob"},
			"200": {ResourceID: "200", ByteWidth: len(ibBytes), BindFlags: "D3D11_BIND_INDEX_BUFFER", InitialDataBufferID: "ib-blob"},
		},
		DrawCalls: []DrawCall{
			{
				IndexCount: 300, IndexBufferID: "200", IndexBufferFormat: "DXGI_FORMAT_R16_UINT",
				VertexBuffers: []DrawCallVertexBuffer{{Slot: 0, BufferID: "100", Stride: 32}},
			},
			// second draw reusing the same mesh → should dedupe
			{
				IndexCount: 300, IndexBufferID: "200", IndexBufferFormat: "DXGI_FORMAT_R16_UINT",
				VertexBuffers: []DrawCallVertexBuffer{{Slot: 0, BufferID: "100", Stride: 32}},
			},
		},
	}
	reader := &fakeBufferReader{bytesByID: map[string][]byte{
		"vb-blob": vbBytes,
		"ib-blob": ibBytes,
	}}
	meshes, err := BuildMeshes(report, reader)
	if err != nil {
		t.Fatalf("BuildMeshes: %v", err)
	}
	if len(meshes) != 1 {
		t.Fatalf("meshes len = %d, want 1 (dedup)", len(meshes))
	}
	if meshes[0].DrawCallCount != 2 {
		t.Errorf("DrawCallCount = %d, want 2", meshes[0].DrawCallCount)
	}
	if meshes[0].IndexCount != 300 {
		t.Errorf("IndexCount = %d, want 300", meshes[0].IndexCount)
	}
	if meshes[0].VertexBufferBytes != len(vbBytes) {
		t.Errorf("VertexBufferBytes = %d, want %d", meshes[0].VertexBufferBytes, len(vbBytes))
	}
	if meshes[0].IndexBufferBytes != len(ibBytes) {
		t.Errorf("IndexBufferBytes = %d, want %d", meshes[0].IndexBufferBytes, len(ibBytes))
	}

	// Full 64-char SHA-256 of vb|ib — the UI truncates for display, but
	// internally we keep the full digest to avoid silent collisions.
	hasher := sha256.New()
	hasher.Write(vbBytes)
	hasher.Write(ibBytes)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))
	if meshes[0].Hash != expectedHash {
		t.Errorf("Hash = %q, want %q", meshes[0].Hash, expectedHash)
	}
}

func TestParseCreateShaderResourceViewBuildsMap(t *testing.T) {
	xmlData := `<rdc>
<chunk name="ID3D11Device::CreateShaderResourceView">
  <ResourceId name="pResource">12345</ResourceId>
  <ResourceId name="ppSRView">99001</ResourceId>
</chunk>
<chunk name="ID3D11Device::CreateShaderResourceView">
  <ResourceId name="pResource">12346</ResourceId>
  <ResourceId name="ppSRView">99002</ResourceId>
</chunk>
</rdc>`
	report, err := parseMeshXML(strings.NewReader(xmlData))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := report.SRVToTexture["99001"]; got != "12345" {
		t.Errorf("SRV 99001 → %q, want 12345", got)
	}
	if got := report.SRVToTexture["99002"]; got != "12346" {
		t.Errorf("SRV 99002 → %q, want 12346", got)
	}
}
