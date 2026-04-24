package roblox

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// robloxAssetDeliveryBatchURL is the modern batch endpoint used by Studio
// and the player. Unlike /v1/assetId/{id}, its response format + transport
// negotiation (Accept-Encoding: zstd) unlocks the full-resolution KTX2
// variant rather than the 1024-capped legacy PNG.
const robloxAssetDeliveryBatchURL = "https://assetdelivery.roblox.com/v1/assets/batch"

// AssetDeliveryBatchRequest is one entry in the JSON array POSTed to the
// batch endpoint. Only AssetID is required for a minimal metadata
// lookup; the other fields mirror what Studio sends so the server picks
// the same CDN location it would for a real game client.
type AssetDeliveryBatchRequest struct {
	AssetID                          int64  `json:"assetId"`
	RequestID                        string `json:"requestId,omitempty"`
	RequestedBuildType               string `json:"requestedBuildType,omitempty"`
	ContentRepresentationPriorityList string `json:"contentRepresentationPriorityList,omitempty"`
}

// AssetDeliveryBatchResponseEntry is one entry in the batch response. The
// important fields are Location (CDN URL to GET for the actual content)
// and AssetTypeID. AssetTypeID 63 identifies a TexturePack container.
type AssetDeliveryBatchResponseEntry struct {
	AssetID     int64  `json:"assetId"`
	Location    string `json:"location"`
	AssetTypeID int    `json:"assetTypeId"`
	RequestID   string `json:"requestId"`
	IsArchived  bool   `json:"isArchived"`
	Errors      []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

// DoAssetDeliveryBatchPost posts the given entries to the batch endpoint
// and returns the parsed JSON response. The cookie from the authenticated
// session is attached. Callers should then GET each Location with the
// zstd-friendly Accept-Encoding header via FetchAssetDeliveryLocation to
// unlock the high-res KTX2 variant.
func DoAssetDeliveryBatchPost(entries []AssetDeliveryBatchRequest, timeout time.Duration) ([]AssetDeliveryBatchResponseEntry, error) {
	body, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("marshal batch request: %w", err)
	}
	response, err := DoRequest(
		http.MethodPost,
		robloxAssetDeliveryBatchURL,
		bytes.NewReader(body),
		GetRoblosecurityCookieHeader(),
		timeout,
		map[string]string{
			"Content-Type":     "application/json",
			"Accept":           "application/json",
			"Accept-Encoding":  "gzip, deflate",
			"User-Agent":       "Roblox/WinInet",
			"Roblox-Browser-Asset-Request": "true",
		},
	)
	if err != nil {
		return nil, fmt.Errorf("batch POST: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("batch POST returned HTTP %d", response.StatusCode)
	}
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read batch response: %w", err)
	}
	var parsed []AssetDeliveryBatchResponseEntry
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return nil, fmt.Errorf("parse batch response: %w", err)
	}
	return parsed, nil
}

// FetchAssetDeliveryLocation GETs a CDN URL returned by the batch endpoint
// with the Accept-Encoding headers that make Roblox serve the full-res
// KTX2 variant (zstd-advertised) rather than the 1024-capped PNG fallback.
// User-Agent matches Studio so server-side content negotiation behaves the
// same way. Returns the raw response bytes; the caller is expected to
// pass them through ktx2.DecompressTransportWrapper before parsing.
func FetchAssetDeliveryLocation(location string, timeout time.Duration) ([]byte, error) {
	trimmed := strings.TrimSpace(location)
	if trimmed == "" {
		return nil, fmt.Errorf("asset delivery location is empty")
	}
	response, err := DoRequest(
		http.MethodGet,
		trimmed,
		nil,
		GetRoblosecurityCookieHeader(),
		timeout,
		map[string]string{
			"Accept-Encoding": "gzip, deflate, zstd",
			"User-Agent":      "Roblox/WinInet",
			"Accept":          "*/*",
		},
	)
	if err != nil {
		return nil, fmt.Errorf("fetch cdn location: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cdn location returned HTTP %d", response.StatusCode)
	}
	return io.ReadAll(response.Body)
}
