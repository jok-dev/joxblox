package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// MeshRendererBinaryProvider returns the bytes of a mesh-renderer binary
// compiled for the current platform, or nil when one was not bundled into this
// build. It is set by the app package when a release build embeds the binary.
var MeshRendererBinaryProvider func() []byte

var (
	errBundledMeshRendererUnavailable = errors.New("bundled mesh renderer unavailable")
	bundledMeshRendererPathOnce       sync.Once
	bundledMeshRendererPath           string
	bundledMeshRendererPathErr        error
)

// prepareBundledMeshRendererBinary writes the bundled mesh-renderer binary to a
// temp directory on first use and returns the resulting path for subsequent
// calls. Returns errBundledMeshRendererUnavailable when no binary was bundled.
func prepareBundledMeshRendererBinary() (string, error) {
	if MeshRendererBinaryProvider == nil {
		return "", errBundledMeshRendererUnavailable
	}
	bundledBytes := MeshRendererBinaryProvider()
	if len(bundledBytes) == 0 {
		return "", errBundledMeshRendererUnavailable
	}

	bundledMeshRendererPathOnce.Do(func() {
		tempDirectoryPath, tempDirErr := os.MkdirTemp("", "joxblox-mesh-renderer-*")
		if tempDirErr != nil {
			bundledMeshRendererPathErr = fmt.Errorf("failed creating bundled mesh renderer temp directory: %w", tempDirErr)
			return
		}
		binaryPath := filepath.Join(tempDirectoryPath, meshRendererBinaryName())
		if writeErr := os.WriteFile(binaryPath, bundledBytes, 0o755); writeErr != nil {
			bundledMeshRendererPathErr = fmt.Errorf("failed writing bundled mesh renderer: %w", writeErr)
			return
		}
		if runtime.GOOS != "windows" {
			if chmodErr := os.Chmod(binaryPath, 0o755); chmodErr != nil {
				bundledMeshRendererPathErr = fmt.Errorf("failed marking bundled mesh renderer executable: %w", chmodErr)
				return
			}
		}
		bundledMeshRendererPath = binaryPath
	})

	if bundledMeshRendererPathErr != nil {
		return "", bundledMeshRendererPathErr
	}
	if bundledMeshRendererPath == "" {
		return "", errBundledMeshRendererUnavailable
	}
	return bundledMeshRendererPath, nil
}
