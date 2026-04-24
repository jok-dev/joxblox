# RenderDoc Meshes View Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Meshes" sub-tab alongside the existing Textures view in the RenderDoc tab. Lists unique meshes (VB+IB pairs deduped by content hash) with stats and a 3D preview.

**Architecture:** New `renderdoc.MeshReport` parsed from the same `.zip.xml` that feeds the existing `Report`. A binding-state replay over chunks pairs `DrawIndexed` with its bound VBs/IB. Rows dedupe by SHA-256 of concatenated buffer bytes. Preview decodes `R32G32B32_FLOAT` positions and feeds the existing `ui.MeshPreviewWidget`.

**Tech Stack:** Go + Fyne v2, `encoding/xml` streaming decoder, existing `renderdoc.BufferStore` (ZIP-backed), existing `ui.MeshPreviewWidget` (raylib-backed).

**Spec:** [docs/superpowers/specs/2026-04-24-renderdoc-meshes-design.md](docs/superpowers/specs/2026-04-24-renderdoc-meshes-design.md)

---

## Task 1: Buffer + InputLayout parsers

Parse `CreateBuffer` and `CreateInputLayout` chunks into typed records. These feed the binding-replay pass in Task 2.

**Files:**
- Create: `internal/renderdoc/meshes.go`
- Create: `internal/renderdoc/meshes_test.go`

- [ ] **Step 1: Write the failing test for CreateBuffer parsing**

File: `internal/renderdoc/meshes_test.go`

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/renderdoc/ -run TestParseCreateBufferChunk -v`
Expected: FAIL (`parseMeshXML` and related symbols undefined).

- [ ] **Step 3: Implement CreateBuffer parsing**

File: `internal/renderdoc/meshes.go`

```go
package renderdoc

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
)

// BufferInfo captures one ID3D11Device::CreateBuffer chunk: the created
// buffer's resource ID, size, bind flags, and the ID of its InitialData
// buffer blob (used by BufferStore.ReadBuffer for vertex/index contents).
type BufferInfo struct {
	ResourceID          string
	ByteWidth           int
	Usage               string
	BindFlags           string
	InitialDataBufferID string
	InitialDataLength   int
}

func (b BufferInfo) IsVertexBuffer() bool {
	return strings.Contains(b.BindFlags, "VERTEX_BUFFER")
}

func (b BufferInfo) IsIndexBuffer() bool {
	return strings.Contains(b.BindFlags, "INDEX_BUFFER")
}

// InputLayoutElement is one D3D11_INPUT_ELEMENT_DESC.
type InputLayoutElement struct {
	SemanticName      string
	SemanticIndex     int
	Format            string
	InputSlot         int
	AlignedByteOffset int
}

// InputLayoutInfo groups all elements for one CreateInputLayout call.
type InputLayoutInfo struct {
	ResourceID string
	Elements   []InputLayoutElement
}

// MeshReport collects the parse output needed to build the Meshes view.
// Buffers and InputLayouts are keyed by resource ID string (the integer
// inside <ResourceId>…</ResourceId> elements, as a string).
type MeshReport struct {
	Buffers      map[string]BufferInfo
	InputLayouts map[string]InputLayoutInfo
}

// ParseMeshReportFromXMLFile parses the capture XML file at path and
// returns a MeshReport. Mirrors ParseCaptureXMLFile's role for textures.
func ParseMeshReportFromXMLFile(path string) (*MeshReport, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open mesh xml: %w", err)
	}
	defer file.Close()
	return parseMeshXML(file)
}

func parseMeshXML(reader io.Reader) (*MeshReport, error) {
	decoder := xml.NewDecoder(reader)
	report := &MeshReport{
		Buffers:      map[string]BufferInfo{},
		InputLayouts: map[string]InputLayoutInfo{},
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("xml parse: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "chunk" {
			continue
		}
		switch attr(start, "name") {
		case "ID3D11Device::CreateBuffer":
			info, parseErr := parseCreateBufferChunk(decoder)
			if parseErr != nil {
				return nil, parseErr
			}
			if info != nil && info.ResourceID != "" {
				report.Buffers[info.ResourceID] = *info
			}
		}
	}
	return report, nil
}

func parseCreateBufferChunk(decoder *xml.Decoder) (*BufferInfo, error) {
	info := &BufferInfo{}
	depth := 1
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("read createbuffer: %w", err)
		}
		switch typed := token.(type) {
		case xml.StartElement:
			depth++
			switch typed.Name.Local {
			case "uint":
				name := attr(typed, "name")
				value, readErr := readIntElement(decoder)
				if readErr != nil {
					return nil, readErr
				}
				depth--
				switch name {
				case "ByteWidth":
					info.ByteWidth = value
				case "InitialDataLength":
					info.InitialDataLength = value
				}
			case "enum":
				name := attr(typed, "name")
				str := attr(typed, "string")
				if skipErr := skipElement(decoder); skipErr != nil {
					return nil, skipErr
				}
				depth--
				switch name {
				case "Usage":
					info.Usage = str
				case "BindFlags":
					info.BindFlags = str
				}
			case "ResourceId":
				if attr(typed, "name") == "pBuffer" {
					value, readErr := readTextElement(decoder)
					if readErr != nil {
						return nil, readErr
					}
					info.ResourceID = strings.TrimSpace(value)
				} else if skipErr := skipElement(decoder); skipErr != nil {
					return nil, skipErr
				}
				depth--
			case "buffer":
				if attr(typed, "name") == "InitialData" {
					value, readErr := readTextElement(decoder)
					if readErr != nil {
						return nil, readErr
					}
					info.InitialDataBufferID = strings.TrimSpace(value)
				} else if skipErr := skipElement(decoder); skipErr != nil {
					return nil, skipErr
				}
				depth--
			}
		case xml.EndElement:
			depth--
		}
	}
	if info.ResourceID == "" {
		return nil, nil
	}
	return info, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/renderdoc/ -run TestParseCreateBufferChunk -v`
Expected: PASS.

- [ ] **Step 5: Add CreateInputLayout failing test**

Append to `meshes_test.go`:

```go
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
```

- [ ] **Step 6: Run test — should fail**

Run: `go test ./internal/renderdoc/ -run TestParseCreateInputLayoutChunk -v`
Expected: FAIL (input layouts still empty).

- [ ] **Step 7: Implement CreateInputLayout parsing**

In `meshes.go`, add to the `switch attr(start, "name")` in `parseMeshXML`:

```go
case "ID3D11Device::CreateInputLayout":
    info, parseErr := parseCreateInputLayoutChunk(decoder)
    if parseErr != nil {
        return nil, parseErr
    }
    if info != nil && info.ResourceID != "" {
        report.InputLayouts[info.ResourceID] = *info
    }
```

Add this function at the bottom of the file:

```go
func parseCreateInputLayoutChunk(decoder *xml.Decoder) (*InputLayoutInfo, error) {
	info := &InputLayoutInfo{}
	depth := 1
	var currentElement *InputLayoutElement
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("read createinputlayout: %w", err)
		}
		switch typed := token.(type) {
		case xml.StartElement:
			depth++
			switch typed.Name.Local {
			case "struct":
				// Starting a new D3D11_INPUT_ELEMENT_DESC.
				currentElement = &InputLayoutElement{}
			case "string":
				if attr(typed, "name") == "SemanticName" && currentElement != nil {
					value, readErr := readTextElement(decoder)
					if readErr != nil {
						return nil, readErr
					}
					currentElement.SemanticName = strings.TrimSpace(value)
				} else if skipErr := skipElement(decoder); skipErr != nil {
					return nil, skipErr
				}
				depth--
			case "uint":
				name := attr(typed, "name")
				value, readErr := readIntElement(decoder)
				if readErr != nil {
					return nil, readErr
				}
				depth--
				if currentElement != nil {
					switch name {
					case "SemanticIndex":
						currentElement.SemanticIndex = value
					case "InputSlot":
						currentElement.InputSlot = value
					case "AlignedByteOffset":
						currentElement.AlignedByteOffset = value
					}
				}
			case "enum":
				name := attr(typed, "name")
				str := attr(typed, "string")
				if skipErr := skipElement(decoder); skipErr != nil {
					return nil, skipErr
				}
				depth--
				if name == "Format" && currentElement != nil {
					currentElement.Format = str
				}
			case "ResourceId":
				if attr(typed, "name") == "pInputLayout" {
					value, readErr := readTextElement(decoder)
					if readErr != nil {
						return nil, readErr
					}
					info.ResourceID = strings.TrimSpace(value)
				} else if skipErr := skipElement(decoder); skipErr != nil {
					return nil, skipErr
				}
				depth--
			}
		case xml.EndElement:
			depth--
			if typed.Name.Local == "struct" && currentElement != nil {
				info.Elements = append(info.Elements, *currentElement)
				currentElement = nil
			}
		}
	}
	if info.ResourceID == "" {
		return nil, nil
	}
	return info, nil
}
```

- [ ] **Step 8: Run all tests in the package**

Run: `go test ./internal/renderdoc/ -v`
Expected: all green, including the two new tests.

- [ ] **Step 9: Commit**

```bash
git add internal/renderdoc/meshes.go internal/renderdoc/meshes_test.go
git commit -m "feat(renderdoc): parse CreateBuffer and CreateInputLayout chunks"
```

---

## Task 2: Binding-state replay + DrawIndexed pairing

Walk the XML in order, tracking the currently-bound VBs, IB, and input layout. At each `DrawIndexed`/`DrawIndexedInstanced`, record a `DrawCall` capturing those bindings and the index count.

**Files:**
- Modify: `internal/renderdoc/meshes.go`
- Modify: `internal/renderdoc/meshes_test.go`

- [ ] **Step 1: Add failing test for draw-call pairing**

Append to `meshes_test.go`:

```go
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
```

- [ ] **Step 2: Run — fails (DrawCalls field doesn't exist)**

Run: `go test ./internal/renderdoc/ -run TestParseDrawCallPairing -v`
Expected: compile error or FAIL.

- [ ] **Step 3: Add types + binding replay in meshes.go**

Add these types to `meshes.go` (near the top, after `InputLayoutInfo`):

```go
// DrawCallVertexBuffer records one bound VB at the time of a draw.
type DrawCallVertexBuffer struct {
	Slot     int
	BufferID string
	Stride   int
	Offset   int
}

// DrawCall captures a DrawIndexed/DrawIndexedInstanced event with the
// buffer + input-layout bindings live at that moment.
type DrawCall struct {
	IndexCount         int
	StartIndexLocation int
	BaseVertexLocation int
	InstanceCount      int // 1 for DrawIndexed
	IndexBufferID      string
	IndexBufferFormat  string
	IndexBufferOffset  int
	VertexBuffers      []DrawCallVertexBuffer
	InputLayoutID      string
}
```

Add to `MeshReport`:

```go
type MeshReport struct {
	Buffers      map[string]BufferInfo
	InputLayouts map[string]InputLayoutInfo
	DrawCalls    []DrawCall
}
```

In `parseMeshXML`, add binding-state locals before the for-loop:

```go
var currentVBs []DrawCallVertexBuffer
var currentIB struct {
	id, format string
	offset     int
}
var currentLayoutID string
```

Inside the `switch attr(start, "name")`, add these cases:

```go
case "ID3D11DeviceContext::IASetVertexBuffers":
    vbs, parseErr := parseIASetVertexBuffersChunk(decoder)
    if parseErr != nil {
        return nil, parseErr
    }
    currentVBs = mergeVertexBufferBindings(currentVBs, vbs)
case "ID3D11DeviceContext::IASetIndexBuffer":
    ib, parseErr := parseIASetIndexBufferChunk(decoder)
    if parseErr != nil {
        return nil, parseErr
    }
    currentIB.id = ib.ID
    currentIB.format = ib.Format
    currentIB.offset = ib.Offset
case "ID3D11DeviceContext::IASetInputLayout":
    id, parseErr := parseIASetInputLayoutChunk(decoder)
    if parseErr != nil {
        return nil, parseErr
    }
    currentLayoutID = id
case "ID3D11DeviceContext::DrawIndexed", "ID3D11DeviceContext::DrawIndexedInstanced":
    dc, parseErr := parseDrawIndexedChunk(decoder)
    if parseErr != nil {
        return nil, parseErr
    }
    if dc == nil {
        break
    }
    dc.IndexBufferID = currentIB.id
    dc.IndexBufferFormat = currentIB.format
    dc.IndexBufferOffset = currentIB.offset
    dc.InputLayoutID = currentLayoutID
    dc.VertexBuffers = append([]DrawCallVertexBuffer(nil), currentVBs...)
    report.DrawCalls = append(report.DrawCalls, *dc)
```

Add the four new chunk parsers at the bottom of the file:

```go
type indexBufferBinding struct {
	ID, Format string
	Offset     int
}

func parseIASetVertexBuffersChunk(decoder *xml.Decoder) ([]DrawCallVertexBuffer, error) {
	depth := 1
	startSlot := 0
	var bufferIDs []string
	var strides []int
	var offsets []int
	inArray := ""
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("read iasetvertexbuffers: %w", err)
		}
		switch typed := token.(type) {
		case xml.StartElement:
			depth++
			switch typed.Name.Local {
			case "array":
				inArray = attr(typed, "name")
			case "uint":
				name := attr(typed, "name")
				value, readErr := readIntElement(decoder)
				if readErr != nil {
					return nil, readErr
				}
				depth--
				if name == "StartSlot" {
					startSlot = value
				} else if inArray == "pStrides" {
					strides = append(strides, value)
				} else if inArray == "pOffsets" {
					offsets = append(offsets, value)
				}
			case "ResourceId":
				value, readErr := readTextElement(decoder)
				if readErr != nil {
					return nil, readErr
				}
				depth--
				if inArray == "ppVertexBuffers" {
					bufferIDs = append(bufferIDs, strings.TrimSpace(value))
				}
			}
		case xml.EndElement:
			depth--
			if typed.Name.Local == "array" {
				inArray = ""
			}
		}
	}
	result := make([]DrawCallVertexBuffer, 0, len(bufferIDs))
	for i, id := range bufferIDs {
		entry := DrawCallVertexBuffer{Slot: startSlot + i, BufferID: id}
		if i < len(strides) {
			entry.Stride = strides[i]
		}
		if i < len(offsets) {
			entry.Offset = offsets[i]
		}
		result = append(result, entry)
	}
	return result, nil
}

func parseIASetIndexBufferChunk(decoder *xml.Decoder) (indexBufferBinding, error) {
	var out indexBufferBinding
	depth := 1
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return out, fmt.Errorf("read iasetindexbuffer: %w", err)
		}
		switch typed := token.(type) {
		case xml.StartElement:
			depth++
			switch typed.Name.Local {
			case "ResourceId":
				if attr(typed, "name") == "pIndexBuffer" {
					value, readErr := readTextElement(decoder)
					if readErr != nil {
						return out, readErr
					}
					out.ID = strings.TrimSpace(value)
				} else if skipErr := skipElement(decoder); skipErr != nil {
					return out, skipErr
				}
				depth--
			case "enum":
				if attr(typed, "name") == "Format" {
					out.Format = attr(typed, "string")
				}
				if skipErr := skipElement(decoder); skipErr != nil {
					return out, skipErr
				}
				depth--
			case "uint":
				if attr(typed, "name") == "Offset" {
					value, readErr := readIntElement(decoder)
					if readErr != nil {
						return out, readErr
					}
					out.Offset = value
					depth--
				} else {
					if skipErr := skipElement(decoder); skipErr != nil {
						return out, skipErr
					}
					depth--
				}
			}
		case xml.EndElement:
			depth--
		}
	}
	return out, nil
}

func parseIASetInputLayoutChunk(decoder *xml.Decoder) (string, error) {
	depth := 1
	id := ""
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return "", fmt.Errorf("read iasetinputlayout: %w", err)
		}
		switch typed := token.(type) {
		case xml.StartElement:
			depth++
			if typed.Name.Local == "ResourceId" && attr(typed, "name") == "pInputLayout" {
				value, readErr := readTextElement(decoder)
				if readErr != nil {
					return "", readErr
				}
				id = strings.TrimSpace(value)
				depth--
			} else if typed.Name.Local == "ResourceId" {
				if skipErr := skipElement(decoder); skipErr != nil {
					return "", skipErr
				}
				depth--
			}
		case xml.EndElement:
			depth--
		}
	}
	return id, nil
}

func parseDrawIndexedChunk(decoder *xml.Decoder) (*DrawCall, error) {
	dc := &DrawCall{InstanceCount: 1}
	depth := 1
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("read drawindexed: %w", err)
		}
		switch typed := token.(type) {
		case xml.StartElement:
			depth++
			switch typed.Name.Local {
			case "uint", "int":
				name := attr(typed, "name")
				value, readErr := readIntElement(decoder)
				if readErr != nil {
					return nil, readErr
				}
				depth--
				switch name {
				case "IndexCount", "IndexCountPerInstance":
					dc.IndexCount = value
				case "StartIndexLocation":
					dc.StartIndexLocation = value
				case "BaseVertexLocation":
					dc.BaseVertexLocation = value
				case "InstanceCount":
					dc.InstanceCount = value
				}
			}
		case xml.EndElement:
			depth--
		}
	}
	return dc, nil
}

// mergeVertexBufferBindings merges new slot bindings into the existing
// current state. D3D11 IASetVertexBuffers replaces bindings starting at
// StartSlot, leaving other slots intact.
func mergeVertexBufferBindings(current, incoming []DrawCallVertexBuffer) []DrawCallVertexBuffer {
	result := append([]DrawCallVertexBuffer(nil), current...)
	for _, binding := range incoming {
		replaced := false
		for i := range result {
			if result[i].Slot == binding.Slot {
				result[i] = binding
				replaced = true
				break
			}
		}
		if !replaced {
			result = append(result, binding)
		}
	}
	return result
}
```

- [ ] **Step 4: Run test — passes**

Run: `go test ./internal/renderdoc/ -run TestParseDrawCallPairing -v`
Expected: PASS.

- [ ] **Step 5: Run whole package**

Run: `go test ./internal/renderdoc/ -v`
Expected: all green.

- [ ] **Step 6: Commit**

```bash
git add internal/renderdoc/meshes.go internal/renderdoc/meshes_test.go
git commit -m "feat(renderdoc): pair draw calls with bound VBs/IB/input layout"
```

---

## Task 3: Mesh dedup by content hash

Walk `DrawCalls`, read the VB+IB bytes via `BufferStore`, hash, and group. Produce `MeshInfo` rows.

**Files:**
- Modify: `internal/renderdoc/meshes.go`
- Modify: `internal/renderdoc/meshes_test.go`

- [ ] **Step 1: Add failing test for MeshInfo building**

Append to `meshes_test.go`:

```go
import "crypto/sha256"
import "encoding/hex"

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

	// Verify hash shape: 16 hex chars, matches SHA-256 prefix of vb|ib.
	hasher := sha256.New()
	hasher.Write(vbBytes)
	hasher.Write(ibBytes)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))[:16]
	if meshes[0].Hash != expectedHash {
		t.Errorf("Hash = %q, want %q", meshes[0].Hash, expectedHash)
	}
}
```

- [ ] **Step 2: Run — should fail**

Run: `go test ./internal/renderdoc/ -run TestBuildMeshesDedupesByHash -v`
Expected: FAIL (`BuildMeshes`, `MeshInfo` undefined).

- [ ] **Step 3: Implement MeshInfo and BuildMeshes**

Add to `meshes.go`:

```go
import (
	"crypto/sha256"
	"encoding/hex"
	// (plus the existing imports)
)

// MeshInfo aggregates one deduplicated mesh (VB+IB pair) across all draw
// calls that used it.
type MeshInfo struct {
	Hash               string
	FirstResourceID    string // resource ID of the first VB slot for display
	VertexBuffers      []DrawCallVertexBuffer
	IndexBufferID      string
	IndexBufferFormat  string
	VertexBufferBytes  int
	IndexBufferBytes   int
	IndexCount         int   // max IndexCount seen across draws — represents the full mesh
	DrawCallCount      int
	InputLayoutID      string // first non-empty layout id seen
}

// BufferReader is the minimal surface needed by BuildMeshes. Satisfied
// by *BufferStore in production.
type BufferReader interface {
	ReadBuffer(id string) ([]byte, error)
}

// BuildMeshes walks report.DrawCalls, hashes each VB+IB byte set, and
// returns one MeshInfo per unique hash. DrawCallCount counts duplicates.
func BuildMeshes(report *MeshReport, reader BufferReader) ([]MeshInfo, error) {
	if report == nil {
		return nil, nil
	}
	byHash := map[string]*MeshInfo{}
	var order []string
	for _, dc := range report.DrawCalls {
		if len(dc.VertexBuffers) == 0 || dc.IndexBufferID == "" {
			continue
		}
		hash, vbBytes, ibBytes, err := hashMeshBuffers(dc, report.Buffers, reader)
		if err != nil {
			return nil, err
		}
		if hash == "" {
			continue
		}
		mesh, exists := byHash[hash]
		if !exists {
			mesh = &MeshInfo{
				Hash:              hash,
				FirstResourceID:   dc.VertexBuffers[0].BufferID,
				VertexBuffers:     append([]DrawCallVertexBuffer(nil), dc.VertexBuffers...),
				IndexBufferID:     dc.IndexBufferID,
				IndexBufferFormat: dc.IndexBufferFormat,
				VertexBufferBytes: vbBytes,
				IndexBufferBytes:  ibBytes,
				InputLayoutID:     dc.InputLayoutID,
			}
			byHash[hash] = mesh
			order = append(order, hash)
		}
		mesh.DrawCallCount++
		if dc.IndexCount > mesh.IndexCount {
			mesh.IndexCount = dc.IndexCount
		}
		if mesh.InputLayoutID == "" {
			mesh.InputLayoutID = dc.InputLayoutID
		}
	}
	out := make([]MeshInfo, 0, len(order))
	for _, hash := range order {
		out = append(out, *byHash[hash])
	}
	return out, nil
}

func hashMeshBuffers(dc DrawCall, buffers map[string]BufferInfo, reader BufferReader) (string, int, int, error) {
	hasher := sha256.New()
	vbBytes := 0
	for _, vb := range dc.VertexBuffers {
		buf, ok := buffers[vb.BufferID]
		if !ok || buf.InitialDataBufferID == "" {
			continue
		}
		data, err := reader.ReadBuffer(buf.InitialDataBufferID)
		if err != nil {
			return "", 0, 0, fmt.Errorf("read VB %s: %w", vb.BufferID, err)
		}
		hasher.Write(data)
		vbBytes += len(data)
	}
	ibBuf, ok := buffers[dc.IndexBufferID]
	if !ok || ibBuf.InitialDataBufferID == "" {
		return "", 0, 0, nil
	}
	ibData, err := reader.ReadBuffer(ibBuf.InitialDataBufferID)
	if err != nil {
		return "", 0, 0, fmt.Errorf("read IB %s: %w", dc.IndexBufferID, err)
	}
	hasher.Write(ibData)
	return hex.EncodeToString(hasher.Sum(nil))[:16], vbBytes, len(ibData), nil
}
```

- [ ] **Step 4: Run — should pass**

Run: `go test ./internal/renderdoc/ -run TestBuildMeshesDedupesByHash -v`
Expected: PASS.

- [ ] **Step 5: Run whole package**

Run: `go test ./internal/renderdoc/ -v`
Expected: all green.

- [ ] **Step 6: Commit**

```bash
git add internal/renderdoc/meshes.go internal/renderdoc/meshes_test.go
git commit -m "feat(renderdoc): dedupe meshes by VB+IB content hash"
```

---

## Task 4: Position-stream decoder

Decode positions as `[]float32` XYZ triples for `R32G32B32_FLOAT`. Return a sentinel error for unsupported formats so the UI can show "preview not supported yet". Also decode indices as `[]uint32`.

**Files:**
- Create: `internal/renderdoc/decode_position.go`
- Create: `internal/renderdoc/decode_position_test.go`

- [ ] **Step 1: Write failing test**

File: `internal/renderdoc/decode_position_test.go`

```go
package renderdoc

import (
	"encoding/binary"
	"errors"
	"math"
	"testing"
)

func TestDecodePositionsR32G32B32Float(t *testing.T) {
	// Two vertices with stride 16 (12 bytes of position + 4 bytes of padding).
	buf := make([]byte, 32)
	binary.LittleEndian.PutUint32(buf[0:], math.Float32bits(1))
	binary.LittleEndian.PutUint32(buf[4:], math.Float32bits(2))
	binary.LittleEndian.PutUint32(buf[8:], math.Float32bits(3))
	binary.LittleEndian.PutUint32(buf[16:], math.Float32bits(4))
	binary.LittleEndian.PutUint32(buf[20:], math.Float32bits(5))
	binary.LittleEndian.PutUint32(buf[24:], math.Float32bits(6))
	positions, err := DecodePositions(buf, "DXGI_FORMAT_R32G32B32_FLOAT", 0, 16)
	if err != nil {
		t.Fatalf("DecodePositions: %v", err)
	}
	want := []float32{1, 2, 3, 4, 5, 6}
	if len(positions) != len(want) {
		t.Fatalf("positions len = %d, want %d", len(positions), len(want))
	}
	for i, v := range want {
		if positions[i] != v {
			t.Errorf("positions[%d] = %f, want %f", i, positions[i], v)
		}
	}
}

func TestDecodePositionsUnsupportedFormat(t *testing.T) {
	_, err := DecodePositions([]byte{0}, "DXGI_FORMAT_R16G16B16A16_SNORM", 0, 8)
	if !errors.Is(err, ErrUnsupportedPositionFormat) {
		t.Errorf("err = %v, want ErrUnsupportedPositionFormat", err)
	}
}

func TestDecodeIndices16(t *testing.T) {
	buf := []byte{1, 0, 2, 0, 3, 0, 4, 0}
	indices, err := DecodeIndices(buf, "DXGI_FORMAT_R16_UINT")
	if err != nil {
		t.Fatalf("DecodeIndices: %v", err)
	}
	want := []uint32{1, 2, 3, 4}
	if len(indices) != len(want) {
		t.Fatalf("indices len = %d, want %d", len(indices), len(want))
	}
	for i, v := range want {
		if indices[i] != v {
			t.Errorf("indices[%d] = %d, want %d", i, indices[i], v)
		}
	}
}

func TestDecodeIndices32(t *testing.T) {
	buf := []byte{1, 0, 0, 0, 2, 0, 0, 0}
	indices, err := DecodeIndices(buf, "DXGI_FORMAT_R32_UINT")
	if err != nil {
		t.Fatalf("DecodeIndices: %v", err)
	}
	if len(indices) != 2 || indices[0] != 1 || indices[1] != 2 {
		t.Errorf("indices = %v, want [1 2]", indices)
	}
}
```

- [ ] **Step 2: Run — should fail (file doesn't exist)**

Run: `go test ./internal/renderdoc/ -run TestDecodePositions -v`
Expected: compile error.

- [ ] **Step 3: Implement decoder**

File: `internal/renderdoc/decode_position.go`

```go
package renderdoc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// ErrUnsupportedPositionFormat is returned when DecodePositions sees a
// DXGI format it can't decode. Callers (the meshes preview) show
// "format not supported yet" rather than failing outright.
var ErrUnsupportedPositionFormat = errors.New("unsupported position format")

// DecodePositions reads XYZ positions out of a vertex buffer byte slice
// using the given format, byte offset within each vertex, and stride.
// Returns a flat []float32{x0,y0,z0,x1,y1,z1,...}.
func DecodePositions(vertexBytes []byte, format string, alignedByteOffset, stride int) ([]float32, error) {
	switch format {
	case "DXGI_FORMAT_R32G32B32_FLOAT":
		return decodeR32G32B32Float(vertexBytes, alignedByteOffset, stride)
	}
	return nil, fmt.Errorf("%w: %s", ErrUnsupportedPositionFormat, format)
}

func decodeR32G32B32Float(vertexBytes []byte, offset, stride int) ([]float32, error) {
	if stride <= 0 {
		return nil, fmt.Errorf("invalid stride %d", stride)
	}
	if offset < 0 || offset+12 > stride {
		return nil, fmt.Errorf("position offset %d + 12 exceeds stride %d", offset, stride)
	}
	vertexCount := len(vertexBytes) / stride
	out := make([]float32, 0, vertexCount*3)
	for i := 0; i < vertexCount; i++ {
		base := i*stride + offset
		if base+12 > len(vertexBytes) {
			break
		}
		out = append(out,
			math.Float32frombits(binary.LittleEndian.Uint32(vertexBytes[base:])),
			math.Float32frombits(binary.LittleEndian.Uint32(vertexBytes[base+4:])),
			math.Float32frombits(binary.LittleEndian.Uint32(vertexBytes[base+8:])),
		)
	}
	return out, nil
}

// DecodeIndices reads indices from an index buffer byte slice in the
// given format, returning them as []uint32 for uniform downstream use.
func DecodeIndices(indexBytes []byte, format string) ([]uint32, error) {
	switch format {
	case "DXGI_FORMAT_R16_UINT":
		count := len(indexBytes) / 2
		out := make([]uint32, count)
		for i := 0; i < count; i++ {
			out[i] = uint32(binary.LittleEndian.Uint16(indexBytes[i*2:]))
		}
		return out, nil
	case "DXGI_FORMAT_R32_UINT":
		count := len(indexBytes) / 4
		out := make([]uint32, count)
		for i := 0; i < count; i++ {
			out[i] = binary.LittleEndian.Uint32(indexBytes[i*4:])
		}
		return out, nil
	}
	return nil, fmt.Errorf("unsupported index format: %s", format)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/renderdoc/ -v`
Expected: all green.

- [ ] **Step 5: Commit**

```bash
git add internal/renderdoc/decode_position.go internal/renderdoc/decode_position_test.go
git commit -m "feat(renderdoc): decode R32G32B32_FLOAT positions and uint16/32 indices"
```

---

## Task 5: Wrap existing RenderDoc tab in a sub-tab container

Move the existing textures UI into a "Textures" sub-tab and add an empty "Meshes" sub-tab placeholder, so we can add content in Task 6 without refactoring the loader flow twice.

**Files:**
- Modify: `internal/app/ui/tabs/renderdoc/renderdoc_tab.go`

- [ ] **Step 1: Find the current entry point**

Run: `grep -n "func NewRenderDocTab" internal/app/ui/tabs/renderdoc/renderdoc_tab.go`
Expected output:

```
57:func NewRenderDocTab(window fyne.Window) fyne.CanvasObject {
```

- [ ] **Step 2: Extract textures sub-tab builder**

In `renderdoc_tab.go`, rename `NewRenderDocTab` to `newTexturesSubTab` and have it return the same `fyne.CanvasObject` it does today (no behavior change — just rename).

Then add a new `NewRenderDocTab` that wraps it:

```go
// NewRenderDocTab builds the RenderDoc tab with two sub-tabs: Textures
// (existing UI) and Meshes (new).
func NewRenderDocTab(window fyne.Window) fyne.CanvasObject {
	textures := newTexturesSubTab(window)
	meshes := newMeshesSubTab(window)
	return container.NewAppTabs(
		container.NewTabItem("Textures", textures),
		container.NewTabItem("Meshes", meshes),
	)
}

// newMeshesSubTab is a placeholder for Task 6. It currently returns a
// simple label so the sub-tab is visible but empty.
func newMeshesSubTab(window fyne.Window) fyne.CanvasObject {
	return widget.NewLabel("Load a RenderDoc capture to view meshes (coming in Task 6).")
}
```

- [ ] **Step 3: Build and launch the app manually to sanity-check**

Run: `go build ./...`
Expected: compiles.

Run: `go run ./cmd/joxblox/` — click the RenderDoc tab, verify both sub-tabs appear, Textures still works, Meshes shows the placeholder.

(If you can't run the UI interactively, at least confirm `go build` succeeds and the `go test ./...` set still passes for packages that changed.)

- [ ] **Step 4: Commit**

```bash
git add internal/app/ui/tabs/renderdoc/renderdoc_tab.go
git commit -m "refactor(renderdoc-tab): wrap textures view in sub-tab container"
```

---

## Task 6: Meshes sub-tab UI

Load a `.rdc` (reusing the existing `ConvertToXML` + `BufferStore`), parse meshes, display the table, and wire the preview pane to `ui.MeshPreviewWidget`.

**Files:**
- Create: `internal/app/ui/tabs/renderdoc/meshes_view.go`
- Modify: `internal/app/ui/tabs/renderdoc/renderdoc_tab.go` (replace placeholder builder)

- [ ] **Step 1: Implement the meshes sub-tab**

File: `internal/app/ui/tabs/renderdoc/meshes_view.go`

```go
package renderdoctab

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"joxblox/internal/app/ui"
	"joxblox/internal/format"
	"joxblox/internal/renderdoc"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	nativeDialog "github.com/sqweek/dialog"
)

type meshesTabState struct {
	meshes         []renderdoc.MeshInfo
	displayMeshes  []renderdoc.MeshInfo
	report         *renderdoc.MeshReport
	bufferStore    *renderdoc.BufferStore
	xmlPath        string
	sortColumn     string
	sortDescending bool
	filterText     string
	selectedRow    int
}

var meshColumnHeaders = []string{"ID", "Verts", "Tris", "VB bytes", "IB bytes", "Draws", "Layout", "Hash"}

func newMeshesSubTab(window fyne.Window) fyne.CanvasObject {
	state := &meshesTabState{
		sortColumn:     "VB bytes",
		sortDescending: true,
		selectedRow:    -1,
	}

	pathLabel := widget.NewLabel("No capture loaded.")
	pathLabel.Wrapping = fyne.TextWrapWord
	summaryLabel := widget.NewLabel("")
	summaryLabel.Wrapping = fyne.TextWrapWord
	countLabel := widget.NewLabel("")

	progressBar := widget.NewProgressBarInfinite()
	progressBar.Hide()

	filterEntry := widget.NewEntry()
	filterEntry.SetPlaceHolder("Filter by hash or layout")

	previewWidget := ui.NewMeshPreviewWidget()
	previewInfoLabel := widget.NewMultiLineEntry()
	previewInfoLabel.Wrapping = fyne.TextWrapWord
	previewInfoLabel.SetText("Select a mesh to preview.")
	previewInfoLabel.Disable()

	previewPane := container.NewBorder(previewInfoLabel, nil, nil, nil, previewWidget)

	var table *widget.Table
	table = widget.NewTableWithHeaders(
		func() (int, int) { return len(state.displayMeshes), len(meshColumnHeaders) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.TableCellID, object fyne.CanvasObject) {
			label := object.(*widget.Label)
			if id.Row < 0 || id.Row >= len(state.displayMeshes) {
				label.SetText("")
				return
			}
			label.SetText(meshColumnValue(state.displayMeshes[id.Row], meshColumnHeaders[id.Col]))
		},
	)
	table.CreateHeader = func() fyne.CanvasObject { return widget.NewButton("", nil) }
	table.UpdateHeader = func(id widget.TableCellID, object fyne.CanvasObject) {
		button := object.(*widget.Button)
		if id.Row == -1 && id.Col >= 0 && id.Col < len(meshColumnHeaders) {
			name := meshColumnHeaders[id.Col]
			label := name
			if state.sortColumn == name {
				if state.sortDescending {
					label = name + " ▼"
				} else {
					label = name + " ▲"
				}
			}
			button.SetText(label)
			button.OnTapped = func() {
				if state.sortColumn == name {
					state.sortDescending = !state.sortDescending
				} else {
					state.sortColumn = name
					state.sortDescending = true
				}
				applyMeshSortAndFilter(state)
				table.Refresh()
			}
			return
		}
		if id.Col == -1 && id.Row >= 0 {
			button.SetText(strconv.Itoa(id.Row + 1))
		} else {
			button.SetText("")
		}
		button.OnTapped = nil
	}
	applyMeshColumnWidths(table)
	table.OnSelected = func(id widget.TableCellID) {
		if id.Row < 0 || id.Row >= len(state.displayMeshes) {
			return
		}
		state.selectedRow = id.Row
		mesh := state.displayMeshes[id.Row]
		loadMeshPreview(state, mesh, previewWidget, previewInfoLabel)
	}

	filterEntry.OnChanged = func(text string) {
		state.filterText = strings.TrimSpace(text)
		applyMeshSortAndFilter(state)
		table.Refresh()
		countLabel.SetText(fmt.Sprintf("Showing %d of %d meshes", len(state.displayMeshes), len(state.meshes)))
	}

	var loadButton *widget.Button
	onLoadFinished := func(report *renderdoc.MeshReport, meshes []renderdoc.MeshInfo, loadedPath, xmlPath string, newStore *renderdoc.BufferStore, loadErr error) {
		progressBar.Hide()
		if loadButton != nil {
			loadButton.Enable()
		}
		if loadErr != nil {
			pathLabel.SetText(fmt.Sprintf("Load failed: %s", loadedPath))
			fyneDialog.ShowError(loadErr, window)
			if newStore != nil {
				_ = newStore.Close()
				renderdoc.RemoveConvertedOutput(xmlPath)
			}
			return
		}
		if state.bufferStore != nil {
			_ = state.bufferStore.Close()
		}
		if state.xmlPath != "" {
			renderdoc.RemoveConvertedOutput(state.xmlPath)
		}
		state.report = report
		state.meshes = meshes
		state.bufferStore = newStore
		state.xmlPath = xmlPath
		state.selectedRow = -1
		applyMeshSortAndFilter(state)
		pathLabel.SetText(fmt.Sprintf("Loaded: %s", loadedPath))
		summaryLabel.SetText(buildMeshSummary(meshes))
		countLabel.SetText(fmt.Sprintf("Showing %d of %d meshes", len(state.displayMeshes), len(state.meshes)))
		previewWidget.Clear()
		previewInfoLabel.SetText("Select a mesh to preview.")
		table.Refresh()
	}

	loadButton = widget.NewButton("Load .rdc…", func() {
		go pickAndLoadMeshCapture(window, progressBar, loadButton, onLoadFinished)
	})

	header := container.NewVBox(
		container.NewBorder(nil, nil, nil, loadButton, pathLabel),
		summaryLabel,
		progressBar,
		filterEntry,
	)
	split := container.NewHSplit(table, previewPane)
	split.Offset = 0.55
	return container.NewBorder(header, countLabel, nil, nil, split)
}

func meshColumnValue(mesh renderdoc.MeshInfo, column string) string {
	switch column {
	case "ID":
		return mesh.FirstResourceID
	case "Verts":
		// We don't track vertex count directly — approximate from VB bytes / stride.
		stride := 0
		if len(mesh.VertexBuffers) > 0 {
			stride = mesh.VertexBuffers[0].Stride
		}
		if stride <= 0 {
			return "-"
		}
		return strconv.Itoa(mesh.VertexBufferBytes / stride)
	case "Tris":
		return strconv.Itoa(mesh.IndexCount / 3)
	case "VB bytes":
		return format.FormatSizeAuto64(int64(mesh.VertexBufferBytes))
	case "IB bytes":
		return format.FormatSizeAuto64(int64(mesh.IndexBufferBytes))
	case "Draws":
		return strconv.Itoa(mesh.DrawCallCount)
	case "Layout":
		return mesh.InputLayoutID
	case "Hash":
		return mesh.Hash
	}
	return ""
}

func applyMeshColumnWidths(table *widget.Table) {
	widths := map[int]float32{0: 80, 1: 80, 2: 80, 3: 100, 4: 100, 5: 70, 6: 90, 7: 150}
	for col, w := range widths {
		table.SetColumnWidth(col, w)
	}
}

func applyMeshSortAndFilter(state *meshesTabState) {
	lower := strings.ToLower(state.filterText)
	filtered := make([]renderdoc.MeshInfo, 0, len(state.meshes))
	for _, mesh := range state.meshes {
		if lower != "" {
			haystack := strings.ToLower(mesh.Hash + " " + mesh.InputLayoutID)
			if !strings.Contains(haystack, lower) {
				continue
			}
		}
		filtered = append(filtered, mesh)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		less := compareMeshes(filtered[i], filtered[j], state.sortColumn)
		if state.sortDescending {
			return !less
		}
		return less
	})
	state.displayMeshes = filtered
}

func compareMeshes(a, b renderdoc.MeshInfo, column string) bool {
	switch column {
	case "ID":
		return a.FirstResourceID < b.FirstResourceID
	case "Verts", "Tris", "VB bytes":
		return a.VertexBufferBytes < b.VertexBufferBytes
	case "IB bytes":
		return a.IndexBufferBytes < b.IndexBufferBytes
	case "Draws":
		return a.DrawCallCount < b.DrawCallCount
	case "Layout":
		return a.InputLayoutID < b.InputLayoutID
	case "Hash":
		return a.Hash < b.Hash
	}
	return false
}

func buildMeshSummary(meshes []renderdoc.MeshInfo) string {
	totalVB, totalIB, draws := int64(0), int64(0), 0
	for _, m := range meshes {
		totalVB += int64(m.VertexBufferBytes)
		totalIB += int64(m.IndexBufferBytes)
		draws += m.DrawCallCount
	}
	return fmt.Sprintf("%d unique meshes · VB %s · IB %s · %d draw calls total",
		len(meshes),
		format.FormatSizeAuto64(totalVB),
		format.FormatSizeAuto64(totalIB),
		draws,
	)
}

func loadMeshPreview(state *meshesTabState, mesh renderdoc.MeshInfo, viewer *ui.MeshPreviewWidget, infoLabel *widget.Entry) {
	infoLabel.SetText(fmt.Sprintf("Decoding %s · %d verts · %d tris…",
		mesh.FirstResourceID,
		vertexCountOf(mesh),
		mesh.IndexCount/3,
	))
	if state.bufferStore == nil {
		infoLabel.SetText("No capture loaded.")
		return
	}
	go func() {
		data, err := buildMeshPreviewData(state.report, mesh, state.bufferStore)
		fyne.Do(func() {
			if err != nil {
				if errors.Is(err, renderdoc.ErrUnsupportedPositionFormat) {
					infoLabel.SetText(fmt.Sprintf("%s · preview format not supported yet", mesh.FirstResourceID))
				} else {
					infoLabel.SetText(fmt.Sprintf("%s · decode failed: %s", mesh.FirstResourceID, err.Error()))
				}
				viewer.Clear()
				return
			}
			infoLabel.SetText(fmt.Sprintf("%s · %d verts · %d tris · %s VB · hash %s",
				mesh.FirstResourceID,
				vertexCountOf(mesh),
				mesh.IndexCount/3,
				format.FormatSizeAuto64(int64(mesh.VertexBufferBytes)),
				mesh.Hash,
			))
			viewer.SetData(data)
		})
	}()
}

func vertexCountOf(mesh renderdoc.MeshInfo) int {
	if len(mesh.VertexBuffers) == 0 || mesh.VertexBuffers[0].Stride <= 0 {
		return 0
	}
	return mesh.VertexBufferBytes / mesh.VertexBuffers[0].Stride
}

func buildMeshPreviewData(report *renderdoc.MeshReport, mesh renderdoc.MeshInfo, store *renderdoc.BufferStore) (ui.MeshPreviewData, error) {
	if report == nil || len(mesh.VertexBuffers) == 0 {
		return ui.MeshPreviewData{}, fmt.Errorf("no geometry for mesh %s", mesh.FirstResourceID)
	}
	layout, ok := report.InputLayouts[mesh.InputLayoutID]
	if !ok {
		return ui.MeshPreviewData{}, fmt.Errorf("input layout %s not found", mesh.InputLayoutID)
	}
	var positionElement *renderdoc.InputLayoutElement
	for i := range layout.Elements {
		if strings.EqualFold(layout.Elements[i].SemanticName, "POSITION") && layout.Elements[i].SemanticIndex == 0 {
			positionElement = &layout.Elements[i]
			break
		}
	}
	if positionElement == nil {
		return ui.MeshPreviewData{}, fmt.Errorf("no POSITION semantic in layout %s", mesh.InputLayoutID)
	}

	// Find the VB matching the position's InputSlot.
	var positionVB renderdoc.DrawCallVertexBuffer
	found := false
	for _, vb := range mesh.VertexBuffers {
		if vb.Slot == positionElement.InputSlot {
			positionVB = vb
			found = true
			break
		}
	}
	if !found {
		return ui.MeshPreviewData{}, fmt.Errorf("no VB bound to position slot %d", positionElement.InputSlot)
	}

	vbBufInfo, ok := report.Buffers[positionVB.BufferID]
	if !ok || vbBufInfo.InitialDataBufferID == "" {
		return ui.MeshPreviewData{}, fmt.Errorf("VB %s has no InitialData", positionVB.BufferID)
	}
	vbBytes, err := store.ReadBuffer(vbBufInfo.InitialDataBufferID)
	if err != nil {
		return ui.MeshPreviewData{}, fmt.Errorf("read VB: %w", err)
	}
	positions, err := renderdoc.DecodePositions(vbBytes, positionElement.Format, positionElement.AlignedByteOffset, positionVB.Stride)
	if err != nil {
		return ui.MeshPreviewData{}, err
	}

	ibBufInfo, ok := report.Buffers[mesh.IndexBufferID]
	if !ok || ibBufInfo.InitialDataBufferID == "" {
		return ui.MeshPreviewData{}, fmt.Errorf("IB %s has no InitialData", mesh.IndexBufferID)
	}
	ibBytes, err := store.ReadBuffer(ibBufInfo.InitialDataBufferID)
	if err != nil {
		return ui.MeshPreviewData{}, fmt.Errorf("read IB: %w", err)
	}
	indices, err := renderdoc.DecodeIndices(ibBytes, mesh.IndexBufferFormat)
	if err != nil {
		return ui.MeshPreviewData{}, err
	}
	triangleCount := uint32(len(indices) / 3)
	return ui.MeshPreviewData{
		RawPositions:         positions,
		RawIndices:           indices,
		TriangleCount:        triangleCount,
		PreviewTriangleCount: triangleCount,
	}, nil
}

func pickAndLoadMeshCapture(window fyne.Window, progressBar *widget.ProgressBarInfinite, loadButton *widget.Button, onFinished func(*renderdoc.MeshReport, []renderdoc.MeshInfo, string, string, *renderdoc.BufferStore, error)) {
	capturePath, err := nativeDialog.File().
		Filter("RenderDoc capture", "rdc").
		Title("Select RenderDoc capture (.rdc)").
		Load()
	if err != nil {
		if errors.Is(err, nativeDialog.Cancelled) {
			return
		}
		fyne.Do(func() { fyneDialog.ShowError(err, window) })
		return
	}
	fyne.Do(func() {
		progressBar.Show()
		if loadButton != nil {
			loadButton.Disable()
		}
	})

	xmlPath, convertErr := renderdoc.ConvertToXML(capturePath)
	if convertErr != nil {
		fyne.Do(func() { onFinished(nil, nil, capturePath, "", nil, convertErr) })
		return
	}
	report, parseErr := renderdoc.ParseMeshReportFromXMLFile(xmlPath)
	if parseErr != nil {
		fyne.Do(func() { onFinished(nil, nil, capturePath, xmlPath, nil, parseErr) })
		return
	}
	store, storeErr := renderdoc.OpenBufferStore(xmlPath)
	if storeErr != nil {
		fyne.Do(func() { onFinished(report, nil, capturePath, xmlPath, nil, storeErr) })
		return
	}
	meshes, buildErr := renderdoc.BuildMeshes(report, store)
	if buildErr != nil {
		_ = store.Close()
		fyne.Do(func() { onFinished(report, nil, capturePath, xmlPath, nil, buildErr) })
		return
	}
	fyne.Do(func() { onFinished(report, meshes, capturePath, xmlPath, store, nil) })
}
```

- [ ] **Step 2: Replace the Task 5 placeholder**

In `internal/app/ui/tabs/renderdoc/renderdoc_tab.go`, delete the placeholder `newMeshesSubTab` function you added in Task 5 (since it now lives in `meshes_view.go` with the same name).

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: compiles.

- [ ] **Step 4: Run the existing tests**

Run: `go test ./...`
Expected: the same pass set as before this plan started. No new tests added here (the UI layer doesn't have them today).

- [ ] **Step 5: Manual smoke test**

Run the app, switch to RenderDoc → Meshes tab, click "Load .rdc…", pick `D:/Downloads/RobloxBatcave week1-v2.rdc`. Expected:
- Table populates with ~50-100 unique mesh rows
- Summary line shows total unique meshes and total bytes
- Clicking a row with an `R32G32B32_FLOAT` POSITION layout renders a 3D preview on the right
- Clicking a row with an unsupported position format shows "preview format not supported yet"

- [ ] **Step 6: Commit**

```bash
git add internal/app/ui/tabs/renderdoc/meshes_view.go internal/app/ui/tabs/renderdoc/renderdoc_tab.go
git commit -m "feat(renderdoc-tab): add Meshes sub-tab with dedup table and 3D preview"
```

---

## Done

After Task 6 the feature is fully in place.

### Spec deviation

The spec proposed a single `.rdc` load shared between both sub-tabs. This plan gives the Meshes sub-tab its own "Load .rdc…" button, mirroring the Textures sub-tab's existing UX. Result: users load once per view, but each view manages its own capture state.

Sharing the capture across sub-tabs would require lifting the load handler into `NewRenderDocTab` and plumbing `*renderdoc.Report`, `*renderdoc.MeshReport`, and `*renderdoc.BufferStore` into both children — a real refactor of the textures tab too. Kept out of v1 to stay focused on getting the meshes view working. Straightforward follow-up if wanted.

### Future work (explicitly out of v1 per spec)

- Non-`R32G32B32_FLOAT` position formats (SNORM16x4 is the other common Roblox format)
- Roblox built-in mesh detection via hash allowlist
- Correlation with rbxl `MeshPart` asset IDs
- Shared capture load across the two sub-tabs (see above)
