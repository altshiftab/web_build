package lit_minifier

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestMinify(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name             string
		input            string
		expectedContains []string
		expectedEqual    string
	}{
		{
			name: "minifies css and html tagged templates",
			input: strings.Join(
				[]string{
					"import {css, html} from 'lit';",
					"const styles = css`",
					"  .a { color: red; }",
					"`;",
					"const styles2 = css`",
					"  .b { color: ${\"black\"}; }",
					"`;",
					"const tpl = html`",
					"  <div>",
					"    Test",
					"  </div>",
					"`;",
				},
				"\n",
			),
			expectedContains: []string{
				"css`.a{color:red}`",
				"css`.b{color:${\"black\"}}`",
				"html`<div> Test </div>`",
			},
		},
		{
			name: "collapses line breaks inside multi-line attribute values",
			input: strings.Join(
				[]string{
					"import {html} from 'lit';",
					"const tpl = html`",
					"  <div",
					"    class=\"foo",
					"bar\">",
					"    Test",
					"  </div>",
					"`;",
				},
				"\n",
			),
			expectedContains: []string{`html` + "`" + `<div class="foo bar"> Test </div>` + "`"},
		},
		{
			name: "preserves code outside the templates verbatim",
			input: strings.Join(
				[]string{
					"import {css} from 'lit';",
					"",
					"// A comment that must survive.",
					"const styles = css`  .a { color: red; }  `;",
					"const untouched = `  keep   this   as-is  `;",
					"function unrelated() { return 1 + 1; }",
				},
				"\n",
			),
			expectedContains: []string{
				"// A comment that must survive.",
				"const untouched = `  keep   this   as-is  `;",
				"function unrelated() { return 1 + 1; }",
				"css`.a{color:red}`",
			},
		},
		{
			name:             "minifies templates nested in other templates",
			input:            "const tpl = html`\n  <style>\n    ${css`\n      .a { color: red; }\n    `}\n  </style>\n`;",
			expectedContains: []string{"css`.a{color:red}`", "html`<style> ${"},
		},
		{
			name:          "leaves other tagged templates alone",
			input:         "const query = sql`  SELECT   1  `;",
			expectedEqual: "const query = sql`  SELECT   1  `;",
		},
		{
			name:          "leaves member expression tags alone",
			input:         "const styles = theme.css`  .a { color: red; }  `;",
			expectedEqual: "const styles = theme.css`  .a { color: red; }  `;",
		},
		{
			name:          "handles regular expressions containing backticks",
			input:         "const re = /`/; const styles = css`  .a { color: red; }  `;",
			expectedEqual: "const re = /`/; const styles = css`.a{color:red}`;",
		},
		{
			name:          "handles division",
			input:         "const half = total / 2; const styles = css`  .a { color: red; }  `;",
			expectedEqual: "const half = total / 2; const styles = css`.a{color:red}`;",
		},
		{
			name:          "collapses line breaks between apostrophes in text",
			input:         "const t = html`<p>Don't\nworry, it's fine</p>`;",
			expectedEqual: "const t = html`<p>Don't worry, it's fine</p>`;",
		},
		{
			name:          "keeps whitespace inside pre",
			input:         "const t = html`\n  <div>\n    <pre>\n  a\n    b\n</pre>\n  </div>\n`;",
			expectedEqual: "const t = html`<div> <pre>\n  a\n    b\n</pre> </div>`;",
		},
		{
			name:          "keeps whitespace inside pre around substitutions",
			input:         "const t = html`  <pre class=${c}>  ${code}\n  ${more}  </pre>  <p>  x  </p>  `;",
			expectedEqual: "const t = html`<pre class=${c}>  ${code}\n  ${more}  </pre> <p> x </p>`;",
		},
		{
			name:          "keeps whitespace inside textarea",
			input:         "const t = html`<textarea>  a\n b</textarea>  <p>  x  </p>`;",
			expectedEqual: "const t = html`<textarea>  a\n b</textarea> <p> x </p>`;",
		},
		{
			name:          "matches pre tags in any case but not custom elements",
			input:         "const t = html`<PRE>  a  </PRE>  <pre-view>  b  </pre-view>`;",
			expectedEqual: "const t = html`<PRE>  a  </PRE> <pre-view> b </pre-view>`;",
		},
		{
			name:          "keeps the end of a pre left open",
			input:         "const t = html`  <pre>  a  `;",
			expectedEqual: "const t = html`<pre>  a  `;",
		},
		{
			name:          "keeps a line continuation before the closing backtick",
			input:         "x = html`<p>  a  </p>\\\n`; y = css`.a { color: red; }\\\n`; z = 1;",
			expectedEqual: "x = html`<p> a </p>\\\n`; y = css`.a { color: red; }\\\n`; z = 1;",
		},
		{
			name:          "keeps line continuations inside html",
			input:         "html`<p title=\"a\\\nb\">  x\\\r\n  y  </p>`",
			expectedEqual: "html`<p title=\"a\\\nb\"> x\\\r\n y </p>`",
		},
		{
			name:          "collapses whitespace after an escaped backslash",
			input:         "html`a\\\\  \n  b`",
			expectedEqual: "html`a\\\\ b`",
		},
		{
			name:          "keeps no-break spaces at the ends of html",
			input:         "html`\u00a0${a}\u00a0`",
			expectedEqual: "html`\u00a0${a}\u00a0`",
		},
		{
			name:          "leaves a template alone when minifying would open a substitution",
			input:         "css`a$ { color: red }`",
			expectedEqual: "css`a$ { color: red }`",
		},
		{
			name:          "minifies a selector starting with a substitution as a selector",
			input:         "css`${sel}:hover { color: red } ${a}${b} .c { color: blue }`",
			expectedEqual: "css`${sel}:hover{color:red}${a}${b} .c{color:blue}`",
		},
		{
			name:          "keeps the last semicolon of a declaration fragment",
			input:         "const d = css`color: red;`; x = css`.a { ${d} background: blue; }`;",
			expectedEqual: "const d = css`color:red;`; x = css`.a{${d} background:blue}`;",
		},
		{
			name:          "leaves a css template alone when a comment holds a substitution",
			input:         "const s = css`/* ${a} */ .b { color: ${c}; }`;",
			expectedEqual: "const s = css`/* ${a} */ .b { color: ${c}; }`;",
		},
		{
			name:          "leaves a css template alone when its text holds the placeholder",
			input:         "const s = css`.a::after { content: \"@TEMPLATE_EXPRESSION\"; color: ${c}; }`;",
			expectedEqual: "const s = css`.a::after { content: \"@TEMPLATE_EXPRESSION\"; color: ${c}; }`;",
		},
		{
			name:          "keeps a kept comment holding a substitution",
			input:         "const s = css`/*! ${a} */ .b { color: ${c}; }`;",
			expectedEqual: "const s = css`/*! ${a} */.b{color:${c}}`;",
		},
		{
			name:          "minifies templates nested behind braces in substitutions",
			input:         "html`  <p>  ${ {a: css`  .a { color: red; }  `}.a }  </p>  ${ (() => { return html`  <b>  x  </b>  `; })() }  `",
			expectedEqual: "html`<p> ${ {a: css`.a{color:red}`}.a } </p> ${ (() => { return html`<b> x </b>`; })() }`",
		},
		{
			name:          "minifies a template tagged across a comment and a line break",
			input:         "css /* tag */\n`  .a { color: red; }  `",
			expectedEqual: "css /* tag */\n`.a{color:red}`",
		},
		{
			name:          "leaves optional chaining member tags alone",
			input:         "a?.html; html`  <b>  </b>  `",
			expectedEqual: "a?.html; html`<b> </b>`",
		},
		{
			name:          "leaves private name tags alone",
			input:         "this.#css`  .a { color: red; }  `",
			expectedEqual: "this.#css`  .a { color: red; }  `",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			actual, err := Minify(testCase.input)
			if err != nil {
				t.Fatalf("minify: %v", err)
			}

			if testCase.expectedEqual != "" && actual != testCase.expectedEqual {
				t.Errorf("expected %q, got %q", testCase.expectedEqual, actual)
			}
			for _, expected := range testCase.expectedContains {
				if !strings.Contains(actual, expected) {
					t.Errorf("expected output to contain %q, got %q", expected, actual)
				}
			}
		})
	}
}

func TestCollapseWhitespace(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "empty", input: "", expected: ""},
		{name: "nothing to collapse", input: "a b c", expected: "a b c"},
		{name: "a run of spaces", input: "a   b", expected: "a b"},
		{name: "every kind of whitespace", input: "a \t\n\f\r b", expected: "a b"},
		{name: "a lone line break", input: "a\nb", expected: "a b"},
		{name: "a lone tab", input: "a\tb", expected: "a b"},
		{name: "runs at the ends", input: "\n  a  \n", expected: " a "},
		{name: "only whitespace", input: " \n ", expected: " "},
		{name: "a line continuation", input: "a\\\n  b", expected: "a\\\n b"},
		{name: "a CRLF line continuation", input: "a\\\r\n  b", expected: "a\\\r\n b"},
		{name: "an escaped space", input: "a\\   b", expected: "a\\  b"},
		{name: "an escaped backslash", input: "a\\\\   b", expected: "a\\\\ b"},
		{name: "a backslash at the end", input: "a  \\", expected: "a \\"},
		{name: "a vertical tab and a no-break space are not collapsed", input: "a\v\vb\u00a0\u00a0c", expected: "a\v\vb\u00a0\u00a0c"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if actual := collapseWhitespace(testCase.input); actual != testCase.expected {
				t.Errorf("expected %q, got %q", testCase.expected, actual)
			}
		})
	}
}

func TestTrimHtmlWhitespace(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "both ends", input: " \t\n a b \f\r ", expected: "a b"},
		{name: "a no-break space is not trimmed", input: "\u00a0a\u00a0", expected: "\u00a0a\u00a0"},
		{name: "a line continuation at the end", input: "a\\\n  ", expected: "a\\\n"},
		{name: "an escaped backslash at the end", input: "a\\\\\n  ", expected: "a\\\\"},
		{name: "only whitespace", input: " \n ", expected: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if actual := trimHtmlWhitespace(testCase.input); actual != testCase.expected {
				t.Errorf("expected %q, got %q", testCase.expected, actual)
			}
		})
	}
}

func TestIsValidQuasi(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		input    string
		expected bool
	}{
		{name: "plain text", input: ".a{color:red}", expected: true},
		{name: "empty", input: "", expected: true},
		{name: "an escaped backtick", input: "a\\`b", expected: true},
		{name: "an escaped substitution", input: "a\\${b}", expected: true},
		{name: "a dollar sign at the end", input: "a$", expected: true},
		{name: "a dollar sign before other text", input: "a$b {", expected: true},
		{name: "a backtick", input: "a`b", expected: false},
		{name: "a substitution", input: "a${b", expected: false},
		{name: "a backslash at the end", input: "a\\", expected: false},
		{name: "an escaped backslash at the end", input: "a\\\\", expected: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if actual := isValidQuasi(testCase.input); actual != testCase.expected {
				t.Errorf("expected %v for %q, got %v", testCase.expected, testCase.input, actual)
			}
		})
	}
}

// modernSyntax are snippets of modern JavaScript, each of which a scanner that misreads it would
// lose its place in: misreading a division as a regular expression, or a regular expression as
// division, a brace as closing a substitution, a quote or a backtick as opening a string or a
// template.
var modernSyntax = []struct {
	name   string
	source string
}{
	{name: "private fields and methods", source: "class A { #n = 4; static #m() {} half() { return this.#n / 2 / A.#m(); } has(o) { return #n in o; } }"},
	{name: "static blocks", source: "class A { static x; static { this.x = /'/.test(\"'\") / 1; } }"},
	{name: "class fields holding arrows", source: "class A { handle = () => { this.x = 1 / 2; }; static styles = [a, b]; }"},
	{name: "getters, setters and computed keys", source: "const o = { get html() { return 1 / 2; }, set css(v) {}, [Symbol.iterator]() { return /`/; } };"},
	{name: "optional chaining", source: "a?.b?.[c]?.(d) / 2 / e; f?.5:1;"},
	{name: "nullish coalescing and logical assignment", source: "a ??= b ?? /'/; c ||= d / 2 / e; f &&= `g`;"},
	{name: "exponentiation", source: "a = b ** 2 / c ** /`/.source.length;"},
	{name: "numeric separators and BigInt", source: "const big = 1_000_000n / 2n; const hex = 0xFF_FF / 2; const exponent = 1e-7 / 2;"},
	{name: "regular expression flags", source: "const re = [/a/d, /b/v, /c/s, /d/y, /(?<y>\\d{4})-(?<m>\\d{2})/u, /(?<=\\$)\\d+/, /[\\p{L}--[a-z]]/v, /\\p{Emoji}/u];"},
	{name: "async functions and await", source: "async function f() { const r = await fetch(`/api/${id}`); return (await r.json()).n / 2; } const g = async () => /'/.test(await h());"},
	{name: "generators and yield", source: "function* g() { yield /`/; yield* other(); const x = yield 1 / 2; }"},
	{name: "for await and for of", source: "async function f() { for await (const x of stream) /'/.test(x); for (const m of /a/g.exec(s)) {} }"},
	{name: "spread, rest and destructuring defaults", source: "const { a = /`/, ...rest } = o; const [b = 1 / 2, ...more] = p; f(...args);"},
	{name: "labels", source: "outer: for (;;) { inner: { break outer; } }"},
	{name: "new.target and import.meta", source: "function F() { if (!new.target) throw 1 / 2; } const u = new URL(\"x\", import.meta.url).href / 2;"},
	{name: "dynamic import and import attributes", source: "import data from \"./data.json\" with { type: \"json\" }; const m = await import(`./m-${n}.js`);"},
	{name: "export forms", source: "export * as ns from \"./ns.js\"; export default class {} export { a as default };"},
	{name: "decorators", source: "@customElement(\"x-a\") class A extends LitElement { @property() x = 1 / 2; }"},
	{name: "string raw and other tags", source: "const p = String.raw`C:\\\\${dir}\\\\` + sql`SELECT ${1 / 2}`;"},
	{name: "hashbang", source: "#!/usr/bin/env node\nconst x = 1 / 2;"},
	{name: "unicode identifiers and escapes", source: "const ä = 1 / 2; const 𝒶 = ä / 2; \\u0062 / 2; const \\u{63} = /'/;"},
	{name: "line separators in strings", source: "const s = \"a\u2028b\u2029c\" / 2;"},
	{name: "line and block comments with quotes", source: "// it's `here`\n/* and \"here\" */ x = 1 / 2; /** @type {import(\"a\").B} */ let y;"},
	{name: "regular expression after a control statement", source: "if (a) /'/.test(b); while (c) /`/.exec(d); for (;;) /\"/.test(e);"},
	{name: "division after a function expression", source: "const f = function () {} / 2; const g = class {} / 2;"},
	{name: "division after keyword property names", source: "const n = x.default / 2 + y.new / 3 + z?.in / 4;"},
	{name: "arrow function bodies", source: "const f = (x) => ({ a: x }) / 2; const g = () => {}\n/'/.test(s);"},
	{name: "keywords as class field names in a substitution", source: "const t = `${ a ? class { class = 1; function = 2 } : 2 }  b  `;"},
	{name: "regular expressions after statements ended by line breaks", source: "for (;;) { if (stop) break\n/'/.test(s) }\nx = a\n{ b }\n/`/.test(c); {} {} /\"/.test(d)"},
	{name: "regular expression after export default class", source: "export default class {}\n/`/.test(s);"},
	{name: "minified control flow", source: "for(let f of o.split(\"/\"))/^r\\d+$/.test(f)?a=f.slice(1):f!==\"\"&&h.push(Number(f));"},
	{
		name:   "regular expression after a declaration opening a static block",
		source: "class A extends LitElement { static { function register() {}\n/[\"']/.test(navigator.userAgent) && register(); } }",
	},
	{
		name:   "division after a conditional's or an async function expression",
		source: "const x = c ? 0 : {a: 1} / 2; const y = \"'\";\nconst f = async function () {} / 2; const s = \"'\";",
	},
	{name: "awaited object literal on the next line", source: "async function f() { const x = await\n{ then(r) { r(4); } } / 2; const s = \"'\"; }"},
}

func TestMinifyModernSyntax(t *testing.T) {
	t.Parallel()
	const tail = "\nconst styles = css`  .a { color: red; }  `;"
	const minifiedTail = "\nconst styles = css`.a{color:red}`;"

	for _, testCase := range modernSyntax {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			actual, err := Minify(testCase.source + tail)
			if err != nil {
				t.Fatalf("minify: %v", err)
			}
			if expected := testCase.source + minifiedTail; actual != expected {
				t.Errorf("expected %q, got %q", expected, actual)
			}
		})
	}
}

// tokenKinds returns the kinds of the tokens of the source, in order.
func tokenKinds(source string) ([]tokenKind, error) {
	s := newScanner(source)
	var kinds []tokenKind
	for {
		if err := s.next(); err != nil {
			return nil, err
		}
		kinds = append(kinds, s.kind)
		if s.kind == tokenEOF {
			return kinds, nil
		}
	}
}

// FuzzMinify checks that the minifier never panics, that it leaves source holding no css or html
// tag byte for byte as it was, and that what it returns scans into the same tokens as what it was
// given: only the text of templates may change, never the JavaScript around and between it.
func FuzzMinify(f *testing.F) {
	for _, testCase := range modernSyntax {
		f.Add(testCase.source)
	}
	f.Add("html`  <p>  ${ {a: css`  .a { color: red; }  `}.a }  </p>  `")
	f.Add("x = `a${ `b${ `c` }d` }e`")
	f.Add("a = {} / 2; {} /a/; ({}) / 2")
	f.Add("x = html`<p>a</p>\\\n`; y = css`a$ { color: red }`")

	f.Fuzz(func(t *testing.T, source string) {
		actual, err := Minify(source)
		if err != nil {
			return
		}
		if !strings.Contains(source, "css") && !strings.Contains(source, "html") && actual != source {
			t.Errorf("expected source without tags to be left alone, got %q from %q", actual, source)
		}

		expectedKinds, err := tokenKinds(source)
		if err != nil {
			t.Fatalf("token kinds of the source: %v", err)
		}
		actualKinds, err := tokenKinds(actual)
		if err != nil {
			t.Fatalf("token kinds of %q, minified from %q: %v", actual, source, err)
		}
		if !slices.Equal(actualKinds, expectedKinds) {
			t.Errorf("expected %q, minified from %q, to scan into the same tokens", actual, source)
		}
	})
}

// benchmarkSource is a module of lit components the size of a large application bundle, mixing
// tagged templates with the code around them: classes with private fields, regular expressions,
// division, comments, strings and untagged templates. It keeps to what the tdewolff/parse lexer the
// minifier used to be built on could read, so that the two can be compared.
func benchmarkSource() string {
	const component = `
/** A component. */
class Component%[1]d extends LitElement {
	static styles = css` + "`" + `
		:host { display: block; padding: calc(var(--gap) / 2); }
		.title { font-weight: 700; color: ${unsafeCSS(theme.accent)}; }
		.row > .cell + .cell { margin-inline-start: 4px; }
	` + "`" + `;
	#count = %[1]d;
	#pattern = /^[a-z]+(?:-[a-z]+)*$/iu;
	get ratio() { return this.count / 2 / (this.items?.length ?? 1); }
	matches(value) { const blank = /^\s*$/.test(value); return !blank && this.#pattern.test(value); }
	render() {
		const label = ` + "`item-${this.#count}`" + `;
		return html` + "`" + `
			<section class="title">
				<h2>${this.heading}</h2>
				<ul>${this.items.map((item) => html` + "`" + `<li class="cell">${item.name}</li>` + "`" + `)}</ul>
				<p title="${label}">Don't panic, it's only a ${"benchmark"}.</p>
			</section>
		` + "`" + `;
	}
}
customElements.define("component-%[1]d", Component%[1]d);
`
	var builder strings.Builder
	builder.WriteString("import {LitElement, css, html, unsafeCSS} from \"lit\";\n")
	for i := range 400 {
		fmt.Fprintf(&builder, component, i)
	}
	return builder.String()
}

func BenchmarkMinify(b *testing.B) {
	source := benchmarkSource()
	b.SetBytes(int64(len(source)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Minify(source); err != nil {
			b.Fatalf("minify: %v", err)
		}
	}
}
