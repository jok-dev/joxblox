package format

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"
)

const Megabyte = 1024 * 1024
const Gigabyte = 1024 * Megabyte

func Clamp[T cmp.Ordered](value, minimum, maximum T) T {
	return max(minimum, min(value, maximum))
}

func FormatSizeAuto(bytesSize int) string {
	return FormatSizeAuto64(int64(bytesSize))
}

// FormatDimensions renders a width × height pair as "WxH".
func FormatDimensions(width, height int) string {
	return fmt.Sprintf("%dx%d", width, height)
}

func FormatSizeAuto64(bytesSize int64) string {
	if bytesSize >= Megabyte {
		return fmt.Sprintf("%.2f MB", float64(bytesSize)/Megabyte)
	}
	if bytesSize >= 1024 {
		return fmt.Sprintf("%.2f KB", float64(bytesSize)/1024.0)
	}
	return fmt.Sprintf("%d bytes", bytesSize)
}

func FormatSizeAutoWithSuffix(value float64, suffix string) string {
	if value >= Megabyte {
		return strconv.FormatFloat(value/Megabyte, 'f', 2, 64) + " MB" + suffix
	}
	if value >= 1024 {
		return strconv.FormatFloat(value/1024.0, 'f', 2, 64) + " KB" + suffix
	}
	return strconv.FormatFloat(value, 'f', 2, 64) + " B" + suffix
}

func FormatIntCommas(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, ch := range s {
		remaining := len(s) - i
		if i > 0 && remaining%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(ch))
	}
	return string(result)
}

func FormatSignedSizeAuto(value int64) string {
	if value == 0 {
		return "0 B"
	}
	if value < 0 {
		if value == -value {
			return "-" + FormatSizeAuto(0)
		}
		return "-" + FormatSizeAuto(int(-value))
	}
	return "+" + FormatSizeAuto(int(value))
}

func AbsInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

// ParseSizeBytes parses a human size string ("1mb", "500 KB", "2.5gb", "1024")
// into bytes. Units are case-insensitive; missing unit = raw bytes. Returns
// false if the input is not parseable.
func ParseSizeBytes(raw string) (int64, bool) {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if trimmed == "" {
		return 0, false
	}
	multiplier := int64(1)
	unitSuffixes := []struct {
		suffix string
		factor int64
	}{
		{"gb", Gigabyte},
		{"g", Gigabyte},
		{"mb", Megabyte},
		{"m", Megabyte},
		{"kb", 1024},
		{"k", 1024},
		{"b", 1},
	}
	numericPart := trimmed
	for _, entry := range unitSuffixes {
		if strings.HasSuffix(trimmed, entry.suffix) {
			numericPart = strings.TrimSpace(strings.TrimSuffix(trimmed, entry.suffix))
			multiplier = entry.factor
			break
		}
	}
	if numericPart == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(numericPart, 64)
	if err != nil {
		return 0, false
	}
	return int64(value * float64(multiplier)), true
}

func FormatSignedIntCommas(value int64) string {
	if value == 0 {
		return "0"
	}
	if value < 0 {
		if value == -value {
			return "-" + FormatIntCommas(0)
		}
		return "-" + FormatIntCommas(-value)
	}
	return "+" + FormatIntCommas(value)
}
