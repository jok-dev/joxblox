package roblox

import (
	"fmt"
	"strings"
)

const (
	SourceAssetDeliveryInGame = "AssetDelivery (In-Game)"
	SourceThumbnailsDirect    = "Thumbnails API (Direct)"
	SourceThumbnailsFallback  = "Thumbnails API (Fallback)"
	SourceNoThumbnail         = "No Thumbnail (Unavailable)"
	StateCompleted            = "Completed"
	StateUnavailable          = "Unavailable"

	AssetTypeImage = 1
	AssetTypeAudio = 3
	AssetTypeMesh  = 4
)

type AssetTypeInfo struct {
	Name               string
	Emoji              string
	SkipRustExtraction bool
	DownloadExtension  string
}

var AssetTypeInfoByID = map[int]AssetTypeInfo{
	AssetTypeImage: {Name: "Image", Emoji: "🖼️", SkipRustExtraction: true, DownloadExtension: "png"},
	2:              {Name: "TShirt", Emoji: "👕", DownloadExtension: "rbxl"},
	3:              {Name: "Audio", Emoji: "🎵", SkipRustExtraction: true, DownloadExtension: "audio"},
	AssetTypeMesh:  {Name: "Mesh", Emoji: "🕸️", SkipRustExtraction: true, DownloadExtension: "rbxm"},
	5:              {Name: "Lua", Emoji: "📜", DownloadExtension: "lua"},
	8:              {Name: "Hat", Emoji: "🎩", DownloadExtension: "rbxl"},
	9:              {Name: "Place", Emoji: "🗺️", DownloadExtension: "rbxl"},
	10:             {Name: "Model", Emoji: "📦", DownloadExtension: "rbxl"},
	11:             {Name: "Shirt", Emoji: "👔", DownloadExtension: "rbxl"},
	12:             {Name: "Pants", Emoji: "👖", DownloadExtension: "rbxl"},
	13:             {Name: "Decal", Emoji: "🏷️", DownloadExtension: "rbxl"},
	17:             {Name: "Head", Emoji: "🗿", DownloadExtension: "rbxl"},
	18:             {Name: "Face", Emoji: "😀", DownloadExtension: "rbxl"},
	19:             {Name: "Gear", Emoji: "⚙️", DownloadExtension: "rbxl"},
	21:             {Name: "Badge", Emoji: "🏅", DownloadExtension: "rbxl"},
	24:             {Name: "Animation", Emoji: "🎬", DownloadExtension: "rbxl"},
	27:             {Name: "Torso", Emoji: "🧍", DownloadExtension: "rbxl"},
	28:             {Name: "RightArm", Emoji: "💪", DownloadExtension: "rbxl"},
	29:             {Name: "LeftArm", Emoji: "🤛", DownloadExtension: "rbxl"},
	30:             {Name: "LeftLeg", Emoji: "🦵", DownloadExtension: "rbxl"},
	31:             {Name: "RightLeg", Emoji: "🦿", DownloadExtension: "rbxl"},
	32:             {Name: "Package", Emoji: "🎁", DownloadExtension: "rbxl"},
	34:             {Name: "GamePass", Emoji: "🎫", DownloadExtension: "rbxl"},
	38:             {Name: "Plugin", Emoji: "🔌", DownloadExtension: "rbxl"},
	40:             {Name: "MeshPart", Emoji: "🧱", DownloadExtension: "rbxm"},
	41:             {Name: "HairAccessory", Emoji: "💇", DownloadExtension: "rbxl"},
	42:             {Name: "FaceAccessory", Emoji: "🎭", DownloadExtension: "rbxl"},
	43:             {Name: "NeckAccessory", Emoji: "📿", DownloadExtension: "rbxl"},
	44:             {Name: "ShoulderAccessory", Emoji: "🧳", DownloadExtension: "rbxl"},
	45:             {Name: "FrontAccessory", Emoji: "🦺", DownloadExtension: "rbxl"},
	46:             {Name: "BackAccessory", Emoji: "🎒", DownloadExtension: "rbxl"},
	47:             {Name: "WaistAccessory", Emoji: "🧵", DownloadExtension: "rbxl"},
	48:             {Name: "ClimbAnimation", Emoji: "🧗", DownloadExtension: "rbxl"},
	49:             {Name: "DeathAnimation", Emoji: "☠️", DownloadExtension: "rbxl"},
	50:             {Name: "FallAnimation", Emoji: "🍂", DownloadExtension: "rbxl"},
	51:             {Name: "IdleAnimation", Emoji: "😴", DownloadExtension: "rbxl"},
	52:             {Name: "JumpAnimation", Emoji: "🦘", DownloadExtension: "rbxl"},
	53:             {Name: "RunAnimation", Emoji: "🏃", DownloadExtension: "rbxl"},
	54:             {Name: "SwimAnimation", Emoji: "🏊", DownloadExtension: "rbxl"},
	55:             {Name: "WalkAnimation", Emoji: "🚶", DownloadExtension: "rbxl"},
	56:             {Name: "PoseAnimation", Emoji: "🕺", DownloadExtension: "rbxl"},
	57:             {Name: "EarAccessory", Emoji: "👂", DownloadExtension: "rbxl"},
	58:             {Name: "EyeAccessory", Emoji: "👓", DownloadExtension: "rbxl"},
	61:             {Name: "EmoteAnimation", Emoji: "🗣️", DownloadExtension: "rbxl"},
	62:             {Name: "Video", Emoji: "🎥", SkipRustExtraction: true, DownloadExtension: "mp4"},
	64:             {Name: "TShirtAccessory", Emoji: "👚", DownloadExtension: "rbxl"},
	65:             {Name: "ShirtAccessory", Emoji: "🥼", DownloadExtension: "rbxl"},
	66:             {Name: "PantsAccessory", Emoji: "🩲", DownloadExtension: "rbxl"},
	67:             {Name: "JacketAccessory", Emoji: "🧥", DownloadExtension: "rbxl"},
	68:             {Name: "SweaterAccessory", Emoji: "🧶", DownloadExtension: "rbxl"},
	69:             {Name: "ShortsAccessory", Emoji: "🩳", DownloadExtension: "rbxl"},
	70:             {Name: "LeftShoeAccessory", Emoji: "👟", DownloadExtension: "rbxl"},
	71:             {Name: "RightShoeAccessory", Emoji: "🥾", DownloadExtension: "rbxl"},
	72:             {Name: "DressSkirtAccessory", Emoji: "👗", DownloadExtension: "rbxl"},
	73:             {Name: "FontFamily", Emoji: "🔤", DownloadExtension: "rbxl"},
	76:             {Name: "EyebrowAccessory", Emoji: "🤨", DownloadExtension: "rbxl"},
	77:             {Name: "EyelashAccessory", Emoji: "🪶", DownloadExtension: "rbxl"},
	78:             {Name: "MoodAnimation", Emoji: "😊", DownloadExtension: "rbxl"},
	79:             {Name: "DynamicHead", Emoji: "🤖", DownloadExtension: "rbxl"},
	88:             {Name: "FaceMakeup", Emoji: "💄", DownloadExtension: "rbxl"},
	89:             {Name: "LipMakeup", Emoji: "👄", DownloadExtension: "rbxl"},
	90:             {Name: "EyeMakeup", Emoji: "👁️", DownloadExtension: "rbxl"},
}

func IsThumbnailFallback(source string) bool {
	return strings.EqualFold(source, SourceThumbnailsFallback)
}

func IsCompletedState(state string) bool {
	return strings.EqualFold(state, StateCompleted)
}

func GetAssetTypeName(assetTypeID int) string {
	if assetTypeID <= 0 {
		return "Unknown"
	}
	if info, exists := AssetTypeInfoByID[assetTypeID]; exists {
		return info.Name
	}
	return fmt.Sprintf("Type %d", assetTypeID)
}

func GetAssetTypeEmoji(assetTypeID int) string {
	if info, exists := AssetTypeInfoByID[assetTypeID]; exists && info.Emoji != "" {
		return info.Emoji
	}
	return "🧩"
}

func ShouldSkipRustExtractionForAssetType(assetTypeID int) bool {
	info, exists := AssetTypeInfoByID[assetTypeID]
	return exists && info.SkipRustExtraction
}

func GetAssetDownloadExtension(assetTypeID int) string {
	if info, exists := AssetTypeInfoByID[assetTypeID]; exists && strings.TrimSpace(info.DownloadExtension) != "" {
		return info.DownloadExtension
	}
	return "rbxl"
}
