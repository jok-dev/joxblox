package extractor

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
)

type cacheKey struct {
	filePath string
	prefixes string
	modTime  int64
	size     int64
	extra    string
}

type assetIDsCacheEntry struct {
	AssetIDs      []int64
	UseCounts     map[int64]int
	References    []Result
	CommandOutput string
}

var (
	positionedRefsCache sync.Map
	mapRenderPartsCache sync.Map
	filteredRefsCache   sync.Map
	assetIDsCache       sync.Map
)

func cacheKeyFor(filePath string, pathPrefixes []string, extra string) (cacheKey, bool) {
	info, err := os.Stat(filePath)
	if err != nil {
		return cacheKey{}, false
	}
	sortedPrefixes := append([]string(nil), pathPrefixes...)
	sort.Strings(sortedPrefixes)
	return cacheKey{
		filePath: filePath,
		prefixes: strings.Join(sortedPrefixes, ","),
		modTime:  info.ModTime().UnixNano(),
		size:     info.Size(),
		extra:    extra,
	}, true
}

func cacheKeyForLimit(filePath string, assetTypeID int, limit int) (cacheKey, bool) {
	return cacheKeyFor(filePath, nil, fmt.Sprintf("type=%d;limit=%d", assetTypeID, limit))
}
