package css_minifier

import "testing"

func TestMinify(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic rule",
			input:    ".a { color: red; }",
			expected: ".a{color:red}",
		},
		{
			name:     "selector lists and multiple declarations",
			input:    ".b   ,   .c { margin: 0px ; padding : 1px  2px; }",
			expected: ".b,.c{margin:0px;padding:1px 2px}",
		},
		{
			name:     "combinators",
			input:    "a > b ~ c + d { color: blue; }",
			expected: "a>b~c+d{color:blue}",
		},
		{
			name:     "descendant combinator before a pseudo-class",
			input:    "a :hover { color: red; }",
			expected: "a :hover{color:red}",
		},
		{
			name:     "pseudo-class",
			input:    "a:hover { color: red; }",
			expected: "a:hover{color:red}",
		},
		{
			name:     "media query",
			input:    "@media ( min-width : 600px ) { .a { color: red; } }",
			expected: "@media (min-width:600px){.a{color:red}}",
		},
		{
			name:     "font-face with strings and url",
			input:    "@font-face { font-family: \"My  Font\"; src: url( \"../f/a.woff2\" ) format( \"woff2\" ); }",
			expected: "@font-face{font-family:\"My  Font\";src:url(\"../f/a.woff2\") format(\"woff2\")}",
		},
		{
			name:     "at-rule without a block",
			input:    "@import url( foo.css );",
			expected: "@import url(foo.css);",
		},
		{
			name:     "calc keeps its spaces",
			input:    ".a { width: calc(100% - 10px); }",
			expected: ".a{width:calc(100% - 10px)}",
		},
		{
			name:     "important",
			input:    ".a { color: red !important ; }",
			expected: ".a{color:red !important}",
		},
		{
			name:     "custom property",
			input:    ":host { --my-var : 1px   solid  red; }",
			expected: ":host{--my-var:1px solid red}",
		},
		{
			name:     "var with fallback",
			input:    ".a { border: var( --my-var , 1px ) ; }",
			expected: ".a{border:var(--my-var,1px)}",
		},
		{
			name:     "comments are removed",
			input:    "/* comment */ .a { /* inner */ color: red; } /* after */",
			expected: ".a{color:red}",
		},
		{
			name:     "bang comments are kept",
			input:    "/*! keep me */ .a { color: red; }",
			expected: "/*! keep me */.a{color:red}",
		},
		{
			name:     "attribute selector strings are preserved",
			input:    "[data-x=\"a  b\"] { color: red; }",
			expected: "[data-x=\"a  b\"]{color:red}",
		},
		{
			name:     "keyframes",
			input:    "@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }",
			expected: "@keyframes spin{from{transform:rotate(0deg)}to{transform:rotate(360deg)}}",
		},
		{
			name:     "template expression placeholder",
			input:    ".a { color: @TEMPLATE_EXPRESSION; }",
			expected: ".a{color:@TEMPLATE_EXPRESSION}",
		},
		{
			name:     "slashes in values",
			input:    ".a{font:12px / 1.5 sans-serif}",
			expected: ".a{font:12px/1.5 sans-serif}",
		},
		{
			name:     "function arguments",
			input:    ".a { background: rgba( 0 , 0 , 0 , .5 ) ; }",
			expected: ".a{background:rgba(0,0,0,.5)}",
		},
		{
			name:     "multiple rules",
			input:    ".a { color: red; }\n\n.b { color: blue; }",
			expected: ".a{color:red}.b{color:blue}",
		},
		{
			name:     "semicolon and brace inside a string",
			input:    ".a { content: \";\"; color: red; }",
			expected: ".a{content:\";\";color:red}",
		},
		{
			name:     "data url with semicolon",
			input:    ".a { background: url(data:image/png;base64,AAAA); }",
			expected: ".a{background:url(data:image/png;base64,AAAA)}",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if actual := Minify(testCase.input); actual != testCase.expected {
				t.Errorf("expected %q, got %q", testCase.expected, actual)
			}
		})
	}
}
