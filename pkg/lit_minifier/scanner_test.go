package lit_minifier

import (
	"errors"
	"slices"
	"strings"
	"testing"

	altshiftErrors "github.com/altshiftab/utils_go/pkg/errors"
)

// scanAll returns the text of every token of the given kinds in the source.
func scanAll(t *testing.T, source string, kinds ...tokenKind) []string {
	t.Helper()
	s := newScanner(source)
	var texts []string
	for {
		if err := s.next(); err != nil {
			t.Fatalf("scanner next: %v", err)
		}
		if s.kind == tokenEOF {
			return texts
		}
		if slices.Contains(kinds, s.kind) {
			texts = append(texts, source[s.start:s.end])
		}
	}
}

func TestScannerRegExps(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		source   string
		expected []string
	}{
		{name: "division after a name", source: "a / b / c"},
		{name: "division after a number", source: "1 / 2 / 3; .5 / 2 / x; 1_000n / 2n / y; 0xFF / 2 / z; 1e-3 / 2 / w"},
		{name: "division after a string", source: `"a" / 2 / b`},
		{name: "division after a parenthesised expression", source: "(a + b) / 42 / i"},
		{name: "division after a member call", source: "a.b() / 2 / c"},
		{name: "division after a bracket", source: "a[0] / 2 / b"},
		{name: "division after this", source: "this / 2 / x; super.x / 2 / y; null / 2 / z"},
		{name: "division after a private name", source: "this.#n / 2 / 3"},
		{name: "division after a keyword used as a property name", source: "x.default / 2 / y; x?.return / 2 / z; x.in / 2 / w"},
		{name: "division after a postfix increment", source: "a++ / 2 / b; c-- / 2 / d"},
		{name: "division assignment", source: "a /= 2; b /= c / d"},
		{name: "division after a template literal", source: "`a` / 2 / b; `${a}` / 2 / c"},
		{name: "division after a function expression", source: "var foo = function foo() {} / 42 / i"},
		{name: "division after a class expression", source: "x = class {} / 2 / y"},
		{name: "division after an object literal", source: "({ valueOf: () => 100 } / 42 / i)"},
		{name: "division after a nested object literal", source: "x = {a: {b: 1} / 2 / c}"},
		{name: "division after of as a name", source: "const of = 4; x = of / 2 / y"},
		{name: "division across a comment", source: "x /* c */ / 2 / y"},
		{name: "division across a line break", source: "a\n/b/g"},
		{name: "regular expression at the start", source: "/a/.test(b)", expected: []string{"/a/"}},
		{name: "regular expression after if parentheses", source: "if (a + b) /42/i.test(c)", expected: []string{"/42/i"}},
		{
			name:     "regular expression after loop parentheses",
			source:   "while (x) /a/.exec(y); for (;;) /b/g.test(z); for(let f of o.split(\"/\"))/^r\\d+$/.test(f)",
			expected: []string{"/a/", "/b/g", `/^r\d+$/`},
		},
		{name: "regular expression after a function declaration", source: "function foo() {} /42/i", expected: []string{"/42/i"}},
		{name: "regular expression after a block", source: "{ console.log('a'); } /42/i", expected: []string{"/42/i"}},
		{name: "regular expression after a class declaration", source: "class A { m() {} } /re/.test(s)", expected: []string{"/re/"}},
		{name: "regular expression after an arrow function body", source: "const f = () => {}\n/a/.test(b)", expected: []string{"/a/"}},
		{
			name:     "regular expression after keywords",
			source:   "return /a/; typeof /b/; void /c/; delete /d/.x; throw /e/; x in /f/; x instanceof /g/.constructor",
			expected: []string{"/a/", "/b/", "/c/", "/d/", "/e/", "/f/", "/g/"},
		},
		{name: "regular expression after else and do", source: "if (a) b(); else /c/.test(d); do /e/.test(f); while (g)", expected: []string{"/c/", "/e/"}},
		{name: "regular expression after await and yield", source: "async function f() { await /a/.exec(b); } function* g() { yield /c/; }", expected: []string{"/a/", "/c/"}},
		{name: "regular expression after of", source: "for (const m of /a/g.exec(s)) {}", expected: []string{"/a/g"}},
		{name: "regular expression after for await parentheses", source: "for await (const x of y) /a/.test(x); await (b) / 2 / c", expected: []string{"/a/"}},
		{name: "regular expression after an arrow", source: "x => /a/.test(x)", expected: []string{"/a/"}},
		{
			name:     "regular expression after jump statements and a line break",
			source:   "for (;;) { if (a) break\n/b/.test(c); if (d) continue\n/e/.test(f); debugger\n/g/.test(h) }",
			expected: []string{"/b/", "/e/", "/g/"},
		},
		{name: "division after a break label", source: "a: for (;;) { break a; } x = b / 2 / c"},
		{
			name:     "regular expression after consecutive blocks",
			source:   "{} {} /a/.test(b); function f() {}\n{ }\n/c/.test(d)",
			expected: []string{"/a/", "/c/"},
		},
		{name: "regular expression after a block on the line after an identifier", source: "x = a\n{ b }\n/c/.test(d)", expected: []string{"/c/"}},
		{name: "division after a destructuring let", source: "let\n{a} = b / 2 / c"},
		{
			name:     "regular expression after export default declarations",
			source:   "export default class {}\n/a/.test(s); export default function f() {}\n/b/.test(s)",
			expected: []string{"/a/", "/b/"},
		},
		{name: "division after export default expressions", source: "export default {a: 1} / 2 / x; export default (function () {}) / 2 / y"},
		{
			name:     "regular expression after operators",
			source:   "a = /x/; b = c || /y/; d = !/z/.test(e); f = [/w/, /v/]; g(/u/); h = i ? /t/ : /s/; j ??= /r/",
			expected: []string{"/x/", "/y/", "/z/", "/w/", "/v/", "/u/", "/t/", "/s/", "/r/"},
		},
		{name: "regular expression after a case colon", source: "switch (x) { case 1: /a/.test(y); }", expected: []string{"/a/"}},
		{name: "regular expression as a property value", source: "({ a: /x/, b: c / d / e })", expected: []string{"/x/"}},
		{name: "regular expression in a template substitution", source: "`${/a/.source}`", expected: []string{"/a/"}},
		{name: "regular expression after a comment", source: "x = /* c */ /a/", expected: []string{"/a/"}},
		{name: "regular expression after return and a line break", source: "return\n/a/.test(b)", expected: []string{"/a/"}},
		{name: "regular expression as a default parameter", source: "function f(re = /a/) {}", expected: []string{"/a/"}},
		{name: "slash inside a character class", source: "x = /[/]/.test(a)", expected: []string{"/[/]/"}},
		{name: "escaped slash", source: `x = /a\/b/`, expected: []string{`/a\/b/`}},
		{name: "number sign, quotes and backticks inside", source: "x = /(^|#)'\"`/u", expected: []string{"/(^|#)'\"`/u"}},
		{name: "class set operations under the v flag", source: `x = /[\p{L}--[a-z]]/v`, expected: []string{`/[\p{L}--[a-z]]/v`}},
		{name: "named groups and lookbehind", source: `x = /(?<=\$)(?<amount>\d+(?:\.\d+)?)/dgimsuy`, expected: []string{`/(?<=\$)(?<amount>\d+(?:\.\d+)?)/dgimsuy`}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			actual := scanAll(t, testCase.source, tokenRegExp)
			if !slices.Equal(actual, testCase.expected) {
				t.Errorf("expected regular expressions %q, got %q", testCase.expected, actual)
			}
		})
	}
}

func TestScannerTemplates(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		source   string
		expected []string
	}{
		{name: "without substitutions", source: "`a`", expected: []string{"`a`"}},
		{name: "with substitutions", source: "`a${b}c${d}e`", expected: []string{"`a${", "}c${", "}e`"}},
		{name: "empty", source: "``; `${a}`", expected: []string{"``", "`${", "}`"}},
		{name: "escapes", source: "`\\`\\${a}`", expected: []string{"`\\`\\${a}`"}},
		{name: "dollar signs", source: "`$a$ ${b}$`", expected: []string{"`$a$ ${", "}$`"}},
		{
			name:     "braces inside a substitution",
			source:   "`a${ {b: 1}.b }c${ (() => { return {d: 2}; })().d }e`",
			expected: []string{"`a${", "}c${", "}e`"},
		},
		{
			name:     "templates nested in substitutions",
			source:   "`a${ `b${ `c` }d` }e`",
			expected: []string{"`a${", "`b${", "`c`", "}d`", "}e`"},
		},
		{name: "spanning lines", source: "`a\n${b}\r\nc`", expected: []string{"`a\n${", "}\r\nc`"}},
		{name: "after a comment holding a backtick", source: "// `\n`a`", expected: []string{"`a`"}},
		{name: "after a string holding a backtick", source: "'`'; `a`", expected: []string{"`a`"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			actual := scanAll(t, testCase.source, tokenTemplate, tokenTemplateHead, tokenTemplateMiddle, tokenTemplateTail)
			if !slices.Equal(actual, testCase.expected) {
				t.Errorf("expected templates %q, got %q", testCase.expected, actual)
			}
		})
	}
}

func TestScannerTokens(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		source   string
		kind     tokenKind
		expected []string
	}{
		{name: "hashbang", source: "#!/usr/bin/env node\nx", kind: tokenName, expected: []string{"x"}},
		{name: "private names", source: "#a in b; this.#c", kind: tokenPrivateName, expected: []string{"#a", "#c"}},
		{name: "unicode identifiers", source: "const ä = 1; const 𝒶 = 2; \\u0061b; \\u{62}c", kind: tokenName, expected: []string{"const", "ä", "const", "𝒶", "\\u0061b", "\\u{62}c"}},
		{name: "non-ASCII whitespace", source: "a\u00a0b\ufeffc\u3000d\u2028e", kind: tokenName, expected: []string{"a", "b", "c", "d", "e"}},
		{name: "line separators inside strings", source: "'a\u2028b' + \"c\u2029d\"", kind: tokenString, expected: []string{"'a\u2028b'", "\"c\u2029d\""}},
		{name: "line continuations inside strings", source: "'a\\\nb' + 'c\\\r\nd'", kind: tokenString, expected: []string{"'a\\\nb'", "'c\\\r\nd'"}},
		{name: "escaped quotes inside strings", source: `'a\'b' + "c\"d"`, kind: tokenString, expected: []string{`'a\'b'`, `"c\"d"`}},
		{name: "numbers", source: "1 1.5 .5 1e3 1E-3 0x1F 0o17 0b11 1_000 10n 1..toString", kind: tokenNumber, expected: []string{"1", "1.5", ".5", "1e3", "1E-3", "0x1F", "0o17", "0b11", "1_000", "10n", "1."}},
		{name: "optional chaining and a conditional before a number", source: "a?.b; c?.5:1", kind: tokenPunctuator, expected: []string{"?.", ";", "?", ":"}},
		{name: "spread and arrows", source: "(...a) => a", kind: tokenPunctuator, expected: []string{"(", "...", ")", "=>"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			actual := scanAll(t, testCase.source, testCase.kind)
			if !slices.Equal(actual, testCase.expected) {
				t.Errorf("expected %q, got %q", testCase.expected, actual)
			}
		})
	}
}

func TestScannerContextsBalance(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name   string
		source string
	}{
		{name: "functions and classes", source: "function f() {} class A extends B { m() {} } x = function () {}; y = class {};"},
		{name: "keywords as class field names", source: "class A { class = 1; function = 2; static class; class }"},
		{name: "keywords as method names", source: "class A { class() {} function() {} static function*() {} }"},
		{name: "keywords as object keys", source: "x = { class: 1, function: 2, class() {}, function() {} };"},
		{name: "keywords as import and export names", source: "import { class as c, function as f } from \"m\"; export { c as class, f as function };"},
		{name: "keywords as member names", source: "a.class; a?.function; a.class = function () {};"},
		{name: "keyword field names inside a substitution", source: "`${ a ? class { class = 1 } : 2 } b`"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			s := newScanner(testCase.source)
			for {
				if err := s.next(); err != nil {
					t.Fatalf("scanner next: %v", err)
				}
				if s.kind == tokenEOF {
					break
				}
			}
			if !slices.Equal(s.contexts, []contextKind{contextBlock}) {
				t.Errorf("expected the contexts to close back to the top level, got %v", s.contexts)
			}
		})
	}
}

func TestScannerErrors(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name             string
		source           string
		expectedError    error
		expectedPosition string
	}{
		{name: "unterminated string", source: "a;\n'abc", expectedError: ErrUnterminatedString, expectedPosition: "line 2, column 1"},
		{name: "line break in a string", source: "'a\nb'", expectedError: ErrUnterminatedString, expectedPosition: "line 1, column 1"},
		{name: "unterminated template", source: "x = `a${b}c", expectedError: ErrUnterminatedTemplate, expectedPosition: "line 1, column 10"},
		{name: "unterminated substitution", source: "x = `a${ `b${c}` ", expectedError: ErrUnterminatedTemplate, expectedPosition: "line 1, column 5"},
		{name: "unterminated regular expression", source: "x = /a", expectedError: ErrUnterminatedRegExp, expectedPosition: "line 1, column 5"},
		{name: "line break in a regular expression", source: "x = /a\n/", expectedError: ErrUnterminatedRegExp},
		{name: "unterminated comment", source: "a /* b", expectedError: ErrUnterminatedComment, expectedPosition: "line 1, column 3"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			s := newScanner(testCase.source)
			var err error
			for err == nil && s.kind != tokenEOF {
				err = s.next()
			}

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("expected error %v, got %v", testCase.expectedError, err)
			}
			if !errors.Is(err, altshiftErrors.ErrParseError) {
				t.Errorf("expected a parse error, got %v", err)
			}
			if !strings.Contains(err.Error(), testCase.expectedPosition) {
				t.Errorf("expected the error to name %q, got %v", testCase.expectedPosition, err)
			}
		})
	}
}
