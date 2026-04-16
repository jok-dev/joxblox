package extractor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"joxblox/internal/debug"
	"joxblox/internal/roblox"
	"joxblox/internal/roblox/mesh"
)

func ExtractAssetIDs(filePath string, assetTypeID int, limit int, stopChannel <-chan struct{}) ([]int64, string, error) {
	result, err := ExtractAssetIDsWithCounts(filePath, assetTypeID, limit, stopChannel)
	if err != nil {
		return nil, "", err
	}
	return result.AssetIDs, result.CommandOutput, nil
}

func ExtractAssetIDsWithCounts(filePath string, assetTypeID int, limit int, stopChannel <-chan struct{}) (AssetIDsResult, error) {
	if strings.TrimSpace(filePath) == "" {
		return AssetIDsResult{UseCounts: map[int64]int{}}, nil
	}
	ck, ckOk := cacheKeyForLimit(filePath, assetTypeID, limit)
	if ckOk {
		if cached, found := assetIDsCache.Load(ck); found {
			entry := cached.(assetIDsCacheEntry)
			debug.Logf("Rusty Asset Tool scan extraction cache hit: %s (limit=%d)", filePath, limit)
			return AssetIDsResult{
				AssetIDs:      entry.AssetIDs,
				UseCounts:     entry.UseCounts,
				References:    entry.References,
				CommandOutput: entry.CommandOutput,
			}, nil
		}
	}
	debug.Logf(
		"Rusty Asset Tool requested for file: %s (limit=%d, assetType=%s (%d))",
		filePath, limit, roblox.GetAssetTypeName(assetTypeID), assetTypeID,
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
	if bundledBinaryPath, bundledErr := prepareBundledBinary(); bundledErr == nil {
		commandName = bundledBinaryPath
		commandArgs = []string{filePath, strconv.Itoa(limit)}
		debug.Logf("Using bundled Rusty Asset Tool binary: %s", bundledBinaryPath)
	} else if !errors.Is(bundledErr, errBundledBinaryUnavailable) {
		debug.Logf("Bundled Rusty Asset Tool prepare failed: %s", bundledErr.Error())
		return AssetIDsResult{UseCounts: map[int64]int{}}, bundledErr
	} else if binaryPath, found := findBinaryPath(); found {
		commandName = binaryPath
		commandArgs = []string{filePath, strconv.Itoa(limit)}
		debug.Logf("Using Rusty Asset Tool binary: %s", binaryPath)
	} else {
		toolDirectoryPath, cargoManifestPath, found := findCargoManifestPath()
		if !found {
			debug.Logf("Rusty Asset Tool unavailable: no bundled binary or Cargo manifest found")
			return AssetIDsResult{UseCounts: map[int64]int{}}, fmt.Errorf("Rusty Asset Tool unavailable: bundled binary not found")
		}
		commandName = "cargo"
		commandArgs = []string{"run", "--release", "--quiet", "--manifest-path", cargoManifestPath, "--", filePath, strconv.Itoa(limit)}
		debug.Logf("Rusty Asset Tool binary missing, using cargo run from %s", toolDirectoryPath)
	}

	command := exec.CommandContext(commandContext, commandName, commandArgs...)
	command.Env = appendCargoEnv(os.Environ())
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	command.Stdout = &stdoutBuffer
	command.Stderr = &stderrBuffer
	runErr := command.Run()
	if commandContext.Err() != nil {
		debug.Logf("Rusty Asset Tool cancelled")
		return AssetIDsResult{UseCounts: map[int64]int{}}, ErrCancelled
	}
	if runErr != nil {
		stderrText := strings.TrimSpace(stderrBuffer.String())
		if stderrText == "" {
			debug.Logf("Rusty Asset Tool failed: %s", runErr.Error())
			return AssetIDsResult{UseCounts: map[int64]int{}}, fmt.Errorf("Rusty Asset Tool failed: %s", runErr.Error())
		}
		debug.Logf("Rusty Asset Tool failed: %s | stderr: %s", runErr.Error(), stderrText)
		return AssetIDsResult{UseCounts: map[int64]int{}}, fmt.Errorf("Rusty Asset Tool failed: %s", stderrText)
	}

	commandOutput := stdoutBuffer.Bytes()
	commandOutputText := string(commandOutput)
	assetIDs, useCounts, refs := parseAssetIDsFromDOMJSON(commandOutputText, limit)
	debug.Logf("Rusty Asset Tool returned JSON bytes=%d and parsed references=%d", len(commandOutput), len(refs))
	if ckOk {
		assetIDsCache.Store(ck, assetIDsCacheEntry{
			AssetIDs:      assetIDs,
			UseCounts:     useCounts,
			References:    refs,
			CommandOutput: commandOutputText,
		})
	}
	return AssetIDsResult{
		AssetIDs:      assetIDs,
		UseCounts:     useCounts,
		References:    refs,
		CommandOutput: commandOutputText,
	}, nil
}

func ExtractFilteredRefs(filePath string, pathPrefixes []string, stopChannel <-chan struct{}) ([]Result, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, nil
	}
	ck, ckOk := cacheKeyFor(filePath, pathPrefixes, "filtered")
	if ckOk {
		if cached, found := filteredRefsCache.Load(ck); found {
			debug.Logf("Rusty Asset Tool filtered extraction cache hit: %s", filePath)
			return cached.([]Result), nil
		}
	}
	prefixArg := strings.Join(pathPrefixes, ",")
	debug.Logf("Rusty Asset Tool filtered extraction: %s (prefixes=%s)", filePath, prefixArg)

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
	if bundledBinaryPath, bundledErr := prepareBundledBinary(); bundledErr == nil {
		commandName = bundledBinaryPath
		commandArgs = []string{filePath, "0", prefixArg}
	} else if !errors.Is(bundledErr, errBundledBinaryUnavailable) {
		return nil, bundledErr
	} else if binaryPath, found := findBinaryPath(); found {
		commandName = binaryPath
		commandArgs = []string{filePath, "0", prefixArg}
	} else {
		toolDirectoryPath, cargoManifestPath, found := findCargoManifestPath()
		if !found {
			return nil, fmt.Errorf("Rusty Asset Tool unavailable: bundled binary not found")
		}
		commandName = "cargo"
		commandArgs = []string{"run", "--release", "--quiet", "--manifest-path", cargoManifestPath, "--", filePath, "0", prefixArg}
		debug.Logf("Rusty Asset Tool binary missing, using cargo run from %s", toolDirectoryPath)
	}

	command := exec.CommandContext(commandContext, commandName, commandArgs...)
	command.Env = appendCargoEnv(os.Environ())
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	command.Stdout = &stdoutBuffer
	command.Stderr = &stderrBuffer
	runErr := command.Run()
	if commandContext.Err() != nil {
		return nil, ErrCancelled
	}
	if runErr != nil {
		stderrText := strings.TrimSpace(stderrBuffer.String())
		if stderrText != "" {
			return nil, fmt.Errorf("Rusty Asset Tool failed: %s", stderrText)
		}
		return nil, fmt.Errorf("Rusty Asset Tool failed: %s", runErr.Error())
	}

	var results []Result
	if err := json.Unmarshal(stdoutBuffer.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("Rusty Asset Tool JSON parse failed: %s", err.Error())
	}
	debug.Logf("Rusty Asset Tool filtered extraction returned %d references", len(results))
	if ckOk {
		filteredRefsCache.Store(ck, results)
	}
	return results, nil
}

func ExtractPositionedRefs(filePath string, pathPrefixes []string, stopChannel <-chan struct{}) ([]PositionedResult, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, nil
	}
	ck, ckOk := cacheKeyFor(filePath, pathPrefixes, "heatmap")
	if ckOk {
		if cached, found := positionedRefsCache.Load(ck); found {
			debug.Logf("Rusty Asset Tool heatmap extraction cache hit: %s", filePath)
			return cached.([]PositionedResult), nil
		}
	}
	prefixArg := strings.Join(pathPrefixes, ",")
	debug.Logf("Rusty Asset Tool heatmap extraction: %s (prefixes=%s)", filePath, prefixArg)

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

	commandName, commandArgs, _, resolveErr := resolveSubcommand("heatmap", filePath, prefixArg)
	if resolveErr != nil {
		return nil, resolveErr
	}

	command := exec.CommandContext(commandContext, commandName, commandArgs...)
	command.Env = appendCargoEnv(os.Environ())
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	command.Stdout = &stdoutBuffer
	command.Stderr = &stderrBuffer
	runErr := command.Run()
	if commandContext.Err() != nil {
		return nil, ErrCancelled
	}
	if runErr != nil {
		stderrText := strings.TrimSpace(stderrBuffer.String())
		if stderrText != "" {
			return nil, fmt.Errorf("Rusty Asset Tool failed: %s", stderrText)
		}
		return nil, fmt.Errorf("Rusty Asset Tool failed: %s", runErr.Error())
	}

	var results []PositionedResult
	if err := json.Unmarshal(stdoutBuffer.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("Rusty Asset Tool JSON parse failed: %s", err.Error())
	}
	debug.Logf("Rusty Asset Tool heatmap extraction returned %d references", len(results))
	if ckOk {
		positionedRefsCache.Store(ck, results)
	}
	return results, nil
}

func ExtractMissingMaterialVariants(filePath string, pathPrefixes []string, stopChannel <-chan struct{}) ([]MissingMaterialVariantResult, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, nil
	}
	prefixArg := strings.Join(pathPrefixes, ",")
	debug.Logf("Rusty Asset Tool material warning extraction: %s (prefixes=%s)", filePath, prefixArg)

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

	commandName, commandArgs, _, resolveErr := resolveSubcommand("material-warnings", filePath, prefixArg)
	if resolveErr != nil {
		return nil, resolveErr
	}

	command := exec.CommandContext(commandContext, commandName, commandArgs...)
	command.Env = appendCargoEnv(os.Environ())
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	command.Stdout = &stdoutBuffer
	command.Stderr = &stderrBuffer
	runErr := command.Run()
	if commandContext.Err() != nil {
		return nil, ErrCancelled
	}
	if runErr != nil {
		stderrText := strings.TrimSpace(stderrBuffer.String())
		if stderrText != "" {
			return nil, fmt.Errorf("Rusty Asset Tool failed: %s", stderrText)
		}
		return nil, fmt.Errorf("Rusty Asset Tool failed: %s", runErr.Error())
	}

	var results []MissingMaterialVariantResult
	if err := json.Unmarshal(stdoutBuffer.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("Rusty Asset Tool JSON parse failed: %s", err.Error())
	}
	debug.Logf("Rusty Asset Tool material warning extraction returned %d missing variants", len(results))
	return results, nil
}

func ExtractMapRenderParts(filePath string, pathPrefixes []string, stopChannel <-chan struct{}) ([]MapRenderPartResult, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, nil
	}
	ck, ckOk := cacheKeyFor(filePath, pathPrefixes, "map")
	if ckOk {
		if cached, found := mapRenderPartsCache.Load(ck); found {
			debug.Logf("Rusty Asset Tool map render extraction cache hit: %s", filePath)
			return cached.([]MapRenderPartResult), nil
		}
	}
	prefixArg := strings.Join(pathPrefixes, ",")
	debug.Logf("Rusty Asset Tool map render extraction: %s (prefixes=%s)", filePath, prefixArg)

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

	commandName, commandArgs, _, resolveErr := resolveSubcommand("map", filePath, prefixArg)
	if resolveErr != nil {
		return nil, resolveErr
	}

	command := exec.CommandContext(commandContext, commandName, commandArgs...)
	command.Env = appendCargoEnv(os.Environ())
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	command.Stdout = &stdoutBuffer
	command.Stderr = &stderrBuffer
	runErr := command.Run()
	if commandContext.Err() != nil {
		return nil, ErrCancelled
	}
	if runErr != nil {
		stderrText := strings.TrimSpace(stderrBuffer.String())
		if stderrText != "" {
			return nil, fmt.Errorf("Rusty Asset Tool failed: %s", stderrText)
		}
		return nil, fmt.Errorf("Rusty Asset Tool failed: %s", runErr.Error())
	}

	var results []MapRenderPartResult
	if err := json.Unmarshal(stdoutBuffer.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("Rusty Asset Tool JSON parse failed: %s", err.Error())
	}
	debug.Logf("Rusty Asset Tool map render extraction returned %d parts", len(results))
	if ckOk {
		mapRenderPartsCache.Store(ck, results)
	}
	return results, nil
}

func ExtractAssetIDsFromBytes(fileBytes []byte, assetTypeID int, limit int) ([]int64, string, error) {
	result, err := ExtractAssetIDsFromBytesWithCounts(fileBytes, assetTypeID, limit)
	if err != nil {
		return nil, "", err
	}
	return result.AssetIDs, result.CommandOutput, nil
}

func ExtractAssetIDsFromBytesWithCounts(fileBytes []byte, assetTypeID int, limit int) (AssetIDsResult, error) {
	if len(fileBytes) == 0 {
		return AssetIDsResult{UseCounts: map[int64]int{}}, nil
	}

	tempFile, createErr := os.CreateTemp("", "joxblox-rusty-asset-tool-*.bin")
	if createErr != nil {
		debug.Logf("Rusty Asset Tool temp file create failed: %s", createErr.Error())
		return AssetIDsResult{UseCounts: map[int64]int{}}, createErr
	}
	tempFilePath := tempFile.Name()
	defer os.Remove(tempFilePath)

	_, writeErr := tempFile.Write(fileBytes)
	closeErr := tempFile.Close()
	if writeErr != nil {
		debug.Logf("Rusty Asset Tool temp file write failed: %s", writeErr.Error())
		return AssetIDsResult{UseCounts: map[int64]int{}}, writeErr
	}
	if closeErr != nil {
		debug.Logf("Rusty Asset Tool temp file close failed: %s", closeErr.Error())
		return AssetIDsResult{UseCounts: map[int64]int{}}, closeErr
	}

	debug.Logf("Rusty Asset Tool processing in-memory payload (%d bytes)", len(fileBytes))
	return ExtractAssetIDsWithCounts(tempFilePath, assetTypeID, limit, nil)
}

func ExtractMeshStatsFromBytes(fileBytes []byte) (mesh.HeaderInfo, error) {
	if len(fileBytes) == 0 {
		return mesh.HeaderInfo{}, fmt.Errorf("mesh data is empty")
	}

	tempFile, createErr := os.CreateTemp("", "joxblox-rusty-asset-tool-mesh-stats-*.bin")
	if createErr != nil {
		debug.Logf("Rusty Asset Tool mesh stats temp file create failed: %s", createErr.Error())
		return mesh.HeaderInfo{}, createErr
	}
	tempFilePath := tempFile.Name()
	defer os.Remove(tempFilePath)

	if _, writeErr := tempFile.Write(fileBytes); writeErr != nil {
		tempFile.Close()
		debug.Logf("Rusty Asset Tool mesh stats temp file write failed: %s", writeErr.Error())
		return mesh.HeaderInfo{}, writeErr
	}
	if closeErr := tempFile.Close(); closeErr != nil {
		debug.Logf("Rusty Asset Tool mesh stats temp file close failed: %s", closeErr.Error())
		return mesh.HeaderInfo{}, closeErr
	}

	return ExtractMeshStatsFromFile(tempFilePath)
}

func ExtractMeshStatsFromFile(filePath string) (mesh.HeaderInfo, error) {
	if strings.TrimSpace(filePath) == "" {
		return mesh.HeaderInfo{}, fmt.Errorf("mesh file path is empty")
	}

	commandName := ""
	commandArgs := []string{}
	if bundledBinaryPath, bundledErr := prepareBundledBinary(); bundledErr == nil {
		commandName = bundledBinaryPath
		commandArgs = []string{"mesh-stats", filePath}
	} else if !errors.Is(bundledErr, errBundledBinaryUnavailable) {
		return mesh.HeaderInfo{}, bundledErr
	} else if binaryPath, found := findBinaryPath(); found {
		commandName = binaryPath
		commandArgs = []string{"mesh-stats", filePath}
	} else {
		toolDirectoryPath, cargoManifestPath, found := findCargoManifestPath()
		if !found {
			return mesh.HeaderInfo{}, fmt.Errorf("Rusty Asset Tool unavailable: bundled binary not found")
		}
		commandName = "cargo"
		commandArgs = []string{"run", "--release", "--quiet", "--manifest-path", cargoManifestPath, "--", "mesh-stats", filePath}
		debug.Logf("Using cargo run for Rusty Asset Tool mesh stats extraction from %s", toolDirectoryPath)
	}

	command := exec.Command(commandName, commandArgs...)
	command.Env = appendCargoEnv(os.Environ())
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	command.Stdout = &stdoutBuffer
	command.Stderr = &stderrBuffer
	if runErr := command.Run(); runErr != nil {
		stderrText := strings.TrimSpace(stderrBuffer.String())
		if stderrText != "" {
			return mesh.HeaderInfo{}, fmt.Errorf("Rusty Asset Tool failed: %s", stderrText)
		}
		return mesh.HeaderInfo{}, fmt.Errorf("Rusty Asset Tool failed: %s", runErr.Error())
	}

	var stats meshStatsRawResult
	if err := json.Unmarshal(stdoutBuffer.Bytes(), &stats); err != nil {
		return mesh.HeaderInfo{}, fmt.Errorf("Rusty Asset Tool JSON parse failed: %s", err.Error())
	}
	return mesh.HeaderInfo{
		Version:  strings.TrimSpace(stats.FormatVersion),
		NumVerts: stats.VertexCount,
		NumFaces: stats.TriangleCount,
	}, nil
}

func ExtractMeshPreviewRawFromBytes(fileBytes []byte, maxTriangles int) (MeshPreviewRawResult, error) {
	return extractMeshPreviewRawFromBytes(fileBytes, maxTriangles, false)
}

// ExtractMeshPreviewRawFromBytesFull extracts the mesh preview without the
// sub-sampling cap that `ExtractMeshPreviewRawFromBytes` applies. Callers that
// want to slice the index buffer by LOD range (via the `Lods` field) must use
// this variant so that triangle boundaries are preserved.
func ExtractMeshPreviewRawFromBytesFull(fileBytes []byte) (MeshPreviewRawResult, error) {
	return extractMeshPreviewRawFromBytes(fileBytes, 0, true)
}

func extractMeshPreviewRawFromBytes(fileBytes []byte, maxTriangles int, unlimited bool) (MeshPreviewRawResult, error) {
	if len(fileBytes) == 0 {
		return MeshPreviewRawResult{}, fmt.Errorf("mesh data is empty")
	}

	tempFile, createErr := os.CreateTemp("", "joxblox-rusty-asset-tool-mesh-preview-*.bin")
	if createErr != nil {
		debug.Logf("Rusty Asset Tool mesh preview temp file create failed: %s", createErr.Error())
		return MeshPreviewRawResult{}, createErr
	}
	tempFilePath := tempFile.Name()
	defer os.Remove(tempFilePath)

	if _, writeErr := tempFile.Write(fileBytes); writeErr != nil {
		tempFile.Close()
		debug.Logf("Rusty Asset Tool mesh preview temp file write failed: %s", writeErr.Error())
		return MeshPreviewRawResult{}, writeErr
	}
	if closeErr := tempFile.Close(); closeErr != nil {
		debug.Logf("Rusty Asset Tool mesh preview temp file close failed: %s", closeErr.Error())
		return MeshPreviewRawResult{}, closeErr
	}

	return extractMeshPreviewRawFromFile(tempFilePath, maxTriangles, unlimited)
}

func ExtractMeshPreviewRawFromFile(filePath string, maxTriangles int) (MeshPreviewRawResult, error) {
	return extractMeshPreviewRawFromFile(filePath, maxTriangles, false)
}

// ExtractMeshPreviewRawFromFileFull mirrors ExtractMeshPreviewRawFromBytesFull
// for callers that already have the mesh on disk.
func ExtractMeshPreviewRawFromFileFull(filePath string) (MeshPreviewRawResult, error) {
	return extractMeshPreviewRawFromFile(filePath, 0, true)
}

func extractMeshPreviewRawFromFile(filePath string, maxTriangles int, unlimited bool) (MeshPreviewRawResult, error) {
	if strings.TrimSpace(filePath) == "" {
		return MeshPreviewRawResult{}, fmt.Errorf("mesh file path is empty")
	}
	if !unlimited && maxTriangles <= 0 {
		maxTriangles = 20000
	}

	extraArgs := []string{"mesh-preview", filePath}
	if !unlimited {
		extraArgs = append(extraArgs, strconv.Itoa(maxTriangles))
	}

	commandName := ""
	commandArgs := []string{}
	if bundledBinaryPath, bundledErr := prepareBundledBinary(); bundledErr == nil {
		commandName = bundledBinaryPath
		commandArgs = extraArgs
	} else if !errors.Is(bundledErr, errBundledBinaryUnavailable) {
		return MeshPreviewRawResult{}, bundledErr
	} else if binaryPath, found := findBinaryPath(); found {
		commandName = binaryPath
		commandArgs = extraArgs
	} else {
		toolDirectoryPath, cargoManifestPath, found := findCargoManifestPath()
		if !found {
			return MeshPreviewRawResult{}, fmt.Errorf("Rusty Asset Tool unavailable: bundled binary not found")
		}
		commandName = "cargo"
		commandArgs = append([]string{"run", "--release", "--quiet", "--manifest-path", cargoManifestPath, "--"}, extraArgs...)
		debug.Logf("Using cargo run for Rusty Asset Tool mesh preview extraction from %s", toolDirectoryPath)
	}

	command := exec.Command(commandName, commandArgs...)
	command.Env = appendCargoEnv(os.Environ())
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	command.Stdout = &stdoutBuffer
	command.Stderr = &stderrBuffer
	if runErr := command.Run(); runErr != nil {
		stderrText := strings.TrimSpace(stderrBuffer.String())
		if stderrText != "" {
			return MeshPreviewRawResult{}, fmt.Errorf("Rusty Asset Tool failed: %s", stderrText)
		}
		return MeshPreviewRawResult{}, fmt.Errorf("Rusty Asset Tool failed: %s", runErr.Error())
	}

	var preview MeshPreviewRawResult
	if err := json.Unmarshal(stdoutBuffer.Bytes(), &preview); err != nil {
		return MeshPreviewRawResult{}, fmt.Errorf("Rusty Asset Tool JSON parse failed: %s", err.Error())
	}
	return preview, nil
}

func ReplaceAssetIDs(inputPath string, outputPath string, replacements map[int64]int64, stopChannel <-chan struct{}) (int, error) {
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
	if bundledBinaryPath, bundledErr := prepareBundledBinary(); bundledErr == nil {
		commandName = bundledBinaryPath
		commandArgs = replaceArgs
		debug.Logf("Using bundled Rusty Asset Tool binary for replace: %s", bundledBinaryPath)
	} else if !errors.Is(bundledErr, errBundledBinaryUnavailable) {
		return 0, bundledErr
	} else if binaryPath, found := findBinaryPath(); found {
		commandName = binaryPath
		commandArgs = replaceArgs
		debug.Logf("Using Rusty Asset Tool binary for replace: %s", binaryPath)
	} else {
		_, cargoManifestPath, found := findCargoManifestPath()
		if !found {
			return 0, fmt.Errorf("Rusty Asset Tool unavailable: bundled binary not found")
		}
		commandName = "cargo"
		commandArgs = append([]string{"run", "--release", "--quiet", "--manifest-path", cargoManifestPath, "--"}, replaceArgs...)
		debug.Logf("Rusty Asset Tool binary missing, using cargo run for replace")
	}

	command := exec.CommandContext(commandContext, commandName, commandArgs...)
	command.Env = appendCargoEnv(os.Environ())
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	command.Stdout = &stdoutBuffer
	command.Stderr = &stderrBuffer
	runErr := command.Run()
	if commandContext.Err() != nil {
		debug.Logf("Rusty Asset Tool replace cancelled")
		return 0, ErrCancelled
	}
	if runErr != nil {
		stderrText := strings.TrimSpace(stderrBuffer.String())
		if stderrText != "" {
			debug.Logf("Rusty Asset Tool replace failed: %s | stderr: %s", runErr.Error(), stderrText)
			return 0, fmt.Errorf("Rusty Asset Tool replace failed: %s", stderrText)
		}
		debug.Logf("Rusty Asset Tool replace failed: %s", runErr.Error())
		return 0, fmt.Errorf("Rusty Asset Tool replace failed: %s", runErr.Error())
	}

	countText := strings.TrimSpace(stdoutBuffer.String())
	count, parseErr := strconv.Atoi(countText)
	if parseErr != nil {
		debug.Logf("Rusty Asset Tool replace returned non-numeric output: %s", countText)
		return 0, nil
	}
	debug.Logf("Rusty Asset Tool replace completed: %d property values replaced", count)
	return count, nil
}

func parseAssetIDsFromDOMJSON(domJSON string, limit int) ([]int64, map[int64]int, []Result) {
	extractorResults := []Result{}
	if unmarshalErr := json.Unmarshal([]byte(domJSON), &extractorResults); unmarshalErr != nil {
		debug.Logf("Rusty Asset Tool JSON parse failed: %s", unmarshalErr.Error())
		return []int64{}, map[int64]int{}, []Result{}
	}

	uniqueAssetIDs := make([]int64, 0, len(extractorResults))
	useCountsByAssetID := map[int64]int{}
	filteredResults := make([]Result, 0, len(extractorResults))
	seenAssetIDs := map[int64]bool{}
	seenReferenceKeys := map[string]bool{}
	for _, result := range extractorResults {
		if result.ID <= 0 {
			continue
		}
		useCount := result.Used
		if useCount <= 0 {
			useCount = 1
		}
		useCountsByAssetID[result.ID] += useCount
		if !seenAssetIDs[result.ID] {
			seenAssetIDs[result.ID] = true
			uniqueAssetIDs = append(uniqueAssetIDs, result.ID)
		}
		referenceKey := AssetReferenceKey(result.ID, result.RawContent)
		if seenReferenceKeys[referenceKey] {
			continue
		}
		seenReferenceKeys[referenceKey] = true
		filteredResults = append(filteredResults, result)
	}
	sort.Slice(uniqueAssetIDs, func(i int, j int) bool {
		return uniqueAssetIDs[i] < uniqueAssetIDs[j]
	})
	if limit > 0 && len(filteredResults) > limit {
		filteredResults = filteredResults[:limit]
	}
	limitedAssetIDs := make([]int64, 0, len(filteredResults))
	limitedUseCounts := map[int64]int{}
	seenLimitedAssetIDs := map[int64]bool{}
	for _, result := range filteredResults {
		if !seenLimitedAssetIDs[result.ID] {
			seenLimitedAssetIDs[result.ID] = true
			limitedAssetIDs = append(limitedAssetIDs, result.ID)
		}
		if useCount, found := useCountsByAssetID[result.ID]; found {
			limitedUseCounts[result.ID] = useCount
		} else {
			limitedUseCounts[result.ID] = 1
		}
	}
	return limitedAssetIDs, limitedUseCounts, filteredResults
}
