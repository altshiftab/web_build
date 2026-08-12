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
