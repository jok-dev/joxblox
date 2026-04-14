package extractor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"joxblox/internal/debug"
)

var (
	errBundledBinaryUnavailable = errors.New("bundled binary unavailable")
	bundledPathOnce             sync.Once
	bundledPath                 string
	bundledPathErr              error
)

func getRepositoryRootPath() (string, error) {
	_, currentFilePath, _, callerOK := runtime.Caller(0)
	if !callerOK || strings.TrimSpace(currentFilePath) == "" {
		return "", fmt.Errorf("unable to resolve source path")
	}
	extractorDirectoryPath := filepath.Dir(currentFilePath)
	internalDirectoryPath := filepath.Dir(extractorDirectoryPath)
	repositoryRootPath := filepath.Dir(internalDirectoryPath)
	return repositoryRootPath, nil
}

func binaryFileName() string {
	fileName := "joxblox-rusty-asset-tool"
	if runtime.GOOS == "windows" {
		fileName += ".exe"
	}
	return fileName
}

func legacyBinaryFileName() string {
	fileName := "rbxl-id-extractor"
	if runtime.GOOS == "windows" {
		fileName += ".exe"
	}
	return fileName
}

func relativeBinaryPath(binaryFileName string) string {
	return filepath.Join("tools", "rbxl-id-extractor", "target", "release", binaryFileName)
}

func prepareBundledBinary() (string, error) {
	if BinaryProvider == nil {
		return "", errBundledBinaryUnavailable
	}
	bundledBytes := BinaryProvider()
	if len(bundledBytes) == 0 {
		return "", errBundledBinaryUnavailable
	}

	bundledPathOnce.Do(func() {
		tempDirectoryPath, tempDirErr := os.MkdirTemp("", "joxblox-rusty-asset-tool-*")
		if tempDirErr != nil {
			bundledPathErr = fmt.Errorf("failed creating bundled binary temp directory: %w", tempDirErr)
			return
		}
		extractorPath := filepath.Join(tempDirectoryPath, binaryFileName())
		if writeErr := os.WriteFile(extractorPath, bundledBytes, 0755); writeErr != nil {
			bundledPathErr = fmt.Errorf("failed writing bundled binary: %w", writeErr)
			return
		}
		if runtime.GOOS != "windows" {
			if chmodErr := os.Chmod(extractorPath, 0755); chmodErr != nil {
				bundledPathErr = fmt.Errorf("failed marking bundled binary executable: %w", chmodErr)
				return
			}
		}
		bundledPath = extractorPath
	})

	if bundledPathErr != nil {
		return "", bundledPathErr
	}
	if strings.TrimSpace(bundledPath) == "" {
		return "", errBundledBinaryUnavailable
	}
	return bundledPath, nil
}

func findBinaryPath() (string, bool) {
	candidatePaths := make([]string, 0, 8)
	if executablePath, err := os.Executable(); err == nil && strings.TrimSpace(executablePath) != "" {
		executableDirectory := filepath.Dir(executablePath)
		candidatePaths = append(candidatePaths,
			filepath.Join(executableDirectory, relativeBinaryPath(binaryFileName())),
			filepath.Join(executableDirectory, binaryFileName()),
			filepath.Join(executableDirectory, relativeBinaryPath(legacyBinaryFileName())),
			filepath.Join(executableDirectory, legacyBinaryFileName()),
		)
	}
	if repositoryRootPath, err := getRepositoryRootPath(); err == nil && strings.TrimSpace(repositoryRootPath) != "" {
		candidatePaths = append(candidatePaths,
			filepath.Join(repositoryRootPath, relativeBinaryPath(binaryFileName())),
			filepath.Join(repositoryRootPath, relativeBinaryPath(legacyBinaryFileName())),
		)
	}
	for _, candidatePath := range candidatePaths {
		if _, err := os.Stat(candidatePath); err == nil {
			return candidatePath, true
		}
	}
	return "", false
}

func findCargoManifestPath() (string, string, bool) {
	if repositoryRootPath, err := getRepositoryRootPath(); err == nil && strings.TrimSpace(repositoryRootPath) != "" {
		toolDirectoryPath := filepath.Join(repositoryRootPath, "tools", "rbxl-id-extractor")
		cargoManifestPath := filepath.Join(toolDirectoryPath, "Cargo.toml")
		if _, err := os.Stat(cargoManifestPath); err == nil {
			return toolDirectoryPath, cargoManifestPath, true
		}
	}
	return "", "", false
}

func resolveSubcommand(subcommand string, filePath string, extraArgs ...string) (string, []string, bool, error) {
	trimmedSubcommand := strings.TrimSpace(subcommand)
	if trimmedSubcommand == "" {
		return "", nil, false, fmt.Errorf("subcommand is required")
	}

	filteredExtraArgs := make([]string, 0, len(extraArgs))
	for _, arg := range extraArgs {
		if strings.TrimSpace(arg) == "" {
			continue
		}
		filteredExtraArgs = append(filteredExtraArgs, arg)
	}

	if bundledBinaryPath, bundledErr := prepareBundledBinary(); bundledErr == nil {
		return resolveSubcommandWithPaths(trimmedSubcommand, filePath, bundledBinaryPath, "", "", filteredExtraArgs...)
	} else if !errors.Is(bundledErr, errBundledBinaryUnavailable) {
		return "", nil, false, bundledErr
	}

	if binaryPath, found := findBinaryPath(); found {
		return resolveSubcommandWithPaths(trimmedSubcommand, filePath, "", binaryPath, "", filteredExtraArgs...)
	}

	_, cargoManifestPath, found := findCargoManifestPath()
	if !found {
		return "", nil, false, fmt.Errorf("Rusty Asset Tool unavailable: bundled binary not found")
	}
	return resolveSubcommandWithPaths(trimmedSubcommand, filePath, "", "", cargoManifestPath, filteredExtraArgs...)
}

func resolveSubcommandWithPaths(subcommand string, filePath string, bundledBinaryPath string, binaryPath string, cargoManifestPath string, extraArgs ...string) (string, []string, bool, error) {
	trimmedSubcommand := strings.TrimSpace(subcommand)
	if trimmedSubcommand == "" {
		return "", nil, false, fmt.Errorf("subcommand is required")
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
		debug.Logf("Using bundled Rusty Asset Tool binary: %s", bundledBinaryPath)
		return bundledBinaryPath, commandArgs, false, nil
	}

	if strings.TrimSpace(binaryPath) != "" {
		commandArgs := append([]string{trimmedSubcommand, filePath}, filteredExtraArgs...)
		debug.Logf("Using Rusty Asset Tool binary: %s", binaryPath)
		return binaryPath, commandArgs, false, nil
	}

	if strings.TrimSpace(cargoManifestPath) == "" {
		return "", nil, false, fmt.Errorf("Rusty Asset Tool unavailable: bundled binary not found")
	}
	toolDirectoryPath := filepath.Dir(cargoManifestPath)
	commandArgs := []string{"run", "--release", "--quiet", "--manifest-path", cargoManifestPath, "--", trimmedSubcommand, filePath}
	commandArgs = append(commandArgs, filteredExtraArgs...)
	debug.Logf("Using cargo run for Rusty Asset Tool %s extraction from %s", trimmedSubcommand, toolDirectoryPath)
	return "cargo", commandArgs, true, nil
}

func appendCargoEnv(env []string) []string {
	if toolDirectoryPath, _, found := findCargoManifestPath(); found {
		cargoHomePath := filepath.Join(os.TempDir(), "joxblox-cargo-home")
		targetPath := filepath.Join(toolDirectoryPath, "target")
		env = append(env,
			fmt.Sprintf("CARGO_HOME=%s", cargoHomePath),
			fmt.Sprintf("CARGO_TARGET_DIR=%s", targetPath),
		)
	}
	return env
}
