// Package svg_minifier performs a conservative cleanup of SVG documents: the
// XML declaration, doctype and comments are removed and whitespace between tags
// is collapsed. It is deliberately not an optimizer like svgo; path data and
// attributes are left untouched.
package svg_minifier

import (
	"regexp"
	"strings"
)

var (
	xmlDeclarationRegexp = regexp.MustCompile(`(?s)<\?xml.*?\?>`)
	doctypeRegexp        = regexp.MustCompile(`(?is)<!DOCTYPE.*?>`)
	commentRegexp        = regexp.MustCompile(`(?s)<!--.*?-->`)
	whitespaceRegexp     = regexp.MustCompile(`\s+`)
)

// Minify minifies the provided SVG document.
func Minify(svg string) string {
	svg = xmlDeclarationRegexp.ReplaceAllString(svg, "")
	svg = doctypeRegexp.ReplaceAllString(svg, "")
	svg = commentRegexp.ReplaceAllString(svg, "")
	svg = whitespaceRegexp.ReplaceAllString(svg, " ")
	svg = strings.ReplaceAll(svg, "> <", "><")
	return strings.TrimSpace(svg)
}
