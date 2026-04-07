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

const rustyAssetToolDefaultLimit = 5_000

var (
	errBundledRustyAssetToolUnavailable = errors.New("bundled Rusty Asset Tool unavailable")
	bundledRustyAssetToolPathOnce       sync.Once
	bundledRustyAssetToolPath           string
	bundledRustyAssetToolPathErr        error
)

type rustyAssetToolResult struct {
	ID               int64    `json:"id"`
	RawContent       string   `json:"rawContent,omitempty"`
	InstanceType     string   `json:"instanceType"`
	InstanceName     string   `json:"instanceName"`
	InstancePath     string   `json:"instancePath"`
	PropertyName     string   `json:"propertyName"`
	Used             int      `json:"used"`
	AllInstancePaths []string `json:"allInstancePaths,omitempty"`
}

type positionedRustyAssetToolResult struct {
	ID           int64    `json:"id"`
	RawContent   string   `json:"rawContent,omitempty"`
	InstanceType string   `json:"instanceType"`
	InstanceName string   `json:"instanceName"`
	InstancePath string   `json:"instancePath"`
	PropertyName string   `json:"propertyName"`
	WorldX       *float64 `json:"worldX,omitempty"`
	WorldY       *float64 `json:"worldY,omitempty"`
	WorldZ       *float64 `json:"worldZ,omitempty"`
}

type mapRenderPartRustyAssetToolResult struct {
	InstanceType string   `json:"instanceType"`
	InstanceName string   `json:"instanceName"`
	InstancePath string   `json:"instancePath"`
	CenterX      *float64 `json:"centerX,omitempty"`
	CenterY      *float64 `json:"centerY,omitempty"`
	CenterZ      *float64 `json:"centerZ,omitempty"`
	SizeX        *float64 `json:"sizeX,omitempty"`
	SizeY        *float64 `json:"sizeY,omitempty"`
	SizeZ        *float64 `json:"sizeZ,omitempty"`
	YawDegrees   *float64 `json:"yawDegrees,omitempty"`
	ColorR       *int     `json:"colorR,omitempty"`
	ColorG       *int     `json:"colorG,omitempty"`
	ColorB       *int     `json:"colorB,omitempty"`
	Transparency *float64 `json:"transparency,omitempty"`
}

type meshStatsRustyAssetToolResult struct {
	FormatVersion string `json:"formatVersion"`
	DecoderSource string `json:"decoderSource"`
	VertexCount   uint32 `json:"vertexCount"`
	TriangleCount uint32 `json:"triangleCount"`
}

type meshPreviewRustyAssetToolResult struct {
	FormatVersion        string    `json:"formatVersion"`
	DecoderSource        string    `json:"decoderSource"`
	VertexCount          uint32    `json:"vertexCount"`
	TriangleCount        uint32    `json:"triangleCount"`
	PreviewTriangleCount uint32    `json:"previewTriangleCount"`
	Positions            []float32 `json:"positions"`
	Indices              []uint32  `json:"indices"`
}

func extractAssetIDsWithRustyAssetToolFromFile(filePath string, assetTypeID int, limit int, stopChannel <-chan struct{}) ([]int64, string, error) {
	assetIDs, _, _, commandOutputText, extractErr := extractAssetIDsWithRustyAssetToolFromFileWithCounts(filePath, assetTypeID, limit, stopChannel)
	return assetIDs, commandOutputText, extractErr
}

func extractAssetIDsWithRustyAssetToolFromFileWithCounts(filePath string, assetTypeID int, limit int, stopChannel <-chan struct{}) ([]int64, map[int64]int, []rustyAssetToolResult, string, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, map[int64]int{}, []rustyAssetToolResult{}, "", nil
	}
	logDebugf(
		"Rusty Asset Tool requested for file: %s (limit=%d, assetType=%s (%d))",
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
	if bundledBinaryPath, bundledErr := prepareBundledRustyAssetToolBinary(); bundledErr == nil {
		commandName = bundledBinaryPath
		commandArgs = []string{filePath, strconv.Itoa(limit)}
		logDebugf("Using bundled Rusty Asset Tool binary: %s", bundledBinaryPath)
	} else if !errors.Is(bundledErr, errBundledRustyAssetToolUnavailable) {
		logDebugf("Bundled Rusty Asset Tool prepare failed: %s", bundledErr.Error())
		return nil, map[int64]int{}, []rustyAssetToolResult{}, "", bundledErr
	} else if binaryPath, found := findRustyAssetToolBinaryPath(); found {
		commandName = binaryPath
		commandArgs = []string{filePath, strconv.Itoa(limit)}
		logDebugf("Using Rusty Asset Tool binary: %s", binaryPath)
	} else {
		toolDirectoryPath, cargoManifestPath, found := findRustyAssetToolCargoManifestPath()
		if !found {
			logDebugf("Rusty Asset Tool unavailable: no bundled binary or Cargo manifest found")
			return nil, map[int64]int{}, []rustyAssetToolResult{}, "", fmt.Errorf("Rusty Asset Tool unavailable: bundled binary not found")
		}
		commandName = "cargo"
		commandArgs = []string{"run", "--release", "--quiet", "--manifest-path", cargoManifestPath, "--", filePath, strconv.Itoa(limit)}
		logDebugf("Rusty Asset Tool binary missing, using cargo run from %s", toolDirectoryPath)
	}

	command := exec.CommandContext(commandContext, commandName, commandArgs...)
	command.Env = os.Environ()
	if toolDirectoryPath, _, found := findRustyAssetToolCargoManifestPath(); found {
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
		logDebugf("Rusty Asset Tool cancelled")
		return nil, map[int64]int{}, []rustyAssetToolResult{}, "", errScanStopped
	}
	if runErr != nil {
		stderrText := strings.TrimSpace(stderrBuffer.String())
		if stderrText == "" {
			logDebugf("Rusty Asset Tool failed: %s", runErr.Error())
			return nil, map[int64]int{}, []rustyAssetToolResult{}, "", fmt.Errorf("Rusty Asset Tool failed: %s", runErr.Error())
		} else {
			logDebugf("Rusty Asset Tool failed: %s | stderr: %s", runErr.Error(), stderrText)
			return nil, map[int64]int{}, []rustyAssetToolResult{}, "", fmt.Errorf("Rusty Asset Tool failed: %s", stderrText)
		}
	}

	commandOutput := stdoutBuffer.Bytes()
	commandOutputText := string(commandOutput)
	assetIDsFromDOM, useCountsByAssetID, extractedReferences := extractAssetIDsFromRustDOMJSON(commandOutputText, limit)
	logDebugf(
		"Rusty Asset Tool returned JSON bytes=%d and parsed references=%d",
		len(commandOutput),
		len(extractedReferences),
	)
	return assetIDsFromDOM, useCountsByAssetID, extractedReferences, commandOutputText, nil
}

func extractFilteredRefsWithRustyAssetTool(filePath string, pathPrefixes []string, stopChannel <-chan struct{}) ([]rustyAssetToolResult, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, nil
	}
	prefixArg := strings.Join(pathPrefixes, ",")
	logDebugf("Rusty Asset Tool filtered extraction: %s (prefixes=%s)", filePath, prefixArg)

	commandContext, cancelCommand := context.WithCancel(context.Background())
	defer cancelCommand()
	if stopChannel != nil {
		go func() {
			select {
			case <-stopChannel:
				cancelCommand()
			case <-commandContext.Done():
			}
		}()
	}

	commandArgs := []string{}
	commandName := ""
	if bundledBinaryPath, bundledErr := prepareBundledRustyAssetToolBinary(); bundledErr == nil {
		commandName = bundledBinaryPath
		commandArgs = []string{filePath, "0", prefixArg}
	} else if !errors.Is(bundledErr, errBundledRustyAssetToolUnavailable) {
		return nil, bundledErr
	} else if binaryPath, found := findRustyAssetToolBinaryPath(); found {
		commandName = binaryPath
		commandArgs = []string{filePath, "0", prefixArg}
	} else {
		toolDirectoryPath, cargoManifestPath, found := findRustyAssetToolCargoManifestPath()
		if !found {
			return nil, fmt.Errorf("Rusty Asset Tool unavailable: bundled binary not found")
		}
		commandName = "cargo"
		commandArgs = []string{"run", "--release", "--quiet", "--manifest-path", cargoManifestPath, "--", filePath, "0", prefixArg}
		logDebugf("Rusty Asset Tool binary missing, using cargo run from %s", toolDirectoryPath)
	}

	command := exec.CommandContext(commandContext, commandName, commandArgs...)
	command.Env = os.Environ()
	if toolDirectoryPath, _, found := findRustyAssetToolCargoManifestPath(); found {
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
		return nil, errScanStopped
	}
	if runErr != nil {
		stderrText := strings.TrimSpace(stderrBuffer.String())
		if stderrText != "" {
			return nil, fmt.Errorf("Rusty Asset Tool failed: %s", stderrText)
		}
		return nil, fmt.Errorf("Rusty Asset Tool failed: %s", runErr.Error())
	}

	var results []rustyAssetToolResult
	if err := json.Unmarshal(stdoutBuffer.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("Rusty Asset Tool JSON parse failed: %s", err.Error())
	}
	logDebugf("Rusty Asset Tool filtered extraction returned %d references", len(results))
	return results, nil
}

func extractPositionedRefsWithRustyAssetTool(filePath string, pathPrefixes []string, stopChannel <-chan struct{}) ([]positionedRustyAssetToolResult, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, nil
	}
	prefixArg := strings.Join(pathPrefixes, ",")
	logDebugf("Rusty Asset Tool heatmap extraction: %s (prefixes=%s)", filePath, prefixArg)

	commandContext, cancelCommand := context.WithCancel(context.Background())
	defer cancelCommand()
	if stopChannel != nil {
		go func() {
			select {
			case <-stopChannel:
				cancelCommand()
			case <-commandContext.Done():
			}
		}()
	}

	commandName, commandArgs, _, resolveErr := resolveRustyAssetToolSubcommandCommand("heatmap", filePath, prefixArg)
	if resolveErr != nil {
		return nil, resolveErr
	}

	command := exec.CommandContext(commandContext, commandName, commandArgs...)
	command.Env = os.Environ()
	if toolDirectoryPath, _, found := findRustyAssetToolCargoManifestPath(); found {
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
		return nil, errScanStopped
	}
	if runErr != nil {
		stderrText := strings.TrimSpace(stderrBuffer.String())
		if stderrText != "" {
			return nil, fmt.Errorf("Rusty Asset Tool failed: %s", stderrText)
		}
		return nil, fmt.Errorf("Rusty Asset Tool failed: %s", runErr.Error())
	}

	var results []positionedRustyAssetToolResult
	if err := json.Unmarshal(stdoutBuffer.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("Rusty Asset Tool JSON parse failed: %s", err.Error())
	}
	logDebugf("Rusty Asset Tool heatmap extraction returned %d references", len(results))
	return results, nil
}

func extractMapRenderPartsWithRustyAssetTool(filePath string, pathPrefixes []string, stopChannel <-chan struct{}) ([]mapRenderPartRustyAssetToolResult, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, nil
	}
	prefixArg := strings.Join(pathPrefixes, ",")
	logDebugf("Rusty Asset Tool map render extraction: %s (prefixes=%s)", filePath, prefixArg)

	commandContext, cancelCommand := context.WithCancel(context.Background())
	defer cancelCommand()
	if stopChannel != nil {
		go func() {
			select {
			case <-stopChannel:
				cancelCommand()
			case <-commandContext.Done():
			}
		}()
	}

	commandName, commandArgs, _, resolveErr := resolveRustyAssetToolSubcommandCommand("map", filePath, prefixArg)
	if resolveErr != nil {
		return nil, resolveErr
	}

	command := exec.CommandContext(commandContext, commandName, commandArgs...)
	command.Env = os.Environ()
	if toolDirectoryPath, _, found := findRustyAssetToolCargoManifestPath(); found {
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
		return nil, errScanStopped
	}
	if runErr != nil {
		stderrText := strings.TrimSpace(stderrBuffer.String())
		if stderrText != "" {
			return nil, fmt.Errorf("Rusty Asset Tool failed: %s", stderrText)
		}
		return nil, fmt.Errorf("Rusty Asset Tool failed: %s", runErr.Error())
	}

	var results []mapRenderPartRustyAssetToolResult
	if err := json.Unmarshal(stdoutBuffer.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("Rusty Asset Tool JSON parse failed: %s", err.Error())
	}
	logDebugf("Rusty Asset Tool map render extraction returned %d parts", len(results))
	return results, nil
}

func extractAssetIDsWithRustyAssetToolFromBytes(fileBytes []byte, assetTypeID int, limit int) ([]int64, string, error) {
	assetIDs, _, _, commandOutputText, extractErr := extractAssetIDsWithRustyAssetToolFromFileWithCountsFromBytes(fileBytes, assetTypeID, limit)
	return assetIDs, commandOutputText, extractErr
}

func extractMeshStatsWithRustyAssetToolFromBytes(fileBytes []byte) (meshHeaderInfo, error) {
	if len(fileBytes) == 0 {
		return meshHeaderInfo{}, fmt.Errorf("mesh data is empty")
	}

	tempFile, createErr := os.CreateTemp("", "joxblox-rusty-asset-tool-mesh-stats-*.bin")
	if createErr != nil {
		logDebugf("Rusty Asset Tool mesh stats temp file create failed: %s", createErr.Error())
		return meshHeaderInfo{}, createErr
	}
	tempFilePath := tempFile.Name()
	defer os.Remove(tempFilePath)

	if _, writeErr := tempFile.Write(fileBytes); writeErr != nil {
		tempFile.Close()
		logDebugf("Rusty Asset Tool mesh stats temp file write failed: %s", writeErr.Error())
		return meshHeaderInfo{}, writeErr
	}
	if closeErr := tempFile.Close(); closeErr != nil {
		logDebugf("Rusty Asset Tool mesh stats temp file close failed: %s", closeErr.Error())
		return meshHeaderInfo{}, closeErr
	}

	return extractMeshStatsWithRustyAssetToolFromFile(tempFilePath)
}

func extractMeshStatsWithRustyAssetToolFromFile(filePath string) (meshHeaderInfo, error) {
	if strings.TrimSpace(filePath) == "" {
		return meshHeaderInfo{}, fmt.Errorf("mesh file path is empty")
	}

	commandName := ""
	commandArgs := []string{}
	if bundledBinaryPath, bundledErr := prepareBundledRustyAssetToolBinary(); bundledErr == nil {
		commandName = bundledBinaryPath
		commandArgs = []string{"mesh-stats", filePath}
	} else if !errors.Is(bundledErr, errBundledRustyAssetToolUnavailable) {
		return meshHeaderInfo{}, bundledErr
	} else if binaryPath, found := findRustyAssetToolBinaryPath(); found {
		commandName = binaryPath
		commandArgs = []string{"mesh-stats", filePath}
	} else {
		toolDirectoryPath, cargoManifestPath, found := findRustyAssetToolCargoManifestPath()
		if !found {
			return meshHeaderInfo{}, fmt.Errorf("Rusty Asset Tool unavailable: bundled binary not found")
		}
		commandName = "cargo"
		commandArgs = []string{"run", "--release", "--quiet", "--manifest-path", cargoManifestPath, "--", "mesh-stats", filePath}
		logDebugf("Using cargo run for Rusty Asset Tool mesh stats extraction from %s", toolDirectoryPath)
	}

	command := exec.Command(commandName, commandArgs...)
	command.Env = os.Environ()
	if toolDirectoryPath, _, found := findRustyAssetToolCargoManifestPath(); found {
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
	if runErr := command.Run(); runErr != nil {
		stderrText := strings.TrimSpace(stderrBuffer.String())
		if stderrText != "" {
			return meshHeaderInfo{}, fmt.Errorf("Rusty Asset Tool failed: %s", stderrText)
		}
		return meshHeaderInfo{}, fmt.Errorf("Rusty Asset Tool failed: %s", runErr.Error())
	}

	var stats meshStatsRustyAssetToolResult
	if err := json.Unmarshal(stdoutBuffer.Bytes(), &stats); err != nil {
		return meshHeaderInfo{}, fmt.Errorf("Rusty Asset Tool JSON parse failed: %s", err.Error())
	}
	return meshHeaderInfo{
		Version:  strings.TrimSpace(stats.FormatVersion),
		NumVerts: stats.VertexCount,
		NumFaces: stats.TriangleCount,
	}, nil
}

func extractMeshPreviewWithRustyAssetToolFromBytes(fileBytes []byte) (meshPreviewData, error) {
	if len(fileBytes) == 0 {
		return meshPreviewData{}, fmt.Errorf("mesh data is empty")
	}

	tempFile, createErr := os.CreateTemp("", "joxblox-rusty-asset-tool-mesh-preview-*.bin")
	if createErr != nil {
		logDebugf("Rusty Asset Tool mesh preview temp file create failed: %s", createErr.Error())
		return meshPreviewData{}, createErr
	}
	tempFilePath := tempFile.Name()
	defer os.Remove(tempFilePath)

	if _, writeErr := tempFile.Write(fileBytes); writeErr != nil {
		tempFile.Close()
		logDebugf("Rusty Asset Tool mesh preview temp file write failed: %s", writeErr.Error())
		return meshPreviewData{}, writeErr
	}
	if closeErr := tempFile.Close(); closeErr != nil {
		logDebugf("Rusty Asset Tool mesh preview temp file close failed: %s", closeErr.Error())
		return meshPreviewData{}, closeErr
	}

	return extractMeshPreviewWithRustyAssetToolFromFile(tempFilePath)
}

func extractMeshPreviewWithRustyAssetToolFromFile(filePath string) (meshPreviewData, error) {
	if strings.TrimSpace(filePath) == "" {
		return meshPreviewData{}, fmt.Errorf("mesh file path is empty")
	}

	commandName := ""
	commandArgs := []string{}
	if bundledBinaryPath, bundledErr := prepareBundledRustyAssetToolBinary(); bundledErr == nil {
		commandName = bundledBinaryPath
		commandArgs = []string{"mesh-preview", filePath, strconv.Itoa(maxMeshPreviewTriangles)}
	} else if !errors.Is(bundledErr, errBundledRustyAssetToolUnavailable) {
		return meshPreviewData{}, bundledErr
	} else if binaryPath, found := findRustyAssetToolBinaryPath(); found {
		commandName = binaryPath
		commandArgs = []string{"mesh-preview", filePath, strconv.Itoa(maxMeshPreviewTriangles)}
	} else {
		toolDirectoryPath, cargoManifestPath, found := findRustyAssetToolCargoManifestPath()
		if !found {
			return meshPreviewData{}, fmt.Errorf("Rusty Asset Tool unavailable: bundled binary not found")
		}
		commandName = "cargo"
		commandArgs = []string{"run", "--release", "--quiet", "--manifest-path", cargoManifestPath, "--", "mesh-preview", filePath, strconv.Itoa(maxMeshPreviewTriangles)}
		logDebugf("Using cargo run for Rusty Asset Tool mesh preview extraction from %s", toolDirectoryPath)
	}

	command := exec.Command(commandName, commandArgs...)
	command.Env = os.Environ()
	if toolDirectoryPath, _, found := findRustyAssetToolCargoManifestPath(); found {
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
	if runErr := command.Run(); runErr != nil {
		stderrText := strings.TrimSpace(stderrBuffer.String())
		if stderrText != "" {
			return meshPreviewData{}, fmt.Errorf("Rusty Asset Tool failed: %s", stderrText)
		}
		return meshPreviewData{}, fmt.Errorf("Rusty Asset Tool failed: %s", runErr.Error())
	}

	var preview meshPreviewRustyAssetToolResult
	if err := json.Unmarshal(stdoutBuffer.Bytes(), &preview); err != nil {
		return meshPreviewData{}, fmt.Errorf("Rusty Asset Tool JSON parse failed: %s", err.Error())
	}
	meshData, buildErr := buildMeshPreviewData(preview.Positions, preview.Indices, preview.TriangleCount, preview.PreviewTriangleCount)
	if buildErr != nil {
		return meshPreviewData{}, buildErr
	}
	return meshData, nil
}

func extractAssetIDsWithRustyAssetToolFromFileWithCountsFromBytes(fileBytes []byte, assetTypeID int, limit int) ([]int64, map[int64]int, []rustyAssetToolResult, string, error) {
	if len(fileBytes) == 0 {
		return nil, map[int64]int{}, []rustyAssetToolResult{}, "", nil
	}

	tempFile, createErr := os.CreateTemp("", "joxblox-rusty-asset-tool-*.bin")
	if createErr != nil {
		logDebugf("Rusty Asset Tool temp file create failed: %s", createErr.Error())
		return nil, map[int64]int{}, []rustyAssetToolResult{}, "", createErr
	}
	tempFilePath := tempFile.Name()
	defer os.Remove(tempFilePath)

	_, writeErr := tempFile.Write(fileBytes)
	closeErr := tempFile.Close()
	if writeErr != nil {
		logDebugf("Rusty Asset Tool temp file write failed: %s", writeErr.Error())
		return nil, map[int64]int{}, []rustyAssetToolResult{}, "", writeErr
	}
	if closeErr != nil {
		logDebugf("Rusty Asset Tool temp file close failed: %s", closeErr.Error())
		return nil, map[int64]int{}, []rustyAssetToolResult{}, "", closeErr
	}

	logDebugf("Rusty Asset Tool processing in-memory payload (%d bytes)", len(fileBytes))
	return extractAssetIDsWithRustyAssetToolFromFileWithCounts(tempFilePath, assetTypeID, limit, nil)
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

func rustyAssetToolBinaryFileName() string {
	fileName := "joxblox-rusty-asset-tool"
	if runtime.GOOS == "windows" {
		fileName += ".exe"
	}
	return fileName
}

func legacyAssetToolBinaryFileName() string {
	fileName := "rbxl-id-extractor"
	if runtime.GOOS == "windows" {
		fileName += ".exe"
	}
	return fileName
}

func rustyAssetToolRelativeBinaryPath(binaryFileName string) string {
	return filepath.Join("tools", "rbxl-id-extractor", "target", "release", binaryFileName)
}

func prepareBundledRustyAssetToolBinary() (string, error) {
	bundledExtractorBytes := bundledRustyAssetToolBinary()
	if len(bundledExtractorBytes) == 0 {
		return "", errBundledRustyAssetToolUnavailable
	}

	bundledRustyAssetToolPathOnce.Do(func() {
		tempDirectoryPath, tempDirErr := os.MkdirTemp("", "joxblox-rusty-asset-tool-*")
		if tempDirErr != nil {
			bundledRustyAssetToolPathErr = fmt.Errorf("failed creating bundled Rusty Asset Tool temp directory: %w", tempDirErr)
			return
		}
		extractorPath := filepath.Join(tempDirectoryPath, rustyAssetToolBinaryFileName())
		if writeErr := os.WriteFile(extractorPath, bundledExtractorBytes, 0755); writeErr != nil {
			bundledRustyAssetToolPathErr = fmt.Errorf("failed writing bundled Rusty Asset Tool binary: %w", writeErr)
			return
		}
		if runtime.GOOS != "windows" {
			if chmodErr := os.Chmod(extractorPath, 0755); chmodErr != nil {
				bundledRustyAssetToolPathErr = fmt.Errorf("failed marking bundled Rusty Asset Tool executable: %w", chmodErr)
				return
			}
		}
		bundledRustyAssetToolPath = extractorPath
	})

	if bundledRustyAssetToolPathErr != nil {
		return "", bundledRustyAssetToolPathErr
	}
	if strings.TrimSpace(bundledRustyAssetToolPath) == "" {
		return "", errBundledRustyAssetToolUnavailable
	}
	return bundledRustyAssetToolPath, nil
}

func findRustyAssetToolBinaryPath() (string, bool) {
	candidatePaths := make([]string, 0, 8)
	if executablePath, err := os.Executable(); err == nil && strings.TrimSpace(executablePath) != "" {
		executableDirectory := filepath.Dir(executablePath)
		candidatePaths = append(candidatePaths,
			filepath.Join(executableDirectory, rustyAssetToolRelativeBinaryPath(rustyAssetToolBinaryFileName())),
			filepath.Join(executableDirectory, rustyAssetToolBinaryFileName()),
			filepath.Join(executableDirectory, rustyAssetToolRelativeBinaryPath(legacyAssetToolBinaryFileName())),
			filepath.Join(executableDirectory, legacyAssetToolBinaryFileName()),
		)
	}
	if repositoryRootPath, err := getRepositoryRootPath(); err == nil && strings.TrimSpace(repositoryRootPath) != "" {
		candidatePaths = append(candidatePaths,
			filepath.Join(repositoryRootPath, rustyAssetToolRelativeBinaryPath(rustyAssetToolBinaryFileName())),
			filepath.Join(repositoryRootPath, rustyAssetToolRelativeBinaryPath(legacyAssetToolBinaryFileName())),
		)
	}
	for _, candidatePath := range candidatePaths {
		if _, err := os.Stat(candidatePath); err == nil {
			return candidatePath, true
		}
	}
	return "", false
}

func findRustyAssetToolCargoManifestPath() (string, string, bool) {
	if repositoryRootPath, err := getRepositoryRootPath(); err == nil && strings.TrimSpace(repositoryRootPath) != "" {
		toolDirectoryPath := filepath.Join(repositoryRootPath, "tools", "rbxl-id-extractor")
		cargoManifestPath := filepath.Join(toolDirectoryPath, "Cargo.toml")
		if _, err := os.Stat(cargoManifestPath); err == nil {
			return toolDirectoryPath, cargoManifestPath, true
		}
	}
	return "", "", false
}

func resolveRustyAssetToolSubcommandCommand(subcommand string, filePath string, extraArgs ...string) (string, []string, bool, error) {
	trimmedSubcommand := strings.TrimSpace(subcommand)
	if trimmedSubcommand == "" {
		return "", nil, false, fmt.Errorf("Rusty Asset Tool subcommand is required")
	}

	filteredExtraArgs := make([]string, 0, len(extraArgs))
	for _, arg := range extraArgs {
		if strings.TrimSpace(arg) == "" {
			continue
		}
		filteredExtraArgs = append(filteredExtraArgs, arg)
	}

	if bundledBinaryPath, bundledErr := prepareBundledRustyAssetToolBinary(); bundledErr == nil {
		return resolveRustyAssetToolSubcommandCommandWithPaths(trimmedSubcommand, filePath, bundledBinaryPath, "", "", filteredExtraArgs...)
	} else if !errors.Is(bundledErr, errBundledRustyAssetToolUnavailable) {
		return "", nil, false, bundledErr
	}

	if binaryPath, found := findRustyAssetToolBinaryPath(); found {
		return resolveRustyAssetToolSubcommandCommandWithPaths(trimmedSubcommand, filePath, "", binaryPath, "", filteredExtraArgs...)
	}

	_, cargoManifestPath, found := findRustyAssetToolCargoManifestPath()
	if !found {
		return "", nil, false, fmt.Errorf("Rusty Asset Tool unavailable: bundled binary not found")
	}
	return resolveRustyAssetToolSubcommandCommandWithPaths(trimmedSubcommand, filePath, "", "", cargoManifestPath, filteredExtraArgs...)
}

func resolveRustyAssetToolSubcommandCommandWithPaths(subcommand string, filePath string, bundledBinaryPath string, binaryPath string, cargoManifestPath string, extraArgs ...string) (string, []string, bool, error) {
	trimmedSubcommand := strings.TrimSpace(subcommand)
	if trimmedSubcommand == "" {
		return "", nil, false, fmt.Errorf("Rusty Asset Tool subcommand is required")
	}

	filteredExtraArgs := make([]string, 0, len(extraArgs))
	for _, arg := range extraArgs {
		if strings.TrimSpace(arg) == "" {
			continue
		}
		filteredExtraArgs = append(filteredExtraArgs, arg)
	}

	if strings.TrimSpace(bundledBinaryPath) != "" {
		commandArgs := append([]string{trimmedSubcommand, filePath}, filteredExtraArgs...)
		logDebugf("Using bundled Rusty Asset Tool binary: %s", bundledBinaryPath)
		return bundledBinaryPath, commandArgs, false, nil
	}

	if strings.TrimSpace(binaryPath) != "" {
		commandArgs := append([]string{trimmedSubcommand, filePath}, filteredExtraArgs...)
		logDebugf("Using Rusty Asset Tool binary: %s", binaryPath)
		return binaryPath, commandArgs, false, nil
	}

	if strings.TrimSpace(cargoManifestPath) == "" {
		return "", nil, false, fmt.Errorf("Rusty Asset Tool unavailable: bundled binary not found")
	}
	toolDirectoryPath := filepath.Dir(cargoManifestPath)
	commandArgs := []string{"run", "--release", "--quiet", "--manifest-path", cargoManifestPath, "--", trimmedSubcommand, filePath}
	commandArgs = append(commandArgs, filteredExtraArgs...)
	logDebugf("Using cargo run for Rusty Asset Tool %s extraction from %s", trimmedSubcommand, toolDirectoryPath)
	return "cargo", commandArgs, true, nil
}

func replaceAssetIDsInRBXLWithRustyAssetTool(inputPath string, outputPath string, replacements map[int64]int64, stopChannel <-chan struct{}) (int, error) {
	if len(replacements) == 0 {
		return 0, fmt.Errorf("no replacements provided")
	}

	replacementsJSON := make(map[string]int64, len(replacements))
	for oldID, newID := range replacements {
		replacementsJSON[strconv.FormatInt(oldID, 10)] = newID
	}
	replacementsBytes, marshalErr := json.Marshal(replacementsJSON)
	if marshalErr != nil {
		return 0, fmt.Errorf("failed to marshal replacements: %w", marshalErr)
	}
	tempFile, createErr := os.CreateTemp("", "rbxl-replacements-*.json")
	if createErr != nil {
		return 0, fmt.Errorf("failed to create temp replacements file: %w", createErr)
	}
	tempFilePath := tempFile.Name()
	defer os.Remove(tempFilePath)
	if _, writeErr := tempFile.Write(replacementsBytes); writeErr != nil {
		tempFile.Close()
		return 0, fmt.Errorf("failed to write temp replacements file: %w", writeErr)
	}
	tempFile.Close()

	commandContext, cancelCommand := context.WithCancel(context.Background())
	defer cancelCommand()
	if stopChannel != nil {
		go func() {
			select {
			case <-stopChannel:
				cancelCommand()
			case <-commandContext.Done():
			}
		}()
	}

	replaceArgs := []string{"replace", inputPath, outputPath, tempFilePath}
	commandName := ""
	commandArgs := []string{}
	if bundledBinaryPath, bundledErr := prepareBundledRustyAssetToolBinary(); bundledErr == nil {
		commandName = bundledBinaryPath
		commandArgs = replaceArgs
		logDebugf("Using bundled Rusty Asset Tool binary for replace: %s", bundledBinaryPath)
	} else if !errors.Is(bundledErr, errBundledRustyAssetToolUnavailable) {
		return 0, bundledErr
	} else if binaryPath, found := findRustyAssetToolBinaryPath(); found {
		commandName = binaryPath
		commandArgs = replaceArgs
		logDebugf("Using Rusty Asset Tool binary for replace: %s", binaryPath)
	} else {
		_, cargoManifestPath, found := findRustyAssetToolCargoManifestPath()
		if !found {
			return 0, fmt.Errorf("Rusty Asset Tool unavailable: bundled binary not found")
		}
		commandName = "cargo"
		commandArgs = append([]string{"run", "--release", "--quiet", "--manifest-path", cargoManifestPath, "--"}, replaceArgs...)
		logDebugf("Rusty Asset Tool binary missing, using cargo run for replace")
	}

	command := exec.CommandContext(commandContext, commandName, commandArgs...)
	command.Env = os.Environ()
	if toolDirectoryPath, _, found := findRustyAssetToolCargoManifestPath(); found {
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
		logDebugf("Rusty Asset Tool replace cancelled")
		return 0, errScanStopped
	}
	if runErr != nil {
		stderrText := strings.TrimSpace(stderrBuffer.String())
		if stderrText != "" {
			logDebugf("Rusty Asset Tool replace failed: %s | stderr: %s", runErr.Error(), stderrText)
			return 0, fmt.Errorf("Rusty Asset Tool replace failed: %s", stderrText)
		}
		logDebugf("Rusty Asset Tool replace failed: %s", runErr.Error())
		return 0, fmt.Errorf("Rusty Asset Tool replace failed: %s", runErr.Error())
	}

	countText := strings.TrimSpace(stdoutBuffer.String())
	count, parseErr := strconv.Atoi(countText)
	if parseErr != nil {
		logDebugf("Rusty Asset Tool replace returned non-numeric output: %s", countText)
		return 0, nil
	}
	logDebugf("Rusty Asset Tool replace completed: %d property values replaced", count)
	return count, nil
}

func extractAssetIDsFromRustDOMJSON(domJSON string, limit int) ([]int64, map[int64]int, []rustyAssetToolResult) {
	extractorResults := []rustyAssetToolResult{}
	if unmarshalErr := json.Unmarshal([]byte(domJSON), &extractorResults); unmarshalErr != nil {
		logDebugf("Rusty Asset Tool JSON parse failed: %s", unmarshalErr.Error())
		return []int64{}, map[int64]int{}, []rustyAssetToolResult{}
	}

	uniqueAssetIDs := make([]int64, 0, len(extractorResults))
	useCountsByAssetID := map[int64]int{}
	filteredResults := make([]rustyAssetToolResult, 0, len(extractorResults))
	seenAssetIDs := map[int64]bool{}
	seenReferenceKeys := map[string]bool{}
	for _, extractorResult := range extractorResults {
		if extractorResult.ID <= 0 {
			continue
		}
		useCount := extractorResult.Used
		if useCount <= 0 {
			useCount = 1
		}
		useCountsByAssetID[extractorResult.ID] += useCount
		if !seenAssetIDs[extractorResult.ID] {
			seenAssetIDs[extractorResult.ID] = true
			uniqueAssetIDs = append(uniqueAssetIDs, extractorResult.ID)
		}
		referenceKey := scanAssetReferenceKey(extractorResult.ID, extractorResult.RawContent)
		if seenReferenceKeys[referenceKey] {
			continue
		}
		seenReferenceKeys[referenceKey] = true
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
	seenLimitedAssetIDs := map[int64]bool{}
	for _, extractorResult := range filteredResults {
		if !seenLimitedAssetIDs[extractorResult.ID] {
			seenLimitedAssetIDs[extractorResult.ID] = true
			limitedAssetIDs = append(limitedAssetIDs, extractorResult.ID)
		}
		if useCount, found := useCountsByAssetID[extractorResult.ID]; found {
			limitedUseCounts[extractorResult.ID] = useCount
		} else {
			limitedUseCounts[extractorResult.ID] = 1
		}
	}
	return limitedAssetIDs, limitedUseCounts, filteredResults
}
