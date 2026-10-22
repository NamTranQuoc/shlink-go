package config

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

var numericPattern = regexp.MustCompile(`^[ \t\n\r\v\f]*[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?[ \t\n\r\v\f]*$`)

// isNumeric ports PHP 8's is_numeric() for strings.
func isNumeric(s string) bool {
	return numericPattern.MatchString(s)
}

var leadingNumber = regexp.MustCompile(`^[ \t\n\r\v\f]*[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?`)

// phpIntCast ports PHP's (int) cast of a string: the leading numeric part is used, anything else yields 0.
// PHPIntCast ports PHP's (int) cast of a string.
func PHPIntCast(s string) int { return phpIntCast(s) }

func phpIntCast(s string) int {
	m := leadingNumber.FindString(s)
	if m == "" {
		return 0
	}
	m = strings.TrimSpace(m)
	if i, err := strconv.ParseInt(m, 10, 64); err == nil {
		return int(i)
	}
	f, err := strconv.ParseFloat(m, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return int(f)
}

// SplitByComma ports splitByComma(): null or blank yields no items, otherwise every comma-separated item, trimmed.
func SplitByComma(v Value) []string {
	if v.IsNull() {
		return []string{}
	}
	s := v.AsString()
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

// IsNumericValue ports is_numeric() over an env var value.
func IsNumericValue(v Value) bool {
	switch v.Kind {
	case KindInt:
		return true
	case KindString:
		return isNumeric(v.Str)
	}
	return false
}
