package renderdoc

import (
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
