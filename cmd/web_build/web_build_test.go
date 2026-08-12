package main

import (
	"errors"
	"testing"
)

func TestParseEntry(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name          string
		input         string
		expectedName  string
		expectedPath  string
		expectedError error
	}{
		{name: "valid entry", input: "styles=./src/styles/only.css", expectedName: "styles", expectedPath: "./src/styles/only.css"},
		{name: "missing separator", input: "styles", expectedError: ErrInvalidEntryFormat},
		{name: "empty name", input: "=path", expectedError: ErrInvalidEntryFormat},
		{name: "empty path", input: "name=", expectedError: ErrInvalidEntryFormat},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			name, path, err := parseEntry(testCase.input)

			if testCase.expectedError != nil {
				if !errors.Is(err, testCase.expectedError) {
					t.Fatalf("expected error %v, got %v", testCase.expectedError, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("parse entry: %v", err)
			}
			if name != testCase.expectedName || path != testCase.expectedPath {
				t.Errorf("expected (%q, %q), got (%q, %q)", testCase.expectedName, testCase.expectedPath, name, path)
			}
		})
	}
}
