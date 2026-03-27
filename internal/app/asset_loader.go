package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
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

	"fyne.io/fyne/v2"
)

const (
	requestTimeout               = 15 * time.Second
	assetDeliveryMetadataMaxAge  = 10 * time.Minute
	thumbnailMetadataCacheMaxAge = 30 * time.Minute
)

var assetIDPattern = regexp.MustCompile(`\d+`)

type assetSelfInfo struct {
	BytesSize   int
	AssetTypeID int
}

var assetSizeCache = struct {
	mutex         sync.RWMutex
	infoByAssetID map[int64]assetSelfInfo
}{
	infoByAssetID: map[int64]assetSelfInfo{},
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

type imageInfo struct {
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
}

type childAssetInfo struct {
	AssetID      int64
	BytesSize    int
	AssetTypeID  int
	Resolved     bool
	InstanceType string
	InstanceName string
	InstancePath string
	PropertyName string
}

type assetPreviewResult struct {
	Image              *imageInfo
	Stats              *imageInfo
	ReferencedAssetIDs []int64
	ChildAssets        []childAssetInfo
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

type thumbnailInfo struct {
	ImageURL string
	State    string
	Version  string
}

type singleAssetLoadRequest struct {
	TargetID         int64
	ThumbnailRequest *rbxThumbRequest
}

type rbxThumbRequest struct {
	Type       string
	TargetID   int64
	Width      int
	Height     int
	Size       string
	Format     string
	IsCircular bool
}

type thumbnailsBatchRequest struct {
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

type assetDeliveryInfo struct {
	Location      string
	RawJSON       string
	AssetTypeID   int
	AssetTypeName string
}

type cachedThumbnailInfo struct {
	Info    thumbnailInfo
	RawJSON string
}

type assetRequestTrace struct {
	mutex       sync.Mutex
	usedDisk    bool
	usedNetwork bool
}

func (trace *assetRequestTrace) markDisk() {
	if trace == nil {
		return
	}
	trace.mutex.Lock()
	trace.usedDisk = true
	trace.mutex.Unlock()
}

func (trace *assetRequestTrace) markNetwork() {
	if trace == nil {
		return
	}
	trace.mutex.Lock()
	trace.usedNetwork = true
	trace.mutex.Unlock()
}

func (trace *assetRequestTrace) classifyRequestSource() heatmapAssetRequestSource {
	if trace == nil {
		return heatmapAssetRequestSourceMemory
	}
	trace.mutex.Lock()
	defer trace.mutex.Unlock()
	if trace.usedNetwork {
		return heatmapAssetRequestSourceNetwork
	}
	if trace.usedDisk {
		return heatmapAssetRequestSourceDisk
	}
	return heatmapAssetRequestSourceMemory
}

type economyAssetDetailsResponse struct {
	AssetTypeID int `json:"AssetTypeId"`
}

type economyAssetDetailsInfo struct {
	AssetTypeID int
	RawJSON     string
}

type assetFileInfo struct {
	Info                     *imageInfo
	IsImage                  bool
	ReferencedAssetIDs       []int64
	RustyAssetToolReferences []rustyAssetToolResult
	RustyAssetToolJSON       string
	FileBytes                []byte
	FileName                 string
}

func parseAssetID(rawInput string) (int64, error) {
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

func parseSingleAssetLoadRequest(rawInput string) (singleAssetLoadRequest, error) {
	trimmedInput := strings.TrimSpace(rawInput)
	if strings.HasPrefix(strings.ToLower(trimmedInput), "rbxthumb://") {
		thumbnailRequest, err := parseRbxThumbRequest(trimmedInput)
		if err != nil {
			return singleAssetLoadRequest{}, err
		}
		return singleAssetLoadRequest{
			TargetID:         thumbnailRequest.TargetID,
			ThumbnailRequest: thumbnailRequest,
		}, nil
	}

	assetID, err := parseAssetID(trimmedInput)
	if err != nil {
		return singleAssetLoadRequest{}, err
	}
	return singleAssetLoadRequest{TargetID: assetID}, nil
}

func parseRbxThumbRequest(rawInput string) (*rbxThumbRequest, error) {
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

	targetIDString := firstNonEmptyString(
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

	return &rbxThumbRequest{
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

	widthString := firstNonEmptyString(queryValues.Get("w"), queryValues.Get("width"))
	heightString := firstNonEmptyString(queryValues.Get("h"), queryValues.Get("height"))
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

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		trimmedValue := strings.TrimSpace(value)
		if trimmedValue != "" {
			return trimmedValue
		}
	}
	return ""
}

func loadSingleAssetPreview(request singleAssetLoadRequest) (*assetPreviewResult, error) {
	return loadSingleAssetPreviewWithTrace(request, nil)
}

func loadSingleAssetPreviewWithTrace(request singleAssetLoadRequest, trace *assetRequestTrace) (*assetPreviewResult, error) {
	if request.ThumbnailRequest != nil {
		return loadThumbnailPreviewWithTrace(request.ThumbnailRequest, trace)
	}
	return loadAssetPreviewWithTrace(request.TargetID, trace)
}

func loadAssetStatsPreviewForReference(assetID int64, assetInput string) (*assetPreviewResult, error) {
	return loadAssetStatsPreviewForReferenceWithTrace(assetID, assetInput, nil)
}

func loadAssetStatsPreviewForReferenceWithTrace(assetID int64, assetInput string, trace *assetRequestTrace) (*assetPreviewResult, error) {
	request, err := buildSingleAssetLoadRequest(assetID, assetInput)
	if err != nil {
		return nil, err
	}
	if request.ThumbnailRequest != nil {
		return loadThumbnailPreviewWithTrace(request.ThumbnailRequest, trace)
	}
	return loadBestImageInfoWithOptionsAndTrace(request.TargetID, true, trace)
}

func buildSingleAssetLoadRequestFromAssetID(assetID int64) singleAssetLoadRequest {
	return singleAssetLoadRequest{TargetID: assetID}
}

func buildSingleAssetLoadRequest(assetID int64, assetInput string) (singleAssetLoadRequest, error) {
	trimmedInput := strings.TrimSpace(assetInput)
	if trimmedInput != "" {
		return parseSingleAssetLoadRequest(trimmedInput)
	}
	if assetID <= 0 {
		return singleAssetLoadRequest{}, fmt.Errorf("Asset ID is invalid")
	}
	return buildSingleAssetLoadRequestFromAssetID(assetID), nil
}

func (request singleAssetLoadRequest) requiresAuth() bool {
	return request.ThumbnailRequest == nil
}

func (request singleAssetLoadRequest) logDescription() string {
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

func scanAssetReferenceKey(assetID int64, assetInput string) string {
	trimmedInput := strings.TrimSpace(assetInput)
	if trimmedInput != "" {
		return strings.ToLower(trimmedInput)
	}
	return strconv.FormatInt(assetID, 10)
}

func scanAssetReferenceDisplayInput(assetID int64, assetInput string) string {
	trimmedInput := strings.TrimSpace(assetInput)
	if trimmedInput != "" {
		return trimmedInput
	}
	return strconv.FormatInt(assetID, 10)
}

func buildThumbnailRequestContentCacheKey(request *rbxThumbRequest, version string) string {
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

func buildAssetFileContentCacheKey(assetID int64, assetTypeID int) string {
	return fmt.Sprintf("asset-file:id=%d:type=%d", assetID, assetTypeID)
}

func buildAssetDeliveryMetadataCacheKey(assetID int64) string {
	return fmt.Sprintf("asset-delivery:id=%d", assetID)
}

func buildAssetThumbnailMetadataCacheKey(assetID int64) string {
	return fmt.Sprintf("thumbnail-meta:asset=%d", assetID)
}

func buildThumbnailRequestMetadataCacheKey(request *rbxThumbRequest) string {
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

func loadThumbnailPreview(request *rbxThumbRequest) (*assetPreviewResult, error) {
	return loadThumbnailPreviewWithTrace(request, nil)
}

func loadThumbnailPreviewWithTrace(request *rbxThumbRequest, trace *assetRequestTrace) (*assetPreviewResult, error) {
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

	return &assetPreviewResult{
		Image:              thumbnailImageInfo,
		Stats:              thumbnailImageInfo,
		ReferencedAssetIDs: []int64{},
		ChildAssets:        []childAssetInfo{},
		TotalBytesSize:     thumbnailImageInfo.BytesSize,
		Source:             sourceThumbnailsDirect,
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

func loadBestImageInfo(assetID int64) (*assetPreviewResult, error) {
	return loadBestImageInfoWithOptions(assetID, false)
}

func loadBestImageInfoWithOptions(assetID int64, skipThumbnail bool) (*assetPreviewResult, error) {
	return loadBestImageInfoWithOptionsAndTrace(assetID, skipThumbnail, nil)
}

func loadBestImageInfoWithOptionsAndTrace(assetID int64, skipThumbnail bool, trace *assetRequestTrace) (*assetPreviewResult, error) {
	deliveryInfo, assetDeliveryErr := fetchAssetDeliveryInfoWithTrace(assetID, trace)
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
				assetTypeName = getAssetTypeName(economyInfo.AssetTypeID)
			}
		}
	}

	var statsInfo *imageInfo
	referencedAssetIDs := []int64{}
	rustyAssetToolRawJSON := ""
	downloadBytes := []byte(nil)
	downloadFileName := ""
	downloadIsOriginal := false
	if assetDeliveryErr == nil && deliveryInfo != nil {
		deliveryFileInfo, deliveryFileErr := fetchAssetFileInfoWithCacheKeyAndTrace(
			deliveryInfo.Location,
			buildAssetFileContentCacheKey(assetID, assetTypeID),
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
				return &assetPreviewResult{
					Image:              deliveryFileInfo.Info,
					Stats:              deliveryFileInfo.Info,
					ReferencedAssetIDs: referencedAssetIDs,
					ChildAssets:        childAssets,
					TotalBytesSize:     totalBytesSize,
					Source:             sourceAssetDeliveryInGame,
					State:              stateCompleted,
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
			if assetTypeID > 0 && assetTypeID != assetTypeImage {
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
	return &assetPreviewResult{
		Image:              thumbnailImageInfo,
		Stats:              chooseStatsInfo(statsInfo, thumbnailImageInfo),
		ReferencedAssetIDs: referencedAssetIDs,
		ChildAssets:        childAssets,
		TotalBytesSize:     totalBytesSize,
		Source:             sourceThumbnailsFallback,
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
	statsInfo *imageInfo,
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
) *assetPreviewResult {
	safeStatsInfo := ensureImageInfo(statsInfo)
	totalBytesSize, childAssets := computeChildAssetsAndTotal(safeStatsInfo.BytesSize, referencedAssetIDs, nil)
	return &assetPreviewResult{
		Image:              &imageInfo{},
		Stats:              safeStatsInfo,
		ReferencedAssetIDs: referencedAssetIDs,
		ChildAssets:        childAssets,
		TotalBytesSize:     totalBytesSize,
		Source:             sourceNoThumbnail,
		State:              stateUnavailable,
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

func fetchThumbnailInfo(assetID int64) (*thumbnailInfo, string, error) {
	return fetchThumbnailInfoWithTrace(assetID, nil)
}

func fetchThumbnailInfoWithTrace(assetID int64, trace *assetRequestTrace) (*thumbnailInfo, string, error) {
	cacheSettings := loadAssetDownloadCacheSettings()
	if cacheSettings.Enabled && cacheSettings.Folder != "" {
		cacheKey := buildAssetThumbnailMetadataCacheKey(assetID)
		var cachedEntry cachedThumbnailInfo
		cacheHit, err := readAssetDownloadJSONCacheEntry(cacheSettings.Folder, cacheKey, thumbnailMetadataCacheMaxAge, &cachedEntry)
		if err != nil {
			logDebugf("Thumbnail metadata cache read failed for %s: %s", cacheKey, err.Error())
		} else if cacheHit {
			trace.markDisk()
			logDebugf("Thumbnail metadata cache hit for %s", cacheKey)
			return &cachedEntry.Info, cachedEntry.RawJSON, nil
		}
	}

	trace.markNetwork()
	response, err := doRobloxThumbnailGet(assetID, requestTimeout)
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
	info := &thumbnailInfo{
		ImageURL: firstResult.ImageURL,
		State:    firstResult.State,
		Version:  firstResult.Version,
	}
	if cacheSettings.Enabled && cacheSettings.Folder != "" {
		cacheKey := buildAssetThumbnailMetadataCacheKey(assetID)
		if err := writeAssetDownloadJSONCacheEntry(cacheSettings.Folder, cacheKey, cachedThumbnailInfo{
			Info:    *info,
			RawJSON: rawResponse,
		}); err != nil {
			logDebugf("Thumbnail metadata cache write failed for %s: %s", cacheKey, err.Error())
		}
	}
	return info, rawResponse, nil
}

func fetchThumbnailInfoForRequest(request *rbxThumbRequest) (*thumbnailInfo, string, error) {
	return fetchThumbnailInfoForRequestWithTrace(request, nil)
}

func fetchThumbnailInfoForRequestWithTrace(request *rbxThumbRequest, trace *assetRequestTrace) (*thumbnailInfo, string, error) {
	cacheSettings := loadAssetDownloadCacheSettings()
	if cacheSettings.Enabled && cacheSettings.Folder != "" {
		cacheKey := buildThumbnailRequestMetadataCacheKey(request)
		var cachedEntry cachedThumbnailInfo
		cacheHit, err := readAssetDownloadJSONCacheEntry(cacheSettings.Folder, cacheKey, thumbnailMetadataCacheMaxAge, &cachedEntry)
		if err != nil {
			logDebugf("Thumbnail request metadata cache read failed for %s: %s", cacheKey, err.Error())
		} else if cacheHit {
			trace.markDisk()
			logDebugf("Thumbnail request metadata cache hit for %s", cacheKey)
			return &cachedEntry.Info, cachedEntry.RawJSON, nil
		}
	}

	requestBody := []thumbnailsBatchRequest{{
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

	trace.markNetwork()
	response, err := doRobloxThumbnailBatchPost(bytes.NewReader(requestJSON), requestTimeout)
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
	info := &thumbnailInfo{
		ImageURL: firstResult.ImageURL,
		State:    firstResult.State,
		Version:  firstResult.Version,
	}
	if cacheSettings.Enabled && cacheSettings.Folder != "" {
		cacheKey := buildThumbnailRequestMetadataCacheKey(request)
		if err := writeAssetDownloadJSONCacheEntry(cacheSettings.Folder, cacheKey, cachedThumbnailInfo{
			Info:    *info,
			RawJSON: rawResponse,
		}); err != nil {
			logDebugf("Thumbnail request metadata cache write failed for %s: %s", cacheKey, err.Error())
		}
	}
	return info, rawResponse, nil
}

func buildThumbnailBatchDebugJSON(request thumbnailsBatchRequest, rawResponse []byte) string {
	debugPayload := struct {
		Request  thumbnailsBatchRequest `json:"request"`
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

func fetchAssetDeliveryInfo(assetID int64) (*assetDeliveryInfo, error) {
	return fetchAssetDeliveryInfoWithTrace(assetID, nil)
}

func fetchAssetDeliveryInfoWithTrace(assetID int64, trace *assetRequestTrace) (*assetDeliveryInfo, error) {
	cacheSettings := loadAssetDownloadCacheSettings()
	if cacheSettings.Enabled && cacheSettings.Folder != "" {
		cacheKey := buildAssetDeliveryMetadataCacheKey(assetID)
		var cachedEntry assetDeliveryInfo
		cacheHit, err := readAssetDownloadJSONCacheEntry(cacheSettings.Folder, cacheKey, assetDeliveryMetadataMaxAge, &cachedEntry)
		if err != nil {
			logDebugf("AssetDelivery metadata cache read failed for %s: %s", cacheKey, err.Error())
		} else if cacheHit {
			trace.markDisk()
			logDebugf("AssetDelivery metadata cache hit for %s", cacheKey)
			return &cachedEntry, nil
		}
	}

	trace.markNetwork()
	response, err := doRobloxAssetDeliveryGet(assetID, requestTimeout)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	responseBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	rawResponse := string(responseBytes)
	info := &assetDeliveryInfo{
		RawJSON:       rawResponse,
		AssetTypeID:   0,
		AssetTypeName: "Unknown",
	}
	var apiResponse assetDeliveryResponse
	if err := json.Unmarshal(responseBytes, &apiResponse); err == nil {
		info.Location = apiResponse.Location
		info.AssetTypeID = apiResponse.AssetTypeID
		info.AssetTypeName = getAssetTypeName(apiResponse.AssetTypeID)
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
			logDebugf("AssetDelivery metadata cache write failed for %s: %s", cacheKey, err.Error())
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

func fetchImageInfo(imageURL string, assetID int64, includeHash bool) (*imageInfo, error) {
	return fetchImageInfoWithCacheKey(imageURL, "", assetID, includeHash)
}

func fetchImageInfoWithCacheKey(imageURL string, cacheKey string, assetID int64, includeHash bool) (*imageInfo, error) {
	return fetchImageInfoWithCacheKeyAndTrace(imageURL, cacheKey, assetID, includeHash, nil)
}

func fetchImageInfoWithCacheKeyAndTrace(imageURL string, cacheKey string, assetID int64, includeHash bool, trace *assetRequestTrace) (*imageInfo, error) {
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
		sha256Value = computeSHA256Hex(imageBytes)
	}
	recompressedPNGByteSize, recompressedJPEGByteSize, recompressErr := computeBestCompressedImageSizes(imageBytes)
	if recompressErr != nil {
		recompressedPNGByteSize = 0
		recompressedJPEGByteSize = 0
	}
	return &imageInfo{
		Resource:                 fyne.NewStaticResource(resourceName, imageBytes),
		Width:                    imageConfig.Width,
		Height:                   imageConfig.Height,
		BytesSize:                len(imageBytes),
		RecompressedPNGByteSize:  recompressedPNGByteSize,
		RecompressedJPEGByteSize: recompressedJPEGByteSize,
		Format:                   strings.ToUpper(imageFormat),
		ContentType:              contentType,
		SHA256:                   sha256Value,
	}, nil
}

func fetchAssetFileInfo(fileURL string, assetID int64, assetTypeID int, includeHash bool) (*assetFileInfo, error) {
	return fetchAssetFileInfoWithCacheKey(fileURL, "", assetID, assetTypeID, includeHash)
}

func fetchAssetFileInfoWithCacheKey(fileURL string, cacheKey string, assetID int64, assetTypeID int, includeHash bool) (*assetFileInfo, error) {
	return fetchAssetFileInfoWithCacheKeyAndTrace(fileURL, cacheKey, assetID, assetTypeID, includeHash, nil)
}

func fetchAssetFileInfoWithCacheKeyAndTrace(fileURL string, cacheKey string, assetID int64, assetTypeID int, includeHash bool, trace *assetRequestTrace) (*assetFileInfo, error) {
	fileBytes, contentType, err := downloadRobloxContentBytesWithCacheKeyAndTrace(fileURL, cacheKey, requestTimeout, trace)
	if err != nil {
		return nil, err
	}
	sha256Value := ""
	if includeHash {
		sha256Value = computeSHA256Hex(fileBytes)
	}
	fileName := buildAssetDownloadFileName(assetID, assetTypeID, contentType, "", false)
	info := &imageInfo{
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
		if isAudioAssetContent(assetTypeID, contentType) {
			audioMetadata, audioErr := extractAudioMetadata(fileName, contentType, fileBytes)
			if audioErr == nil && audioMetadata != nil {
				info.Duration = audioMetadata.Duration
				if strings.TrimSpace(audioMetadata.Format) != "" {
					info.Format = audioMetadata.Format
				}
			}
		}
		referencedAssetIDs := []int64{}
		rustyAssetToolReferences := []rustyAssetToolResult{}
		rustyAssetToolJSON := ""
		var extractErr error
		if !shouldSkipRustExtractionForAssetType(assetTypeID) {
			referencedAssetIDs, rustyAssetToolReferences, rustyAssetToolJSON, extractErr = extractReferencedAssetIDsFromBytes(fileBytes, assetTypeID)
		}
		if extractErr != nil {
			return nil, extractErr
		}
		return &assetFileInfo{
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
	recompressedPNGByteSize, recompressedJPEGByteSize, recompressErr := computeBestCompressedImageSizes(fileBytes)
	if recompressErr == nil {
		info.RecompressedPNGByteSize = recompressedPNGByteSize
		info.RecompressedJPEGByteSize = recompressedJPEGByteSize
	}
	referencedAssetIDs := []int64{}
	rustyAssetToolReferences := []rustyAssetToolResult{}
	rustyAssetToolJSON := ""
	var extractErr error
	if !shouldSkipRustExtractionForAssetType(assetTypeID) {
		referencedAssetIDs, rustyAssetToolReferences, rustyAssetToolJSON, extractErr = extractReferencedAssetIDsFromBytes(fileBytes, assetTypeID)
	}
	if extractErr != nil {
		return nil, extractErr
	}
	return &assetFileInfo{
		Info:                     info,
		IsImage:                  true,
		ReferencedAssetIDs:       referencedAssetIDs,
		RustyAssetToolReferences: rustyAssetToolReferences,
		RustyAssetToolJSON:       rustyAssetToolJSON,
		FileBytes:                fileBytes,
		FileName:                 fileName,
	}, nil
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
		fileExtension = getAssetDownloadExtension(assetTypeID)
	}
	return fmt.Sprintf("asset_%d.%s", assetID, fileExtension)
}

func buildFallbackWarningText(warningReason string) string {
	if warningReason == "" {
		return "failed to get in-game version, showing a thumbnail instead, note that this is not representitive of the actual asset size, dimentions etc"
	}

	return fmt.Sprintf(
		"failed to get in-game version, showing a thumbnail instead, note that this is not representitive of the actual asset size, dimentions etc. reason: %s",
		warningReason,
	)
}

func fetchAssetDetailsFromEconomy(assetID int64) (*economyAssetDetailsInfo, error) {
	response, err := doRobloxEconomyDetailsGet(assetID, requestTimeout)
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
	return &economyAssetDetailsInfo{
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

func chooseStatsInfo(preferredStats *imageInfo, fallbackStats *imageInfo) *imageInfo {
	if preferredStats != nil {
		return preferredStats
	}
	return fallbackStats
}

func ensureImageInfo(info *imageInfo) *imageInfo {
	if info != nil {
		return info
	}
	return &imageInfo{}
}

func computeSHA256Hex(payload []byte) string {
	hashBytes := sha256.Sum256(payload)
	return hex.EncodeToString(hashBytes[:])
}

func computeBestCompressedImageSizes(imageBytes []byte) (int, int, error) {
	decodedImage, _, decodeErr := image.Decode(bytes.NewReader(imageBytes))
	if decodeErr != nil {
		return 0, 0, decodeErr
	}
	var recompressedBuffer bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if encodeErr := encoder.Encode(&recompressedBuffer, decodedImage); encodeErr != nil {
		return 0, 0, encodeErr
	}
	var recompressedJPEGBuffer bytes.Buffer
	if encodeErr := jpeg.Encode(&recompressedJPEGBuffer, decodedImage, &jpeg.Options{Quality: 1}); encodeErr != nil {
		return 0, 0, encodeErr
	}
	return recompressedBuffer.Len(), recompressedJPEGBuffer.Len(), nil
}

func computeChildAssetsAndTotal(selfBytesSize int, referencedAssetIDs []int64, rustyAssetToolReferences []rustyAssetToolResult) (int, []childAssetInfo) {
	return computeChildAssetsAndTotalWithTrace(selfBytesSize, referencedAssetIDs, rustyAssetToolReferences, nil)
}

func computeChildAssetsAndTotalWithTrace(selfBytesSize int, referencedAssetIDs []int64, rustyAssetToolReferences []rustyAssetToolResult, trace *assetRequestTrace) (int, []childAssetInfo) {
	totalBytesSize := selfBytesSize
	childAssets := make([]childAssetInfo, 0, len(referencedAssetIDs))
	referenceByAssetID := map[int64]rustyAssetToolResult{}
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
			childAssets = append(childAssets, childAssetInfo{
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
		childAssets = append(childAssets, childAssetInfo{
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

func getAssetSelfInfo(assetID int64) (assetSelfInfo, error) {
	return getAssetSelfInfoWithTrace(assetID, nil)
}

func getAssetSelfInfoWithTrace(assetID int64, trace *assetRequestTrace) (assetSelfInfo, error) {
	assetSizeCache.mutex.RLock()
	cachedAssetInfo, found := assetSizeCache.infoByAssetID[assetID]
	assetSizeCache.mutex.RUnlock()
	if found {
		return cachedAssetInfo, nil
	}

	assetDeliveryInfo, deliveryErr := fetchAssetDeliveryInfoWithTrace(assetID, trace)
	selfInfo := assetSelfInfo{}
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
		buildAssetFileContentCacheKey(assetID, selfInfo.AssetTypeID),
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

func extractReferencedAssetIDsFromBytes(fileBytes []byte, assetTypeID int) ([]int64, []rustyAssetToolResult, string, error) {
	rustAssetIDs, _, rustyAssetToolReferences, rustyAssetToolJSON, rustErr := extractAssetIDsWithRustyAssetToolFromFileWithCountsFromBytes(fileBytes, assetTypeID, rustyAssetToolDefaultLimit)
	if rustErr != nil {
		logDebugf("Referenced asset extraction Rusty Asset Tool path errored: %s", rustErr.Error())
		return []int64{}, []rustyAssetToolResult{}, rustyAssetToolJSON, rustErr
	}
	logDebugf("Referenced asset extraction Rusty Asset Tool path returned %d IDs", len(rustAssetIDs))
	return rustAssetIDs, rustyAssetToolReferences, rustyAssetToolJSON, nil
}
