package lit_minifier

import (
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
			name: "removes newlines inside multi-line attribute values",
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
			expectedContains: []string{`html` + "`" + `<div class="foobar"> Test </div>` + "`"},
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
