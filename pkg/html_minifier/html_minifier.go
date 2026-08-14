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
	tokenRegexp          = regexp.MustCompile(`<[^>]*>|[^<]+`)
	tagNameRegexp        = regexp.MustCompile(`^</?([^\s/>]+)`)
)

// whitespaceInsignificantTagNames lists elements next to which inter-tag
// whitespace never renders: the document skeleton, metadata, and block-level
// containers whose edges collapse whitespace. Whitespace adjacent to any other
// element (inline elements, and custom elements, which default to inline) is
// kept, as it may be significant.
var whitespaceInsignificantTagNames = map[string]struct{}{
	"!doctype": {},
	"address":  {}, "article": {}, "aside": {}, "base": {}, "blockquote": {},
	"body": {}, "br": {}, "caption": {}, "col": {}, "colgroup": {},
	"dd": {}, "details": {}, "dialog": {}, "div": {}, "dl": {}, "dt": {},
	"fieldset": {}, "figcaption": {}, "figure": {}, "footer": {}, "form": {},
	"h1": {}, "h2": {}, "h3": {}, "h4": {}, "h5": {}, "h6": {},
	"head": {}, "header": {}, "hr": {}, "html": {},
	"legend": {}, "li": {}, "link": {},
	"main": {}, "menu": {}, "meta": {}, "nav": {}, "noscript": {},
	"ol": {}, "optgroup": {}, "option": {},
	"p": {}, "search": {}, "section": {}, "summary": {},
	"table": {}, "tbody": {}, "td": {}, "template": {}, "tfoot": {},
	"th": {}, "thead": {}, "title": {}, "tr": {}, "ul": {},
}

func isWhitespaceInsignificantTag(token string) bool {
	match := tagNameRegexp.FindStringSubmatch(token)
	if match == nil {
		return false
	}

	_, ok := whitespaceInsignificantTagNames[strings.ToLower(match[1])]
	return ok
}

func minifySegment(segment string) string {
	segment = commentRegexp.ReplaceAllString(segment, "")
	segment = whitespaceRegexp.ReplaceAllString(segment, " ")

	tokens := tokenRegexp.FindAllString(segment, -1)

	var output strings.Builder
	for i, token := range tokens {
		// A whitespace-only token sits between two tags; drop it when either
		// neighbour cannot render adjacent whitespace.
		if token == " " && i > 0 && i < len(tokens)-1 {
			if isWhitespaceInsignificantTag(tokens[i-1]) || isWhitespaceInsignificantTag(tokens[i+1]) {
				continue
			}
		}
		output.WriteString(token)
	}

	return output.String()
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
