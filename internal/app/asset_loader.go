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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
)

const (
	requestTimeout = 15 * time.Second
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
	RustExtractorJSON  string
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

type economyAssetDetailsResponse struct {
	AssetTypeID int `json:"AssetTypeId"`
}

type economyAssetDetailsInfo struct {
	AssetTypeID int
	RawJSON     string
}

type assetFileInfo struct {
	Info                    *imageInfo
	IsImage                 bool
	ReferencedAssetIDs      []int64
	RustExtractorReferences []rustExtractorResult
	RustExtractorJSON       string
	FileBytes               []byte
	FileName                string
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

func loadBestImageInfo(assetID int64) (*assetPreviewResult, error) {
	deliveryInfo, assetDeliveryErr := fetchAssetDeliveryInfo(assetID)
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
	rustExtractorRawJSON := ""
	downloadBytes := []byte(nil)
	downloadFileName := ""
	downloadIsOriginal := false
	if assetDeliveryErr == nil && deliveryInfo != nil {
		deliveryFileInfo, deliveryFileErr := fetchAssetFileInfo(deliveryInfo.Location, assetID, assetTypeID, true)
		if deliveryFileErr != nil {
			if isRustExtractorFailure(deliveryFileErr) {
				return nil, deliveryFileErr
			}
			assetDeliveryErr = deliveryFileErr
		} else if deliveryFileInfo != nil {
			statsInfo = deliveryFileInfo.Info
			referencedAssetIDs = deliveryFileInfo.ReferencedAssetIDs
			rustExtractorRawJSON = deliveryFileInfo.RustExtractorJSON
			downloadBytes = append([]byte(nil), deliveryFileInfo.FileBytes...)
			downloadFileName = deliveryFileInfo.FileName
			downloadIsOriginal = !deliveryFileInfo.IsImage
			if deliveryFileInfo.IsImage {
				totalBytesSize, childAssets := computeChildAssetsAndTotal(
					deliveryFileInfo.Info.BytesSize,
					referencedAssetIDs,
					deliveryFileInfo.RustExtractorReferences,
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
					RustExtractorJSON:  deliveryFileInfo.RustExtractorJSON,
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

	thumbnailInfo, thumbnailRawJSON, thumbnailErr := fetchThumbnailInfo(assetID)
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
				rustExtractorRawJSON,
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
				rustExtractorRawJSON,
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

	thumbnailImageInfo, thumbnailImageErr := fetchImageInfo(thumbnailInfo.ImageURL, assetID, true)
	if thumbnailImageErr != nil {
		if statsInfo != nil {
			return buildNoThumbnailPreviewResult(
				statsInfo,
				referencedAssetIDs,
				assetDeliveryRawJSON,
				thumbnailRawJSON,
				economyRawJSON,
				rustExtractorRawJSON,
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
		RustExtractorJSON:  rustExtractorRawJSON,
		AssetTypeID:        assetTypeID,
		AssetTypeName:      assetTypeName,
		DownloadBytes:      downloadBytes,
		DownloadFileName:   downloadFileName,
		DownloadIsOriginal: downloadIsOriginal,
	}, nil
}

func isRustExtractorFailure(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "rust extractor failed")
}

func buildNoThumbnailPreviewResult(
	statsInfo *imageInfo,
	referencedAssetIDs []int64,
	assetDeliveryRawJSON string,
	thumbnailRawJSON string,
	economyRawJSON string,
	rustExtractorRawJSON string,
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
		RustExtractorJSON:  rustExtractorRawJSON,
		AssetTypeID:        assetTypeID,
		AssetTypeName:      assetTypeName,
		DownloadBytes:      append([]byte(nil), downloadBytes...),
		DownloadFileName:   downloadFileName,
		DownloadIsOriginal: downloadIsOriginal,
	}
}

func fetchThumbnailInfo(assetID int64) (*thumbnailInfo, string, error) {
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
		if response.StatusCode == http.StatusTooManyRequests {
			return nil, rawResponse, fmt.Errorf("HTTP 429 (too many requests for thumbnails)")
		}
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
	return &thumbnailInfo{
		ImageURL: firstResult.ImageURL,
		State:    firstResult.State,
		Version:  firstResult.Version,
	}, rawResponse, nil
}

func fetchAssetDeliveryInfo(assetID int64) (*assetDeliveryInfo, error) {
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
	response, err := doRobloxAuthenticatedGet(imageURL, requestTimeout)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}

	imageBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	imageConfig, imageFormat, err := image.DecodeConfig(bytes.NewReader(imageBytes))
	if err != nil {
		return nil, err
	}
	contentType := strings.Split(response.Header.Get("Content-Type"), ";")[0]
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
	response, err := doRobloxAuthenticatedGet(fileURL, requestTimeout)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}

	fileBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	contentType := strings.Split(response.Header.Get("Content-Type"), ";")[0]
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
		rustExtractorReferences := []rustExtractorResult{}
		rustExtractorJSON := ""
		var extractErr error
		if !shouldSkipRustExtractionForAssetType(assetTypeID) {
			referencedAssetIDs, rustExtractorReferences, rustExtractorJSON, extractErr = extractReferencedAssetIDsFromBytes(fileBytes, assetTypeID)
		}
		if extractErr != nil {
			return nil, extractErr
		}
		return &assetFileInfo{
			Info:                    info,
			IsImage:                 false,
			ReferencedAssetIDs:      referencedAssetIDs,
			RustExtractorReferences: rustExtractorReferences,
			RustExtractorJSON:       rustExtractorJSON,
			FileBytes:               fileBytes,
			FileName:                fileName,
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
	rustExtractorReferences := []rustExtractorResult{}
	rustExtractorJSON := ""
	var extractErr error
	if !shouldSkipRustExtractionForAssetType(assetTypeID) {
		referencedAssetIDs, rustExtractorReferences, rustExtractorJSON, extractErr = extractReferencedAssetIDsFromBytes(fileBytes, assetTypeID)
	}
	if extractErr != nil {
		return nil, extractErr
	}
	return &assetFileInfo{
		Info:                    info,
		IsImage:                 true,
		ReferencedAssetIDs:      referencedAssetIDs,
		RustExtractorReferences: rustExtractorReferences,
		RustExtractorJSON:       rustExtractorJSON,
		FileBytes:               fileBytes,
		FileName:                fileName,
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

func computeChildAssetsAndTotal(selfBytesSize int, referencedAssetIDs []int64, rustExtractorReferences []rustExtractorResult) (int, []childAssetInfo) {
	totalBytesSize := selfBytesSize
	childAssets := make([]childAssetInfo, 0, len(referencedAssetIDs))
	referenceByAssetID := map[int64]rustExtractorResult{}
	for _, reference := range rustExtractorReferences {
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
		referencedAssetInfo, infoErr := getAssetSelfInfo(referencedAssetID)
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
	assetSizeCache.mutex.RLock()
	cachedAssetInfo, found := assetSizeCache.infoByAssetID[assetID]
	assetSizeCache.mutex.RUnlock()
	if found {
		return cachedAssetInfo, nil
	}

	assetDeliveryInfo, deliveryErr := fetchAssetDeliveryInfo(assetID)
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

	fileInfo, fileErr := fetchAssetFileInfo(assetDeliveryInfo.Location, assetID, selfInfo.AssetTypeID, false)
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

func extractReferencedAssetIDsFromBytes(fileBytes []byte, assetTypeID int) ([]int64, []rustExtractorResult, string, error) {
	rustAssetIDs, _, rustExtractorReferences, rustExtractorJSON, rustErr := extractAssetIDsWithRustFromFileWithCountsFromBytes(fileBytes, assetTypeID, rustExtractorDefaultLimit)
	if rustErr != nil {
		logDebugf("Referenced asset extraction Rust path errored: %s", rustErr.Error())
		return []int64{}, []rustExtractorResult{}, rustExtractorJSON, rustErr
	}
	logDebugf("Referenced asset extraction Rust path returned %d IDs", len(rustAssetIDs))
	return rustAssetIDs, rustExtractorReferences, rustExtractorJSON, nil
}
