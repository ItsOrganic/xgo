// Package pkgb provides string utilities for the realistic benchmark
// scenario's synthetic "service" endpoints.
package pkgb

import (
	"strings"
	"unicode"
)

// Slugify lowercases s and replaces runs of non-alphanumeric characters
// with a single hyphen, trimming leading/trailing hyphens.
func Slugify(s string) string {
	var b strings.Builder
	lastHyphen := true
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// WordCount returns the number of whitespace-separated words in s.
func WordCount(s string) int {
	return len(strings.Fields(s))
}

// Reverse returns s with its runes reversed (unicode-safe).
func Reverse(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

// TitleCase upper-cases the first letter of every word.
func TitleCase(s string) string {
	fields := strings.Fields(s)
	for i, f := range fields {
		if f == "" {
			continue
		}
		r := []rune(f)
		r[0] = unicode.ToUpper(r[0])
		fields[i] = string(r)
	}
	return strings.Join(fields, " ")
}
