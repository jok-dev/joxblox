package roblox

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
	AssetID                              int64  `json:"assetId"`
	RequestID                            string `json:"requestId,omitempty"`
	RequestedBuildType                   string `json:"requestedBuildType,omitempty"`
	AssetType                            string `json:"assetType,omitempty"`
	ContentRepresentationPriorityList    string `json:"contentRepresentationPriorityList,omitempty"`
	DoNotFallbackToBaselineRepresentation string `json:"doNotFallbackToBaselineRepresentation,omitempty"`
}

// AssetDeliveryBatchResponseEntry is one entry in the batch response. The
// important fields are Location (CDN URL to GET for the actual content)
// and AssetTypeID. AssetTypeID 63 identifies a TexturePack container.
//
// ContentRepresentationSpecifier echoes back which representation the
// server actually selected from the request's priority list. It is absent
// when the server fell back to the baseline (legacy 1024-capped PNG) —
// callers checking this field can detect whether the high-res KTX2 was
// served or not.
type AssetDeliveryBatchResponseEntry struct {
	AssetID                        int64                           `json:"assetId"`
	Location                       string                          `json:"location"`
	AssetTypeID                    int                             `json:"assetTypeId"`
	RequestID                      string                          `json:"requestId"`
	IsArchived                     bool                            `json:"isArchived"`
	ContentRepresentationSpecifier *ContentRepresentationSpecifier `json:"contentRepresentationSpecifier,omitempty"`
	AssetMetadatas                 []AssetMetadata                 `json:"assetMetadatas,omitempty"`
	Errors                         []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

// AssetMetadata is a server-side hint attached to a batch response entry.
// Type 1 has been observed carrying a small numeric string (e.g. "799");
// the meaning is not yet known.
type AssetMetadata struct {
	MetadataType int    `json:"metadataType"`
	Value        string `json:"value"`
}

// ContentRepresentationSpecifier identifies a single representation
// variant of an asset on the CDN. It pairs a container format with a
// version string and a binary fidelity tier (base64-encoded little-endian
// uint16). Studio's batch requests pass an array of these — base64-wrapped
// JSON — in the contentRepresentationPriorityList field.
type ContentRepresentationSpecifier struct {
	Format                   string `json:"format"`
	MajorVersion             string `json:"majorVersion"`
	Fidelity                 string `json:"fidelity"`
	SkipGenerationIfNotExist bool   `json:"skipGenerationIfNotExist,omitempty"`
}

// KTX2FidelityDefault is the lowest mip-pack tier — covers the smallest
// mips (1×1 up to 64×64) in one file. It is the universal floor: every
// transcoded Image asset has at least this tier.
//
// Higher tier IDs cover non-overlapping higher-mip ranges. Above tier 192
// each tier holds exactly one mip and dimensions double per +64 step,
// stopping at whatever the asset was uploaded at:
//
//	tier  64 → mips 1..64    (full chain, 7 levels)
//	tier 128 → mips 128..256 (2 levels)
//	tier 192 → mip   512
//	tier 256 → mip  1024
//	tier 320 → mip  2048
//	tier 384 → mip  4096
//	tier 448 → mip  8192
//
// Tiers above the asset's upload resolution return error 406 ("Asset
// content representation is being generated"). To find the actual maximum
// dimension served, probe descending tiers via
// FetchHighestKTX2RepresentationByAssetID.
const (
	KTX2FidelityDefault uint16 = 64
	KTX2FidelityTier128 uint16 = 128
	KTX2FidelityTier192 uint16 = 192
	KTX2FidelityTier256 uint16 = 256
	KTX2FidelityTier320 uint16 = 320
	KTX2FidelityTier384 uint16 = 384
	KTX2FidelityTier448 uint16 = 448
)

// ktx2DescendingTiers lists the tier IDs to probe when looking for the
// highest-resolution representation an asset has, ordered largest first.
// Stops at tier 448 (8192) — the platform's documented upload cap is 8K.
var ktx2DescendingTiers = []uint16{
	KTX2FidelityTier448,
	KTX2FidelityTier384,
	KTX2FidelityTier320,
	KTX2FidelityTier256,
	KTX2FidelityTier192,
	KTX2FidelityTier128,
	KTX2FidelityDefault,
}

// BuildKTX2ContentRepresentationPriorityList returns the base64-encoded
// JSON that asset-delivery's batch endpoint expects in the
// contentRepresentationPriorityList field, asking for a KTX2
// representation at the given fidelity tier.
//
// The returned list prefers majorVersion "6rdo" (BasisU RDO supercompressed)
// and falls back to "6" (plain KTX 2.0) at the same fidelity, matching the
// priority list Studio sends.
func BuildKTX2ContentRepresentationPriorityList(fidelity uint16) string {
	return BuildKTX2ContentRepresentationPriorityListMulti([]uint16{fidelity})
}

// BuildKTX2ContentRepresentationPriorityListMulti builds the priority list
// for multiple fidelities at once. The server walks the list in order and
// returns the first specifier it has stored, so callers asking "give me
// the highest available representation" pass tiers in *descending* order.
//
// Each tier is expanded to two specifiers (6rdo preferred, plain 6 fallback)
// matching what Studio sends.
func BuildKTX2ContentRepresentationPriorityListMulti(fidelities []uint16) string {
	specifiers := make([]ContentRepresentationSpecifier, 0, len(fidelities)*2)
	for _, fidelity := range fidelities {
		fidelityBytes := []byte{byte(fidelity), byte(fidelity >> 8)}
		fidelityField := base64.StdEncoding.EncodeToString(fidelityBytes)
		specifiers = append(specifiers,
			ContentRepresentationSpecifier{Format: "ktx2", MajorVersion: "6rdo", Fidelity: fidelityField},
			ContentRepresentationSpecifier{Format: "ktx2", MajorVersion: "6", Fidelity: fidelityField},
		)
	}
	specifierJSON, _ := json.Marshal(specifiers)
	return base64.StdEncoding.EncodeToString(specifierJSON)
}

// DecodeKTX2Fidelity decodes a base64-encoded fidelity field (the form the
// server returns inside ContentRepresentationSpecifier) back to the uint16
// tier ID. Returns 0 if the input is malformed.
func DecodeKTX2Fidelity(encoded string) uint16 {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(decoded) < 2 {
		return 0
	}
	return uint16(decoded[0]) | uint16(decoded[1])<<8
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
			"Content-Type":                 "application/json",
			"Accept":                       "application/json",
			"User-Agent":                   "Roblox/WinInet",
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

// FetchKTX2RepresentationByAssetID does a one-shot batch lookup for a KTX2
// representation of assetID at the given fidelity tier, then GETs the CDN
// location. Returns the still-transport-wrapped CDN bytes (run through
// ktx2.DecompressTransportWrapper before parsing) plus the response entry
// so callers can verify which specifier the server matched.
//
// If the server falls back to the baseline (legacy 1024-capped PNG), the
// returned bytes are PNG and entry.ContentRepresentationSpecifier is nil.
// Callers wanting to require the high-res KTX2 should treat that as an error.
func FetchKTX2RepresentationByAssetID(assetID int64, fidelity uint16, timeout time.Duration) ([]byte, *AssetDeliveryBatchResponseEntry, error) {
	priorityList := BuildKTX2ContentRepresentationPriorityList(fidelity)
	entries, err := DoAssetDeliveryBatchPost([]AssetDeliveryBatchRequest{
		{
			AssetID:                              assetID,
			RequestID:                            "0",
			AssetType:                            "Image",
			ContentRepresentationPriorityList:    priorityList,
			DoNotFallbackToBaselineRepresentation: "true",
		},
	}, timeout)
	if err != nil {
		return nil, nil, err
	}
	if len(entries) == 0 {
		return nil, nil, fmt.Errorf("batch returned no entries for asset %d", assetID)
	}
	entry := &entries[0]
	if len(entry.Errors) > 0 {
		return nil, entry, fmt.Errorf("batch entry error code=%d: %s", entry.Errors[0].Code, entry.Errors[0].Message)
	}
	if entry.Location == "" {
		return nil, entry, fmt.Errorf("batch entry has no location for asset %d", assetID)
	}
	rawBytes, err := FetchAssetDeliveryLocation(entry.Location, timeout)
	if err != nil {
		return nil, entry, err
	}
	return rawBytes, entry, nil
}

// FetchHighestKTX2RepresentationByAssetID returns the highest-resolution
// KTX2 representation Roblox has stored for the asset. Internally it sends
// a single batch request whose priority list enumerates every known tier
// in descending order — the server picks the first available match and
// returns it, so this is one round-trip regardless of which tier matches.
//
// The returned bytes are still transport-wrapped (run through
// ktx2.DecompressTransportWrapper before parsing). The entry's
// ContentRepresentationSpecifier.Fidelity identifies which tier matched
// (decode with DecodeKTX2Fidelity).
func FetchHighestKTX2RepresentationByAssetID(assetID int64, timeout time.Duration) ([]byte, *AssetDeliveryBatchResponseEntry, error) {
	results, err := FetchHighestKTX2RepresentationsByAssetIDs([]int64{assetID}, timeout)
	if err != nil {
		return nil, nil, err
	}
	result, ok := results[assetID]
	if !ok {
		return nil, nil, fmt.Errorf("asset %d missing from batch response", assetID)
	}
	if result.Err != nil {
		return nil, result.Entry, result.Err
	}
	return result.Bytes, result.Entry, nil
}

// HighestKTX2Result is the per-asset outcome from
// FetchHighestKTX2RepresentationsByAssetIDs. Bytes is the still-transport-
// wrapped CDN payload (nil if Err is set). Entry is the batch response
// entry — present even on per-asset errors so callers can surface details.
type HighestKTX2Result struct {
	Bytes []byte
	Entry *AssetDeliveryBatchResponseEntry
	Err   error
}

// FetchHighestKTX2RepresentationsByAssetIDs fetches the highest-resolution
// KTX2 representation for many assets in one batch request, then GETs each
// returned CDN location in parallel. Result count = input count; per-asset
// failures are reported via HighestKTX2Result.Err while the overall call
// still succeeds. A top-level error is returned only when the batch POST
// itself fails.
//
// The CDN GETs run concurrently with a small fan-out cap to avoid
// overwhelming the network and the rate limiter. Order of input is not
// preserved — callers should look up by asset ID in the returned map.
func FetchHighestKTX2RepresentationsByAssetIDs(assetIDs []int64, timeout time.Duration) (map[int64]HighestKTX2Result, error) {
	if len(assetIDs) == 0 {
		return map[int64]HighestKTX2Result{}, nil
	}
	priorityList := BuildKTX2ContentRepresentationPriorityListMulti(ktx2DescendingTiers)
	requests := make([]AssetDeliveryBatchRequest, 0, len(assetIDs))
	for index, assetID := range assetIDs {
		requests = append(requests, AssetDeliveryBatchRequest{
			AssetID:                              assetID,
			RequestID:                            strconv.Itoa(index),
			AssetType:                            "Image",
			ContentRepresentationPriorityList:    priorityList,
			DoNotFallbackToBaselineRepresentation: "true",
		})
	}
	entries, err := DoAssetDeliveryBatchPost(requests, timeout)
	if err != nil {
		return nil, err
	}
	results := make(map[int64]HighestKTX2Result, len(assetIDs))
	indexByRequestID := make(map[string]int, len(assetIDs))
	for index := range requests {
		indexByRequestID[requests[index].RequestID] = index
	}

	type cdnTask struct {
		assetID  int64
		entry    AssetDeliveryBatchResponseEntry
	}
	pending := make([]cdnTask, 0, len(entries))
	for _, entry := range entries {
		index, ok := indexByRequestID[entry.RequestID]
		if !ok {
			continue
		}
		assetID := assetIDs[index]
		entryCopy := entry
		if len(entry.Errors) > 0 {
			results[assetID] = HighestKTX2Result{
				Entry: &entryCopy,
				Err:   fmt.Errorf("batch entry error code=%d: %s", entry.Errors[0].Code, entry.Errors[0].Message),
			}
			continue
		}
		if entry.Location == "" {
			results[assetID] = HighestKTX2Result{
				Entry: &entryCopy,
				Err:   fmt.Errorf("batch entry has no location for asset %d", assetID),
			}
			continue
		}
		pending = append(pending, cdnTask{assetID: assetID, entry: entry})
	}

	resultsLock := make(chan struct{}, 1)
	resultsLock <- struct{}{}
	const cdnConcurrency = 4
	semaphore := make(chan struct{}, cdnConcurrency)
	done := make(chan struct{})
	for index := range pending {
		semaphore <- struct{}{}
		go func(task cdnTask) {
			defer func() {
				<-semaphore
				done <- struct{}{}
			}()
			rawBytes, fetchErr := FetchAssetDeliveryLocation(task.entry.Location, timeout)
			entryCopy := task.entry
			result := HighestKTX2Result{Entry: &entryCopy}
			if fetchErr != nil {
				result.Err = fetchErr
			} else {
				result.Bytes = rawBytes
			}
			<-resultsLock
			results[task.assetID] = result
			resultsLock <- struct{}{}
		}(pending[index])
	}
	for range pending {
		<-done
	}
	return results, nil
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
