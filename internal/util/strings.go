package util

import "strings"

// ParseTags parses a comma-separated "key=value" string into a map.
// Entries that are empty or don't contain exactly one "=" are silently skipped.
func ParseTags(tags string) map[string]string {
	tagsMap := make(map[string]string)
	for _, e := range strings.Split(tags, ",") {
		if len(strings.TrimSpace(e)) == 0 {
			continue
		}
		parts := strings.Split(e, "=")
		if len(parts) == 2 {
			tagsMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return tagsMap
}

// SplitRegions splits a comma-separated region string into a slice,
// trimming whitespace and filtering empty entries.
func SplitRegions(s string) []string {
	var regions []string
	for _, r := range strings.Split(s, ",") {
		r = strings.TrimSpace(r)
		if r != "" {
			regions = append(regions, r)
		}
	}
	return regions
}
