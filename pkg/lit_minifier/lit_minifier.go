// Package lit_minifier minifies the contents of css and html tagged template
// literals in JavaScript source. The source is scanned rather than parsed;
// template contents are minified and spliced back, leaving all other code
// byte-identical.
package lit_minifier

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/altshiftab/web_build/pkg/css_minifier"
)

const placeholder = "@TEMPLATE_EXPRESSION"

var (
	// preformattedTagRegexp matches the start and end tags of the elements whose whitespace is
	// content, and not those of custom elements whose names begin the same.
	preformattedTagRegexp = regexp.MustCompile(`(?i)<(/?)(pre|textarea)(?:[\s/][^>]*)?>`)
)

type tag int

const (
	tagNone tag = iota
	tagCss
	tagHtml
)

// taggedTemplate is a css or html tagged template literal, by the spans of its quasis: the text
// between the backticks and around the substitutions.
type taggedTemplate struct {
	isCss       bool
	quasiStarts []int
	quasiEnds   []int
}

func (template *taggedTemplate) addQuasi(start int, end int) {
	template.quasiStarts = append(template.quasiStarts, start)
	template.quasiEnds = append(template.quasiEnds, end)
}

// replacement is a span of the source and the text that replaces it.
type replacement struct {
	start int
	end   int
	text  string
}

// minifyQuasis minifies the quasis as one text, a placeholder standing in for each substitution, so
// that what a substitution splits, such as a rule or an element, is minified whole. A substitution
// the minifier dropped, as the CSS minifier drops a comment holding one, would leave every later
// substitution in the place of the one before it; the quasis are then left as they were.
func minifyQuasis(quasis []string, minify func(string) string) []string {
	parts := strings.Split(minify(strings.Join(quasis, placeholder)), placeholder)
	if len(parts) != len(quasis) {
		return quasis
	}
	return parts
}

// htmlWhitespace is the whitespace of HTML, which is also what \s matches in RE2.
const htmlWhitespace = " \t\n\f\r"

func isHtmlWhitespace(character byte) bool {
	return strings.IndexByte(htmlWhitespace, character) != -1
}

// escapeLength returns the length of the escape sequence starting with the backslash at the given
// index: the backslash and the character it escapes, a CRLF pair being one line terminator.
func escapeLength(text string, index int) int {
	if strings.HasPrefix(text[index+1:], "\r\n") {
		return 3
	}
	return 2
}

// collapseWhitespace replaces each run of HTML whitespace with a single space. A whitespace
// character that a backslash escapes, as in a line continuation, belongs to the escape and is
// left as it is. Text with nothing to collapse is returned as it is.
func collapseWhitespace(text string) string {
	var output strings.Builder
	copied := 0
	for i := 0; i < len(text); {
		if text[i] == '\\' {
			i += escapeLength(text, i)
			continue
		}
		if !isHtmlWhitespace(text[i]) {
			i++
			continue
		}
		end := i + 1
		for end < len(text) && isHtmlWhitespace(text[end]) {
			end++
		}
		if end-i > 1 || text[i] != ' ' {
			output.Grow(len(text))
			output.WriteString(text[copied:i])
			output.WriteByte(' ')
			copied = end
		}
		i = end
	}
	if copied == 0 {
		return text
	}
	output.WriteString(text[copied:])
	return output.String()
}

// endsInEscape reports whether the text ends in a backslash that escapes what would follow it.
func endsInEscape(text string) bool {
	backslashes := len(text) - len(strings.TrimRight(text, "\\"))
	return backslashes%2 == 1
}

// trimHtmlWhitespace trims HTML whitespace from both ends of the text, short of a character a
// backslash escapes.
func trimHtmlWhitespace(text string) string {
	trimmed := strings.TrimRight(text, htmlWhitespace)
	if endsInEscape(trimmed) && len(trimmed) < len(text) {
		trimmed = text[:len(trimmed)+1]
	}
	return strings.TrimLeft(trimmed, htmlWhitespace)
}

// isValidQuasi reports whether the text can stand between a template literal's backticks and
// substitutions as it is: it holds no backtick and no "${" that is not escaped, and does not end
// in a backslash, which would escape the delimiter after it. Minifying can produce one, as when
// trimming leaves a line continuation's backslash at the end.
func isValidQuasi(text string) bool {
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\\':
			if i+1 == len(text) {
				return false
			}
			i++
		case '`':
			return false
		case '$':
			if i+1 < len(text) && text[i+1] == '{' {
				return false
			}
		}
	}
	return true
}

// minifyHtml collapses each run of whitespace to a single space, line breaks included, and trims
// the ends, except inside pre and textarea elements, where whitespace is content.
func minifyHtml(html string) string {
	var output strings.Builder
	position := 0
	openElement := ""

	for _, match := range preformattedTagRegexp.FindAllStringSubmatchIndex(html, -1) {
		isEndTag := match[3] > match[2]
		name := strings.ToLower(html[match[4]:match[5]])
		switch {
		case openElement == "" && !isEndTag:
			output.WriteString(collapseWhitespace(html[position:match[1]]))
			position = match[1]
			openElement = name
		case openElement == name && isEndTag:
			output.WriteString(html[position:match[0]])
			position = match[0]
			openElement = ""
		}
	}

	rest := html[position:]
	if openElement == "" {
		rest = collapseWhitespace(rest)
	}
	output.WriteString(rest)

	if openElement != "" {
		// The end is inside the element, whose whitespace is content.
		return strings.TrimLeft(output.String(), htmlWhitespace)
	}
	return trimHtmlWhitespace(output.String())
}

// replacements minifies the template's quasis and returns those that changed.
func (template *taggedTemplate) replacements(source string) []*replacement {
	quasis := make([]string, len(template.quasiStarts))
	for i := range quasis {
		quasis[i] = source[template.quasiStarts[i]:template.quasiEnds[i]]
	}

	minify := minifyHtml
	if template.isCss {
		minify = css_minifier.Minify
	}
	minified := minifyQuasis(quasis, minify)
	for _, minifiedQuasi := range minified {
		if !isValidQuasi(minifiedQuasi) {
			return nil
		}
	}

	var replacements []*replacement
	for i, quasi := range quasis {
		if minified[i] != quasi {
			replacements = append(replacements, &replacement{
				start: template.quasiStarts[i],
				end:   template.quasiEnds[i],
				text:  minified[i],
			})
		}
	}
	return replacements
}

func applyReplacements(source string, replacements []*replacement) string {
	if len(replacements) == 0 {
		return source
	}

	// A template nested in another's substitution is complete before the outer one is.
	slices.SortFunc(replacements, func(a *replacement, b *replacement) int {
		return cmp.Compare(a.start, b.start)
	})

	var output strings.Builder
	output.Grow(len(source))
	position := 0
	for _, currentReplacement := range replacements {
		output.WriteString(source[position:currentReplacement.start])
		output.WriteString(currentReplacement.text)
		position = currentReplacement.end
	}
	output.WriteString(source[position:])
	return output.String()
}

// Minify minifies css and html tagged template literals in the provided source. A template is
// tagged when a css or html identifier precedes it, other than as a property name; templates
// nested in another's substitutions are minified too.
func Minify(source string) (string, error) {
	s := newScanner(source)

	var replacements []*replacement
	pendingTag := tagNone

	for {
		if err := s.next(); err != nil {
			return "", fmt.Errorf("scanner next: %w", err)
		}

		switch s.kind {
		case tokenEOF:
			return applyReplacements(source, replacements), nil
		case tokenTemplate:
			if pendingTag != tagNone {
				template := &taggedTemplate{isCss: pendingTag == tagCss}
				template.addQuasi(s.start+len("`"), s.end-len("`"))
				replacements = append(replacements, template.replacements(source)...)
			}
		case tokenTemplateHead:
			if pendingTag != tagNone {
				template := &taggedTemplate{isCss: pendingTag == tagCss}
				template.addQuasi(s.start+len("`"), s.end-len("${"))
				s.attachTemplate(template)
			}
		case tokenTemplateMiddle:
			if s.template != nil {
				s.template.addQuasi(s.start+len("}"), s.end-len("${"))
			}
		case tokenTemplateTail:
			if s.template != nil {
				s.template.addQuasi(s.start+len("}"), s.end-len("`"))
				replacements = append(replacements, s.template.replacements(source)...)
			}
		default:
			// Any other token matters only as the tag before a template, below.
		}

		pendingTag = tagNone
		if s.kind == tokenName && !s.memberName {
			switch source[s.start:s.end] {
			case "css":
				pendingTag = tagCss
			case "html":
				pendingTag = tagHtml
			}
		}
	}
}
