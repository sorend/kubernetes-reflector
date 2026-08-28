package glob

import (
	"regexp"
	"strings"
)

func ParsePatterns(patterns string) []*regexp.Regexp {
	if strings.TrimSpace(patterns) == "" {
		return nil
	}

	parts := strings.Split(patterns, ",")
	result := make([]*regexp.Regexp, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		expr := "^" + regexp.QuoteMeta(part) + "$"
		expr = strings.ReplaceAll(expr, `\*`, `.*`)
		expr = strings.ReplaceAll(expr, `\?`, `.`)
		result = append(result, regexp.MustCompile(expr))
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

func IsExcluded(ns string, patterns []*regexp.Regexp) bool {
	if len(patterns) == 0 || ns == "" {
		return false
	}

	for _, pattern := range patterns {
		if pattern != nil && pattern.MatchString(ns) {
			return true
		}
	}

	return false
}
