package common

import (
	"path/filepath"
	"strings"
)

func IsRobloxDOMFilePath(filePath string) bool {
	extension := strings.ToLower(filepath.Ext(strings.TrimSpace(filePath)))
	return extension == ".rbxl" || extension == ".rbxm"
}
