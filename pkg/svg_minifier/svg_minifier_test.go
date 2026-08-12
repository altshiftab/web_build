package svg_minifier

import (
	"strings"
	"testing"
)

func TestMinify(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "removes declaration and comments",
			input: strings.Join(
				[]string{
					`<?xml version="1.0" encoding="UTF-8"?>`,
					"<!-- a comment -->",
					`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">`,
					`    <rect width="10" height="10"/>`,
					"</svg>",
				},
				"\n",
			),
			expected: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10"/></svg>`,
		},
		{
			name:     "keeps the viewBox",
			input:    `<svg viewBox="0 0 1 1"><path d="M 0 0 L 1 1"/></svg>`,
			expected: `<svg viewBox="0 0 1 1"><path d="M 0 0 L 1 1"/></svg>`,
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
