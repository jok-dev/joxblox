package app

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

const (
	maxBinarySample       = 8000
	scannerBufferCapacity = 2 * 1024 * 1024
)

var (
	rbxAssetIDPattern        = regexp.MustCompile(`(?i)rbxassetid://\s*(\d+)`)
	rbxThumbPattern          = regexp.MustCompile(`(?i)rbxthumb://[^\s"'<>]+`)
	rawLargeNumberPattern    = regexp.MustCompile(`\b\d{8,}\b`)
	robloxContextLinePattern = regexp.MustCompile(`(?i)(rbxassetid|assetid|texture|image|decal|thumbnail|meshid|soundid)`)
	errScanLimitReached      = errors.New("scan limit reached")
	errScanStopped           = errors.New("scan stopped")
)

type scanHit struct {
	AssetID            int64
	AssetInput         string
	FilePath           string
	UseCount           int
	InstanceType       string
	InstanceName       string
	InstancePath       string
	PropertyName       string
	AllInstancePaths   []string
	SceneSurfaceArea   float64
	LargestSurfacePath string
}

type extractedScanReference struct {
	AssetID    int64
	AssetInput string
}

type stopSignal struct {
	channel chan struct{}
	once    sync.Once
}

func newStopSignal() *stopSignal {
	return &stopSignal{
		channel: make(chan struct{}),
	}
}

func (signal *stopSignal) Stop() {
	if signal == nil {
		return
	}
	signal.once.Do(func() {
		close(signal.channel)
	})
}

func scanFolderForAssetIDs(rootPath string, limit int, stopChannel <-chan struct{}) ([]scanHit, error) {
	results := []scanHit{}
	seenReferenceKeys := map[string]bool{}

	walkErr := filepath.WalkDir(rootPath, func(path string, entry os.DirEntry, err error) error {
		select {
		case <-stopChannel:
			return errScanStopped
		default:
		}

		if err != nil || entry.IsDir() {
			return nil
		}

		if isProbablyBinaryFile(path) {
			return nil
		}

		assetReferences, parseErr := extractAssetReferencesFromFile(path, stopChannel)
		if parseErr != nil {
			if errors.Is(parseErr, errScanStopped) {
				return errScanStopped
			}
			return nil
		}

		for _, assetReference := range assetReferences {
			select {
			case <-stopChannel:
				return errScanStopped
			default:
			}

			referenceKey := scanAssetReferenceKey(assetReference.AssetID, assetReference.AssetInput)
			if seenReferenceKeys[referenceKey] {
				continue
			}

			seenReferenceKeys[referenceKey] = true
			results = append(results, scanHit{
				AssetID:    assetReference.AssetID,
				AssetInput: assetReference.AssetInput,
				FilePath:   path,
				UseCount:   1,
			})
			if limit > 0 && len(results) >= limit {
				return errScanLimitReached
			}
		}

		return nil
	})

	if walkErr != nil && !errors.Is(walkErr, errScanLimitReached) && !errors.Is(walkErr, errScanStopped) {
		return nil, walkErr
	}

	if errors.Is(walkErr, errScanStopped) {
		return results, errScanStopped
	}

	return results, nil
}

func extractAssetReferencesFromFile(filePath string, stopChannel <-chan struct{}) ([]extractedScanReference, error) {
	fileHandle, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer fileHandle.Close()

	assetReferences := []extractedScanReference{}
	seenReferenceKeys := map[string]bool{}

	fileScanner := bufio.NewScanner(fileHandle)
	fileScanner.Buffer(make([]byte, maxBinarySample), scannerBufferCapacity)
	for fileScanner.Scan() {
		select {
		case <-stopChannel:
			return nil, errScanStopped
		default:
		}
		extractAssetReferencesFromLine(fileScanner.Text(), seenReferenceKeys, &assetReferences)
	}

	if err := fileScanner.Err(); err != nil {
		return nil, err
	}

	return assetReferences, nil
}

func extractAssetReferencesFromLine(line string, seenReferenceKeys map[string]bool, output *[]extractedScanReference) {
	thumbMatches := rbxThumbPattern.FindAllString(line, -1)
	for _, thumbMatch := range thumbMatches {
		loadRequest, err := parseSingleAssetLoadRequest(thumbMatch)
		if err != nil {
			continue
		}
		referenceKey := scanAssetReferenceKey(loadRequest.TargetID, thumbMatch)
		if seenReferenceKeys[referenceKey] {
			continue
		}
		seenReferenceKeys[referenceKey] = true
		*output = append(*output, extractedScanReference{
			AssetID:    loadRequest.TargetID,
			AssetInput: strings.TrimSpace(thumbMatch),
		})
	}

	matches := rbxAssetIDPattern.FindAllStringSubmatch(line, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		assetID, err := strconv.ParseInt(match[1], 10, 64)
		referenceKey := scanAssetReferenceKey(assetID, "")
		if err != nil || seenReferenceKeys[referenceKey] {
			continue
		}

		seenReferenceKeys[referenceKey] = true
		*output = append(*output, extractedScanReference{AssetID: assetID})
	}

	if !robloxContextLinePattern.MatchString(line) {
		return
	}

	rawMatches := rawLargeNumberPattern.FindAllString(line, -1)
	for _, rawMatch := range rawMatches {
		assetID, err := strconv.ParseInt(rawMatch, 10, 64)
		referenceKey := scanAssetReferenceKey(assetID, "")
		if err != nil || seenReferenceKeys[referenceKey] {
			continue
		}

		seenReferenceKeys[referenceKey] = true
		*output = append(*output, extractedScanReference{AssetID: assetID})
	}
}

func isProbablyBinaryFile(filePath string) bool {
	fileHandle, err := os.Open(filePath)
	if err != nil {
		return true
	}
	defer fileHandle.Close()

	buffer := make([]byte, maxBinarySample)
	readCount, readErr := fileHandle.Read(buffer)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return true
	}

	for index := 0; index < readCount; index++ {
		if buffer[index] == 0 {
			return true
		}
	}

	return false
}

func scanFolderDiffForAssetIDs(sourcePath string, limit int, stopChannel <-chan struct{}) ([]scanHit, error) {
	sourceParts := strings.SplitN(sourcePath, "\n", 2)
	if len(sourceParts) != 2 {
		return nil, fmt.Errorf("invalid folder diff source format")
	}
	baselineFolderPath := strings.TrimSpace(sourceParts[0])
	targetFolderPath := strings.TrimSpace(sourceParts[1])
	if baselineFolderPath == "" || targetFolderPath == "" {
		return nil, fmt.Errorf("both baseline and target folders are required")
	}
	baselineInfo, baselineErr := os.Stat(baselineFolderPath)
	if baselineErr != nil {
		return nil, baselineErr
	}
	if !baselineInfo.IsDir() {
		return nil, fmt.Errorf("baseline path must be a folder")
	}
	targetInfo, targetErr := os.Stat(targetFolderPath)
	if targetErr != nil {
		return nil, targetErr
	}
	if !targetInfo.IsDir() {
		return nil, fmt.Errorf("target path must be a folder")
	}

	baselineHits, baselineScanErr := scanFolderForAssetIDs(baselineFolderPath, 0, stopChannel)
	if errors.Is(baselineScanErr, errScanStopped) {
		return nil, errScanStopped
	}
	if baselineScanErr != nil {
		return nil, baselineScanErr
	}

	targetHits, targetScanErr := scanFolderForAssetIDs(targetFolderPath, 0, stopChannel)
	if errors.Is(targetScanErr, errScanStopped) {
		return nil, errScanStopped
	}
	if targetScanErr != nil {
		return nil, targetScanErr
	}

	baselineReferenceKeys := map[string]bool{}
	for _, hit := range baselineHits {
		baselineReferenceKeys[scanAssetReferenceKey(hit.AssetID, hit.AssetInput)] = true
	}
	diffHits := make([]scanHit, 0, len(targetHits))
	for _, hit := range targetHits {
		if baselineReferenceKeys[scanAssetReferenceKey(hit.AssetID, hit.AssetInput)] {
			continue
		}
		diffHits = append(diffHits, hit)
		if limit > 0 && len(diffHits) >= limit {
			break
		}
	}
	return diffHits, nil
}
