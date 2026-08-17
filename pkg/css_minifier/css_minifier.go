// Package css_minifier provides a scoped CSS minifier replicating the observable
// behavior of postcss-minify: comments are removed (except /*! ones), selectors
// are tightened around combinators and commas, declaration values collapse
// whitespace runs to single spaces (so calc() expressions stay intact) and are
// tightened around commas, colons, slashes and parenthesis edges, and trailing
// semicolons are dropped. Strings are always preserved verbatim.
package css_minifier

import (
	"regexp"
	"strings"
)

// The template expression placeholder used by the lit minifier also starts with
// "@" but is not an at-rule.
const templateExpressionPlaceholder = "@TEMPLATE_EXPRESSION"

var (
	whitespaceRegexp        = regexp.MustCompile(`\s+`)
	selectorSeparatorRegexp = regexp.MustCompile(`\s*([>~+,])\s*`)
	valueSeparatorRegexp    = regexp.MustCompile(`\s*([,:/])\s*`)
	openParenthesisRegexp   = regexp.MustCompile(`\(\s+`)
	closeParenthesisRegexp  = regexp.MustCompile(`\s+\)`)
	importantSuffixRegexp   = regexp.MustCompile(`(?i)\s*!\s*important$`)
	atRuleNameRegexp        = regexp.MustCompile(`^@[-\w]+`)
)

type segmentPart struct {
	text     string
	isString bool
}

// segment splits text into string and non-string parts so transformations never
// touch string contents. Backslash escapes inside strings are honored.
func segment(text string) []*segmentPart {
	var parts []*segmentPart
	var current strings.Builder
	var quote byte

	for i := 0; i < len(text); i++ {
		character := text[i]

		if quote != 0 {
			current.WriteByte(character)
			if character == '\\' && i+1 < len(text) {
				i++
				current.WriteByte(text[i])
			} else if character == quote {
				parts = append(parts, &segmentPart{text: current.String(), isString: true})
				current.Reset()
				quote = 0
			}
			continue
		}

		if character == '"' || character == '\'' {
			if current.Len() != 0 {
				parts = append(parts, &segmentPart{text: current.String()})
				current.Reset()
			}
			quote = character
			current.WriteByte(character)
			continue
		}

		current.WriteByte(character)
	}

	if current.Len() != 0 {
		parts = append(parts, &segmentPart{text: current.String(), isString: quote != 0})
	}

	return parts
}

func transformOutsideStrings(text string, transform func(string) string) string {
	var output strings.Builder
	for _, part := range segment(text) {
		if part.isString {
			output.WriteString(part.text)
		} else {
			output.WriteString(transform(part.text))
		}
	}
	return output.String()
}

func stripComments(css string) string {
	var output strings.Builder
	var quote byte

	for i := 0; i < len(css); i++ {
		character := css[i]

		if quote != 0 {
			output.WriteByte(character)
			if character == '\\' && i+1 < len(css) {
				i++
				output.WriteByte(css[i])
			} else if character == quote {
				quote = 0
			}
			continue
		}

		if character == '"' || character == '\'' {
			quote = character
			output.WriteByte(character)
			continue
		}

		if character == '/' && i+1 < len(css) && css[i+1] == '*' {
			end := strings.Index(css[i+2:], "*/")
			commentEnd := len(css)
			if end != -1 {
				commentEnd = i + 2 + end + 2
			}
			if i+2 < len(css) && css[i+2] == '!' {
				output.WriteString(css[i:commentEnd])
			}
			i = commentEnd - 1
			continue
		}

		output.WriteByte(character)
	}

	return output.String()
}

func minifySelector(selector string) string {
	return strings.TrimSpace(transformOutsideStrings(selector, func(part string) string {
		part = whitespaceRegexp.ReplaceAllString(part, " ")
		return selectorSeparatorRegexp.ReplaceAllString(part, "$1")
	}))
}

func minifyValue(value string) string {
	return strings.TrimSpace(transformOutsideStrings(value, func(part string) string {
		part = whitespaceRegexp.ReplaceAllString(part, " ")
		part = valueSeparatorRegexp.ReplaceAllString(part, "$1")
		part = openParenthesisRegexp.ReplaceAllString(part, "(")
		return closeParenthesisRegexp.ReplaceAllString(part, ")")
	}))
}

func minifyDeclaration(declaration string) string {
	colonIndex := strings.IndexByte(declaration, ':')
	if colonIndex == -1 {
		return minifyValue(declaration)
	}

	property := strings.TrimSpace(declaration[:colonIndex])
	value := importantSuffixRegexp.ReplaceAllString(minifyValue(declaration[colonIndex+1:]), " !important")
	return property + ":" + value
}

func minifyAtRulePrelude(prelude string) string {
	name := atRuleNameRegexp.FindString(prelude)
	if name == "" {
		return minifyValue(prelude)
	}

	parameters := minifyValue(prelude[len(name):])
	if parameters == "" {
		return name
	}
	return name + " " + parameters
}

type statementKind int

const (
	statementKindDeclaration statementKind = iota
	statementKindAtRule
	statementKindBlock
	statementKindComment
)

type statement struct {
	kind statementKind
	text string
}

// minifyStatements splits the text into top-level statements: kept comments pass
// through, blocks recurse, and everything else is an at-rule or a declaration.
// Declarations are joined with semicolons (none after the last); standalone
// at-rules always keep their semicolon; blocks and comments need no separator.
func minifyStatements(css string) string {
	var statements []*statement
	var current strings.Builder
	var quote byte
	parenthesisDepth := 0

	flushStatement := func() {
		text := strings.TrimSpace(current.String())
		current.Reset()
		if text == "" {
			return
		}
		if strings.HasPrefix(text, "@") && !strings.HasPrefix(text, templateExpressionPlaceholder) {
			statements = append(statements, &statement{kind: statementKindAtRule, text: minifyAtRulePrelude(text) + ";"})
		} else {
			statements = append(statements, &statement{kind: statementKindDeclaration, text: minifyDeclaration(text)})
		}
	}

	for i := 0; i < len(css); i++ {
		character := css[i]

		if quote != 0 {
			current.WriteByte(character)
			if character == '\\' && i+1 < len(css) {
				i++
				current.WriteByte(css[i])
			} else if character == quote {
				quote = 0
			}
			continue
		}

		switch character {
		case '"', '\'':
			quote = character
			current.WriteByte(character)
		case '(':
			parenthesisDepth++
			current.WriteByte(character)
		case ')':
			parenthesisDepth--
			current.WriteByte(character)
		case '/':
			// Only kept (/*!) comments remain after stripComments.
			if i+1 < len(css) && css[i+1] == '*' {
				end := strings.Index(css[i+2:], "*/")
				commentEnd := len(css)
				if end != -1 {
					commentEnd = i + 2 + end + 2
				}
				flushStatement()
				statements = append(statements, &statement{kind: statementKindComment, text: css[i:commentEnd]})
				i = commentEnd - 1
			} else {
				current.WriteByte(character)
			}
		case ';':
			if parenthesisDepth == 0 {
				flushStatement()
			} else {
				current.WriteByte(character)
			}
		case '{':
			depth := 1
			var innerQuote byte
			j := i + 1
			for ; j < len(css) && depth > 0; j++ {
				innerCharacter := css[j]
				if innerQuote != 0 {
					switch innerCharacter {
					case '\\':
						j++
					case innerQuote:
						innerQuote = 0
					}
					continue
				}
				switch innerCharacter {
				case '"', '\'':
					innerQuote = innerCharacter
				case '{':
					depth++
				case '}':
					depth--
				}
			}

			prelude := strings.TrimSpace(current.String())
			inner := css[i+1 : j-1]
			current.Reset()

			var minifiedPrelude string
			if strings.HasPrefix(prelude, "@") {
				minifiedPrelude = minifyAtRulePrelude(prelude)
			} else {
				minifiedPrelude = minifySelector(prelude)
			}
			statements = append(statements, &statement{
				kind: statementKindBlock,
				text: minifiedPrelude + "{" + minifyStatements(inner) + "}",
			})
			i = j - 1
		default:
			current.WriteByte(character)
		}
	}

	flushStatement()

	var output strings.Builder
	for index, currentStatement := range statements {
		output.WriteString(currentStatement.text)
		if currentStatement.kind == statementKindDeclaration && index < len(statements)-1 {
			output.WriteString(";")
		}
	}
	return output.String()
}

// Minify minifies the provided CSS.
func Minify(css string) string {
	return strings.TrimSpace(minifyStatements(stripComments(css)))
}
