package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
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
	requestTimeout        = 15 * time.Second
	thumbnailURLTemplate  = "https://thumbnails.roblox.com/v1/assets?assetIds=%d&size=768x432&format=Png&isCircular=false"
	assetDeliveryURLBase  = "https://assetdelivery.roblox.com/v1/assetId/%d"
	economyDetailsURLBase = "https://economy.roblox.com/v2/assets/%d/details"
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
	Resource    *fyne.StaticResource
	Width       int
	Height      int
	BytesSize   int
	Format      string
	ContentType string
}

type childAssetInfo struct {
	AssetID     int64
	BytesSize   int
	AssetTypeID int
	Resolved    bool
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
	Info               *imageInfo
	IsImage            bool
	ReferencedAssetIDs []int64
	RustExtractorJSON  string
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
	if assetDeliveryErr == nil && deliveryInfo != nil {
		deliveryFileInfo, deliveryFileErr := fetchAssetFileInfo(deliveryInfo.Location, assetID)
		if deliveryFileErr != nil {
			assetDeliveryErr = deliveryFileErr
		} else if deliveryFileInfo != nil {
			statsInfo = deliveryFileInfo.Info
			referencedAssetIDs = deliveryFileInfo.ReferencedAssetIDs
			rustExtractorRawJSON = deliveryFileInfo.RustExtractorJSON
			if deliveryFileInfo.IsImage {
				totalBytesSize, childAssets := computeChildAssetsAndTotal(deliveryFileInfo.Info.BytesSize, referencedAssetIDs)
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
				}, nil
			}
			if assetTypeID > 0 && assetTypeID != 1 {
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
		return nil, fmt.Errorf("AssetDelivery failed (%s) and thumbnail lookup failed (%s)", assetDeliveryErrText, thumbnailErr.Error())
	}

	if thumbnailInfo.ImageURL == "" {
		return nil, fmt.Errorf("No image available. State: %s. AssetDelivery error: %s", thumbnailInfo.State, assetDeliveryErrText)
	}

	thumbnailImageInfo, thumbnailImageErr := fetchImageInfo(thumbnailInfo.ImageURL, assetID)
	if thumbnailImageErr != nil {
		return nil, fmt.Errorf("Thumbnail download failed (%s). AssetDelivery error: %s", thumbnailImageErr.Error(), assetDeliveryErrText)
	}

	totalBytesSize, childAssets := computeChildAssetsAndTotal(chooseStatsInfo(statsInfo, thumbnailImageInfo).BytesSize, referencedAssetIDs)
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
	}, nil
}

func fetchThumbnailInfo(assetID int64) (*thumbnailInfo, string, error) {
	urlString := fmt.Sprintf(thumbnailURLTemplate, assetID)

	response, err := doAuthenticatedGet(urlString)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HTTP %d", response.StatusCode)
	}

	responseBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, "", err
	}
	rawResponse := string(responseBytes)

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
	urlString := fmt.Sprintf(assetDeliveryURLBase, assetID)
	response, err := doAuthenticatedGet(urlString)
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

func fetchImageInfo(imageURL string, assetID int64) (*imageInfo, error) {
	response, err := doAuthenticatedGet(imageURL)
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

	return &imageInfo{
		Resource:    fyne.NewStaticResource(resourceName, imageBytes),
		Width:       imageConfig.Width,
		Height:      imageConfig.Height,
		BytesSize:   len(imageBytes),
		Format:      strings.ToUpper(imageFormat),
		ContentType: contentType,
	}, nil
}

func fetchAssetFileInfo(fileURL string, assetID int64) (*assetFileInfo, error) {
	response, err := doAuthenticatedGet(fileURL)
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
	info := &imageInfo{
		Resource:    nil,
		Width:       0,
		Height:      0,
		BytesSize:   len(fileBytes),
		Format:      inferFormatFromContentType(contentType),
		ContentType: contentType,
	}

	imageConfig, imageFormat, decodeErr := image.DecodeConfig(bytes.NewReader(fileBytes))
	if decodeErr != nil {
		referencedAssetIDs, rustExtractorJSON := extractReferencedAssetIDsFromBytes(fileBytes)
		return &assetFileInfo{
			Info:               info,
			IsImage:            false,
			ReferencedAssetIDs: referencedAssetIDs,
			RustExtractorJSON:  rustExtractorJSON,
		}, nil
	}

	resourceName := fmt.Sprintf("asset_%d.%s", assetID, imageFormat)
	info.Resource = fyne.NewStaticResource(resourceName, fileBytes)
	info.Width = imageConfig.Width
	info.Height = imageConfig.Height
	info.Format = strings.ToUpper(imageFormat)
	referencedAssetIDs, rustExtractorJSON := extractReferencedAssetIDsFromBytes(fileBytes)
	return &assetFileInfo{
		Info:               info,
		IsImage:            true,
		ReferencedAssetIDs: referencedAssetIDs,
		RustExtractorJSON:  rustExtractorJSON,
	}, nil
}

func doAuthenticatedGet(urlString string) (*http.Response, error) {
	httpClient := &http.Client{Timeout: requestTimeout}
	request, err := http.NewRequest(http.MethodGet, urlString, nil)
	if err != nil {
		return nil, err
	}

	cookieHeader := GetRoblosecurityCookieHeader()
	if cookieHeader != "" {
		request.Header.Set("Cookie", cookieHeader)
	}

	return httpClient.Do(request)
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
	urlString := fmt.Sprintf(economyDetailsURLBase, assetID)
	response, err := doAuthenticatedGet(urlString)
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

func computeChildAssetsAndTotal(selfBytesSize int, referencedAssetIDs []int64) (int, []childAssetInfo) {
	totalBytesSize := selfBytesSize
	childAssets := make([]childAssetInfo, 0, len(referencedAssetIDs))
	for _, referencedAssetID := range referencedAssetIDs {
		referencedAssetInfo, infoErr := getAssetSelfInfo(referencedAssetID)
		if infoErr != nil || referencedAssetInfo.BytesSize <= 0 {
			childAssets = append(childAssets, childAssetInfo{
				AssetID:     referencedAssetID,
				BytesSize:   0,
				AssetTypeID: referencedAssetInfo.AssetTypeID,
				Resolved:    false,
			})
			continue
		}
		totalBytesSize += referencedAssetInfo.BytesSize
		childAssets = append(childAssets, childAssetInfo{
			AssetID:     referencedAssetID,
			BytesSize:   referencedAssetInfo.BytesSize,
			AssetTypeID: referencedAssetInfo.AssetTypeID,
			Resolved:    true,
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

	fileInfo, fileErr := fetchAssetFileInfo(assetDeliveryInfo.Location, assetID)
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

func extractReferencedAssetIDsFromBytes(fileBytes []byte) ([]int64, string) {
	rustAssetIDs, rustExtractorJSON, rustErr := extractAssetIDsWithRustFromBytes(fileBytes, rustExtractorDefaultLimit)
	if rustErr != nil {
		logDebugf("Referenced asset extraction Rust path errored: %s", rustErr.Error())
		return []int64{}, rustExtractorJSON
	}
	logDebugf("Referenced asset extraction Rust path returned %d IDs", len(rustAssetIDs))
	return rustAssetIDs, rustExtractorJSON
}
