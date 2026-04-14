package report

import (
	"strings"

	"joxblox/internal/extractor"
)

func PositionedRefTarget(ref extractor.PositionedResult) (instancePath string, instanceType string) {
	instancePath = strings.TrimSpace(ref.InstancePath)
	instanceType = strings.TrimSpace(ref.InstanceType)
	if strings.EqualFold(instanceType, "SurfaceAppearance") {
		parentPath := ParentInstancePath(instancePath)
		if parentPath != "" {
			return parentPath, "MeshPart"
		}
	}
	return instancePath, instanceType
}

func ParentInstancePath(instancePath string) string {
	trimmedPath := strings.TrimSpace(instancePath)
	if trimmedPath == "" {
		return ""
	}
	lastDotIndex := strings.LastIndex(trimmedPath, ".")
	if lastDotIndex <= 0 {
		return ""
	}
	return strings.TrimSpace(trimmedPath[:lastDotIndex])
}

func NormalizeInstanceType(instanceType string) string {
	return strings.ToLower(strings.TrimSpace(instanceType))
}

func IsMeshContentProperty(propertyName string) bool {
	return propertyName == "meshid" || propertyName == "meshcontent"
}

func IsTextureContentProperty(propertyName string) bool {
	return propertyName == "textureid" || propertyName == "texturecontent"
}

func IsSurfaceAppearanceProperty(propertyName string, instanceType string) bool {
	normalizedInstanceType := NormalizeInstanceType(instanceType)
	return normalizedInstanceType == "surfaceappearance" || strings.Contains(propertyName, "mapcontent")
}
