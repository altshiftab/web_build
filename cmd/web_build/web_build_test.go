package main

import (
	"errors"
	"io"
	"maps"
	"testing"

	argumentParserErrors "github.com/altshiftab/utils_go/pkg/cli/argument_parser/errors"
	altshiftErrors "github.com/altshiftab/utils_go/pkg/errors"
)

func TestParserValidates(t *testing.T) {
	t.Parallel()
	if err := newParser(&arguments{}, io.Discard).Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

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
		{name: "path containing a separator", input: "a=b=c", expectedName: "a", expectedPath: "b=c"},
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
				if !errors.Is(err, altshiftErrors.ErrValidationError) {
					t.Errorf("expected a validation error, got %v", err)
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

func TestParseArguments(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name          string
		argv          []string
		expected      arguments
		expectedError error
	}{
		{
			name:     "defaults",
			argv:     nil,
			expected: arguments{sourceDirectory: "src", outputDirectory: "dist", publicPath: "/"},
		},
		{
			name: "every option",
			argv: []string{
				"--source", "frontend/src",
				"--output=frontend/dist",
				"--public-path", "/static/",
				"--preload-fonts", `^fonts/roboto-.*\.woff2$`,
				"--entry", "a=./a.ts",
				"--entry", "b=./b.css",
				"--splitting",
				"--skip-modulepreload",
				"--sourcemap",
			},
			expected: arguments{
				sourceDirectory:     "frontend/src",
				outputDirectory:     "frontend/dist",
				publicPath:          "/static/",
				preloadFontsPattern: `^fonts/roboto-.*\.woff2$`,
				entries:             []string{"a=./a.ts", "b=./b.css"},
				splitting:           true,
				skipModulePreload:   true,
				sourceMaps:          true,
			},
		},
		{
			name:          "a single-dash long option is not accepted",
			argv:          []string{"-splitting"},
			expectedError: argumentParserErrors.ErrNameNotFound,
		},
		{
			name:          "an abbreviated option is not accepted",
			argv:          []string{"--split"},
			expectedError: argumentParserErrors.ErrNameNotFound,
		},
		{
			name:          "help",
			argv:          []string{"--help"},
			expectedError: argumentParserErrors.ErrHelp,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			args := &arguments{}
			err := newParser(args, io.Discard).ParseArgs(testCase.argv)

			if testCase.expectedError != nil {
				if !errors.Is(err, testCase.expectedError) {
					t.Fatalf("expected error %v, got %v", testCase.expectedError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse args: %v", err)
			}

			if args.sourceDirectory != testCase.expected.sourceDirectory ||
				args.outputDirectory != testCase.expected.outputDirectory ||
				args.publicPath != testCase.expected.publicPath ||
				args.preloadFontsPattern != testCase.expected.preloadFontsPattern ||
				args.splitting != testCase.expected.splitting ||
				args.skipModulePreload != testCase.expected.skipModulePreload ||
				args.sourceMaps != testCase.expected.sourceMaps {
				t.Errorf("expected %+v, got %+v", testCase.expected, *args)
			}
			if len(args.entries) != len(testCase.expected.entries) {
				t.Fatalf("expected entries %q, got %q", testCase.expected.entries, args.entries)
			}
			for i := range args.entries {
				if args.entries[i] != testCase.expected.entries[i] {
					t.Errorf("expected entries %q, got %q", testCase.expected.entries, args.entries)
				}
			}
		})
	}
}

func TestMakeConfig(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name                 string
		args                 *arguments
		expectedExtraEntries map[string]string
		expectedPreloadFonts string
		expectedError        error
	}{
		{
			name:                 "no extras",
			args:                 &arguments{sourceDirectory: "src", outputDirectory: "dist", publicPath: "/"},
			expectedExtraEntries: map[string]string{},
		},
		{
			name: "entries and a preload pattern",
			args: &arguments{
				entries:             []string{"a=./a.ts", "b=./b.css"},
				preloadFontsPattern: `^fonts/.*\.woff2$`,
			},
			expectedExtraEntries: map[string]string{"a": "./a.ts", "b": "./b.css"},
			expectedPreloadFonts: `^fonts/.*\.woff2$`,
		},
		{
			name:          "an invalid entry",
			args:          &arguments{entries: []string{"a"}},
			expectedError: ErrInvalidEntryFormat,
		},
		{
			name:          "an invalid preload pattern",
			args:          &arguments{preloadFontsPattern: "("},
			expectedError: altshiftErrors.ErrParseError,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			configuration, err := makeConfig(testCase.args)

			if testCase.expectedError != nil {
				if !errors.Is(err, testCase.expectedError) {
					t.Fatalf("expected error %v, got %v", testCase.expectedError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("make config: %v", err)
			}

			if !maps.Equal(configuration.ExtraEntries, testCase.expectedExtraEntries) {
				t.Errorf("expected extra entries %v, got %v", testCase.expectedExtraEntries, configuration.ExtraEntries)
			}
			if testCase.expectedPreloadFonts == "" {
				if configuration.PreloadFonts != nil {
					t.Errorf("expected no preload pattern, got %v", configuration.PreloadFonts)
				}
			} else if configuration.PreloadFonts == nil || configuration.PreloadFonts.String() != testCase.expectedPreloadFonts {
				t.Errorf("expected preload pattern %q, got %v", testCase.expectedPreloadFonts, configuration.PreloadFonts)
			}
			if configuration.SourceDirectory != testCase.args.sourceDirectory ||
				configuration.OutputDirectory != testCase.args.outputDirectory ||
				configuration.PublicPath != testCase.args.publicPath {
				t.Errorf("expected the directories and public path to carry over, got %+v", configuration)
			}
		})
	}
}
