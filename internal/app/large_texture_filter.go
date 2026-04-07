package app

import (
	"math"
	"strconv"
	"strings"
)

const defaultLargeTextureThreshold = 4096.0

func parseLargeTextureThreshold(rawValue string) float64 {
	trimmedValue := strings.TrimSpace(rawValue)
	if trimmedValue == "" {
		return defaultLargeTextureThreshold
	}
	parsedValue, err := strconv.ParseFloat(trimmedValue, 64)
	if err != nil || parsedValue <= 0 {
		return defaultLargeTextureThreshold
	}
	return parsedValue
}

func formatLargeTextureThreshold(value float64) string {
	if value <= 0 {
		value = defaultLargeTextureThreshold
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func scanResultTextureByteCost(result scanResult) int {
	if result.TextureBytes > 0 {
		return result.TextureBytes
	}
	if result.Width > 0 && result.Height > 0 && result.BytesSize > 0 {
		return result.BytesSize
	}
	return 0
}

func computeLargeTextureScore(textureBytes int, sceneSurfaceArea float64) float64 {
	if textureBytes <= 0 || sceneSurfaceArea <= 0 {
		return 0
	}
	return float64(textureBytes) / sceneSurfaceArea
}

func refreshLargeTextureMetrics(result scanResult) scanResult {
	result.LargeTextureScore = computeLargeTextureScore(scanResultTextureByteCost(result), result.SceneSurfaceArea)
	return result
}

func isLargeTexture(result scanResult, threshold float64) bool {
	if threshold <= 0 {
		threshold = defaultLargeTextureThreshold
	}
	score := result.LargeTextureScore
	if score <= 0 {
		score = computeLargeTextureScore(scanResultTextureByteCost(result), result.SceneSurfaceArea)
	}
	return score >= threshold
}

func formatLargeTextureScore(score float64) string {
	if score <= 0 {
		return "-"
	}
	if score >= megabyte {
		return strconv.FormatFloat(score/megabyte, 'f', 2, 64) + " MB/stud^2"
	}
	if score >= 1024 {
		return strconv.FormatFloat(score/1024.0, 'f', 2, 64) + " KB/stud^2"
	}
	return strconv.FormatFloat(score, 'f', 2, 64) + " B/stud^2"
}

func formatSceneSurfaceArea(area float64) string {
	if area <= 0 {
		return "-"
	}
	return strconv.FormatFloat(area, 'f', -1, 64) + " stud^2"
}

func buildSceneSurfaceAreaIndexFromMapRenderParts(parts []mapRenderPartRustyAssetToolResult) map[string]float64 {
	areaByPath := map[string]float64{}
	for _, part := range parts {
		instancePath := strings.TrimSpace(part.InstancePath)
		if instancePath == "" {
			continue
		}
		area := sceneSurfaceAreaForDimensions(valueOrZero(part.SizeX), valueOrZero(part.SizeY), valueOrZero(part.SizeZ))
		if area <= 0 {
			continue
		}
		areaByPath[instancePath] = area
	}
	return areaByPath
}

func buildSceneSurfaceAreaIndexFromHeatmapParts(parts []rbxlHeatmapMapPart) map[string]float64 {
	areaByPath := map[string]float64{}
	for _, part := range parts {
		instancePath := strings.TrimSpace(part.InstancePath)
		if instancePath == "" {
			continue
		}
		area := sceneSurfaceAreaForDimensions(part.SizeX, part.SizeY, part.SizeZ)
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

func estimateSceneSurfaceAreaForPaths(primaryPath string, alternatePaths []string, areaByPath map[string]float64) float64 {
	bestArea, _ := estimateSceneSurfaceAreaAndPathForPaths(primaryPath, alternatePaths, areaByPath)
	return bestArea
}

func estimateSceneSurfaceAreaAndPathForPaths(primaryPath string, alternatePaths []string, areaByPath map[string]float64) (float64, string) {
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

func minPositiveFloat64(left float64, right float64) float64 {
	if left <= 0 {
		return right
	}
	if right <= 0 {
		return left
	}
	return math.Min(left, right)
}

func maxPositiveFloat64(left float64, right float64) float64 {
	if left <= 0 {
		return right
	}
	if right <= 0 {
		return left
	}
	return math.Max(left, right)
}

func valueOrZero(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}
