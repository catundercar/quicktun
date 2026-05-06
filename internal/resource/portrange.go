package resource

import (
	"fmt"
	"strconv"
	"strings"
)

// ParsePortRange parses a "min-max" string. Both endpoints inclusive.
// Validates: min <= max, both fit in uint16, range is wide enough to hold
// at least one rathole control port plus one service port (i.e. max > min).
func ParsePortRange(s string) (uint16, uint16, error) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid port range %q: expected min-max", s)
	}
	minI, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid min port: %w", err)
	}
	maxI, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid max port: %w", err)
	}
	if minI > maxI {
		return 0, 0, fmt.Errorf("invalid port range %q: min %d > max %d", s, minI, maxI)
	}
	if minI == maxI {
		return 0, 0, fmt.Errorf("port range %q too narrow: need at least 2 ports (control + service)", s)
	}
	return uint16(minI), uint16(maxI), nil
}
