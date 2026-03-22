package app

import (
	"fmt"
	"strings"
)

const (
	sourceAssetDeliveryInGame = "AssetDelivery (In-Game)"
	sourceThumbnailsFallback  = "Thumbnails API (Fallback)"
	sourceNoThumbnail         = "No Thumbnail (Unavailable)"
	stateCompleted            = "Completed"
	stateUnavailable          = "Unavailable"

	assetTypeImage = 1
	assetTypeMesh  = 4
)

type assetTypeInfo struct {
	Name               string
	Emoji              string
	SkipRustExtraction bool
}

var assetTypeInfoByID = map[int]assetTypeInfo{
	assetTypeImage: {Name: "Image", Emoji: "🖼️", SkipRustExtraction: true},
	2:              {Name: "TShirt", Emoji: "👕"},
	3:              {Name: "Audio", Emoji: "🎵", SkipRustExtraction: true},
	assetTypeMesh:  {Name: "Mesh", Emoji: "🕸️", SkipRustExtraction: true},
	5:              {Name: "Lua", Emoji: "📜"},
	8:              {Name: "Hat", Emoji: "🎩"},
	9:              {Name: "Place", Emoji: "🗺️"},
	10:             {Name: "Model", Emoji: "📦"},
	11:             {Name: "Shirt", Emoji: "👔"},
	12:             {Name: "Pants", Emoji: "👖"},
	13:             {Name: "Decal", Emoji: "🏷️"},
	17:             {Name: "Head", Emoji: "🗿"},
	18:             {Name: "Face", Emoji: "😀"},
	19:             {Name: "Gear", Emoji: "⚙️"},
	21:             {Name: "Badge", Emoji: "🏅"},
	24:             {Name: "Animation", Emoji: "🎬"},
	27:             {Name: "Torso", Emoji: "🧍"},
	28:             {Name: "RightArm", Emoji: "💪"},
	29:             {Name: "LeftArm", Emoji: "🤛"},
	30:             {Name: "LeftLeg", Emoji: "🦵"},
	31:             {Name: "RightLeg", Emoji: "🦿"},
	32:             {Name: "Package", Emoji: "🎁"},
	34:             {Name: "GamePass", Emoji: "🎫"},
	38:             {Name: "Plugin", Emoji: "🔌"},
	40:             {Name: "MeshPart", Emoji: "🧱"},
	41:             {Name: "HairAccessory", Emoji: "💇"},
	42:             {Name: "FaceAccessory", Emoji: "🎭"},
	43:             {Name: "NeckAccessory", Emoji: "📿"},
	44:             {Name: "ShoulderAccessory", Emoji: "🧳"},
	45:             {Name: "FrontAccessory", Emoji: "🦺"},
	46:             {Name: "BackAccessory", Emoji: "🎒"},
	47:             {Name: "WaistAccessory", Emoji: "🧵"},
	48:             {Name: "ClimbAnimation", Emoji: "🧗"},
	49:             {Name: "DeathAnimation", Emoji: "☠️"},
	50:             {Name: "FallAnimation", Emoji: "🍂"},
	51:             {Name: "IdleAnimation", Emoji: "😴"},
	52:             {Name: "JumpAnimation", Emoji: "🦘"},
	53:             {Name: "RunAnimation", Emoji: "🏃"},
	54:             {Name: "SwimAnimation", Emoji: "🏊"},
	55:             {Name: "WalkAnimation", Emoji: "🚶"},
	56:             {Name: "PoseAnimation", Emoji: "🕺"},
	57:             {Name: "EarAccessory", Emoji: "👂"},
	58:             {Name: "EyeAccessory", Emoji: "👓"},
	61:             {Name: "EmoteAnimation", Emoji: "🗣️"},
	62:             {Name: "Video", Emoji: "🎥", SkipRustExtraction: true},
	64:             {Name: "TShirtAccessory", Emoji: "👚"},
	65:             {Name: "ShirtAccessory", Emoji: "🥼"},
	66:             {Name: "PantsAccessory", Emoji: "🩲"},
	67:             {Name: "JacketAccessory", Emoji: "🧥"},
	68:             {Name: "SweaterAccessory", Emoji: "🧶"},
	69:             {Name: "ShortsAccessory", Emoji: "🩳"},
	70:             {Name: "LeftShoeAccessory", Emoji: "👟"},
	71:             {Name: "RightShoeAccessory", Emoji: "🥾"},
	72:             {Name: "DressSkirtAccessory", Emoji: "👗"},
	73:             {Name: "FontFamily", Emoji: "🔤"},
	76:             {Name: "EyebrowAccessory", Emoji: "🤨"},
	77:             {Name: "EyelashAccessory", Emoji: "🪶"},
	78:             {Name: "MoodAnimation", Emoji: "😊"},
	79:             {Name: "DynamicHead", Emoji: "🤖"},
	88:             {Name: "FaceMakeup", Emoji: "💄"},
	89:             {Name: "LipMakeup", Emoji: "👄"},
	90:             {Name: "EyeMakeup", Emoji: "👁️"},
}

func isThumbnailFallback(source string) bool {
	return strings.EqualFold(source, sourceThumbnailsFallback)
}

func isCompletedState(state string) bool {
	return strings.EqualFold(state, stateCompleted)
}

func getAssetTypeName(assetTypeID int) string {
	if assetTypeID <= 0 {
		return "Unknown"
	}
	if assetTypeInfo, exists := assetTypeInfoByID[assetTypeID]; exists {
		return assetTypeInfo.Name
	}
	return fmt.Sprintf("Type %d", assetTypeID)
}

func getAssetTypeEmoji(assetTypeID int) string {
	if assetTypeInfo, exists := assetTypeInfoByID[assetTypeID]; exists && assetTypeInfo.Emoji != "" {
		return assetTypeInfo.Emoji
	}
	return "🧩"
}

func shouldSkipRustExtractionForAssetType(assetTypeID int) bool {
	assetTypeInfo, exists := assetTypeInfoByID[assetTypeID]
	return exists && assetTypeInfo.SkipRustExtraction
}
