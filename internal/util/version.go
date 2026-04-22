package util

import (
	"strconv"
	"strings"
)

// CompareVersions compares two version strings numerically.
// It extracts numeric segments from each version (skipping non-numeric parts
// like "mysql_aurora") and compares them segment-by-segment.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
//
// Examples:
//   - "8.0.mysql_aurora.3.04.0" → segments [8, 0, 3, 4, 0]
//   - "15.4" → segments [15, 4]
//   - "15.10" → segments [15, 10]
func CompareVersions(a, b string) int {
	aSegs := ExtractVersionSegments(a)
	bSegs := ExtractVersionSegments(b)

	maxLen := len(aSegs)
	if len(bSegs) > maxLen {
		maxLen = len(bSegs)
	}

	for i := 0; i < maxLen; i++ {
		aVal := 0
		if i < len(aSegs) {
			aVal = aSegs[i]
		}
		bVal := 0
		if i < len(bSegs) {
			bVal = bSegs[i]
		}
		if aVal < bVal {
			return -1
		}
		if aVal > bVal {
			return 1
		}
	}
	return 0
}

// ExtractVersionSegments splits a version string by "." and returns only the
// numeric segments as integers. Non-numeric parts (like "mysql_aurora") are skipped.
func ExtractVersionSegments(version string) []int {
	parts := strings.Split(version, ".")
	var segments []int
	for _, p := range parts {
		if n, err := strconv.Atoi(p); err == nil {
			segments = append(segments, n)
		}
	}
	return segments
}
