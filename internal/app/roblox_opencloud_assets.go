package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"joxblox/internal/roblox"
)

const (
	robloxOpenCloudAssetsAPIBaseURL  = "https://apis.roblox.com/assets/v1"
	robloxOpenCloudCreateAssetURL    = robloxOpenCloudAssetsAPIBaseURL + "/assets"
	robloxOpenCloudUploadTimeout     = 60 * time.Second
	robloxOpenCloudOperationTimeout  = 2 * time.Minute
	robloxOpenCloudOperationPollWait = 2 * time.Second
)

var errRateLimited = errors.New("rate limited by Roblox (HTTP 429)")
var errUploadForbidden = errors.New("forbidden (HTTP 403)")

var robloxOpenCloudUploadRateLimitPolicy = roblox.HttpRateLimitPolicy{
	InitialBackoff: 5 * time.Second,
	MaxBackoff:     30 * time.Second,
	MaxRetries:     0,
}

type robloxOpenCloudCreator struct {
	IsGroup bool
	ID      int64
}

type robloxOpenCloudCreateAssetRequest struct {
	AssetType       string                              `json:"assetType"`
	CreationContext robloxOpenCloudAssetCreationContext `json:"creationContext"`
	Description     string                              `json:"description,omitempty"`
	DisplayName     string                              `json:"displayName"`
}

type robloxOpenCloudAssetCreationContext struct {
	Creator robloxOpenCloudAssetCreator `json:"creator"`
}

type robloxOpenCloudAssetCreator struct {
	GroupID int64 `json:"groupId,omitempty"`
	UserID  int64 `json:"userId,omitempty"`
}

type robloxOpenCloudOperation struct {
	Path     string                              `json:"path"`
	Done     bool                                `json:"done"`
	Error    *robloxOpenCloudOperationError      `json:"error"`
	Response *robloxOpenCloudCreateAssetResponse `json:"response"`
}

type robloxOpenCloudCreateAssetResponse struct {
	AssetID int64 `json:"assetId"`
}

type robloxOpenCloudOperationError struct {
	Code    int64           `json:"code"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details"`
}

func (response *robloxOpenCloudCreateAssetResponse) UnmarshalJSON(data []byte) error {
	type rawCreateAssetResponse struct {
		AssetID json.RawMessage `json:"assetId"`
	}

	var rawResponse rawCreateAssetResponse
	if err := json.Unmarshal(data, &rawResponse); err != nil {
		return err
	}

	if len(rawResponse.AssetID) == 0 {
		response.AssetID = 0
		return nil
	}

	if err := json.Unmarshal(rawResponse.AssetID, &response.AssetID); err == nil {
		return nil
	}

	var assetIDString string
	if err := json.Unmarshal(rawResponse.AssetID, &assetIDString); err != nil {
		return fmt.Errorf("invalid asset ID value")
	}
	parsedAssetID, err := strconv.ParseInt(strings.TrimSpace(assetIDString), 10, 64)
	if err != nil {
		return fmt.Errorf("invalid asset ID value")
	}
	response.AssetID = parsedAssetID
	return nil
}

func uploadDecalToRobloxOpenCloud(
	apiKey string,
	creator robloxOpenCloudCreator,
	displayName string,
	description string,
	fileName string,
	fileBytes []byte,
	stopChannel <-chan struct{},
) (int64, error) {
	trimmedAPIKey := strings.TrimSpace(apiKey)
	if trimmedAPIKey == "" {
		return 0, fmt.Errorf("Open Cloud API key is required")
	}
	if creator.ID <= 0 {
		return 0, fmt.Errorf("creator ID must be a positive integer")
	}
	if strings.TrimSpace(displayName) == "" {
		return 0, fmt.Errorf("display name is required")
	}
	if len(fileBytes) == 0 {
		return 0, fmt.Errorf("file content is empty")
	}

	operation, err := createRobloxOpenCloudDecal(trimmedAPIKey, creator, displayName, description, fileName, fileBytes)
	if err != nil {
		return 0, err
	}
	if operation.Error != nil {
		return 0, fmt.Errorf("upload failed: %s", formatRobloxOpenCloudOperationError(operation.Error))
	}
	if operation.Response != nil && operation.Response.AssetID > 0 {
		return operation.Response.AssetID, nil
	}
	if strings.TrimSpace(operation.Path) == "" {
		return 0, fmt.Errorf("upload failed: Roblox did not return an operation path")
	}

	return pollRobloxOpenCloudAssetID(trimmedAPIKey, operation.Path, stopChannel)
}

func createRobloxOpenCloudDecal(
	apiKey string,
	creator robloxOpenCloudCreator,
	displayName string,
	description string,
	fileName string,
	fileBytes []byte,
) (*robloxOpenCloudOperation, error) {
	requestPayload := robloxOpenCloudCreateAssetRequest{
		AssetType:   "Image",
		Description: strings.TrimSpace(description),
		DisplayName: strings.TrimSpace(displayName),
	}
	if creator.IsGroup {
		requestPayload.CreationContext.Creator.GroupID = creator.ID
	} else {
		requestPayload.CreationContext.Creator.UserID = creator.ID
	}

	requestJSON, err := json.Marshal(requestPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode upload request: %w", err)
	}

	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)
	if err := writer.WriteField("request", string(requestJSON)); err != nil {
		return nil, fmt.Errorf("failed to write request field: %w", err)
	}
	filePart, err := writer.CreateFormFile("fileContent", fileName)
	if err != nil {
		return nil, fmt.Errorf("failed to create file field: %w", err)
	}
	if _, err := filePart.Write(fileBytes); err != nil {
		return nil, fmt.Errorf("failed to write file field: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize upload body: %w", err)
	}

	response, err := roblox.DoRequestWithRateLimitPolicy(
		http.MethodPost,
		robloxOpenCloudCreateAssetURL,
		&requestBody,
		"",
		robloxOpenCloudUploadTimeout,
		map[string]string{
			"Content-Type": writer.FormDataContentType(),
			"x-api-key":    apiKey,
		},
		robloxOpenCloudUploadRateLimitPolicy,
	)
	if err != nil {
		return nil, fmt.Errorf("upload request failed: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read upload response: %w", err)
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return nil, errRateLimited
	}
	if response.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%w: %s", errUploadForbidden, compactRobloxOpenCloudBody(responseBody))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("upload request returned HTTP %d: %s", response.StatusCode, compactRobloxOpenCloudBody(responseBody))
	}

	var operation robloxOpenCloudOperation
	if err := json.Unmarshal(responseBody, &operation); err != nil {
		return nil, fmt.Errorf("failed to decode upload response: %w", err)
	}
	return &operation, nil
}

func pollRobloxOpenCloudAssetID(apiKey string, operationPath string, stopChannel <-chan struct{}) (int64, error) {
	deadline := time.Now().Add(robloxOpenCloudOperationTimeout)
	for {
		select {
		case <-stopChannel:
			return 0, errScanStopped
		default:
		}

		operation, err := getRobloxOpenCloudOperation(apiKey, operationPath)
		if err != nil {
			return 0, err
		}
		if operation.Error != nil {
			return 0, fmt.Errorf("upload failed: %s", formatRobloxOpenCloudOperationError(operation.Error))
		}
		if operation.Done {
			if operation.Response == nil || operation.Response.AssetID <= 0 {
				return 0, fmt.Errorf("upload finished without an asset ID")
			}
			return operation.Response.AssetID, nil
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("upload operation timed out")
		}

		select {
		case <-stopChannel:
			return 0, errScanStopped
		case <-time.After(robloxOpenCloudOperationPollWait):
		}
	}
}

func getRobloxOpenCloudOperation(apiKey string, operationPath string) (*robloxOpenCloudOperation, error) {
	trimmedPath := strings.TrimLeft(strings.TrimSpace(operationPath), "/")
	if trimmedPath == "" {
		return nil, fmt.Errorf("operation path is empty")
	}

	response, err := roblox.DoRequest(
		http.MethodGet,
		robloxOpenCloudAssetsAPIBaseURL+"/"+trimmedPath,
		nil,
		"",
		robloxOpenCloudUploadTimeout,
		map[string]string{
			"x-api-key": apiKey,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("operation poll failed: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read operation response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("operation poll returned HTTP %d: %s", response.StatusCode, compactRobloxOpenCloudBody(responseBody))
	}

	var operation robloxOpenCloudOperation
	if err := json.Unmarshal(responseBody, &operation); err != nil {
		return nil, fmt.Errorf("failed to decode operation response: %w", err)
	}
	return &operation, nil
}

func formatRobloxOpenCloudOperationError(operationErr *robloxOpenCloudOperationError) string {
	if operationErr == nil {
		return "unknown upload error"
	}

	parts := make([]string, 0, 3)
	if operationErr.Code != 0 {
		parts = append(parts, fmt.Sprintf("code %d", operationErr.Code))
	}
	if trimmedMessage := strings.TrimSpace(operationErr.Message); trimmedMessage != "" {
		parts = append(parts, trimmedMessage)
	}
	if trimmedDetails := compactRobloxOpenCloudBody(operationErr.Details); trimmedDetails != "" && trimmedDetails != "null" {
		parts = append(parts, trimmedDetails)
	}
	if len(parts) == 0 {
		return "unknown upload error"
	}
	return strings.Join(parts, ": ")
}

func compactRobloxOpenCloudBody(body []byte) string {
	trimmedBody := strings.TrimSpace(string(body))
	if trimmedBody == "" {
		return ""
	}
	return trimmedBody
}
