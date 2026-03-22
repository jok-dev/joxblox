package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

const rustExtractorDefaultLimit = 5_000

type rustExtractorResult struct {
	AssetIDs       []int64        `json:"asset_ids"`
	AssetUseCounts map[string]int `json:"asset_use_counts"`
}

func extractAssetIDsWithRustFromFile(filePath string, limit int, stopChannel <-chan struct{}) ([]int64, string, error) {
	assetIDs, _, commandOutputText, extractErr := extractAssetIDsWithRustFromFileWithCounts(filePath, limit, stopChannel)
	return assetIDs, commandOutputText, extractErr
}

func extractAssetIDsWithRustFromFileWithCounts(filePath string, limit int, stopChannel <-chan struct{}) ([]int64, map[int64]int, string, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, map[int64]int{}, "", nil
	}
	logDebugf("Rust extractor requested for file: %s (limit=%d)", filePath, limit)
	repoRootPath, rootErr := getRepositoryRootPath()
	if rootErr != nil {
		logDebugf("Rust extractor skipped (repo root unavailable): %s", rootErr.Error())
		return nil, map[int64]int{}, "", nil
	}

	toolDirectoryPath := filepath.Join(repoRootPath, "tools", "rbxl-id-extractor")
	binaryPath := filepath.Join(toolDirectoryPath, "target", "release", "rbxl-id-extractor")
	cargoHomePath := filepath.Join(os.TempDir(), "joxblox-cargo-home")
	targetPath := filepath.Join(toolDirectoryPath, "target")
	cargoManifestPath := filepath.Join(toolDirectoryPath, "Cargo.toml")

	commandContext, cancelCommand := context.WithCancel(context.Background())
	defer cancelCommand()
	go func() {
		select {
		case <-stopChannel:
			cancelCommand()
		case <-commandContext.Done():
		}
	}()

	commandArgs := []string{}
	commandName := binaryPath
	if _, binaryErr := os.Stat(binaryPath); binaryErr == nil {
		commandArgs = []string{filePath, strconv.Itoa(limit)}
		logDebugf("Using Rust extractor binary: %s", binaryPath)
	} else {
		commandName = "cargo"
		commandArgs = []string{"run", "--release", "--quiet", "--manifest-path", cargoManifestPath, "--", filePath, strconv.Itoa(limit)}
		logDebugf("Rust extractor binary missing, using cargo run")
	}

	command := exec.CommandContext(commandContext, commandName, commandArgs...)
	command.Env = append(os.Environ(),
		fmt.Sprintf("CARGO_HOME=%s", cargoHomePath),
		fmt.Sprintf("CARGO_TARGET_DIR=%s", targetPath),
	)
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	command.Stdout = &stdoutBuffer
	command.Stderr = &stderrBuffer
	runErr := command.Run()
	if commandContext.Err() != nil {
		logDebugf("Rust extractor cancelled")
		return nil, map[int64]int{}, "", errScanStopped
	}
	if runErr != nil {
		stderrText := strings.TrimSpace(stderrBuffer.String())
		if stderrText == "" {
			logDebugf("Rust extractor failed: %s", runErr.Error())
			return nil, map[int64]int{}, "", fmt.Errorf("Rust extractor failed: %s", runErr.Error())
		} else {
			logDebugf("Rust extractor failed: %s | stderr: %s", runErr.Error(), stderrText)
			return nil, map[int64]int{}, "", fmt.Errorf("Rust extractor failed: %s", stderrText)
		}
	}

	commandOutput := stdoutBuffer.Bytes()
	commandOutputText := string(commandOutput)
	assetIDsFromDOM, useCountsByAssetID := extractAssetIDsFromRustDOMJSON(commandOutputText, limit)
	logDebugf(
		"Rust extractor returned JSON bytes=%d and parsed IDs=%d",
		len(commandOutput),
		len(assetIDsFromDOM),
	)
	return assetIDsFromDOM, useCountsByAssetID, commandOutputText, nil
}

func extractAssetIDsWithRustFromBytes(fileBytes []byte, limit int) ([]int64, string, error) {
	if len(fileBytes) == 0 {
		return nil, "", nil
	}

	tempFile, createErr := os.CreateTemp("", "rbxl-id-extractor-*.bin")
	if createErr != nil {
		logDebugf("Rust byte extraction temp file create failed: %s", createErr.Error())
		return nil, "", createErr
	}
	tempFilePath := tempFile.Name()
	defer os.Remove(tempFilePath)

	_, writeErr := tempFile.Write(fileBytes)
	closeErr := tempFile.Close()
	if writeErr != nil {
		logDebugf("Rust byte extraction temp file write failed: %s", writeErr.Error())
		return nil, "", writeErr
	}
	if closeErr != nil {
		logDebugf("Rust byte extraction temp file close failed: %s", closeErr.Error())
		return nil, "", closeErr
	}

	logDebugf("Rust extractor processing in-memory payload (%d bytes)", len(fileBytes))
	return extractAssetIDsWithRustFromFile(tempFilePath, limit, nil)
}

func getRepositoryRootPath() (string, error) {
	_, currentFilePath, _, callerOK := runtime.Caller(0)
	if !callerOK || strings.TrimSpace(currentFilePath) == "" {
		return "", fmt.Errorf("unable to resolve source path")
	}
	appDirectoryPath := filepath.Dir(currentFilePath)
	internalDirectoryPath := filepath.Dir(appDirectoryPath)
	repositoryRootPath := filepath.Dir(internalDirectoryPath)
	return repositoryRootPath, nil
}

func extractAssetIDsFromRustDOMJSON(domJSON string, limit int) ([]int64, map[int64]int) {
	extractorResult := rustExtractorResult{}
	if unmarshalErr := json.Unmarshal([]byte(domJSON), &extractorResult); unmarshalErr != nil {
		logDebugf("Rust extractor JSON parse failed: %s", unmarshalErr.Error())
		return []int64{}, map[int64]int{}
	}

	uniqueAssetIDs := make([]int64, 0, len(extractorResult.AssetIDs))
	useCountsByAssetID := map[int64]int{}
	for rawAssetID, useCount := range extractorResult.AssetUseCounts {
		parsedAssetID, parseErr := strconv.ParseInt(rawAssetID, 10, 64)
		if parseErr != nil {
			continue
		}
		useCountsByAssetID[parsedAssetID] = useCount
	}
	seenAssetIDs := map[int64]bool{}
	for _, assetID := range extractorResult.AssetIDs {
		if seenAssetIDs[assetID] {
			continue
		}
		seenAssetIDs[assetID] = true
		uniqueAssetIDs = append(uniqueAssetIDs, assetID)
	}
	sort.Slice(uniqueAssetIDs, func(leftIndex int, rightIndex int) bool {
		return uniqueAssetIDs[leftIndex] < uniqueAssetIDs[rightIndex]
	})
	if limit > 0 && len(uniqueAssetIDs) > limit {
		uniqueAssetIDs = uniqueAssetIDs[:limit]
	}
	limitedUseCounts := map[int64]int{}
	for _, assetID := range uniqueAssetIDs {
		if useCount, found := useCountsByAssetID[assetID]; found {
			limitedUseCounts[assetID] = useCount
			continue
		}
		limitedUseCounts[assetID] = 1
	}
	return uniqueAssetIDs, limitedUseCounts
}
