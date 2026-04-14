package loader

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"joxblox/internal/debug"
	"joxblox/internal/roblox"
)

type assetDownloadCacheMetadata struct {
	CacheKey    string `json:"cacheKey"`
	URL         string `json:"url"`
	ContentType string `json:"contentType"`
}

type assetDownloadJSONCacheEnvelope struct {
	SavedAtUnix int64           `json:"savedAtUnix"`
	Payload     json.RawMessage `json:"payload"`
}

type assetDownloadCacheMetrics struct {
	DiskHits   int64
	DiskMisses int64
	NetFetches int64
	DiskWrites int64
}

var assetDownloadCacheMetricState = struct {
	diskHits   atomic.Int64
	diskMisses atomic.Int64
	netFetches atomic.Int64
	diskWrites atomic.Int64
}{}

func snapshotAssetDownloadCacheMetrics() assetDownloadCacheMetrics {
	return assetDownloadCacheMetrics{
		DiskHits:   assetDownloadCacheMetricState.diskHits.Load(),
		DiskMisses: assetDownloadCacheMetricState.diskMisses.Load(),
		NetFetches: assetDownloadCacheMetricState.netFetches.Load(),
		DiskWrites: assetDownloadCacheMetricState.diskWrites.Load(),
	}
}

func downloadRobloxContentBytes(urlString string, timeout time.Duration) ([]byte, string, error) {
	return downloadRobloxContentBytesWithCacheKeyAndTrace(urlString, "", timeout, nil)
}

func DownloadRobloxContentBytesWithCacheKey(urlString string, cacheKey string, timeout time.Duration) ([]byte, string, error) {
	return downloadRobloxContentBytesWithCacheKeyAndTrace(urlString, cacheKey, timeout, nil)
}

func downloadRobloxContentBytesWithCacheKeyAndTrace(urlString string, cacheKey string, timeout time.Duration, trace *AssetRequestTrace) ([]byte, string, error) {
	trimmedURL := strings.TrimSpace(urlString)
	if trimmedURL == "" {
		return nil, "", fmt.Errorf("download URL is empty")
	}
	normalizedCacheKey := normalizeAssetDownloadCacheKey(cacheKey, trimmedURL)

	cacheSettings := LoadCacheSettings()
	if cacheSettings.Enabled && cacheSettings.Folder != "" {
		cachedBytes, cachedContentType, cacheHit, err := readAssetDownloadCacheEntry(cacheSettings.Folder, normalizedCacheKey)
		if err != nil {
			debug.Logf("Asset cache read failed for %s: %s", normalizedCacheKey, err.Error())
		} else if cacheHit {
			assetDownloadCacheMetricState.diskHits.Add(1)
			trace.MarkDisk()
			debug.Logf("Asset cache hit for %s", normalizedCacheKey)
			return cachedBytes, cachedContentType, nil
		}
		assetDownloadCacheMetricState.diskMisses.Add(1)
	}

	response, err := roblox.DoAuthenticatedGet(trimmedURL, timeout)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HTTP %d", response.StatusCode)
	}

	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, "", err
	}
	assetDownloadCacheMetricState.netFetches.Add(1)
	trace.MarkNetwork()

	contentType := normalizeDownloadedContentType(response.Header.Get("Content-Type"), bodyBytes)
	if cacheSettings.Enabled && cacheSettings.Folder != "" && len(bodyBytes) > 0 {
		if err := writeAssetDownloadCacheEntry(cacheSettings.Folder, normalizedCacheKey, trimmedURL, contentType, bodyBytes); err != nil {
			debug.Logf("Asset cache write failed for %s: %s", normalizedCacheKey, err.Error())
		}
	}

	return bodyBytes, contentType, nil
}

func normalizeAssetDownloadCacheKey(cacheKey string, urlString string) string {
	trimmedCacheKey := strings.TrimSpace(cacheKey)
	if trimmedCacheKey != "" {
		return trimmedCacheKey
	}
	return strings.TrimSpace(urlString)
}

func normalizeDownloadedContentType(rawContentType string, bodyBytes []byte) string {
	contentType := strings.TrimSpace(strings.Split(rawContentType, ";")[0])
	if contentType != "" {
		return contentType
	}
	if len(bodyBytes) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.Split(http.DetectContentType(bodyBytes), ";")[0])
}

func readAssetDownloadCacheEntry(cacheFolder string, cacheKey string) ([]byte, string, bool, error) {
	dataPath, metadataPath := assetDownloadCachePaths(cacheFolder, cacheKey)
	bodyBytes, err := os.ReadFile(dataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", false, nil
		}
		return nil, "", false, err
	}
	if len(bodyBytes) == 0 {
		return nil, "", false, nil
	}

	contentType := ""
	metadataBytes, metadataErr := os.ReadFile(metadataPath)
	if metadataErr == nil {
		var metadata assetDownloadCacheMetadata
		if err := json.Unmarshal(metadataBytes, &metadata); err == nil {
			contentType = strings.TrimSpace(metadata.ContentType)
		}
	}
	if contentType == "" {
		contentType = normalizeDownloadedContentType("", bodyBytes)
	}

	return bodyBytes, contentType, true, nil
}

func readAssetDownloadJSONCacheEntry(cacheFolder string, cacheKey string, maxAge time.Duration, target any) (bool, error) {
	cachePath := assetDownloadJSONCachePath(cacheFolder, cacheKey)
	cacheBytes, err := os.ReadFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	var envelope assetDownloadJSONCacheEnvelope
	if err := json.Unmarshal(cacheBytes, &envelope); err != nil {
		return false, err
	}
	if envelope.SavedAtUnix <= 0 || len(envelope.Payload) == 0 {
		return false, nil
	}
	if maxAge > 0 && time.Since(time.Unix(envelope.SavedAtUnix, 0)) > maxAge {
		return false, nil
	}
	if target == nil {
		return false, errors.New("json cache target is nil")
	}
	if err := json.Unmarshal(envelope.Payload, target); err != nil {
		return false, err
	}
	return true, nil
}

func writeAssetDownloadCacheEntry(cacheFolder string, cacheKey string, sourceURL string, contentType string, bodyBytes []byte) error {
	dataPath, metadataPath := assetDownloadCachePaths(cacheFolder, cacheKey)
	cacheDirectory := filepath.Dir(dataPath)
	if err := os.MkdirAll(cacheDirectory, 0o755); err != nil {
		return err
	}

	if err := os.WriteFile(dataPath, bodyBytes, 0o644); err != nil {
		return err
	}

	metadataBytes, err := json.Marshal(assetDownloadCacheMetadata{
		CacheKey:    cacheKey,
		URL:         sourceURL,
		ContentType: strings.TrimSpace(contentType),
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(metadataPath, metadataBytes, 0o644); err != nil {
		return err
	}

	debug.Logf("Asset cache stored for %s", cacheKey)
	assetDownloadCacheMetricState.diskWrites.Add(1)
	return nil
}

func writeAssetDownloadJSONCacheEntry(cacheFolder string, cacheKey string, payload any) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	envelopeBytes, err := json.Marshal(assetDownloadJSONCacheEnvelope{
		SavedAtUnix: time.Now().Unix(),
		Payload:     payloadBytes,
	})
	if err != nil {
		return err
	}

	cachePath := assetDownloadJSONCachePath(cacheFolder, cacheKey)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(cachePath, envelopeBytes, 0o644); err != nil {
		return err
	}
	return nil
}

func assetDownloadCachePaths(cacheFolder string, cacheKey string) (string, string) {
	basePath := assetDownloadCacheBasePath(cacheFolder, cacheKey)
	return basePath + ".bin", basePath + ".json"
}

func assetDownloadJSONCachePath(cacheFolder string, cacheKey string) string {
	return assetDownloadCacheBasePath(cacheFolder, cacheKey) + ".entry.json"
}

func assetDownloadCacheBasePath(cacheFolder string, cacheKey string) string {
	hashBytes := sha256.Sum256([]byte(cacheKey))
	hashString := hex.EncodeToString(hashBytes[:])
	return filepath.Join(strings.TrimSpace(cacheFolder), hashString[:2], hashString[2:4], hashString)
}
