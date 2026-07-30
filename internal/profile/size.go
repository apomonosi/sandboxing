package profile

import (
	"fmt"
	"strconv"
	"strings"
)

// unitMultipliers covers both binary (GiB) and decimal (GB) units, largest
// suffix first so e.g. "GiB" doesn't get matched by a "B" prefix check.
var unitMultipliers = []struct {
	suffix string
	factor int64
}{
	{"TiB", 1 << 40}, {"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10},
	{"TB", 1_000_000_000_000}, {"GB", 1_000_000_000}, {"MB", 1_000_000}, {"KB", 1_000},
	{"B", 1},
}

// ParseSize parses a size string like "4GiB", "20GB", or "512" (bytes) into
// a byte count. Zero and negative sizes are rejected.
func ParseSize(s string) (int64, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, fmt.Errorf("empty size")
	}
	for _, u := range unitMultipliers {
		if strings.HasSuffix(trimmed, u.suffix) {
			numPart := strings.TrimSpace(strings.TrimSuffix(trimmed, u.suffix))
			n, err := strconv.ParseFloat(numPart, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid size %q: %w", s, err)
			}
			if n <= 0 {
				return 0, fmt.Errorf("invalid size %q: must be positive", s)
			}
			return int64(n * float64(u.factor)), nil
		}
	}
	// No recognized unit suffix: treat as a plain byte count.
	n, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: no recognized unit and not a plain integer", s)
	}
	if n <= 0 {
		return 0, fmt.Errorf("invalid size %q: must be positive", s)
	}
	return n, nil
}
