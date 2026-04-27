package loader

import (
	"math"
	"strconv"
	"strings"

	"joxblox/internal/format"
)

// DefaultLargeTextureThreshold is the default score cutoff (texture bytes per square stud)
// for the scan tab "large textures" and report oversized-texture logic.
var DefaultLargeTextureThreshold float64 = 4096

func ParseLargeTextureThreshold(rawValue string) float64 {
	trimmedValue := strings.TrimSpace(rawValue)
	if trimmedValue == "" {
		return DefaultLargeTextureThreshold
	}
	parsedValue, err := strconv.ParseFloat(trimmedValue, 64)
	if err != nil || parsedValue <= 0 {
		return DefaultLargeTextureThreshold
	}
	return parsedValue
}

func FormatLargeTextureThreshold(value float64) string {
	if value <= 0 {
		value = DefaultLargeTextureThreshold
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func ScanResultTextureByteCost(result ScanResult) int {
	// Prefer the GPU-resident estimate when known — this is what the
	// engine actually pays per texture (BC compression + mip chain),
	// not the on-disk PNG size. Falls back to texture bytes / asset
	// bytes when GPU footprint isn't computable (mesh assets, scan
	// rows missing dimensions, etc).
	if gpuBytes := ScanResultGPUMemoryBytes(result); gpuBytes > 0 {
		return int(gpuBytes)
	}
	if result.TextureBytes > 0 {
		return result.TextureBytes
	}
	if result.Width > 0 && result.Height > 0 && result.BytesSize > 0 {
		return result.BytesSize
	}
	return 0
}

func ComputeLargeTextureScore(textureBytes int, sceneSurfaceArea float64) float64 {
	if textureBytes <= 0 || sceneSurfaceArea <= 0 {
		return 0
	}
	return float64(textureBytes) / sceneSurfaceArea
}

func RefreshLargeTextureMetrics(result ScanResult) ScanResult {
	result.LargeTextureScore = ComputeLargeTextureScore(ScanResultTextureByteCost(result), result.SceneSurfaceArea)
	return result
}

func IsLargeTexture(result ScanResult, threshold float64) bool {
	if threshold <= 0 {
		threshold = DefaultLargeTextureThreshold
	}
	score := result.LargeTextureScore
	if score <= 0 {
		score = ComputeLargeTextureScore(ScanResultTextureByteCost(result), result.SceneSurfaceArea)
	}
	return score >= threshold
}

func FormatLargeTextureScore(score float64) string {
	if score <= 0 {
		return "-"
	}
	if score >= format.Megabyte {
		return strconv.FormatFloat(score/format.Megabyte, 'f', 2, 64) + " MB/stud^2"
	}
	if score >= 1024 {
		return strconv.FormatFloat(score/1024.0, 'f', 2, 64) + " KB/stud^2"
	}
	return strconv.FormatFloat(score, 'f', 2, 64) + " B/stud^2"
}

func FormatSceneSurfaceArea(area float64) string {
	if area <= 0 {
		return "-"
	}
	return strconv.FormatFloat(area, 'f', -1, 64) + " stud^2"
}

type surfaceAreaPart interface {
	GetInstancePath() string
	GetDimensions() (float64, float64, float64)
}

func BuildSceneSurfaceAreaIndex[T surfaceAreaPart](parts []T) map[string]float64 {
	areaByPath := map[string]float64{}
	for _, part := range parts {
		instancePath := strings.TrimSpace(part.GetInstancePath())
		if instancePath == "" {
			continue
		}
		sizeX, sizeY, sizeZ := part.GetDimensions()
		area := sceneSurfaceAreaForDimensions(sizeX, sizeY, sizeZ)
		if area <= 0 {
			continue
		}
		areaByPath[instancePath] = area
	}
	return areaByPath
}

func sceneSurfaceAreaForDimensions(sizeX float64, sizeY float64, sizeZ float64) float64 {
	if sizeX <= 0 || sizeY <= 0 || sizeZ <= 0 {
		return 0
	}
	return math.Max(sizeX*sizeY, math.Max(sizeX*sizeZ, sizeY*sizeZ))
}

func EstimateSceneSurfaceAreaForPaths(primaryPath string, alternatePaths []string, areaByPath map[string]float64) float64 {
	bestArea, _ := EstimateSceneSurfaceAreaAndPathForPaths(primaryPath, alternatePaths, areaByPath)
	return bestArea
}

func EstimateSceneSurfaceAreaAndPathForPaths(primaryPath string, alternatePaths []string, areaByPath map[string]float64) (float64, string) {
	bestArea := resolveSceneSurfaceAreaForPath(primaryPath, areaByPath)
	bestPath := strings.TrimSpace(primaryPath)
	for _, instancePath := range alternatePaths {
		nextArea := resolveSceneSurfaceAreaForPath(instancePath, areaByPath)
		if nextArea > bestArea {
			bestArea = nextArea
			bestPath = strings.TrimSpace(instancePath)
		}
		if bestArea <= 0 && strings.TrimSpace(bestPath) == "" && strings.TrimSpace(instancePath) != "" {
			bestPath = strings.TrimSpace(instancePath)
		}
	}
	if bestArea <= 0 && strings.TrimSpace(bestPath) == "" {
		bestPath = strings.TrimSpace(primaryPath)
	}
	return bestArea, bestPath
}

func resolveSceneSurfaceAreaForPath(instancePath string, areaByPath map[string]float64) float64 {
	if len(areaByPath) == 0 {
		return 0
	}
	currentPath := strings.TrimSpace(instancePath)
	for currentPath != "" {
		if area, found := areaByPath[currentPath]; found && area > 0 {
			return area
		}
		currentPath = parentInstancePath(currentPath)
	}
	return 0
}

func parentInstancePath(instancePath string) string {
	trimmedPath := strings.TrimSpace(instancePath)
	if trimmedPath == "" {
		return ""
	}
	lastDot := strings.LastIndex(trimmedPath, ".")
	lastSlash := strings.LastIndexAny(trimmedPath, `/\`)
	splitIndex := lastDot
	if lastSlash > splitIndex {
		splitIndex = lastSlash
	}
	if splitIndex <= 0 {
		return ""
	}
	return strings.TrimSpace(trimmedPath[:splitIndex])
}

func MinPositiveFloat64(left float64, right float64) float64 {
	if left <= 0 {
		return right
	}
	if right <= 0 {
		return left
	}
	return math.Min(left, right)
}

func MaxPositiveFloat64(left float64, right float64) float64 {
	if left <= 0 {
		return right
	}
	if right <= 0 {
		return left
	}
	return math.Max(left, right)
}
