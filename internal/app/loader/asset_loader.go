package loader

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"joxblox/internal/debug"
	"joxblox/internal/extractor"
	"joxblox/internal/heatmap"
	"joxblox/internal/renderdoc"
	"joxblox/internal/roblox"
	"joxblox/internal/roblox/ktx2"

	"fyne.io/fyne/v2"
)

const (
	requestTimeout = 15 * time.Second
)

// CacheSettings holds the configuration for the asset download cache.
type CacheSettings struct {
	Enabled bool
	Folder  string
}

// LoadCacheSettings returns the current cache configuration.
var LoadCacheSettings func() CacheSettings = func() CacheSettings { return CacheSettings{} }

// AudioMetadata holds extracted audio file metadata.
type AudioMetadata struct {
	Duration time.Duration
	Format   string
}

// ExtractAudio extracts audio metadata from file bytes.
var ExtractAudio func(fileName, contentType string, data []byte) (*AudioMetadata, error)

// IsAudioContent returns whether the given asset type and content type represent audio.
var IsAudioContent func(assetTypeID int, contentType string) bool

var assetIDPattern = regexp.MustCompile(`\d+`)

type AssetSelfInfo struct {
	BytesSize   int
	AssetTypeID int
}

var assetSizeCache = struct {
	mutex         sync.RWMutex
	infoByAssetID map[int64]AssetSelfInfo
}{
	infoByAssetID: map[int64]AssetSelfInfo{},
}

type thumbnailsResponse struct {
	Data []thumbnailsData `json:"data"`
}

type thumbnailsData struct {
	ImageURL string `json:"imageUrl"`
	State    string `json:"state"`
	TargetID int64  `json:"targetId"`
	Version  string `json:"version"`
}

type ImageInfo struct {
	Resource                 *fyne.StaticResource
	Width                    int
	Height                   int
	Duration                 time.Duration
	BytesSize                int
	RecompressedPNGByteSize  int
	RecompressedJPEGByteSize int
	Format                   string
	ContentType              string
	SHA256                   string
	// HasAlphaChannel is true when the decoded image exposes an alpha channel,
	// regardless of per-pixel content. Roblox's upload pipeline appears to
	// inspect actual alpha values (see NonOpaqueAlphaPixels) — a PNG with an
	// all-255 alpha channel is still stored as BC1, not BC3.
	HasAlphaChannel bool
	// NonOpaqueAlphaPixels counts how many pixels have an alpha byte < 255.
	// Meaningful only when HasAlphaChannel is true. Zero means the alpha
	// channel is entirely opaque and Roblox will still pick BC1. A small
	// non-zero fraction (< 5% of total pixels) marks a "wasteful" BC3 — the
	// artist could likely drop the alpha channel and save 2x the GPU memory.
	NonOpaqueAlphaPixels int64
}

type ChildAssetInfo struct {
	AssetID      int64
	BytesSize    int
	AssetTypeID  int
	Resolved     bool
	InstanceType string
	InstanceName string
	InstancePath string
	PropertyName string
}

type AssetPreviewResult struct {
	Image              *ImageInfo
	Stats              *ImageInfo
	ReferencedAssetIDs []int64
	ChildAssets        []ChildAssetInfo
	TotalBytesSize     int
	Source             string
	State              string
	WarningMessage     string
	AssetDeliveryJSON  string
	ThumbnailJSON      string
	EconomyJSON        string
	RustyAssetToolJSON string
	AssetTypeID        int
	AssetTypeName      string
	DownloadBytes      []byte
	DownloadFileName   string
	DownloadIsOriginal bool
}

type ThumbnailInfo struct {
	ImageURL string
	State    string
	Version  string
}

type SingleAssetLoadRequest struct {
	TargetID         int64
	ThumbnailRequest *RbxThumbRequest
}

type RbxThumbRequest struct {
	Type       string
	TargetID   int64
	Width      int
	Height     int
	Size       string
	Format     string
	IsCircular bool
}

type ThumbnailsBatchRequest struct {
	Type       string `json:"type"`
	TargetID   int64  `json:"targetId"`
	Size       string `json:"size"`
	Format     string `json:"format"`
	IsCircular bool   `json:"isCircular"`
}

type assetDeliveryResponse struct {
	Location    string                    `json:"location"`
	AssetTypeID int                       `json:"assetTypeId"`
	Errors      []assetDeliveryErrorEntry `json:"errors"`
}

type assetDeliveryErrorEntry struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type AssetDeliveryInfo struct {
	Location      string
	RawJSON       string
	AssetTypeID   int
	AssetTypeName string
}

type CachedThumbnailInfo struct {
	Info    ThumbnailInfo
	RawJSON string
}

type AssetRequestTrace struct {
	mutex       sync.Mutex
	usedDisk    bool
	usedNetwork bool
}

func (trace *AssetRequestTrace) MarkDisk() {
	if trace == nil {
		return
	}
	trace.mutex.Lock()
	trace.usedDisk = true
	trace.mutex.Unlock()
}

func (trace *AssetRequestTrace) MarkNetwork() {
	if trace == nil {
		return
	}
	trace.mutex.Lock()
	trace.usedNetwork = true
	trace.mutex.Unlock()
}

func (trace *AssetRequestTrace) ClassifyRequestSource() heatmap.RequestSource {
	if trace == nil {
		return heatmap.SourceMemory
	}
	trace.mutex.Lock()
	defer trace.mutex.Unlock()
	if trace.usedNetwork {
		return heatmap.SourceNetwork
	}
	if trace.usedDisk {
		return heatmap.SourceDisk
	}
	return heatmap.SourceMemory
}

type economyAssetDetailsResponse struct {
	AssetTypeID int `json:"AssetTypeId"`
}

type EconomyAssetDetailsInfo struct {
	AssetTypeID int
	RawJSON     string
}

type AssetFileInfo struct {
	Info                     *ImageInfo
	IsImage                  bool
	ReferencedAssetIDs       []int64
	RustyAssetToolReferences []extractor.Result
	RustyAssetToolJSON       string
	FileBytes                []byte
	FileName                 string
}

func ParseAssetID(rawInput string) (int64, error) {
	trimmedInput := strings.TrimSpace(rawInput)
	if trimmedInput == "" {
		return 0, fmt.Errorf("Please enter an asset ID")
	}

	assetIDString := assetIDPattern.FindString(trimmedInput)
	if assetIDString == "" {
		return 0, fmt.Errorf("Could not find a numeric asset ID in the input")
	}

	assetID, err := strconv.ParseInt(assetIDString, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("Asset ID is invalid")
	}

	return assetID, nil
}

func ParseSingleAssetLoadRequest(rawInput string) (SingleAssetLoadRequest, error) {
	trimmedInput := strings.TrimSpace(rawInput)
	if strings.HasPrefix(strings.ToLower(trimmedInput), "rbxthumb://") {
		thumbnailRequest, err := parseRbxThumbRequest(trimmedInput)
		if err != nil {
			return SingleAssetLoadRequest{}, err
		}
		return SingleAssetLoadRequest{
			TargetID:         thumbnailRequest.TargetID,
			ThumbnailRequest: thumbnailRequest,
		}, nil
	}

	assetID, err := ParseAssetID(trimmedInput)
	if err != nil {
		return SingleAssetLoadRequest{}, err
	}
	return SingleAssetLoadRequest{TargetID: assetID}, nil
}

func parseRbxThumbRequest(rawInput string) (*RbxThumbRequest, error) {
	trimmedInput := strings.TrimSpace(rawInput)
	normalizedInput := strings.ToLower(trimmedInput)
	if !strings.HasPrefix(normalizedInput, "rbxthumb://") {
		return nil, fmt.Errorf("Input is not an rbxthumb URL")
	}

	queryString := strings.TrimSpace(trimmedInput[len("rbxthumb://"):])
	if queryString == "" {
		return nil, fmt.Errorf("rbxthumb URL is missing parameters")
	}

	queryValues, err := url.ParseQuery(queryString)
	if err != nil {
		return nil, fmt.Errorf("Could not parse rbxthumb URL")
	}

	thumbnailType := strings.TrimSpace(queryValues.Get("type"))
	if thumbnailType == "" {
		return nil, fmt.Errorf("rbxthumb URL is missing a type parameter")
	}

	targetIDString := FirstNonEmptyString(
		queryValues.Get("id"),
		queryValues.Get("targetId"),
		queryValues.Get("assetId"),
	)
	if strings.TrimSpace(targetIDString) == "" {
		return nil, fmt.Errorf("rbxthumb URL is missing an id parameter")
	}
	targetID, err := strconv.ParseInt(strings.TrimSpace(targetIDString), 10, 64)
	if err != nil || targetID <= 0 {
		return nil, fmt.Errorf("rbxthumb target ID is invalid")
	}

	width, height, err := parseThumbnailDimensions(queryValues)
	if err != nil {
		return nil, err
	}

	format, err := normalizeThumbnailFormat(queryValues.Get("format"))
	if err != nil {
		return nil, err
	}

	isCircular := false
	isCircularString := strings.TrimSpace(queryValues.Get("isCircular"))
	if isCircularString != "" {
		parsedIsCircular, parseErr := strconv.ParseBool(isCircularString)
		if parseErr != nil {
			return nil, fmt.Errorf("rbxthumb isCircular parameter is invalid")
		}
		isCircular = parsedIsCircular
	}

	return &RbxThumbRequest{
		Type:       thumbnailType,
		TargetID:   targetID,
		Width:      width,
		Height:     height,
		Size:       fmt.Sprintf("%dx%d", width, height),
		Format:     format,
		IsCircular: isCircular,
	}, nil
}

func parseThumbnailDimensions(queryValues url.Values) (int, int, error) {
	sizeValue := strings.TrimSpace(queryValues.Get("size"))
	if sizeValue != "" {
		sizeParts := strings.SplitN(sizeValue, "x", 2)
		if len(sizeParts) == 2 {
			width, widthErr := strconv.Atoi(strings.TrimSpace(sizeParts[0]))
			height, heightErr := strconv.Atoi(strings.TrimSpace(sizeParts[1]))
			if widthErr == nil && heightErr == nil && width > 0 && height > 0 {
				return width, height, nil
			}
		}
		return 0, 0, fmt.Errorf("rbxthumb size parameter is invalid")
	}

	widthString := FirstNonEmptyString(queryValues.Get("w"), queryValues.Get("width"))
	heightString := FirstNonEmptyString(queryValues.Get("h"), queryValues.Get("height"))
	if strings.TrimSpace(widthString) == "" || strings.TrimSpace(heightString) == "" {
		return 0, 0, fmt.Errorf("rbxthumb URL is missing width and height parameters")
	}

	width, widthErr := strconv.Atoi(strings.TrimSpace(widthString))
	height, heightErr := strconv.Atoi(strings.TrimSpace(heightString))
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("rbxthumb width/height parameters are invalid")
	}
	return width, height, nil
}

func normalizeThumbnailFormat(rawFormat string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(rawFormat)) {
	case "", "png":
		return "Png", nil
	case "jpeg", "jpg":
		return "Jpeg", nil
	case "webp":
		return "Webp", nil
	default:
		return "", fmt.Errorf("rbxthumb format must be png, jpeg, or webp")
	}
}

func FirstNonEmptyString(values ...string) string {
	for _, value := range values {
		trimmedValue := strings.TrimSpace(value)
		if trimmedValue != "" {
			return trimmedValue
		}
	}
	return ""
}

func LoadSingleAssetPreview(request SingleAssetLoadRequest) (*AssetPreviewResult, error) {
	return LoadSingleAssetPreviewWithTrace(request, nil)
}

func LoadSingleAssetPreviewWithTrace(request SingleAssetLoadRequest, trace *AssetRequestTrace) (*AssetPreviewResult, error) {
	if request.ThumbnailRequest != nil {
		return loadThumbnailPreviewWithTrace(request.ThumbnailRequest, trace)
	}
	return LoadBestImageInfoWithOptionsAndTrace(request.TargetID, false, trace)
}

func LoadAssetStatsPreviewForReference(assetID int64, assetInput string) (*AssetPreviewResult, error) {
	return LoadAssetStatsPreviewForReferenceWithTrace(assetID, assetInput, nil)
}

func LoadAssetStatsPreviewForReferenceWithTrace(assetID int64, assetInput string, trace *AssetRequestTrace) (*AssetPreviewResult, error) {
	request, err := BuildSingleAssetLoadRequest(assetID, assetInput)
	if err != nil {
		return nil, err
	}
	if request.ThumbnailRequest != nil {
		return loadThumbnailPreviewWithTrace(request.ThumbnailRequest, trace)
	}
	return LoadBestImageInfoWithOptionsAndTrace(request.TargetID, true, trace)
}

func BuildSingleAssetLoadRequestFromAssetID(assetID int64) SingleAssetLoadRequest {
	return SingleAssetLoadRequest{TargetID: assetID}
}

func BuildSingleAssetLoadRequest(assetID int64, assetInput string) (SingleAssetLoadRequest, error) {
	trimmedInput := strings.TrimSpace(assetInput)
	if trimmedInput != "" {
		return ParseSingleAssetLoadRequest(trimmedInput)
	}
	if assetID <= 0 {
		return SingleAssetLoadRequest{}, fmt.Errorf("Asset ID is invalid")
	}
	return BuildSingleAssetLoadRequestFromAssetID(assetID), nil
}

func (request SingleAssetLoadRequest) RequiresAuth() bool {
	return request.ThumbnailRequest == nil
}

func (request SingleAssetLoadRequest) LogDescription() string {
	if request.ThumbnailRequest == nil {
		return fmt.Sprintf("asset %d", request.TargetID)
	}
	return fmt.Sprintf(
		"rbxthumb %s %d (%s)",
		request.ThumbnailRequest.Type,
		request.TargetID,
		request.ThumbnailRequest.Size,
	)
}

func ScanAssetReferenceDisplayInput(assetID int64, assetInput string) string {
	trimmedInput := strings.TrimSpace(assetInput)
	if trimmedInput != "" {
		return trimmedInput
	}
	return strconv.FormatInt(assetID, 10)
}

func buildThumbnailRequestContentCacheKey(request *RbxThumbRequest, version string) string {
	if request == nil {
		return ""
	}
	return fmt.Sprintf(
		"thumbnail:type=%s:id=%d:size=%s:format=%s:circular=%t:version=%s",
		strings.ToLower(strings.TrimSpace(request.Type)),
		request.TargetID,
		strings.ToLower(strings.TrimSpace(request.Size)),
		strings.ToLower(strings.TrimSpace(request.Format)),
		request.IsCircular,
		strings.TrimSpace(version),
	)
}

func buildAssetThumbnailContentCacheKey(assetID int64, version string) string {
	return fmt.Sprintf("thumbnail:asset=%d:version=%s", assetID, strings.TrimSpace(version))
}

func BuildAssetFileContentCacheKey(assetID int64, assetTypeID int) string {
	return fmt.Sprintf("asset-file:id=%d:type=%d", assetID, assetTypeID)
}

func buildAssetDeliveryMetadataCacheKey(assetID int64) string {
	return fmt.Sprintf("asset-delivery:id=%d", assetID)
}

func buildAssetThumbnailMetadataCacheKey(assetID int64) string {
	return fmt.Sprintf("thumbnail-meta:asset=%d", assetID)
}

func buildThumbnailRequestMetadataCacheKey(request *RbxThumbRequest) string {
	if request == nil {
		return ""
	}
	return fmt.Sprintf(
		"thumbnail-meta:type=%s:id=%d:size=%s:format=%s:circular=%t",
		strings.ToLower(strings.TrimSpace(request.Type)),
		request.TargetID,
		strings.ToLower(strings.TrimSpace(request.Size)),
		strings.ToLower(strings.TrimSpace(request.Format)),
		request.IsCircular,
	)
}

func loadThumbnailPreview(request *RbxThumbRequest) (*AssetPreviewResult, error) {
	return loadThumbnailPreviewWithTrace(request, nil)
}

func loadThumbnailPreviewWithTrace(request *RbxThumbRequest, trace *AssetRequestTrace) (*AssetPreviewResult, error) {
	thumbnailDetails, thumbnailRawJSON, err := fetchThumbnailInfoForRequestWithTrace(request, trace)
	if err != nil {
		return nil, err
	}
	if thumbnailDetails.ImageURL == "" {
		return nil, fmt.Errorf("No thumbnail image available. State: %s", thumbnailDetails.State)
	}

	thumbnailImageInfo, thumbnailImageErr := fetchImageInfoWithCacheKeyAndTrace(
		thumbnailDetails.ImageURL,
		buildThumbnailRequestContentCacheKey(request, thumbnailDetails.Version),
		request.TargetID,
		true,
		trace,
	)
	if thumbnailImageErr != nil {
		return nil, fmt.Errorf("Thumbnail download failed (%s)", thumbnailImageErr.Error())
	}

	return &AssetPreviewResult{
		Image:              thumbnailImageInfo,
		Stats:              thumbnailImageInfo,
		ReferencedAssetIDs: []int64{},
		ChildAssets:        []ChildAssetInfo{},
		TotalBytesSize:     thumbnailImageInfo.BytesSize,
		Source:             roblox.SourceThumbnailsDirect,
		State:              thumbnailDetails.State,
		WarningMessage:     "",
		AssetDeliveryJSON:  "",
		ThumbnailJSON:      thumbnailRawJSON,
		EconomyJSON:        "",
		RustyAssetToolJSON: "",
		AssetTypeID:        0,
		AssetTypeName:      "Thumbnail",
		DownloadBytes:      []byte{},
		DownloadFileName:   "",
		DownloadIsOriginal: false,
	}, nil
}

func LoadBestImageInfo(assetID int64) (*AssetPreviewResult, error) {
	return LoadBestImageInfoWithOptions(assetID, false)
}

func LoadBestImageInfoWithOptions(assetID int64, skipThumbnail bool) (*AssetPreviewResult, error) {
	return LoadBestImageInfoWithOptionsAndTrace(assetID, skipThumbnail, nil)
}

func LoadBestImageInfoWithOptionsAndTrace(assetID int64, skipThumbnail bool, trace *AssetRequestTrace) (*AssetPreviewResult, error) {
	deliveryInfo, assetDeliveryErr := FetchAssetDeliveryInfoWithTrace(assetID, trace)
	assetDeliveryRawJSON := ""
	assetTypeID := 0
	assetTypeName := "Unknown"
	if deliveryInfo != nil {
		assetDeliveryRawJSON = deliveryInfo.RawJSON
		assetTypeID = deliveryInfo.AssetTypeID
		assetTypeName = deliveryInfo.AssetTypeName
	}

	economyRawJSON := ""
	if assetTypeID <= 0 {
		economyInfo, economyErr := fetchAssetDetailsFromEconomy(assetID)
		if economyErr == nil && economyInfo != nil {
			economyRawJSON = economyInfo.RawJSON
			if economyInfo.AssetTypeID > 0 {
				assetTypeID = economyInfo.AssetTypeID
				assetTypeName = roblox.GetAssetTypeName(economyInfo.AssetTypeID)
			}
		}
	}

	var statsInfo *ImageInfo
	referencedAssetIDs := []int64{}
	rustyAssetToolRawJSON := ""
	downloadBytes := []byte(nil)
	downloadFileName := ""
	downloadIsOriginal := false
	if assetDeliveryErr == nil && deliveryInfo != nil {
		deliveryFileInfo, deliveryFileErr := fetchAssetFileInfoWithCacheKeyAndTrace(
			deliveryInfo.Location,
			BuildAssetFileContentCacheKey(assetID, assetTypeID),
			assetID,
			assetTypeID,
			true,
			trace,
		)
		if deliveryFileErr != nil {
			if isRustyAssetToolFailure(deliveryFileErr) {
				return nil, deliveryFileErr
			}
			assetDeliveryErr = deliveryFileErr
		} else if deliveryFileInfo != nil {
			statsInfo = deliveryFileInfo.Info
			referencedAssetIDs = deliveryFileInfo.ReferencedAssetIDs
			rustyAssetToolRawJSON = deliveryFileInfo.RustyAssetToolJSON
			downloadBytes = append([]byte(nil), deliveryFileInfo.FileBytes...)
			downloadFileName = deliveryFileInfo.FileName
			downloadIsOriginal = !deliveryFileInfo.IsImage
			if deliveryFileInfo.IsImage {
				totalBytesSize, childAssets := computeChildAssetsAndTotalWithTrace(
					deliveryFileInfo.Info.BytesSize,
					referencedAssetIDs,
					deliveryFileInfo.RustyAssetToolReferences,
					trace,
				)
				return &AssetPreviewResult{
					Image:              deliveryFileInfo.Info,
					Stats:              deliveryFileInfo.Info,
					ReferencedAssetIDs: referencedAssetIDs,
					ChildAssets:        childAssets,
					TotalBytesSize:     totalBytesSize,
					Source:             roblox.SourceAssetDeliveryInGame,
					State:              roblox.StateCompleted,
					WarningMessage:     "",
					AssetDeliveryJSON:  assetDeliveryRawJSON,
					ThumbnailJSON:      "",
					EconomyJSON:        economyRawJSON,
					RustyAssetToolJSON: deliveryFileInfo.RustyAssetToolJSON,
					AssetTypeID:        assetTypeID,
					AssetTypeName:      assetTypeName,
					DownloadBytes:      downloadBytes,
					DownloadFileName:   downloadFileName,
					DownloadIsOriginal: false,
				}, nil
			}
			if assetTypeID > 0 && assetTypeID != roblox.AssetTypeImage {
				assetDeliveryErr = fmt.Errorf("asset type %s (%d) is not directly previewable from AssetDelivery payload", assetTypeName, assetTypeID)
			} else {
				assetDeliveryErr = fmt.Errorf("AssetDelivery file is not an image preview")
			}
		}
	}

	if skipThumbnail {
		if statsInfo != nil {
			return buildNoThumbnailPreviewResult(
				statsInfo,
				referencedAssetIDs,
				assetDeliveryRawJSON,
				"",
				economyRawJSON,
				rustyAssetToolRawJSON,
				assetTypeID,
				assetTypeName,
				"",
				downloadBytes,
				downloadFileName,
				downloadIsOriginal,
			), nil
		}
		assetDeliveryErrText := "unknown"
		if assetDeliveryErr != nil {
			assetDeliveryErrText = assetDeliveryErr.Error()
		}
		return nil, fmt.Errorf("AssetDelivery failed (%s)", assetDeliveryErrText)
	}

	thumbnailInfo, thumbnailRawJSON, thumbnailErr := fetchThumbnailInfoWithTrace(assetID, trace)
	assetDeliveryErrText := "unknown AssetDelivery error"
	if assetDeliveryErr != nil {
		assetDeliveryErrText = assetDeliveryErr.Error()
	}
	if thumbnailErr != nil {
		if statsInfo != nil {
			return buildNoThumbnailPreviewResult(
				statsInfo,
				referencedAssetIDs,
				assetDeliveryRawJSON,
				thumbnailRawJSON,
				economyRawJSON,
				rustyAssetToolRawJSON,
				assetTypeID,
				assetTypeName,
				fmt.Sprintf("Thumbnail lookup failed (%s). AssetDelivery details are shown without a preview image.", thumbnailErr.Error()),
				downloadBytes,
				downloadFileName,
				downloadIsOriginal,
			), nil
		}
		return nil, fmt.Errorf("AssetDelivery failed (%s) and thumbnail lookup failed (%s)", assetDeliveryErrText, thumbnailErr.Error())
	}

	if thumbnailInfo.ImageURL == "" {
		if statsInfo != nil {
			return buildNoThumbnailPreviewResult(
				statsInfo,
				referencedAssetIDs,
				assetDeliveryRawJSON,
				thumbnailRawJSON,
				economyRawJSON,
				rustyAssetToolRawJSON,
				assetTypeID,
				assetTypeName,
				fmt.Sprintf("Thumbnail image URL is empty (state=%s). AssetDelivery details are shown without a preview image.", thumbnailInfo.State),
				downloadBytes,
				downloadFileName,
				downloadIsOriginal,
			), nil
		}
		return nil, fmt.Errorf("No image available. State: %s. AssetDelivery error: %s", thumbnailInfo.State, assetDeliveryErrText)
	}

	thumbnailImageInfo, thumbnailImageErr := fetchImageInfoWithCacheKeyAndTrace(
		thumbnailInfo.ImageURL,
		buildAssetThumbnailContentCacheKey(assetID, thumbnailInfo.Version),
		assetID,
		true,
		trace,
	)
	if thumbnailImageErr != nil {
		if statsInfo != nil {
			return buildNoThumbnailPreviewResult(
				statsInfo,
				referencedAssetIDs,
				assetDeliveryRawJSON,
				thumbnailRawJSON,
				economyRawJSON,
				rustyAssetToolRawJSON,
				assetTypeID,
				assetTypeName,
				fmt.Sprintf("Thumbnail download failed (%s). AssetDelivery details are shown without a preview image.", thumbnailImageErr.Error()),
				downloadBytes,
				downloadFileName,
				downloadIsOriginal,
			), nil
		}
		return nil, fmt.Errorf("Thumbnail download failed (%s). AssetDelivery error: %s", thumbnailImageErr.Error(), assetDeliveryErrText)
	}

	totalBytesSize, childAssets := computeChildAssetsAndTotal(
		chooseStatsInfo(statsInfo, thumbnailImageInfo).BytesSize,
		referencedAssetIDs,
		nil,
	)
	return &AssetPreviewResult{
		Image:              thumbnailImageInfo,
		Stats:              chooseStatsInfo(statsInfo, thumbnailImageInfo),
		ReferencedAssetIDs: referencedAssetIDs,
		ChildAssets:        childAssets,
		TotalBytesSize:     totalBytesSize,
		Source:             roblox.SourceThumbnailsFallback,
		State:              thumbnailInfo.State,
		WarningMessage:     assetDeliveryErrText,
		AssetDeliveryJSON:  assetDeliveryRawJSON,
		ThumbnailJSON:      thumbnailRawJSON,
		EconomyJSON:        economyRawJSON,
		RustyAssetToolJSON: rustyAssetToolRawJSON,
		AssetTypeID:        assetTypeID,
		AssetTypeName:      assetTypeName,
		DownloadBytes:      downloadBytes,
		DownloadFileName:   downloadFileName,
		DownloadIsOriginal: downloadIsOriginal,
	}, nil
}

func isRustyAssetToolFailure(err error) bool {
	if err == nil {
		return false
	}
	normalized := strings.ToLower(err.Error())
	return strings.Contains(normalized, "rusty asset tool failed") || strings.Contains(normalized, "rust extractor failed")
}

func buildNoThumbnailPreviewResult(
	statsInfo *ImageInfo,
	referencedAssetIDs []int64,
	assetDeliveryRawJSON string,
	thumbnailRawJSON string,
	economyRawJSON string,
	rustyAssetToolRawJSON string,
	assetTypeID int,
	assetTypeName string,
	warningMessage string,
	downloadBytes []byte,
	downloadFileName string,
	downloadIsOriginal bool,
) *AssetPreviewResult {
	safeStatsInfo := ensureImageInfo(statsInfo)
	totalBytesSize, childAssets := computeChildAssetsAndTotal(safeStatsInfo.BytesSize, referencedAssetIDs, nil)
	return &AssetPreviewResult{
		Image:              &ImageInfo{},
		Stats:              safeStatsInfo,
		ReferencedAssetIDs: referencedAssetIDs,
		ChildAssets:        childAssets,
		TotalBytesSize:     totalBytesSize,
		Source:             roblox.SourceNoThumbnail,
		State:              roblox.StateUnavailable,
		WarningMessage:     warningMessage,
		AssetDeliveryJSON:  assetDeliveryRawJSON,
		ThumbnailJSON:      thumbnailRawJSON,
		EconomyJSON:        economyRawJSON,
		RustyAssetToolJSON: rustyAssetToolRawJSON,
		AssetTypeID:        assetTypeID,
		AssetTypeName:      assetTypeName,
		DownloadBytes:      append([]byte(nil), downloadBytes...),
		DownloadFileName:   downloadFileName,
		DownloadIsOriginal: downloadIsOriginal,
	}
}

func fetchThumbnailInfo(assetID int64) (*ThumbnailInfo, string, error) {
	return fetchThumbnailInfoWithTrace(assetID, nil)
}

func fetchThumbnailInfoWithTrace(assetID int64, trace *AssetRequestTrace) (*ThumbnailInfo, string, error) {
	cacheSettings := LoadCacheSettings()
	if cacheSettings.Enabled && cacheSettings.Folder != "" {
		cacheKey := buildAssetThumbnailMetadataCacheKey(assetID)
		var cachedEntry CachedThumbnailInfo
		cacheHit, err := readAssetDownloadJSONCacheEntry(cacheSettings.Folder, cacheKey, 0, &cachedEntry)
		if err != nil {
			debug.Logf("Thumbnail metadata cache read failed for %s: %s", cacheKey, err.Error())
		} else if cacheHit {
			debug.Logf("Thumbnail metadata cache hit for %s", cacheKey)
			return &cachedEntry.Info, cachedEntry.RawJSON, nil
		}
	}

	response, err := roblox.DoThumbnailGet(assetID, requestTimeout)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	responseBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, "", err
	}
	rawResponse := string(responseBytes)
	if response.StatusCode != http.StatusOK {
		return nil, rawResponse, fmt.Errorf("HTTP %d", response.StatusCode)
	}

	var apiResponse thumbnailsResponse
	if err := json.Unmarshal(responseBytes, &apiResponse); err != nil {
		return nil, rawResponse, err
	}

	if len(apiResponse.Data) == 0 {
		return nil, rawResponse, fmt.Errorf("No thumbnail response returned for this asset")
	}

	firstResult := apiResponse.Data[0]
	info := &ThumbnailInfo{
		ImageURL: firstResult.ImageURL,
		State:    firstResult.State,
		Version:  firstResult.Version,
	}
	if cacheSettings.Enabled && cacheSettings.Folder != "" {
		cacheKey := buildAssetThumbnailMetadataCacheKey(assetID)
		if err := writeAssetDownloadJSONCacheEntry(cacheSettings.Folder, cacheKey, CachedThumbnailInfo{
			Info:    *info,
			RawJSON: rawResponse,
		}); err != nil {
			debug.Logf("Thumbnail metadata cache write failed for %s: %s", cacheKey, err.Error())
		}
	}
	return info, rawResponse, nil
}

func fetchThumbnailInfoForRequest(request *RbxThumbRequest) (*ThumbnailInfo, string, error) {
	return fetchThumbnailInfoForRequestWithTrace(request, nil)
}

func fetchThumbnailInfoForRequestWithTrace(request *RbxThumbRequest, trace *AssetRequestTrace) (*ThumbnailInfo, string, error) {
	cacheSettings := LoadCacheSettings()
	if cacheSettings.Enabled && cacheSettings.Folder != "" {
		cacheKey := buildThumbnailRequestMetadataCacheKey(request)
		var cachedEntry CachedThumbnailInfo
		cacheHit, err := readAssetDownloadJSONCacheEntry(cacheSettings.Folder, cacheKey, 0, &cachedEntry)
		if err != nil {
			debug.Logf("Thumbnail request metadata cache read failed for %s: %s", cacheKey, err.Error())
		} else if cacheHit {
			debug.Logf("Thumbnail request metadata cache hit for %s", cacheKey)
			return &cachedEntry.Info, cachedEntry.RawJSON, nil
		}
	}

	requestBody := []ThumbnailsBatchRequest{{
		Type:       request.Type,
		TargetID:   request.TargetID,
		Size:       request.Size,
		Format:     request.Format,
		IsCircular: request.IsCircular,
	}}
	requestJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, "", err
	}

	response, err := roblox.DoThumbnailBatchPost(bytes.NewReader(requestJSON), requestTimeout)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()

	responseBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, "", err
	}
	rawResponse := buildThumbnailBatchDebugJSON(requestBody[0], responseBytes)
	if response.StatusCode != http.StatusOK {
		return nil, rawResponse, fmt.Errorf("HTTP %d", response.StatusCode)
	}

	var apiResponse thumbnailsResponse
	if err := json.Unmarshal(responseBytes, &apiResponse); err != nil {
		return nil, rawResponse, err
	}
	if len(apiResponse.Data) == 0 {
		return nil, rawResponse, fmt.Errorf("No thumbnail response returned for this request")
	}

	firstResult := apiResponse.Data[0]
	info := &ThumbnailInfo{
		ImageURL: firstResult.ImageURL,
		State:    firstResult.State,
		Version:  firstResult.Version,
	}
	if cacheSettings.Enabled && cacheSettings.Folder != "" {
		cacheKey := buildThumbnailRequestMetadataCacheKey(request)
		if err := writeAssetDownloadJSONCacheEntry(cacheSettings.Folder, cacheKey, CachedThumbnailInfo{
			Info:    *info,
			RawJSON: rawResponse,
		}); err != nil {
			debug.Logf("Thumbnail request metadata cache write failed for %s: %s", cacheKey, err.Error())
		}
	}
	return info, rawResponse, nil
}

func buildThumbnailBatchDebugJSON(request ThumbnailsBatchRequest, rawResponse []byte) string {
	debugPayload := struct {
		Request  ThumbnailsBatchRequest `json:"request"`
		Response json.RawMessage        `json:"response"`
	}{
		Request:  request,
		Response: json.RawMessage(rawResponse),
	}
	debugJSON, err := json.MarshalIndent(debugPayload, "", "  ")
	if err != nil {
		return string(rawResponse)
	}
	return string(debugJSON)
}

func FetchAssetDeliveryInfo(assetID int64) (*AssetDeliveryInfo, error) {
	return FetchAssetDeliveryInfoWithTrace(assetID, nil)
}

func FetchAssetDeliveryInfoWithTrace(assetID int64, trace *AssetRequestTrace) (*AssetDeliveryInfo, error) {
	cacheSettings := LoadCacheSettings()
	if cacheSettings.Enabled && cacheSettings.Folder != "" {
		cacheKey := buildAssetDeliveryMetadataCacheKey(assetID)
		var cachedEntry AssetDeliveryInfo
		cacheHit, err := readAssetDownloadJSONCacheEntry(cacheSettings.Folder, cacheKey, 0, &cachedEntry)
		if err != nil {
			debug.Logf("AssetDelivery metadata cache read failed for %s: %s", cacheKey, err.Error())
		} else if cacheHit {
			debug.Logf("AssetDelivery metadata cache hit for %s", cacheKey)
			return &cachedEntry, nil
		}
	}

	response, err := roblox.DoAssetDeliveryGet(assetID, requestTimeout)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	responseBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	rawResponse := string(responseBytes)
	info := &AssetDeliveryInfo{
		RawJSON:       rawResponse,
		AssetTypeID:   0,
		AssetTypeName: "Unknown",
	}
	var apiResponse assetDeliveryResponse
	if err := json.Unmarshal(responseBytes, &apiResponse); err == nil {
		info.Location = apiResponse.Location
		info.AssetTypeID = apiResponse.AssetTypeID
		info.AssetTypeName = roblox.GetAssetTypeName(apiResponse.AssetTypeID)
	}

	if response.StatusCode != http.StatusOK {
		reason := extractAssetDeliveryFailureReason(rawResponse)
		if reason != "" {
			return info, fmt.Errorf("%s", reason)
		}
		return info, fmt.Errorf("AssetDelivery returned HTTP %d", response.StatusCode)
	}

	if apiResponse.Location == "" {
		reason := extractAssetDeliveryFailureReason(rawResponse)
		if reason != "" {
			return info, fmt.Errorf("%s", reason)
		}
		return info, fmt.Errorf("AssetDelivery did not return a location")
	}

	if cacheSettings.Enabled && cacheSettings.Folder != "" {
		cacheKey := buildAssetDeliveryMetadataCacheKey(assetID)
		if err := writeAssetDownloadJSONCacheEntry(cacheSettings.Folder, cacheKey, info); err != nil {
			debug.Logf("AssetDelivery metadata cache write failed for %s: %s", cacheKey, err.Error())
		}
	}

	return info, nil
}

func extractAssetDeliveryFailureReason(rawResponse string) string {
	if rawResponse == "" {
		return ""
	}

	var parsedResponse assetDeliveryResponse
	if err := json.Unmarshal([]byte(rawResponse), &parsedResponse); err != nil {
		return ""
	}

	for _, errorEntry := range parsedResponse.Errors {
		trimmedMessage := strings.TrimSpace(errorEntry.Message)
		if trimmedMessage != "" {
			return trimmedMessage
		}
	}

	return ""
}

func fetchImageInfo(imageURL string, assetID int64, includeHash bool) (*ImageInfo, error) {
	return fetchImageInfoWithCacheKey(imageURL, "", assetID, includeHash)
}

func fetchImageInfoWithCacheKey(imageURL string, cacheKey string, assetID int64, includeHash bool) (*ImageInfo, error) {
	return fetchImageInfoWithCacheKeyAndTrace(imageURL, cacheKey, assetID, includeHash, nil)
}

func fetchImageInfoWithCacheKeyAndTrace(imageURL string, cacheKey string, assetID int64, includeHash bool, trace *AssetRequestTrace) (*ImageInfo, error) {
	imageBytes, contentType, err := downloadRobloxContentBytesWithCacheKeyAndTrace(imageURL, cacheKey, requestTimeout, trace)
	if err != nil {
		return nil, err
	}

	imageConfig, imageFormat, err := image.DecodeConfig(bytes.NewReader(imageBytes))
	if err != nil {
		return nil, err
	}
	resourceName := fmt.Sprintf("asset_%d.%s", assetID, imageFormat)

	sha256Value := ""
	if includeHash {
		sha256Value = ComputeSHA256Hex(imageBytes)
	}
	analysis, analysisErr := analyzeImage(imageBytes)
	if analysisErr != nil {
		analysis = imageAnalysis{}
	}
	return &ImageInfo{
		Resource:                 fyne.NewStaticResource(resourceName, imageBytes),
		Width:                    imageConfig.Width,
		Height:                   imageConfig.Height,
		BytesSize:                len(imageBytes),
		RecompressedPNGByteSize:  analysis.RecompressedPNGBytes,
		RecompressedJPEGByteSize: analysis.RecompressedJPEGBytes,
		Format:                   strings.ToUpper(imageFormat),
		ContentType:              contentType,
		SHA256:                   sha256Value,
		HasAlphaChannel:          analysis.HasAlphaChannel,
		NonOpaqueAlphaPixels:     analysis.NonOpaqueAlphaPixels,
	}, nil
}

func fetchAssetFileInfo(fileURL string, assetID int64, assetTypeID int, includeHash bool) (*AssetFileInfo, error) {
	return fetchAssetFileInfoWithCacheKey(fileURL, "", assetID, assetTypeID, includeHash)
}

func fetchAssetFileInfoWithCacheKey(fileURL string, cacheKey string, assetID int64, assetTypeID int, includeHash bool) (*AssetFileInfo, error) {
	return fetchAssetFileInfoWithCacheKeyAndTrace(fileURL, cacheKey, assetID, assetTypeID, includeHash, nil)
}

func fetchAssetFileInfoWithCacheKeyAndTrace(fileURL string, cacheKey string, assetID int64, assetTypeID int, includeHash bool, trace *AssetRequestTrace) (*AssetFileInfo, error) {
	// Image assets: KTX2 drives metadata (true upload dim, served wire
	// bytes, BCn format), the legacy PNG fetch — run in parallel — keeps
	// the fyne preview Resource and pixel-level analysis (alpha pixel
	// count, recompressed PNG/JPEG sizes) working. If KTX2 fails (no auth,
	// no transcoded variant, network error) we fall through to the
	// existing PNG-only path below for graceful degradation.
	if assetID > 0 && assetTypeID == roblox.AssetTypeImage {
		if ktx2Info, ktx2Err := fetchImageAssetFileInfoUsingKTX2(assetID, includeHash, fileURL, cacheKey, trace); ktx2Err == nil {
			return ktx2Info, nil
		}
	}

	fileBytes, contentType, err := downloadRobloxContentBytesWithCacheKeyAndTrace(fileURL, cacheKey, requestTimeout, trace)
	if err != nil {
		return nil, err
	}
	sha256Value := ""
	if includeHash {
		sha256Value = ComputeSHA256Hex(fileBytes)
	}
	fileName := buildAssetDownloadFileName(assetID, assetTypeID, contentType, "", false)
	info := &ImageInfo{
		Resource:                 nil,
		Width:                    0,
		Height:                   0,
		BytesSize:                len(fileBytes),
		RecompressedPNGByteSize:  0,
		RecompressedJPEGByteSize: 0,
		Format:                   inferFormatFromContentType(contentType),
		ContentType:              contentType,
		SHA256:                   sha256Value,
	}

	imageConfig, imageFormat, decodeErr := image.DecodeConfig(bytes.NewReader(fileBytes))
	if decodeErr != nil {
		if IsAudioContent != nil && IsAudioContent(assetTypeID, contentType) {
			if ExtractAudio != nil {
				audioMetadata, audioErr := ExtractAudio(fileName, contentType, fileBytes)
				if audioErr == nil && audioMetadata != nil {
					info.Duration = audioMetadata.Duration
					if strings.TrimSpace(audioMetadata.Format) != "" {
						info.Format = audioMetadata.Format
					}
				}
			}
		}
		referencedAssetIDs := []int64{}
		rustyAssetToolReferences := []extractor.Result{}
		rustyAssetToolJSON := ""
		var extractErr error
		if !roblox.ShouldSkipRustExtractionForAssetType(assetTypeID) {
			referencedAssetIDs, rustyAssetToolReferences, rustyAssetToolJSON, extractErr = extractReferencedAssetIDsFromBytes(fileBytes, assetTypeID)
		}
		if extractErr != nil {
			return nil, extractErr
		}
		return &AssetFileInfo{
			Info:                     info,
			IsImage:                  false,
			ReferencedAssetIDs:       referencedAssetIDs,
			RustyAssetToolReferences: rustyAssetToolReferences,
			RustyAssetToolJSON:       rustyAssetToolJSON,
			FileBytes:                fileBytes,
			FileName:                 fileName,
		}, nil
	}

	resourceName := fmt.Sprintf("asset_%d.%s", assetID, imageFormat)
	fileName = buildAssetDownloadFileName(assetID, assetTypeID, contentType, imageFormat, true)
	info.Resource = fyne.NewStaticResource(resourceName, fileBytes)
	info.Width = imageConfig.Width
	info.Height = imageConfig.Height
	info.Format = strings.ToUpper(imageFormat)
	analysis, analysisErr := analyzeImage(fileBytes)
	if analysisErr == nil {
		info.RecompressedPNGByteSize = analysis.RecompressedPNGBytes
		info.RecompressedJPEGByteSize = analysis.RecompressedJPEGBytes
		info.HasAlphaChannel = analysis.HasAlphaChannel
		info.NonOpaqueAlphaPixels = analysis.NonOpaqueAlphaPixels
	}
	referencedAssetIDs := []int64{}
	rustyAssetToolReferences := []extractor.Result{}
	rustyAssetToolJSON := ""
	var extractErr error
	if !roblox.ShouldSkipRustExtractionForAssetType(assetTypeID) {
		referencedAssetIDs, rustyAssetToolReferences, rustyAssetToolJSON, extractErr = extractReferencedAssetIDsFromBytes(fileBytes, assetTypeID)
	}
	if extractErr != nil {
		return nil, extractErr
	}
	return &AssetFileInfo{
		Info:                     info,
		IsImage:                  true,
		ReferencedAssetIDs:       referencedAssetIDs,
		RustyAssetToolReferences: rustyAssetToolReferences,
		RustyAssetToolJSON:       rustyAssetToolJSON,
		FileBytes:                fileBytes,
		FileName:                 fileName,
	}, nil
}

// fetchImageAssetFileInfoUsingKTX2 fetches the highest-resolution KTX2
// representation of an image asset and packages it as an AssetFileInfo.
// Width/Height come from the KTX2 header (true upload resolution).
// BytesSize is the wire size as served (transport-compressed BC payload),
// which is what the player actually downloads.
//
// For the preview Resource the helper decodes mip 0 to RGBA and re-encodes
// as PNG. When the BC format isn't supported by joxblox's local decoders
// (e.g. BC7), the helper concurrently fetches the legacy PNG and uses
// that instead — same fallback path as the old behaviour, just on-demand
// rather than always-on. Returns an error only if KTX2 itself fails so
// the caller can fall back to the legacy-only path.
func fetchImageAssetFileInfoUsingKTX2(assetID int64, includeHash bool, fileURL string, cacheKey string, trace *AssetRequestTrace) (*AssetFileInfo, error) {
	rawBytes, _, fetchErr := roblox.FetchHighestKTX2RepresentationByAssetID(assetID, requestTimeout)
	if fetchErr != nil {
		return nil, fetchErr
	}
	unwrapped, unwrapErr := ktx2.DecompressTransportWrapper(rawBytes)
	if unwrapErr != nil {
		return nil, fmt.Errorf("ktx2 transport unwrap: %w", unwrapErr)
	}
	container, parseErr := ktx2.Parse(unwrapped)
	if parseErr != nil {
		return nil, fmt.Errorf("ktx2 parse: %w", parseErr)
	}

	sha256Value := ""
	if includeHash {
		sha256Value = ComputeSHA256Hex(rawBytes)
	}

	formatLabel := fmt.Sprintf("KTX2/%s", container.Header.VkFormatName())
	fileName := fmt.Sprintf("asset_%d.ktx2", assetID)
	info := &ImageInfo{
		Resource:        nil,
		Width:           int(container.Header.PixelWidth),
		Height:          int(container.Header.PixelHeight),
		BytesSize:       len(rawBytes),
		Format:          formatLabel,
		ContentType:     "application/ktx2",
		SHA256:          sha256Value,
		HasAlphaChannel: container.Header.HasAlpha(),
	}

	if rgba, decodeErr := decodeKTX2Mip0ToRGBA(container); decodeErr == nil {
		info.NonOpaqueAlphaPixels = countNonOpaqueAlphaPixels(rgba)
		previewPNG, encodeErr := encodeFullResolutionPNG(rgba)
		if encodeErr == nil {
			info.Resource = fyne.NewStaticResource(fmt.Sprintf("asset_%d.png", assetID), previewPNG)
			info.RecompressedPNGByteSize = len(previewPNG)
		}
	} else {
		// BC format we can't decode locally (e.g. BC7). Fall back to the
		// legacy 1024-capped PNG just for the preview Resource — KTX2
		// metadata above is still authoritative.
		legacyBytes, _, legacyErr := downloadRobloxContentBytesWithCacheKeyAndTrace(fileURL, cacheKey, requestTimeout, trace)
		if legacyErr == nil && len(legacyBytes) > 0 {
			if _, legacyFormat, configErr := image.DecodeConfig(bytes.NewReader(legacyBytes)); configErr == nil {
				info.Resource = fyne.NewStaticResource(fmt.Sprintf("asset_%d.%s", assetID, legacyFormat), legacyBytes)
			}
			if analysis, analysisErr := analyzeImage(legacyBytes); analysisErr == nil {
				info.RecompressedPNGByteSize = analysis.RecompressedPNGBytes
				info.RecompressedJPEGByteSize = analysis.RecompressedJPEGBytes
				info.NonOpaqueAlphaPixels = analysis.NonOpaqueAlphaPixels
			}
		}
	}

	return &AssetFileInfo{
		Info:               info,
		IsImage:            true,
		ReferencedAssetIDs: []int64{},
		FileBytes:          rawBytes,
		FileName:           fileName,
	}, nil
}

// decodeKTX2Mip0ToRGBA decompresses mip 0 of the supplied KTX2 container
// (handling BasisLZ/Zstd supercompression internally via DecompressLevel)
// and decodes the BCn payload into an *image.NRGBA. Returns an error for
// VkFormat values joxblox doesn't have a local decoder for.
func decodeKTX2Mip0ToRGBA(container *ktx2.Container) (*image.NRGBA, error) {
	if len(container.Levels) == 0 {
		return nil, fmt.Errorf("ktx2: no mip levels")
	}
	mipBytes, decompressErr := container.DecompressLevel(0)
	if decompressErr != nil {
		return nil, decompressErr
	}
	width, height := container.MipDimensions(0)
	switch container.Header.VkFormat {
	case ktx2.VkFormatBC1RGBUnorm, ktx2.VkFormatBC1RGBSrgb,
		ktx2.VkFormatBC1RGBAUnorm, ktx2.VkFormatBC1RGBASrgb:
		return renderdoc.DecodeBC1(mipBytes, width, height)
	case ktx2.VkFormatBC3Unorm, ktx2.VkFormatBC3Srgb:
		return renderdoc.DecodeBC3(mipBytes, width, height)
	}
	return nil, fmt.Errorf("ktx2: no local decoder for vkFormat %d (%s)", container.Header.VkFormat, container.Header.VkFormatName())
}

// encodeFullResolutionPNG encodes rgba as PNG bytes at its native
// dimensions. No downscaling — the preview matches the asset's actual
// upload resolution.
func encodeFullResolutionPNG(rgba *image.NRGBA) ([]byte, error) {
	var buffer bytes.Buffer
	if encodeErr := png.Encode(&buffer, rgba); encodeErr != nil {
		return nil, encodeErr
	}
	return buffer.Bytes(), nil
}

// countNonOpaqueAlphaPixels walks rgba once and counts pixels whose alpha
// byte is < 255. Mirrors the count produced by analyzeImage on a PNG —
// the threshold the optimizer uses to flag wasteful BC3 vs BC1 textures.
func countNonOpaqueAlphaPixels(rgba *image.NRGBA) int64 {
	pixels := rgba.Pix
	stride := rgba.Stride
	bounds := rgba.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	var count int64
	for y := 0; y < height; y++ {
		row := pixels[y*stride : y*stride+width*4]
		for x := 0; x < width; x++ {
			if row[x*4+3] < 255 {
				count++
			}
		}
	}
	return count
}

func buildAssetDownloadFileName(assetID int64, assetTypeID int, contentType string, imageFormat string, isImage bool) string {
	fileExtension := "bin"
	if isImage {
		trimmedImageFormat := strings.ToLower(strings.TrimSpace(imageFormat))
		if trimmedImageFormat != "" {
			fileExtension = trimmedImageFormat
		}
	} else {
		_ = contentType
		fileExtension = roblox.GetAssetDownloadExtension(assetTypeID)
	}
	return fmt.Sprintf("asset_%d.%s", assetID, fileExtension)
}

func BuildFallbackWarningText(warningReason string) string {
	if warningReason == "" {
		return "failed to get in-game version, showing a thumbnail instead, note that this is not representitive of the actual asset size, dimentions etc"
	}

	return fmt.Sprintf(
		"failed to get in-game version, showing a thumbnail instead, note that this is not representitive of the actual asset size, dimentions etc. reason: %s",
		warningReason,
	)
}

func fetchAssetDetailsFromEconomy(assetID int64) (*EconomyAssetDetailsInfo, error) {
	response, err := roblox.DoEconomyDetailsGet(assetID, requestTimeout)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}

	responseBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	var detailsResponse economyAssetDetailsResponse
	if err := json.Unmarshal(responseBytes, &detailsResponse); err != nil {
		return nil, err
	}
	return &EconomyAssetDetailsInfo{
		AssetTypeID: detailsResponse.AssetTypeID,
		RawJSON:     string(responseBytes),
	}, nil
}

func inferFormatFromContentType(contentType string) string {
	trimmedContentType := strings.TrimSpace(contentType)
	if trimmedContentType == "" {
		return "UNKNOWN"
	}

	contentTypeParts := strings.Split(trimmedContentType, "/")
	if len(contentTypeParts) < 2 {
		return strings.ToUpper(trimmedContentType)
	}
	return strings.ToUpper(contentTypeParts[1])
}

func chooseStatsInfo(preferredStats *ImageInfo, fallbackStats *ImageInfo) *ImageInfo {
	if preferredStats != nil {
		return preferredStats
	}
	return fallbackStats
}

func ensureImageInfo(info *ImageInfo) *ImageInfo {
	if info != nil {
		return info
	}
	return &ImageInfo{}
}

func ComputeSHA256Hex(payload []byte) string {
	hashBytes := sha256.Sum256(payload)
	return hex.EncodeToString(hashBytes[:])
}

type imageAnalysis struct {
	RecompressedPNGBytes  int
	RecompressedJPEGBytes int
	HasAlphaChannel       bool
	NonOpaqueAlphaPixels  int64
}

func analyzeImage(imageBytes []byte) (imageAnalysis, error) {
	decodedImage, _, decodeErr := image.Decode(bytes.NewReader(imageBytes))
	if decodeErr != nil {
		return imageAnalysis{}, decodeErr
	}
	var recompressedBuffer bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if encodeErr := encoder.Encode(&recompressedBuffer, decodedImage); encodeErr != nil {
		return imageAnalysis{}, encodeErr
	}
	var recompressedJPEGBuffer bytes.Buffer
	if encodeErr := jpeg.Encode(&recompressedJPEGBuffer, decodedImage, &jpeg.Options{Quality: 1}); encodeErr != nil {
		return imageAnalysis{}, encodeErr
	}
	hasAlpha, nonOpaque := classifyImageAlpha(decodedImage)
	return imageAnalysis{
		RecompressedPNGBytes:  recompressedBuffer.Len(),
		RecompressedJPEGBytes: recompressedJPEGBuffer.Len(),
		HasAlphaChannel:       hasAlpha,
		NonOpaqueAlphaPixels:  nonOpaque,
	}, nil
}

// classifyImageAlpha reports whether the source image exposes an alpha
// channel and, if so, counts how many pixels have a < 255 alpha byte. The
// count is what drives BC1/BC3 classification and the "wasteful BC3"
// threshold downstream — zero means Roblox will still pick BC1, and tiny
// non-zero fractions mark textures whose alpha could probably be dropped.
func classifyImageAlpha(img image.Image) (hasAlpha bool, nonOpaque int64) {
	switch img.ColorModel() {
	case color.RGBAModel, color.NRGBAModel, color.RGBA64Model, color.NRGBA64Model, color.AlphaModel, color.Alpha16Model:
		hasAlpha = true
	default:
		return false, 0
	}

	if rgba, ok := img.(*image.RGBA); ok {
		return true, countNonOpaqueAlpha(rgba.Pix, rgba.Stride, rgba.Rect)
	}
	if nrgba, ok := img.(*image.NRGBA); ok {
		return true, countNonOpaqueAlpha(nrgba.Pix, nrgba.Stride, nrgba.Rect)
	}

	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a < 0xFFFF {
				nonOpaque++
			}
		}
	}
	return true, nonOpaque
}

func countNonOpaqueAlpha(pix []byte, stride int, rect image.Rectangle) int64 {
	width := rect.Dx()
	height := rect.Dy()
	var count int64
	for row := 0; row < height; row++ {
		base := row * stride
		for col := 0; col < width; col++ {
			if pix[base+col*4+3] != 0xFF {
				count++
			}
		}
	}
	return count
}

func computeChildAssetsAndTotal(selfBytesSize int, referencedAssetIDs []int64, rustyAssetToolReferences []extractor.Result) (int, []ChildAssetInfo) {
	return computeChildAssetsAndTotalWithTrace(selfBytesSize, referencedAssetIDs, rustyAssetToolReferences, nil)
}

func computeChildAssetsAndTotalWithTrace(selfBytesSize int, referencedAssetIDs []int64, rustyAssetToolReferences []extractor.Result, trace *AssetRequestTrace) (int, []ChildAssetInfo) {
	totalBytesSize := selfBytesSize
	childAssets := make([]ChildAssetInfo, 0, len(referencedAssetIDs))
	referenceByAssetID := map[int64]extractor.Result{}
	for _, reference := range rustyAssetToolReferences {
		if reference.ID <= 0 {
			continue
		}
		if _, exists := referenceByAssetID[reference.ID]; exists {
			continue
		}
		referenceByAssetID[reference.ID] = reference
	}
	for _, referencedAssetID := range referencedAssetIDs {
		referenceContext := referenceByAssetID[referencedAssetID]
		referencedAssetInfo, infoErr := getAssetSelfInfoWithTrace(referencedAssetID, trace)
		if infoErr != nil || referencedAssetInfo.BytesSize <= 0 {
			childAssets = append(childAssets, ChildAssetInfo{
				AssetID:      referencedAssetID,
				BytesSize:    0,
				AssetTypeID:  referencedAssetInfo.AssetTypeID,
				Resolved:     false,
				InstanceType: strings.TrimSpace(referenceContext.InstanceType),
				InstanceName: strings.TrimSpace(referenceContext.InstanceName),
				InstancePath: strings.TrimSpace(referenceContext.InstancePath),
				PropertyName: strings.TrimSpace(referenceContext.PropertyName),
			})
			continue
		}
		totalBytesSize += referencedAssetInfo.BytesSize
		childAssets = append(childAssets, ChildAssetInfo{
			AssetID:      referencedAssetID,
			BytesSize:    referencedAssetInfo.BytesSize,
			AssetTypeID:  referencedAssetInfo.AssetTypeID,
			Resolved:     true,
			InstanceType: strings.TrimSpace(referenceContext.InstanceType),
			InstanceName: strings.TrimSpace(referenceContext.InstanceName),
			InstancePath: strings.TrimSpace(referenceContext.InstancePath),
			PropertyName: strings.TrimSpace(referenceContext.PropertyName),
		})
	}
	return totalBytesSize, childAssets
}

func getAssetSelfInfo(assetID int64) (AssetSelfInfo, error) {
	return getAssetSelfInfoWithTrace(assetID, nil)
}

func getAssetSelfInfoWithTrace(assetID int64, trace *AssetRequestTrace) (AssetSelfInfo, error) {
	assetSizeCache.mutex.RLock()
	cachedAssetInfo, found := assetSizeCache.infoByAssetID[assetID]
	assetSizeCache.mutex.RUnlock()
	if found {
		return cachedAssetInfo, nil
	}

	assetDeliveryInfo, deliveryErr := FetchAssetDeliveryInfoWithTrace(assetID, trace)
	selfInfo := AssetSelfInfo{}
	if assetDeliveryInfo != nil {
		selfInfo.AssetTypeID = assetDeliveryInfo.AssetTypeID
	}
	if deliveryErr != nil || assetDeliveryInfo == nil || strings.TrimSpace(assetDeliveryInfo.Location) == "" {
		if deliveryErr != nil {
			return selfInfo, deliveryErr
		}
		return selfInfo, fmt.Errorf("asset delivery location unavailable")
	}

	fileInfo, fileErr := fetchAssetFileInfoWithCacheKeyAndTrace(
		assetDeliveryInfo.Location,
		BuildAssetFileContentCacheKey(assetID, selfInfo.AssetTypeID),
		assetID,
		selfInfo.AssetTypeID,
		false,
		trace,
	)
	if fileErr != nil || fileInfo == nil || fileInfo.Info == nil {
		if fileErr != nil {
			return selfInfo, fileErr
		}
		return selfInfo, fmt.Errorf("asset file info unavailable")
	}
	selfInfo.BytesSize = fileInfo.Info.BytesSize

	assetSizeCache.mutex.Lock()
	assetSizeCache.infoByAssetID[assetID] = selfInfo
	assetSizeCache.mutex.Unlock()
	return selfInfo, nil
}

func extractReferencedAssetIDsFromBytes(fileBytes []byte, assetTypeID int) ([]int64, []extractor.Result, string, error) {
	result, rustErr := extractor.ExtractAssetIDsFromBytesWithCounts(fileBytes, assetTypeID, extractor.DefaultLimit)
	if rustErr != nil {
		debug.Logf("Referenced asset extraction Rusty Asset Tool path errored: %s", rustErr.Error())
		return []int64{}, []extractor.Result{}, result.CommandOutput, rustErr
	}
	debug.Logf("Referenced asset extraction Rusty Asset Tool path returned %d IDs", len(result.AssetIDs))
	return result.AssetIDs, result.References, result.CommandOutput, nil
}
