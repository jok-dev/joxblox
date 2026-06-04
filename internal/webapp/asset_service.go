package webapp

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"path/filepath"
	"strings"

	"joxblox/internal/app/loader"
	"joxblox/internal/audio"
	"joxblox/internal/roblox"
	"joxblox/internal/roblox/mesh"
)

// AssetDetails is the serializable result the Single Asset view renders. It
// mirrors the pre-formatted *Display fields the Fyne AssetView consumed, plus a
// data-URL preview so the image flows to the webview without a Fyne resource.
type AssetDetails struct {
	AssetID int64 `json:"assetId"`

	// PreviewDataURL is a "data:<mime>;base64,…" image, or "" when no preview
	// could be produced (e.g. mesh/model assets, which are deferred).
	PreviewDataURL string `json:"previewDataUrl"`
	StatusMessage  string `json:"statusMessage"`

	SourceDisplay    string `json:"sourceDisplay"`
	StateDescription string `json:"stateDescription"`
	WarningMessage   string `json:"warningMessage"`

	AssetTypeDisplay   string `json:"assetTypeDisplay"`
	DimensionsLabel    string `json:"dimensionsLabel"`
	DimensionsDisplay  string `json:"dimensionsDisplay"`
	DurationDisplay    string `json:"durationDisplay"`
	SelfSizeDisplay    string `json:"selfSizeDisplay"`
	TotalSizeDisplay   string `json:"totalSizeDisplay"`
	FormatDisplay      string `json:"formatDisplay"`
	ContentTypeDisplay string `json:"contentTypeDisplay"`
	ReferencedDisplay  string `json:"referencedDisplay"`

	ReferencedAssetIDs []int64 `json:"referencedAssetIds"`
	FileSHA256Display  string  `json:"fileSha256Display"`

	AssetDeliveryJSON  string `json:"assetDeliveryJson"`
	ThumbnailJSON      string `json:"thumbnailJson"`
	EconomyJSON        string `json:"economyJson"`
	RustyAssetToolJSON string `json:"rustyAssetToolJson"`
}

// AssetService exposes single-asset lookup to the frontend, reusing the same
// loader pipeline the Fyne Single Asset tab used.
type AssetService struct {
	ctx context.Context
}

func NewAssetService() *AssetService { return &AssetService{} }

func (s *AssetService) startup(ctx context.Context) { s.ctx = ctx }

// FetchAsset parses an asset ID / rbxassetid URL / rbxthumb URL, loads the best
// available preview, and returns serializable details for the web UI. Mesh
// assets currently return metadata without a preview image (3D preview is
// deferred).
func (s *AssetService) FetchAsset(input string) (AssetDetails, error) {
	request, err := loader.ParseSingleAssetLoadRequest(input)
	if err != nil {
		return AssetDetails{}, err
	}

	if request.RequiresAuth() {
		if authErr := roblox.ValidateCurrentAuthCookie(); authErr != nil {
			return AssetDetails{}, fmt.Errorf("%s", roblox.SanitizeAuthErrorMessage(authErr))
		}
	}

	previewResult, loadErr := loader.LoadSingleAssetPreview(request)
	if loadErr != nil {
		return AssetDetails{}, loadErr
	}

	data := loader.BuildAssetViewDataFromPreview(request.TargetID, previewResult, loader.AssetReferenceContext{})
	loader.PopulateAssetViewDisplayFields(&data)

	return AssetDetails{
		AssetID:            data.AssetID,
		PreviewDataURL:     buildPreviewDataURL(data.PreviewImageInfo),
		StatusMessage:      buildAssetStatusMessage(previewResult),
		SourceDisplay:      data.SourceDisplay,
		StateDescription:   data.StateDescription,
		WarningMessage:     data.FailureReasonText,
		AssetTypeDisplay:   data.AssetTypeDisplay,
		DimensionsLabel:    data.DimensionsLabel,
		DimensionsDisplay:  data.DimensionsDisplay,
		DurationDisplay:    buildDurationDisplay(data.StatsInfo),
		SelfSizeDisplay:    data.SelfSizeDisplay,
		TotalSizeDisplay:   data.TotalSizeDisplay,
		FormatDisplay:      data.FormatDisplay,
		ContentTypeDisplay: data.ContentTypeDisplay,
		ReferencedDisplay:  data.ReferencedDisplay,
		ReferencedAssetIDs: data.ReferencedAssetIDs,
		FileSHA256Display:  data.FileSHA256Display,
		AssetDeliveryJSON:  data.AssetDeliveryRawJSON,
		ThumbnailJSON:      data.ThumbnailRawJSON,
		EconomyJSON:        data.EconomyRawJSON,
		RustyAssetToolJSON: data.RustyAssetToolRawJSON,
	}, nil
}

// DownloadAsset re-loads the asset for the given ID / rbxassetid / rbxthumb
// input and saves its original bytes to a location chosen via the native save
// dialog. It returns the absolute path the file was written to, or "" (without
// error) when the user cancels. The download bytes are the same original asset
// payload the loader pipeline already resolves for previews.
func (s *AssetService) DownloadAsset(input string) (string, error) {
	request, err := loader.ParseSingleAssetLoadRequest(input)
	if err != nil {
		return "", err
	}

	if request.RequiresAuth() {
		if authErr := roblox.ValidateCurrentAuthCookie(); authErr != nil {
			return "", fmt.Errorf("%s", roblox.SanitizeAuthErrorMessage(authErr))
		}
	}

	previewResult, loadErr := loader.LoadSingleAssetPreview(request)
	if loadErr != nil {
		return "", loadErr
	}

	// Image-content assets (Image, TexturePack) are saved as a re-encoded PNG so
	// the download is always a usable image regardless of the original delivery
	// format. Every other asset type keeps its original bytes.
	if isImageContentAsset(previewResult.AssetTypeID) {
		return s.downloadImageAsPNG(request.TargetID, previewResult)
	}

	if len(previewResult.DownloadBytes) == 0 {
		return "", fmt.Errorf("No downloadable asset content is available for this asset.")
	}

	defaultName := strings.TrimSpace(previewResult.DownloadFileName)
	if defaultName == "" {
		defaultName = fmt.Sprintf("asset_%d", request.TargetID)
	}
	filterPattern := "*"
	if ext := filepath.Ext(defaultName); ext != "" {
		filterPattern = "*" + ext
	}

	return saveBytesViaDialogPath(s.ctx, "Download asset", defaultName, "Asset file", filterPattern, previewResult.DownloadBytes)
}

// isImageContentAsset reports whether an asset type is delivered as raw image
// content (Image, TexturePack) — the types we re-encode to PNG on download.
// Both are exactly the types whose download extension is "png".
func isImageContentAsset(assetTypeID int) bool {
	info, ok := roblox.AssetTypeInfoByID[assetTypeID]
	return ok && info.DownloadExtension == "png"
}

// downloadImageAsPNG decodes an image asset (preferring the original download
// payload, falling back to the already-decoded preview bytes) and saves it as a
// PNG via the native save dialog. Returns the saved path, or "" when cancelled.
func (s *AssetService) downloadImageAsPNG(assetID int64, result *loader.AssetPreviewResult) (string, error) {
	decoded, decodeErr := decodeImage(result.DownloadBytes)
	if decodeErr != nil {
		// Original bytes weren't a decodable image (e.g. a compressed texture
		// format) — fall back to the displayable preview bytes.
		decoded, decodeErr = decodeImage(result.Image.ImageBytes())
		if decodeErr != nil {
			return "", fmt.Errorf("No decodable image content is available for this asset.")
		}
	}

	var buffer bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if encodeErr := encoder.Encode(&buffer, decoded); encodeErr != nil {
		return "", encodeErr
	}

	return saveBytesViaDialogPath(s.ctx, "Download image as PNG", imagePNGFileName(assetID, result.DownloadFileName), "PNG image", "*.png", buffer.Bytes())
}

// decodeImage decodes encoded image bytes, returning an error for empty or
// undecodable input.
func decodeImage(imageBytes []byte) (image.Image, error) {
	if len(imageBytes) == 0 {
		return nil, fmt.Errorf("no image bytes")
	}
	decoded, _, err := image.Decode(bytes.NewReader(imageBytes))
	return decoded, err
}

// imagePNGFileName builds a ".png" default filename from the loader's suggested
// download name (swapping its extension), or "asset_<id>.png" when absent.
func imagePNGFileName(assetID int64, downloadFileName string) string {
	base := strings.TrimSpace(downloadFileName)
	if base == "" {
		return fmt.Sprintf("asset_%d.png", assetID)
	}
	return strings.TrimSuffix(base, filepath.Ext(base)) + ".png"
}

// buildPreviewDataURL encodes the preview image bytes as a base64 data URL.
// The MIME type is sniffed from the bytes so it stays correct even when the
// preview was re-encoded (e.g. a KTX2 asset previewed as PNG). Returns "" when
// there is no preview image.
func buildPreviewDataURL(info *loader.ImageInfo) string {
	imageBytes := info.ImageBytes()
	if len(imageBytes) == 0 {
		return ""
	}
	mimeType := http.DetectContentType(imageBytes)
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(imageBytes)
}

// buildDurationDisplay renders an audio asset's playback length using the same
// compact formatter the Fyne audio controls use, or "" for non-audio assets and
// assets whose duration could not be extracted.
func buildDurationDisplay(info *loader.ImageInfo) string {
	if info == nil || info.Duration <= 0 {
		return ""
	}
	return audio.FormatDurationCompact(info.Duration)
}

// buildAssetStatusMessage mirrors the human-readable status the Fyne tab set
// after a successful load, minus the per-request source breakdown (a
// debug/heatmap concern not surfaced in the web UI).
func buildAssetStatusMessage(result *loader.AssetPreviewResult) string {
	if result == nil {
		return ""
	}
	switch {
	case mesh.IsMeshAssetType(result.AssetTypeID) && len(result.DownloadBytes) > 0:
		if strings.EqualFold(result.Source, roblox.SourceAssetDeliveryInGame) {
			return "Mesh loaded."
		}
		return fmt.Sprintf("Mesh loaded with thumbnail metadata fallback (state: %s).", result.State)
	case strings.EqualFold(result.Source, roblox.SourceAssetDeliveryInGame):
		return "Image loaded."
	case strings.EqualFold(result.Source, roblox.SourceThumbnailsDirect):
		return fmt.Sprintf("Thumbnail loaded (state: %s).", result.State)
	default:
		return fmt.Sprintf("Loaded fallback thumbnail (state: %s).", result.State)
	}
}
