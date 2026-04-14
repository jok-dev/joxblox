package extractor

import (
	"errors"
	"strconv"
	"strings"
)

var (
	ErrCancelled   = errors.New("extractor cancelled")
	BinaryProvider func() []byte
	DefaultLimit   = 5000
)

type Result struct {
	ID               int64    `json:"id"`
	RawContent       string   `json:"rawContent,omitempty"`
	InstanceType     string   `json:"instanceType"`
	InstanceName     string   `json:"instanceName"`
	InstancePath     string   `json:"instancePath"`
	PropertyName     string   `json:"propertyName"`
	Used             int      `json:"used"`
	AllInstancePaths []string `json:"allInstancePaths,omitempty"`
}

type PositionedResult struct {
	ID           int64    `json:"id"`
	RawContent   string   `json:"rawContent,omitempty"`
	InstanceType string   `json:"instanceType"`
	InstanceName string   `json:"instanceName"`
	InstancePath string   `json:"instancePath"`
	PropertyName string   `json:"propertyName"`
	WorldX       *float64 `json:"worldX,omitempty"`
	WorldY       *float64 `json:"worldY,omitempty"`
	WorldZ       *float64 `json:"worldZ,omitempty"`
}

type MapRenderPartResult struct {
	InstanceType string   `json:"instanceType"`
	InstanceName string   `json:"instanceName"`
	InstancePath string   `json:"instancePath"`
	MaterialKey  string   `json:"materialKey,omitempty"`
	CenterX      *float64 `json:"centerX,omitempty"`
	CenterY      *float64 `json:"centerY,omitempty"`
	CenterZ      *float64 `json:"centerZ,omitempty"`
	SizeX        *float64 `json:"sizeX,omitempty"`
	SizeY        *float64 `json:"sizeY,omitempty"`
	SizeZ        *float64 `json:"sizeZ,omitempty"`
	BasisSizeX   *float64 `json:"basisSizeX,omitempty"`
	BasisSizeY   *float64 `json:"basisSizeY,omitempty"`
	BasisSizeZ   *float64 `json:"basisSizeZ,omitempty"`
	YawDegrees   *float64 `json:"yawDegrees,omitempty"`
	RotationXX   *float64 `json:"rotationXx,omitempty"`
	RotationXY   *float64 `json:"rotationXy,omitempty"`
	RotationXZ   *float64 `json:"rotationXz,omitempty"`
	RotationYX   *float64 `json:"rotationYx,omitempty"`
	RotationYY   *float64 `json:"rotationYy,omitempty"`
	RotationYZ   *float64 `json:"rotationYz,omitempty"`
	RotationZX   *float64 `json:"rotationZx,omitempty"`
	RotationZY   *float64 `json:"rotationZy,omitempty"`
	RotationZZ   *float64 `json:"rotationZz,omitempty"`
	ColorR       *int     `json:"colorR,omitempty"`
	ColorG       *int     `json:"colorG,omitempty"`
	ColorB       *int     `json:"colorB,omitempty"`
	Transparency *float64 `json:"transparency,omitempty"`
}

func (p MapRenderPartResult) GetInstancePath() string { return p.InstancePath }

func (p MapRenderPartResult) GetDimensions() (float64, float64, float64) {
	return ptrOrZero(p.SizeX), ptrOrZero(p.SizeY), ptrOrZero(p.SizeZ)
}

type MissingMaterialVariantResult struct {
	VariantName  string `json:"variantName"`
	InstanceType string `json:"instanceType"`
	InstanceName string `json:"instanceName"`
	InstancePath string `json:"instancePath"`
}

type MeshPreviewRawResult struct {
	FormatVersion        string    `json:"formatVersion"`
	DecoderSource        string    `json:"decoderSource"`
	VertexCount          uint32    `json:"vertexCount"`
	TriangleCount        uint32    `json:"triangleCount"`
	PreviewTriangleCount uint32    `json:"previewTriangleCount"`
	Positions            []float32 `json:"positions"`
	Indices              []uint32  `json:"indices"`
}

type meshStatsRawResult struct {
	FormatVersion string `json:"formatVersion"`
	DecoderSource string `json:"decoderSource"`
	VertexCount   uint32 `json:"vertexCount"`
	TriangleCount uint32 `json:"triangleCount"`
}

type AssetIDsResult struct {
	AssetIDs      []int64
	UseCounts     map[int64]int
	References    []Result
	CommandOutput string
}

func ptrOrZero(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func AssetReferenceKey(assetID int64, assetInput string) string {
	trimmedInput := strings.TrimSpace(assetInput)
	if trimmedInput != "" {
		return strings.ToLower(trimmedInput)
	}
	return strconv.FormatInt(assetID, 10)
}
