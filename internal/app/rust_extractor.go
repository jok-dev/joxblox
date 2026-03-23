package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const rustExtractorDefaultLimit = 5_000

var (
	errBundledRustExtractorUnavailable = errors.New("bundled rust extractor unavailable")
	bundledRustExtractorPathOnce       sync.Once
	bundledRustExtractorPath           string
	bundledRustExtractorPathErr        error
)

type rustExtractorResult struct {
	ID           int64  `json:"id"`
	InstanceType string `json:"instanceType"`
	InstanceName string `json:"instanceName"`
	InstancePath string `json:"instancePath"`
	PropertyName string `json:"propertyName"`
	Used         int    `json:"used"`
}

func extractAssetIDsWithRustFromFile(filePath string, assetTypeID int, limit int, stopChannel <-chan struct{}) ([]int64, string, error) {
	assetIDs, _, _, commandOutputText, extractErr := extractAssetIDsWithRustFromFileWithCounts(filePath, assetTypeID, limit, stopChannel)
	return assetIDs, commandOutputText, extractErr
}

func extractAssetIDsWithRustFromFileWithCounts(filePath string, assetTypeID int, limit int, stopChannel <-chan struct{}) ([]int64, map[int64]int, []rustExtractorResult, string, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, map[int64]int{}, []rustExtractorResult{}, "", nil
	}
	logDebugf(
		"Rust extractor requested for file: %s (limit=%d, assetType=%s (%d))",
		filePath,
		limit,
		getAssetTypeName(assetTypeID),
		assetTypeID,
	)

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
	commandName := ""
	if bundledBinaryPath, bundledErr := prepareBundledRustExtractorBinary(); bundledErr == nil {
		commandName = bundledBinaryPath
		commandArgs = []string{filePath, strconv.Itoa(limit)}
		logDebugf("Using bundled Rust extractor binary: %s", bundledBinaryPath)
	} else if !errors.Is(bundledErr, errBundledRustExtractorUnavailable) {
		logDebugf("Bundled Rust extractor prepare failed: %s", bundledErr.Error())
		return nil, map[int64]int{}, []rustExtractorResult{}, "", bundledErr
	} else if binaryPath, found := findRustExtractorBinaryPath(); found {
		commandName = binaryPath
		commandArgs = []string{filePath, strconv.Itoa(limit)}
		logDebugf("Using Rust extractor binary: %s", binaryPath)
	} else {
		toolDirectoryPath, cargoManifestPath, found := findRustExtractorCargoManifestPath()
		if !found {
			logDebugf("Rust extractor unavailable: no bundled binary or Cargo manifest found")
			return nil, map[int64]int{}, []rustExtractorResult{}, "", fmt.Errorf("Rust extractor unavailable: bundled binary not found")
		}
		commandName = "cargo"
		commandArgs = []string{"run", "--release", "--quiet", "--manifest-path", cargoManifestPath, "--", filePath, strconv.Itoa(limit)}
		logDebugf("Rust extractor binary missing, using cargo run from %s", toolDirectoryPath)
	}

	command := exec.CommandContext(commandContext, commandName, commandArgs...)
	command.Env = os.Environ()
	if toolDirectoryPath, _, found := findRustExtractorCargoManifestPath(); found {
		cargoHomePath := filepath.Join(os.TempDir(), "joxblox-cargo-home")
		targetPath := filepath.Join(toolDirectoryPath, "target")
		command.Env = append(command.Env,
			fmt.Sprintf("CARGO_HOME=%s", cargoHomePath),
			fmt.Sprintf("CARGO_TARGET_DIR=%s", targetPath),
		)
	}
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	command.Stdout = &stdoutBuffer
	command.Stderr = &stderrBuffer
	runErr := command.Run()
	if commandContext.Err() != nil {
		logDebugf("Rust extractor cancelled")
		return nil, map[int64]int{}, []rustExtractorResult{}, "", errScanStopped
	}
	if runErr != nil {
		stderrText := strings.TrimSpace(stderrBuffer.String())
		if stderrText == "" {
			logDebugf("Rust extractor failed: %s", runErr.Error())
			return nil, map[int64]int{}, []rustExtractorResult{}, "", fmt.Errorf("Rust extractor failed: %s", runErr.Error())
		} else {
			logDebugf("Rust extractor failed: %s | stderr: %s", runErr.Error(), stderrText)
			return nil, map[int64]int{}, []rustExtractorResult{}, "", fmt.Errorf("Rust extractor failed: %s", stderrText)
		}
	}

	commandOutput := stdoutBuffer.Bytes()
	commandOutputText := string(commandOutput)
	assetIDsFromDOM, useCountsByAssetID, extractedReferences := extractAssetIDsFromRustDOMJSON(commandOutputText, limit)
	logDebugf(
		"Rust extractor returned JSON bytes=%d and parsed references=%d",
		len(commandOutput),
		len(extractedReferences),
	)
	return assetIDsFromDOM, useCountsByAssetID, extractedReferences, commandOutputText, nil
}

func extractAssetIDsWithRustFromBytes(fileBytes []byte, assetTypeID int, limit int) ([]int64, string, error) {
	assetIDs, _, _, commandOutputText, extractErr := extractAssetIDsWithRustFromFileWithCountsFromBytes(fileBytes, assetTypeID, limit)
	return assetIDs, commandOutputText, extractErr
}

func extractAssetIDsWithRustFromFileWithCountsFromBytes(fileBytes []byte, assetTypeID int, limit int) ([]int64, map[int64]int, []rustExtractorResult, string, error) {
	if len(fileBytes) == 0 {
		return nil, map[int64]int{}, []rustExtractorResult{}, "", nil
	}

	tempFile, createErr := os.CreateTemp("", "rbxl-id-extractor-*.bin")
	if createErr != nil {
		logDebugf("Rust byte extraction temp file create failed: %s", createErr.Error())
		return nil, map[int64]int{}, []rustExtractorResult{}, "", createErr
	}
	tempFilePath := tempFile.Name()
	defer os.Remove(tempFilePath)

	_, writeErr := tempFile.Write(fileBytes)
	closeErr := tempFile.Close()
	if writeErr != nil {
		logDebugf("Rust byte extraction temp file write failed: %s", writeErr.Error())
		return nil, map[int64]int{}, []rustExtractorResult{}, "", writeErr
	}
	if closeErr != nil {
		logDebugf("Rust byte extraction temp file close failed: %s", closeErr.Error())
		return nil, map[int64]int{}, []rustExtractorResult{}, "", closeErr
	}

	logDebugf("Rust extractor processing in-memory payload (%d bytes)", len(fileBytes))
	return extractAssetIDsWithRustFromFileWithCounts(tempFilePath, assetTypeID, limit, nil)
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

func rustExtractorBinaryFileName() string {
	fileName := "rbxl-id-extractor"
	if runtime.GOOS == "windows" {
		fileName += ".exe"
	}
	return fileName
}

func rustExtractorRelativeBinaryPath() string {
	return filepath.Join("tools", "rbxl-id-extractor", "target", "release", rustExtractorBinaryFileName())
}

func prepareBundledRustExtractorBinary() (string, error) {
	bundledExtractorBytes := bundledRustExtractorBinary()
	if len(bundledExtractorBytes) == 0 {
		return "", errBundledRustExtractorUnavailable
	}

	bundledRustExtractorPathOnce.Do(func() {
		tempDirectoryPath, tempDirErr := os.MkdirTemp("", "joxblox-rbxl-id-extractor-*")
		if tempDirErr != nil {
			bundledRustExtractorPathErr = fmt.Errorf("failed creating bundled rust extractor temp directory: %w", tempDirErr)
			return
		}
		extractorPath := filepath.Join(tempDirectoryPath, rustExtractorBinaryFileName())
		if writeErr := os.WriteFile(extractorPath, bundledExtractorBytes, 0755); writeErr != nil {
			bundledRustExtractorPathErr = fmt.Errorf("failed writing bundled rust extractor: %w", writeErr)
			return
		}
		if runtime.GOOS != "windows" {
			if chmodErr := os.Chmod(extractorPath, 0755); chmodErr != nil {
				bundledRustExtractorPathErr = fmt.Errorf("failed marking bundled rust extractor executable: %w", chmodErr)
				return
			}
		}
		bundledRustExtractorPath = extractorPath
	})

	if bundledRustExtractorPathErr != nil {
		return "", bundledRustExtractorPathErr
	}
	if strings.TrimSpace(bundledRustExtractorPath) == "" {
		return "", errBundledRustExtractorUnavailable
	}
	return bundledRustExtractorPath, nil
}

func findRustExtractorBinaryPath() (string, bool) {
	candidatePaths := make([]string, 0, 4)
	if executablePath, err := os.Executable(); err == nil && strings.TrimSpace(executablePath) != "" {
		executableDirectory := filepath.Dir(executablePath)
		candidatePaths = append(candidatePaths,
			filepath.Join(executableDirectory, rustExtractorRelativeBinaryPath()),
			filepath.Join(executableDirectory, rustExtractorBinaryFileName()),
		)
	}
	if repositoryRootPath, err := getRepositoryRootPath(); err == nil && strings.TrimSpace(repositoryRootPath) != "" {
		candidatePaths = append(candidatePaths, filepath.Join(repositoryRootPath, rustExtractorRelativeBinaryPath()))
	}
	for _, candidatePath := range candidatePaths {
		if _, err := os.Stat(candidatePath); err == nil {
			return candidatePath, true
		}
	}
	return "", false
}

func findRustExtractorCargoManifestPath() (string, string, bool) {
	if repositoryRootPath, err := getRepositoryRootPath(); err == nil && strings.TrimSpace(repositoryRootPath) != "" {
		toolDirectoryPath := filepath.Join(repositoryRootPath, "tools", "rbxl-id-extractor")
		cargoManifestPath := filepath.Join(toolDirectoryPath, "Cargo.toml")
		if _, err := os.Stat(cargoManifestPath); err == nil {
			return toolDirectoryPath, cargoManifestPath, true
		}
	}
	return "", "", false
}

func extractAssetIDsFromRustDOMJSON(domJSON string, limit int) ([]int64, map[int64]int, []rustExtractorResult) {
	extractorResults := []rustExtractorResult{}
	if unmarshalErr := json.Unmarshal([]byte(domJSON), &extractorResults); unmarshalErr != nil {
		logDebugf("Rust extractor JSON parse failed: %s", unmarshalErr.Error())
		return []int64{}, map[int64]int{}, []rustExtractorResult{}
	}

	uniqueAssetIDs := make([]int64, 0, len(extractorResults))
	useCountsByAssetID := map[int64]int{}
	filteredResults := make([]rustExtractorResult, 0, len(extractorResults))
	seenAssetIDs := map[int64]bool{}
	for _, extractorResult := range extractorResults {
		if extractorResult.ID <= 0 {
			continue
		}
		if seenAssetIDs[extractorResult.ID] {
			continue
		}
		seenAssetIDs[extractorResult.ID] = true
		uniqueAssetIDs = append(uniqueAssetIDs, extractorResult.ID)
		if extractorResult.Used > 0 {
			useCountsByAssetID[extractorResult.ID] = extractorResult.Used
		} else {
			useCountsByAssetID[extractorResult.ID] = 1
		}
		filteredResults = append(filteredResults, extractorResult)
	}
	sort.Slice(uniqueAssetIDs, func(leftIndex int, rightIndex int) bool {
		return uniqueAssetIDs[leftIndex] < uniqueAssetIDs[rightIndex]
	})
	if limit > 0 && len(filteredResults) > limit {
		filteredResults = filteredResults[:limit]
	}
	limitedAssetIDs := make([]int64, 0, len(filteredResults))
	limitedUseCounts := map[int64]int{}
	for _, extractorResult := range filteredResults {
		limitedAssetIDs = append(limitedAssetIDs, extractorResult.ID)
		if useCount, found := useCountsByAssetID[extractorResult.ID]; found {
			limitedUseCounts[extractorResult.ID] = useCount
		} else {
			limitedUseCounts[extractorResult.ID] = 1
		}
	}
	return limitedAssetIDs, limitedUseCounts, filteredResults
}
