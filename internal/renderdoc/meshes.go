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

// MeshReport collects the parse output needed to build the Meshes view.
// Buffers and InputLayouts are keyed by resource ID string (the integer
// inside <ResourceId>…</ResourceId> elements, as a string).
type MeshReport struct {
	Buffers      map[string]BufferInfo
	InputLayouts map[string]InputLayoutInfo
	DrawCalls    []DrawCall
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
		DrawCalls:    []DrawCall{},
	}
	var currentVBs []DrawCallVertexBuffer
	var currentIB struct {
		id, format string
		offset     int
	}
	var currentLayoutID string
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
		case "ID3D11Device::CreateInputLayout":
			info, parseErr := parseCreateInputLayoutChunk(decoder)
			if parseErr != nil {
				return nil, parseErr
			}
			if info != nil && info.ResourceID != "" {
				report.InputLayouts[info.ResourceID] = *info
			}
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
