package lit_minifier

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	altshiftErrors "github.com/altshiftab/utils_go/pkg/errors"
)

var (
	ErrUnterminatedComment  = errors.New("unterminated comment")
	ErrUnterminatedString   = errors.New("unterminated string literal")
	ErrUnterminatedTemplate = errors.New("unterminated template literal")
	ErrUnterminatedRegExp   = errors.New("unterminated regular expression literal")
)

type tokenKind int

const (
	// tokenNone is the kind of the token before the first.
	tokenNone tokenKind = iota
	tokenEOF
	// tokenName is an identifier or a keyword.
	tokenName
	tokenPrivateName
	tokenNumber
	tokenString
	tokenRegExp
	// tokenTemplate is a template literal without substitutions: `...`.
	tokenTemplate
	// tokenTemplateHead is the start of a template literal up to its first substitution: `...${.
	tokenTemplateHead
	// tokenTemplateMiddle is the text between two substitutions: }...${.
	tokenTemplateMiddle
	// tokenTemplateTail is the text after the last substitution: }...`.
	tokenTemplateTail
	tokenPunctuator
)

// contextKind says what an open bracket encloses, after the token contexts of acorn's tokenizer.
// Whether a slash begins a regular expression or divides depends on whether an expression may
// start where it stands, which one token of look-behind cannot always tell: a closing parenthesis
// ends an expression unless it closes the head of an if, for, while or with statement, and a
// closing brace ends a statement unless it closes an object literal or the body of a function or
// class expression.
type contextKind int

const (
	contextBlock contextKind = iota
	contextObject
	contextSubstitution
	contextStatementParens
	contextParens
	contextFunctionDeclaration
	contextFunctionExpression
)

func (kind contextKind) isExpression() bool {
	return kind == contextObject || kind == contextParens || kind == contextFunctionExpression
}

func (kind contextKind) isFunction() bool {
	return kind == contextFunctionDeclaration || kind == contextFunctionExpression
}

const (
	keywordAwait  = "await"
	keywordReturn = "return"
)

// isKeywordBeforeExpression reports whether an expression may start right after the keyword, so
// that a slash there begins a regular expression. yield and await count as keywords, as they are
// in modules and in the functions where they mean something.
func isKeywordBeforeExpression(name string) bool {
	switch name {
	case keywordAwait, "case", "default", "delete", "do", "else", "extends", "in", "instanceof",
		"new", keywordReturn, "throw", "typeof", "void", "yield":
		return true
	default:
		return false
	}
}

func isReservedWord(name string) bool {
	switch name {
	case "break", "case", "catch", "class", "const", "continue", "debugger", "default", "delete",
		"do", "else", "enum", "export", "extends", "false", "finally", "for", "function", "if",
		"import", "in", "instanceof", "new", "null", keywordReturn, "super", "switch", "this", "throw",
		"true", "try", "typeof", "var", "void", "while", "with":
		return true
	default:
		return false
	}
}

func isASCIIIdentifierStart(character byte) bool {
	return 'a' <= character && character <= 'z' || 'A' <= character && character <= 'Z' ||
		character == '$' || character == '_' || character == '\\'
}

func isASCIIIdentifierPart(character byte) bool {
	return isASCIIIdentifierStart(character) || isDigit(character)
}

func isDigit(character byte) bool {
	return '0' <= character && character <= '9'
}

func isLineTerminatorRune(r rune) bool {
	return r == '\u2028' || r == '\u2029'
}

func isWhitespaceRune(r rune) bool {
	return r == '\uFEFF' || unicode.Is(unicode.Zs, r)
}

// scanner splits JavaScript into tokens, as far as finding the tagged template literals in it
// requires: it tells regular expressions from division, and template literals and their
// substitutions from the code around them, but reads no further into the grammar. Anything that is
// not whitespace or a line terminator is taken for part of an identifier outside of ASCII, and any
// other ASCII character for a punctuator; what is not JavaScript is left for esbuild to report.
type scanner struct {
	source   string
	position int

	// kind, start and end are the current token: its kind and the bytes of the source it spans.
	kind  tokenKind
	start int
	end   int
	// template is the tagged template a template middle or tail continues, if it is one.
	template *taggedTemplate
	// memberName is set when the current token is a name that follows a dot, and so is a property
	// name rather than a keyword or a variable.
	memberName bool
	// lineBreakBefore is set when a line terminator separates the current token from the previous.
	lineBreakBefore bool

	contexts []contextKind
	// templates has an entry for each substitution context, the tagged template it is in.
	templates []*taggedTemplate
	// templateStarts has an entry for each substitution context, where its template begins.
	templateStarts []int
	// exprAllowed is whether an expression may start after the current token.
	exprAllowed bool

	previousKind tokenKind
	previousText string
	// previousName is the previous token's text when it was a name and not a property name.
	previousName string
}

func newScanner(source string) *scanner {
	return &scanner{
		source:      source,
		contexts:    []contextKind{contextBlock},
		exprAllowed: true,
	}
}

func (s *scanner) syntaxError(err error, position int) error {
	line := 1 + strings.Count(s.source[:position], "\n")
	lineStart := strings.LastIndexByte(s.source[:position], '\n') + 1
	column := 1 + utf8.RuneCountInString(s.source[lineStart:position])
	return altshiftErrors.NewWithTrace(
		fmt.Errorf("%w: %w at line %d, column %d", altshiftErrors.ErrParseError, err, line, column),
	)
}

// attachTemplate records the tagged template whose head is the current token, so that its middles
// and tail carry it.
func (s *scanner) attachTemplate(template *taggedTemplate) {
	s.templates[len(s.templates)-1] = template
}

func (s *scanner) current() contextKind {
	return s.contexts[len(s.contexts)-1]
}

func (s *scanner) push(kind contextKind) {
	s.contexts = append(s.contexts, kind)
}

func (s *scanner) pop() contextKind {
	kind := s.current()
	s.contexts = s.contexts[:len(s.contexts)-1]
	return kind
}

// next advances to the next token, which is tokenEOF at the end of the source.
func (s *scanner) next() error {
	s.lineBreakBefore = false
	if err := s.skipTrivia(); err != nil {
		return err
	}

	s.start = s.position
	s.template = nil
	s.memberName = false

	if s.position == len(s.source) {
		if len(s.templateStarts) != 0 {
			return s.syntaxError(ErrUnterminatedTemplate, s.templateStarts[len(s.templateStarts)-1])
		}
		s.kind = tokenEOF
		s.end = s.position
		return nil
	}

	character := s.source[s.position]
	switch {
	case character == '`':
		s.position++
		hasSubstitution, err := s.scanTemplateSpan()
		if err != nil {
			return err
		}
		if hasSubstitution {
			s.kind = tokenTemplateHead
			s.push(contextSubstitution)
			s.templates = append(s.templates, nil)
			s.templateStarts = append(s.templateStarts, s.start)
		} else {
			s.kind = tokenTemplate
		}
	case character == '}' && s.current() == contextSubstitution:
		s.position++
		s.template = s.templates[len(s.templates)-1]
		hasSubstitution, err := s.scanTemplateSpan()
		if err != nil {
			return err
		}
		if hasSubstitution {
			s.kind = tokenTemplateMiddle
		} else {
			s.kind = tokenTemplateTail
			s.pop()
			s.templates = s.templates[:len(s.templates)-1]
			s.templateStarts = s.templateStarts[:len(s.templateStarts)-1]
		}
	case character == '"' || character == '\'':
		if err := s.scanString(character); err != nil {
			return err
		}
		s.kind = tokenString
	case isDigit(character) || character == '.' && s.position+1 < len(s.source) && isDigit(s.source[s.position+1]):
		s.scanNumber()
		s.kind = tokenNumber
	case character == '/' && s.exprAllowed:
		if err := s.scanRegExp(); err != nil {
			return err
		}
		s.kind = tokenRegExp
	case character == '#' && s.position+1 < len(s.source) && s.isIdentifierStartAt(s.position+1):
		s.position++
		s.scanName()
		s.kind = tokenPrivateName
	case character >= utf8.RuneSelf || isASCIIIdentifierStart(character):
		s.scanName()
		s.kind = tokenName
	default:
		s.scanPunctuator()
		s.kind = tokenPunctuator
	}
	s.end = s.position

	s.updateContext()
	return nil
}

func (s *scanner) isIdentifierStartAt(position int) bool {
	character := s.source[position]
	if character < utf8.RuneSelf {
		return isASCIIIdentifierStart(character)
	}
	r, _ := utf8.DecodeRuneInString(s.source[position:])
	return !isWhitespaceRune(r) && !isLineTerminatorRune(r)
}

func (s *scanner) skipTrivia() error {
	for s.position < len(s.source) {
		character := s.source[s.position]
		switch {
		case character == ' ' || character == '\t' || character == '\v' || character == '\f':
			s.position++
		case character == '\n' || character == '\r':
			s.lineBreakBefore = true
			s.position++
		case character == '/' && s.position+1 < len(s.source) && s.source[s.position+1] == '/',
			character == '#' && s.position == 0 && strings.HasPrefix(s.source, "#!"):
			s.skipToLineTerminator()
		case character == '/' && s.position+1 < len(s.source) && s.source[s.position+1] == '*':
			length := strings.Index(s.source[s.position+2:], "*/")
			if length == -1 {
				return s.syntaxError(ErrUnterminatedComment, s.position)
			}
			comment := s.source[s.position+2 : s.position+2+length]
			if strings.ContainsAny(comment, "\n\r\u2028\u2029") {
				s.lineBreakBefore = true
			}
			s.position += 2 + length + 2
		case character >= utf8.RuneSelf:
			r, size := utf8.DecodeRuneInString(s.source[s.position:])
			switch {
			case isLineTerminatorRune(r):
				s.lineBreakBefore = true
			case !isWhitespaceRune(r):
				return nil
			}
			s.position += size
		default:
			return nil
		}
	}
	return nil
}

// skipToLineTerminator skips a single-line comment, leaving the line terminator that ends it.
func (s *scanner) skipToLineTerminator() {
	for s.position < len(s.source) {
		switch s.source[s.position] {
		case '\n', '\r':
			return
		case 0xE2:
			if r, _ := utf8.DecodeRuneInString(s.source[s.position:]); isLineTerminatorRune(r) {
				return
			}
		}
		s.position++
	}
}

// scanTemplateSpan scans template characters from just after a backtick or the brace closing a
// substitution, through the backtick or the "${" that ends them, and reports which of the two it
// was.
func (s *scanner) scanTemplateSpan() (bool, error) {
	for i := s.position; i < len(s.source); i++ {
		switch s.source[i] {
		case '`':
			s.position = i + 1
			return false, nil
		case '\\':
			i++
		case '$':
			if i+1 < len(s.source) && s.source[i+1] == '{' {
				s.position = i + 2
				return true, nil
			}
		}
	}
	return false, s.syntaxError(ErrUnterminatedTemplate, s.start)
}

// scanString scans a string literal. Line separators are allowed in it, as they are since ES2019.
func (s *scanner) scanString(quote byte) error {
	for i := s.position + 1; i < len(s.source); i++ {
		switch s.source[i] {
		case quote:
			s.position = i + 1
			return nil
		case '\\':
			// A line continuation may end in a CRLF pair.
			if strings.HasPrefix(s.source[i+1:], "\r\n") {
				i++
			}
			i++
		case '\n', '\r':
			return s.syntaxError(ErrUnterminatedString, s.position)
		}
	}
	return s.syntaxError(ErrUnterminatedString, s.position)
}

// scanRegExp scans a regular expression literal and its flags. A slash inside a character class
// does not end it.
func (s *scanner) scanRegExp() error {
	inClass := false
	for i := s.position + 1; i < len(s.source); i++ {
		character := s.source[i]
		switch character {
		case '\\':
			i++
			if i < len(s.source) && (s.source[i] == '\n' || s.source[i] == '\r') {
				return s.syntaxError(ErrUnterminatedRegExp, s.position)
			}
		case '\n', '\r':
			return s.syntaxError(ErrUnterminatedRegExp, s.position)
		case 0xE2:
			if r, _ := utf8.DecodeRuneInString(s.source[i:]); isLineTerminatorRune(r) {
				return s.syntaxError(ErrUnterminatedRegExp, s.position)
			}
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '/':
			if inClass {
				continue
			}
			i++
			for i < len(s.source) && isASCIIIdentifierPart(s.source[i]) {
				i++
			}
			s.position = i
			return nil
		}
	}
	return s.syntaxError(ErrUnterminatedRegExp, s.position)
}

// scanNumber scans a numeric literal: decimal, with an optional fraction and exponent, hexadecimal,
// octal or binary, with numeric separators and a BigInt suffix. Identifier characters directly
// after it are taken along, as nothing valid can follow a number there.
func (s *scanner) scanNumber() {
	digits := func() {
		for s.position < len(s.source) && (isDigit(s.source[s.position]) || s.source[s.position] == '_') {
			s.position++
		}
	}

	rest := s.source[s.position:]
	if len(rest) > 1 && rest[0] == '0' && strings.ContainsRune("xXoObB", rune(rest[1])) {
		s.position += 2
	} else {
		digits()
		if s.position < len(s.source) && s.source[s.position] == '.' {
			s.position++
			digits()
		}
		if s.position < len(s.source) && (s.source[s.position] == 'e' || s.source[s.position] == 'E') {
			s.position++
			if s.position < len(s.source) && (s.source[s.position] == '+' || s.source[s.position] == '-') {
				s.position++
			}
		}
	}
	for s.position < len(s.source) && isASCIIIdentifierPart(s.source[s.position]) {
		s.position++
	}
}

// scanName scans an identifier name. A unicode escape sequence is taken whole, braces included.
func (s *scanner) scanName() {
	for s.position < len(s.source) {
		character := s.source[s.position]
		if character >= utf8.RuneSelf {
			r, size := utf8.DecodeRuneInString(s.source[s.position:])
			if isWhitespaceRune(r) || isLineTerminatorRune(r) {
				return
			}
			s.position += size
			continue
		}
		if !isASCIIIdentifierPart(character) {
			return
		}
		if character == '\\' && strings.HasPrefix(s.source[s.position+1:], "u{") {
			if end := strings.IndexByte(s.source[s.position:], '}'); end != -1 {
				s.position += end
			}
		}
		s.position++
	}
}

// scanPunctuator scans a punctuator, as far as the context tracking needs one told from another:
// "...", "?.", "=>", "++" and "--" are taken whole, and every other character on its own.
func (s *scanner) scanPunctuator() {
	rest := s.source[s.position:]
	length := 1
	switch rest[0] {
	case '.':
		if strings.HasPrefix(rest, "...") {
			length = 3
		}
	case '?':
		// "?." followed by a digit is a conditional operator and a number: a?.5:1.
		if len(rest) > 1 && rest[1] == '.' && (len(rest) == 2 || !isDigit(rest[2])) {
			length = 2
		}
	case '=':
		if strings.HasPrefix(rest, "=>") {
			length = 2
		}
	case '+', '-':
		if len(rest) > 1 && rest[1] == rest[0] {
			length = 2
		}
	}
	s.position += length
}

func (s *scanner) previousPunctuator(text string) bool {
	return s.previousKind == tokenPunctuator && s.previousText == text
}

// previousIsIdentifier reports whether the previous token was a name other than a reserved word.
func (s *scanner) previousIsIdentifier() bool {
	return s.previousName != "" && !isReservedWord(s.previousName)
}

// previousIsBeforeExpression reports whether an expression may follow the previous token by its
// kind alone, before any context is taken into account.
func (s *scanner) previousIsBeforeExpression() bool {
	switch s.previousKind {
	case tokenPunctuator:
		switch s.previousText {
		case ")", "]", "}", ".", "?.", "++", "--":
			return false
		default:
			return true
		}
	case tokenTemplateHead, tokenTemplateMiddle:
		return true
	case tokenName:
		return isKeywordBeforeExpression(s.previousName)
	default:
		return false
	}
}

// braceIsBlock reports whether an opening brace begins a block or a function body, as opposed to
// an object literal.
func (s *scanner) braceIsBlock() bool {
	parent := s.current()
	switch {
	case parent.isFunction():
		return true
	case s.previousPunctuator(":") && (parent == contextBlock || parent == contextObject):
		// A label or a case clause, or an object literal's property value.
		return parent == contextBlock
	case s.previousName == keywordReturn || s.previousName == "yield" || s.previousName == keywordAwait ||
		s.previousIsIdentifier() && s.exprAllowed:
		// Automatic semicolon insertion makes a brace on the next line a block.
		return s.lineBreakBefore
	case s.previousKind == tokenNone, s.previousName == "else", s.previousName == "do",
		s.previousPunctuator(";"), s.previousPunctuator(")"), s.previousPunctuator("=>"):
		return true
	case s.previousPunctuator("{"):
		return parent == contextBlock
	case s.previousName == "var", s.previousName == "const", s.previousIsIdentifier():
		return false
	default:
		return !s.exprAllowed
	}
}

// functionContext tells a function or class in statement position, a declaration, from one in
// expression position.
func (s *scanner) functionContext() contextKind {
	switch {
	case !s.previousIsBeforeExpression(), s.previousName == "else":
		return contextFunctionDeclaration
	case s.previousPunctuator(";") && s.current() != contextStatementParens:
		// After a statement; only in the head of a for statement does an expression follow one.
		return contextFunctionDeclaration
	case s.previousName == keywordReturn && s.lineBreakBefore:
		// Automatic semicolon insertion ends the return statement.
		return contextFunctionDeclaration
	case (s.previousPunctuator(":") || s.previousPunctuator("{")) && s.current() == contextBlock:
		// After a label or a case clause, or at the start of a block.
		return contextFunctionDeclaration
	default:
		return contextFunctionExpression
	}
}

// closeBracket pops the context a closing parenthesis or brace ends. A parenthesis never closes a
// substitution: in a mismatched one, the brace that ends the substitution has to find it.
func (s *scanner) closeBracket(isParenthesis bool) {
	if len(s.contexts) == 1 {
		s.exprAllowed = true
		return
	}
	if isParenthesis && s.current() == contextSubstitution {
		s.exprAllowed = false
		return
	}

	closed := s.pop()
	if closed == contextBlock && s.current().isFunction() {
		closed = s.pop()
	}
	s.exprAllowed = !closed.isExpression()
}

// updateContext updates the contexts and whether an expression may follow, for the current token.
func (s *scanner) updateContext() {
	text := s.source[s.start:s.end]

	switch s.kind {
	case tokenName:
		if s.previousPunctuator(".") || s.previousPunctuator("?.") {
			s.memberName = true
			s.exprAllowed = false
			break
		}
		switch text {
		case "function", "class":
			s.push(s.functionContext())
			s.exprAllowed = false
		case "of":
			// After a binding, as in for (x of y), "of" is an operator; at the start of an
			// expression, it is a variable.
			s.exprAllowed = !s.exprAllowed
		default:
			s.exprAllowed = isKeywordBeforeExpression(text)
		}
	case tokenPunctuator:
		switch text {
		case "(":
			switch s.previousName {
			case "if", "for", "while", "with":
				s.push(contextStatementParens)
			default:
				s.push(contextParens)
			}
			s.exprAllowed = true
		case ")":
			s.closeBracket(true)
		case "}":
			s.closeBracket(false)
		case "{":
			if s.braceIsBlock() {
				s.push(contextBlock)
			} else {
				s.push(contextObject)
			}
			s.exprAllowed = true
		case ":":
			// A function or class context still open at a colon was a property name.
			if s.current().isFunction() {
				s.pop()
			}
			s.exprAllowed = true
		case "]", ".", "?.":
			s.exprAllowed = false
		case "++", "--":
			// Whether prefix or postfix, they leave the question open.
		default:
			s.exprAllowed = true
		}
	case tokenTemplateHead, tokenTemplateMiddle:
		s.exprAllowed = true
	default:
		s.exprAllowed = false
	}

	s.previousKind = s.kind
	s.previousText = text
	switch {
	case s.kind != tokenName || s.memberName:
		s.previousName = ""
	case text == keywordAwait && s.previousName == "for":
		// The parentheses of for await are a statement's head just as those of for are.
	default:
		s.previousName = text
	}
}
