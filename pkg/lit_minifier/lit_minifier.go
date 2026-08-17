// Package lit_minifier minifies the contents of css and html tagged template
// literals in JavaScript source, mirroring @altshiftab/minify-lit. The source is
// tokenized rather than fully parsed; template contents are minified and spliced
// back, leaving all other code byte-identical.
package lit_minifier

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"

	altshiftErrors "github.com/altshiftab/utils_go/pkg/errors"
	"github.com/altshiftab/web_build/pkg/css_minifier"
	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/js"
)

const placeholder = "@TEMPLATE_EXPRESSION"

var (
	quotedSpanRegexp = regexp.MustCompile("\"[^\"]*\"|'[^']*'|`[^`]*`")
	whitespaceRegexp = regexp.MustCompile(`\s+`)
	newlineReplacer  = strings.NewReplacer("\n", "", "\r", "")
)

type token struct {
	tokenType js.TokenType
	text      string
}

func isInsignificant(tokenType js.TokenType) bool {
	switch tokenType {
	case js.WhitespaceToken, js.LineTerminatorToken, js.CommentToken, js.CommentLineTerminatorToken:
		return true
	default:
		return false
	}
}

// allowsDivision reports whether a slash after the given token is division
// rather than the start of a regular expression literal. A slash after a close
// brace is treated as a regular expression (statement position), which matches
// how real-world code uses it.
func allowsDivision(tokenType js.TokenType) bool {
	if js.IsIdentifier(tokenType) || js.IsNumeric(tokenType) {
		return true
	}
	switch tokenType {
	case js.ThisToken, js.TrueToken, js.FalseToken, js.NullToken,
		js.StringToken, js.RegExpToken, js.TemplateToken, js.TemplateEndToken,
		js.CloseParenToken, js.CloseBracketToken, js.IncrToken, js.DecrToken:
		return true
	default:
		return false
	}
}

func tokenize(source string) ([]*token, error) {
	lexer := js.NewLexer(parse.NewInputString(source))

	var tokens []*token
	var previousSignificant js.TokenType

	for {
		tokenType, text := lexer.Next()
		if tokenType == js.ErrorToken {
			if err := lexer.Err(); !errors.Is(err, io.EOF) {
				return nil, altshiftErrors.NewWithTrace(fmt.Errorf("js lexer: %w", err))
			}
			break
		}

		// The lexer leaves the division-or-regexp decision to its caller.
		if (tokenType == js.DivToken || tokenType == js.DivEqToken) && !allowsDivision(previousSignificant) {
			tokenType, text = lexer.RegExp()
			if tokenType == js.ErrorToken {
				if err := lexer.Err(); err != nil {
					return nil, altshiftErrors.NewWithTrace(fmt.Errorf("js lexer regexp: %w", err))
				}
				break
			}
		}

		tokens = append(tokens, &token{tokenType: tokenType, text: string(text)})
		if !isInsignificant(tokenType) {
			previousSignificant = tokenType
		}
	}

	return tokens, nil
}

func minifyCssQuasis(quasis []string) []string {
	parts := strings.Split(css_minifier.Minify(strings.Join(quasis, placeholder)), placeholder)
	minified := make([]string, len(quasis))
	for i := range quasis {
		if i < len(parts) {
			minified[i] = parts[i]
		}
	}
	return minified
}

func minifyHtmlQuasis(quasis []string) []string {
	minified := make([]string, len(quasis))
	for i, quasi := range quasis {
		minifiedQuasi := quotedSpanRegexp.ReplaceAllStringFunc(quasi, newlineReplacer.Replace)
		minifiedQuasi = whitespaceRegexp.ReplaceAllString(minifiedQuasi, " ")

		if i == 0 {
			minifiedQuasi = strings.TrimLeftFunc(minifiedQuasi, unicode.IsSpace)
		}
		if i == len(quasis)-1 {
			minifiedQuasi = strings.TrimRightFunc(minifiedQuasi, unicode.IsSpace)
		}

		minified[i] = minifiedQuasi
	}
	return minified
}

func minifyQuasis(quasis []string, isCss bool) []string {
	if isCss {
		return minifyCssQuasis(quasis)
	}
	return minifyHtmlQuasis(quasis)
}

// processTaggedTemplate consumes the template starting at the given index,
// writes its minified form, and returns the index of the first token after it.
// Expression tokens are transformed recursively so nested tagged templates are
// minified too.
func processTaggedTemplate(tokens []*token, start int, isCss bool, output *strings.Builder) int {
	first := tokens[start]

	if first.tokenType == js.TemplateToken {
		// The raw text is `...`.
		quasis := minifyQuasis([]string{first.text[1 : len(first.text)-1]}, isCss)
		output.WriteString("`" + quasis[0] + "`")
		return start + 1
	}

	// The raw text of a template start is `...${, of a middle }...${, of an end }...`.
	quasis := []string{first.text[1 : len(first.text)-2]}
	var expressions [][]*token
	var currentExpression []*token

	i := start + 1
	depth := 0
	for i < len(tokens) {
		currentToken := tokens[i]
		switch currentToken.tokenType {
		case js.TemplateStartToken:
			depth++
			currentExpression = append(currentExpression, currentToken)
		case js.TemplateMiddleToken:
			if depth == 0 {
				quasis = append(quasis, currentToken.text[1:len(currentToken.text)-2])
				expressions = append(expressions, currentExpression)
				currentExpression = nil
			} else {
				currentExpression = append(currentExpression, currentToken)
			}
		case js.TemplateEndToken:
			if depth == 0 {
				quasis = append(quasis, currentToken.text[1:len(currentToken.text)-1])
				expressions = append(expressions, currentExpression)

				minified := minifyQuasis(quasis, isCss)
				output.WriteString("`" + minified[0] + "${")
				for expressionIndex, expression := range expressions {
					transformTokens(expression, output)
					if expressionIndex < len(expressions)-1 {
						output.WriteString("}" + minified[expressionIndex+1] + "${")
					}
				}
				output.WriteString("}" + minified[len(minified)-1] + "`")
				return i + 1
			}
			depth--
			currentExpression = append(currentExpression, currentToken)
		default:
			currentExpression = append(currentExpression, currentToken)
		}
		i++
	}

	// An unterminated template; emit the collected tokens verbatim.
	output.WriteString(first.text)
	for _, expressionToken := range currentExpression {
		output.WriteString(expressionToken.text)
	}
	return i
}

func transformTokens(tokens []*token, output *strings.Builder) {
	var previousSignificant js.TokenType

	for i := 0; i < len(tokens); {
		currentToken := tokens[i]

		if currentToken.tokenType == js.IdentifierToken &&
			(currentToken.text == "css" || currentToken.text == "html") &&
			previousSignificant != js.DotToken {
			templateIndex := i + 1
			for templateIndex < len(tokens) && isInsignificant(tokens[templateIndex].tokenType) {
				templateIndex++
			}

			if templateIndex < len(tokens) &&
				(tokens[templateIndex].tokenType == js.TemplateToken ||
					tokens[templateIndex].tokenType == js.TemplateStartToken) {
				for _, skippedToken := range tokens[i:templateIndex] {
					output.WriteString(skippedToken.text)
				}
				i = processTaggedTemplate(tokens, templateIndex, currentToken.text == "css", output)
				previousSignificant = js.TemplateEndToken
				continue
			}
		}

		output.WriteString(currentToken.text)
		if !isInsignificant(currentToken.tokenType) {
			previousSignificant = currentToken.tokenType
		}
		i++
	}
}

// Minify minifies css and html tagged template literals in the provided source.
func Minify(source string) (string, error) {
	tokens, err := tokenize(source)
	if err != nil {
		return "", fmt.Errorf("tokenize: %w", err)
	}

	var output strings.Builder
	transformTokens(tokens, &output)
	return output.String(), nil
}
