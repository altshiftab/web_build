// Package html_minifier collapses whitespace and strips comments from HTML,
// leaving pre, textarea, script and style contents untouched.
package html_minifier

import (
	"regexp"
	"strings"
)

var (
	preservedBlockRegexp = regexp.MustCompile(`(?is)<(?:pre|textarea|script|style)\b[^>]*>.*?</(?:pre|textarea|script|style)>`)
	commentRegexp        = regexp.MustCompile(`(?s)<!--.*?-->`)
	whitespaceRegexp     = regexp.MustCompile(`\s+`)
)

func minifySegment(segment string) string {
	segment = commentRegexp.ReplaceAllString(segment, "")
	segment = whitespaceRegexp.ReplaceAllString(segment, " ")
	return strings.ReplaceAll(segment, "> <", "><")
}

// Minify minifies the provided HTML.
func Minify(html string) string {
	var output strings.Builder
	lastEnd := 0
	for _, match := range preservedBlockRegexp.FindAllStringIndex(html, -1) {
		output.WriteString(minifySegment(html[lastEnd:match[0]]))
		output.WriteString(html[match[0]:match[1]])
		lastEnd = match[1]
	}
	output.WriteString(minifySegment(html[lastEnd:]))

	return strings.TrimSpace(output.String())
}
