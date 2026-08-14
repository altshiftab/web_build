package html_minifier

import "testing"

func TestMinify(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "collapses whitespace and removes comments",
			input:    "<!DOCTYPE html>\n<html>\n  <head>\n    <!-- a comment -->\n    <title>Test</title>\n  </head>\n</html>",
			expected: "<!DOCTYPE html><html><head><title>Test</title></head></html>",
		},
		{
			name:     "keeps single spaces in text",
			input:    "<p>some   spaced    text</p>",
			expected: "<p>some spaced text</p>",
		},
		{
			name:     "preserves pre content",
			input:    "<body><pre>  spaced\n   out  </pre></body>",
			expected: "<body><pre>  spaced\n   out  </pre></body>",
		},
		{
			name:     "preserves script content",
			input:    "<script>\nconst a = 1;\n</script>",
			expected: "<script>\nconst a = 1;\n</script>",
		},
		{
			name:     "preserves space between inline elements",
			input:    "<p><span>Already signed in as</span> <span>user@example.com</span></p>",
			expected: "<p><span>Already signed in as</span> <span>user@example.com</span></p>",
		},
		{
			name:     "collapses newline between inline elements to a space",
			input:    "<p><span>a</span>\n    <span>b</span></p>",
			expected: "<p><span>a</span> <span>b</span></p>",
		},
		{
			name:     "strips whitespace adjacent to block elements",
			input:    "<div>\n  <p>a</p>\n  <span>b</span> <span>c</span>\n</div>",
			expected: "<div><p>a</p><span>b</span> <span>c</span></div>",
		},
		{
			name:     "preserves space between custom elements",
			input:    "<body><magic-dialog></magic-dialog> <feedback-dialog></feedback-dialog></body>",
			expected: "<body><magic-dialog></magic-dialog> <feedback-dialog></feedback-dialog></body>",
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
