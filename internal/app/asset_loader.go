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
	"time"

	"fyne.io/fyne/v2"
)

const (
	requestTimeout       = 15 * time.Second
	thumbnailURLTemplate = "https://thumbnails.roblox.com/v1/assets?assetIds=%d&size=768x432&format=Png&isCircular=false"
	assetDeliveryURLBase = "https://assetdelivery.roblox.com/v1/assetId/%d"
)

var assetIDPattern = regexp.MustCompile(`\d+`)

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

type thumbnailInfo struct {
	ImageURL string
	State    string
	Version  string
}

type assetDeliveryResponse struct {
	Location string                    `json:"location"`
	Errors   []assetDeliveryErrorEntry `json:"errors"`
}

type assetDeliveryErrorEntry struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
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

func loadBestImageInfo(assetID int64) (*imageInfo, string, string, string, string, string, error) {
	assetDeliveryLocation, assetDeliveryRawJSON, assetDeliveryErr := fetchAssetDeliveryLocation(assetID)
	if assetDeliveryErr == nil {
		deliveryImageInfo, deliveryImageErr := fetchImageInfo(assetDeliveryLocation, assetID)
		if deliveryImageErr == nil {
			return deliveryImageInfo, "AssetDelivery (In-Game)", "Completed", "", assetDeliveryRawJSON, "", nil
		}
		assetDeliveryErr = deliveryImageErr
	}

	thumbnailInfo, thumbnailRawJSON, thumbnailErr := fetchThumbnailInfo(assetID)
	if thumbnailErr != nil {
		return nil, "", "", "", assetDeliveryRawJSON, thumbnailRawJSON, fmt.Errorf("AssetDelivery failed (%s) and thumbnail lookup failed (%s)", assetDeliveryErr.Error(), thumbnailErr.Error())
	}

	if thumbnailInfo.ImageURL == "" {
		return nil, "", "", "", assetDeliveryRawJSON, thumbnailRawJSON, fmt.Errorf("No image available. State: %s. AssetDelivery error: %s", thumbnailInfo.State, assetDeliveryErr.Error())
	}

	thumbnailImageInfo, thumbnailImageErr := fetchImageInfo(thumbnailInfo.ImageURL, assetID)
	if thumbnailImageErr != nil {
		return nil, "", "", "", assetDeliveryRawJSON, thumbnailRawJSON, fmt.Errorf("Thumbnail download failed (%s). AssetDelivery error: %s", thumbnailImageErr.Error(), assetDeliveryErr.Error())
	}

	return thumbnailImageInfo, "Thumbnails API (Fallback)", thumbnailInfo.State, assetDeliveryErr.Error(), assetDeliveryRawJSON, thumbnailRawJSON, nil
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

func fetchAssetDeliveryLocation(assetID int64) (string, string, error) {
	urlString := fmt.Sprintf(assetDeliveryURLBase, assetID)
	response, err := doAuthenticatedGet(urlString)
	if err != nil {
		return "", "", err
	}
	defer response.Body.Close()

	responseBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return "", "", err
	}
	rawResponse := string(responseBytes)

	if response.StatusCode != http.StatusOK {
		reason := extractAssetDeliveryFailureReason(rawResponse)
		if reason != "" {
			return "", rawResponse, fmt.Errorf("%s", reason)
		}
		return "", rawResponse, fmt.Errorf("AssetDelivery returned HTTP %d", response.StatusCode)
	}

	var apiResponse assetDeliveryResponse
	if err := json.Unmarshal(responseBytes, &apiResponse); err != nil {
		return "", rawResponse, err
	}

	if apiResponse.Location == "" {
		reason := extractAssetDeliveryFailureReason(rawResponse)
		if reason != "" {
			return "", rawResponse, fmt.Errorf("%s", reason)
		}
		return "", rawResponse, fmt.Errorf("AssetDelivery did not return a location")
	}

	return apiResponse.Location, rawResponse, nil
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
