package app

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
)

type rustExtractorCacheKey struct {
	filePath string
	prefixes string
	modTime  int64
	size     int64
	extra    string
}

type assetIDsExtractorCacheEntry struct {
	AssetIDs      []int64
	UseCounts     map[int64]int
	References    []rustyAssetToolResult
	CommandOutput string
}

var (
	positionedRefsExtractorCache sync.Map
	mapRenderPartsExtractorCache sync.Map
	filteredRefsExtractorCache   sync.Map
	assetIDsExtractorCache       sync.Map
)

func rustExtractorCacheKeyFor(filePath string, pathPrefixes []string, extra string) (rustExtractorCacheKey, bool) {
	info, err := os.Stat(filePath)
	if err != nil {
		return rustExtractorCacheKey{}, false
	}
	sortedPrefixes := append([]string(nil), pathPrefixes...)
	sort.Strings(sortedPrefixes)
	return rustExtractorCacheKey{
		filePath: filePath,
		prefixes: strings.Join(sortedPrefixes, ","),
		modTime:  info.ModTime().UnixNano(),
		size:     info.Size(),
		extra:    extra,
	}, true
}

func rustExtractorCacheKeyForLimit(filePath string, assetTypeID int, limit int) (rustExtractorCacheKey, bool) {
	return rustExtractorCacheKeyFor(filePath, nil, fmt.Sprintf("type=%d;limit=%d", assetTypeID, limit))
}
