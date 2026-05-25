package webapp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"joxblox/internal/debug"
)

// Scan reports are cached on disk keyed by the file's identity (absolute path +
// modtime + size) and the scan options, so re-opening the Asset Breakdown tab
// for an unchanged file shows the previous results instantly without
// re-processing. This cache is always on and independent of the asset-download
// cache toggle in Settings — it stores only derived report metadata, never
// asset bytes.

// scanCacheDir returns (creating if needed) the directory holding cached scan
// reports under the OS cache dir.
func scanCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "joxblox", "scan-cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// scanCacheKey derives a stable key from the file's identity and the scan
// options. A changed file (different modtime or size) or different options
// produce a different key, so stale results are never served.
func scanCacheKey(path string, options ScanOptions) (string, error) {
	absolutePath, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		absolutePath = strings.TrimSpace(path)
	}
	info, err := os.Stat(absolutePath)
	if err != nil {
		return "", err
	}

	// Normalize prefixes so equivalent selections (order/case) share a key.
	prefixes := parsePathPrefixes(options.PathFilter)
	for i := range prefixes {
		prefixes[i] = strings.ToLower(prefixes[i])
	}
	sort.Strings(prefixes)

	keySource := fmt.Sprintf(
		"%s|%d|%d|%s|%.4f",
		absolutePath,
		info.ModTime().UnixNano(),
		info.Size(),
		strings.Join(prefixes, ","),
		options.LargeTextureThresholdMB,
	)
	hashBytes := sha256.Sum256([]byte(keySource))
	return hex.EncodeToString(hashBytes[:]), nil
}

func scanCachePath(key string) (string, error) {
	dir, err := scanCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, key+".json"), nil
}

// readScanCache returns the cached report for this file+options if present.
func readScanCache(path string, options ScanOptions) (ScanReport, bool, error) {
	key, err := scanCacheKey(path, options)
	if err != nil {
		return ScanReport{}, false, err
	}
	cachePath, err := scanCachePath(key)
	if err != nil {
		return ScanReport{}, false, err
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return ScanReport{}, false, nil
		}
		return ScanReport{}, false, err
	}
	var report ScanReport
	if err := json.Unmarshal(data, &report); err != nil {
		return ScanReport{}, false, err
	}
	return report, true, nil
}

// writeScanCache persists a freshly-computed report. Failures are logged but
// non-fatal — caching is a convenience, not a correctness requirement.
func writeScanCache(path string, options ScanOptions, report ScanReport) {
	key, err := scanCacheKey(path, options)
	if err != nil {
		debug.Logf("Scan cache key failed for %s: %s", path, err.Error())
		return
	}
	cachePath, err := scanCachePath(key)
	if err != nil {
		debug.Logf("Scan cache path failed for %s: %s", path, err.Error())
		return
	}
	data, err := json.Marshal(report)
	if err != nil {
		debug.Logf("Scan cache marshal failed for %s: %s", path, err.Error())
		return
	}
	if err := os.WriteFile(cachePath, data, 0o644); err != nil {
		debug.Logf("Scan cache write failed for %s: %s", path, err.Error())
	}
}
