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
		case "ID3D11Device::CreateInputLayout":
			info, parseErr := parseCreateInputLayoutChunk(decoder)
			if parseErr != nil {
				return nil, parseErr
			}
			if info != nil && info.ResourceID != "" {
				report.InputLayouts[info.ResourceID] = *info
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
