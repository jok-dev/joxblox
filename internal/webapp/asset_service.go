package webapp

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
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
