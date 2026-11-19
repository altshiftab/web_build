// Package lit_minifier minifies the contents of css and html tagged template
// literals in JavaScript source, mirroring @altshiftab/minify-lit. The source is
// scanned rather than parsed; template contents are minified and spliced back,
// leaving all other code byte-identical.
package lit_minifier

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

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

func isHtmlWhitespace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\n' || character == '\f' || character == '\r'
}

// collapseWhitespace replaces each run of HTML whitespace with a single space. Text with nothing to
// collapse is returned as it is.
func collapseWhitespace(text string) string {
	var output strings.Builder
	copied := 0
	for i := 0; i < len(text); {
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

	minified := strings.TrimLeftFunc(output.String(), unicode.IsSpace)
	if openElement == "" {
		minified = strings.TrimRightFunc(minified, unicode.IsSpace)
	}
	return minified
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
