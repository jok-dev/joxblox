package app

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

const (
	maxBinarySample       = 8000
	scannerBufferCapacity = 2 * 1024 * 1024
)

var (
	rbxAssetIDPattern        = regexp.MustCompile(`(?i)rbxassetid://\s*(\d+)`)
	rawLargeNumberPattern    = regexp.MustCompile(`\b\d{8,}\b`)
	robloxContextLinePattern = regexp.MustCompile(`(?i)(rbxassetid|assetid|texture|image|decal|thumbnail|meshid|soundid)`)
	errScanLimitReached      = errors.New("scan limit reached")
	errScanStopped           = errors.New("scan stopped")
)

type scanHit struct {
	AssetID  int64
	FilePath string
	UseCount int
}

func scanFolderForAssetIDs(rootPath string, limit int, stopChannel <-chan struct{}) ([]scanHit, error) {
	results := []scanHit{}
	seenAssetIDs := map[int64]bool{}

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

		assetIDs, parseErr := extractAssetIDsFromFile(path, stopChannel)
		if parseErr != nil {
			if errors.Is(parseErr, errScanStopped) {
				return errScanStopped
			}
			return nil
		}

		for _, assetID := range assetIDs {
			select {
			case <-stopChannel:
				return errScanStopped
			default:
			}

			if seenAssetIDs[assetID] {
				continue
			}

			seenAssetIDs[assetID] = true
			results = append(results, scanHit{
				AssetID:  assetID,
				FilePath: path,
				UseCount: 1,
			})
			if len(results) >= limit {
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

func extractAssetIDsFromFile(filePath string, stopChannel <-chan struct{}) ([]int64, error) {
	fileHandle, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer fileHandle.Close()

	assetIDs := []int64{}
	seenAssetIDs := map[int64]bool{}

	fileScanner := bufio.NewScanner(fileHandle)
	fileScanner.Buffer(make([]byte, maxBinarySample), scannerBufferCapacity)
	for fileScanner.Scan() {
		select {
		case <-stopChannel:
			return nil, errScanStopped
		default:
		}
		extractAssetIDsFromLine(fileScanner.Text(), seenAssetIDs, &assetIDs)
	}

	if err := fileScanner.Err(); err != nil {
		return nil, err
	}

	return assetIDs, nil
}

func extractAssetIDsFromLine(line string, seenAssetIDs map[int64]bool, output *[]int64) {
	matches := rbxAssetIDPattern.FindAllStringSubmatch(line, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		assetID, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil || seenAssetIDs[assetID] {
			continue
		}

		seenAssetIDs[assetID] = true
		*output = append(*output, assetID)
	}

	if !robloxContextLinePattern.MatchString(line) {
		return
	}

	rawMatches := rawLargeNumberPattern.FindAllString(line, -1)
	for _, rawMatch := range rawMatches {
		assetID, err := strconv.ParseInt(rawMatch, 10, 64)
		if err != nil || seenAssetIDs[assetID] {
			continue
		}

		seenAssetIDs[assetID] = true
		*output = append(*output, assetID)
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
