package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"

	argumentParser "github.com/altshiftab/utils_go/pkg/cli/argument_parser"
	"github.com/altshiftab/utils_go/pkg/cli/argument_parser/option"
	altshiftErrors "github.com/altshiftab/utils_go/pkg/errors"
	altshiftLog "github.com/altshiftab/utils_go/pkg/log"
	altshiftErrorLogger "github.com/altshiftab/utils_go/pkg/log/error_logger"
	"github.com/altshiftab/web_build/pkg/build"
	buildConfig "github.com/altshiftab/web_build/pkg/build/types/config"
)

const (
	programName = "web_build"
	exitUsage   = 2
)

var ErrInvalidEntryFormat = errors.New("invalid entry format (expected name=path)")

type arguments struct {
	sourceDirectory     string
	outputDirectory     string
	publicPath          string
	preloadFontsPattern string
	entries             []string
	splitting           bool
	skipModulePreload   bool
	sourceMaps          bool
}

func newParser(args *arguments, output io.Writer) *argumentParser.Parser {
	return &argumentParser.Parser{
		ProgramName: programName,
		Description: "Build a site for production: bundle and minify the TypeScript entries in the source directory's scripts directory, hash the assets, and write each HTML page with its scripts and stylesheets under subresource integrity.",
		Output:      output,
		Options: []option.Option{
			option.WithMetavar(
				option.WithDefault(option.NewStringOption(0, "source", "the source directory", false, &args.sourceDirectory), "src"),
				"PATH",
			),
			option.WithMetavar(
				option.WithDefault(option.NewStringOption(0, "output", "the output directory, replaced as a whole", false, &args.outputDirectory), "dist"),
				"PATH",
			),
			option.WithMetavar(
				option.WithDefault(option.NewStringOption(0, "public-path", "the public path prefix of emitted URLs", false, &args.publicPath), "/"),
				"PATH",
			),
			option.WithMetavar(
				option.NewStringOption(0, "preload-fonts", "a regular expression matching the font assets to preload", false, &args.preloadFontsPattern),
				"REGEXP",
			),
			option.WithMetavar(
				option.NewStringsOption(0, "entry", "an extra entry point; may be repeated", false, &args.entries),
				"NAME=PATH",
			),
			option.NewBoolOption(0, "splitting", "emit ES modules with code splitting: dynamic imports become lazy chunks with import map integrity", false, &args.splitting),
			option.NewBoolOption(0, "skip-modulepreload", "omit the modulepreload links for the static import closure; the import map still provides integrity for every chunk", false, &args.skipModulePreload),
			option.NewBoolOption(0, "sourcemap", "emit linked .map files alongside the js and css outputs", false, &args.sourceMaps),
		},
		DisableAbbrev: true,
	}
}

func parseEntry(value string) (string, string, error) {
	name, path, found := strings.Cut(value, "=")
	if !found || name == "" || path == "" {
		return "", "", fmt.Errorf("%w: %w: %s", altshiftErrors.ErrValidationError, ErrInvalidEntryFormat, value)
	}
	return name, path, nil
}

// makeConfig validates the parsed arguments and turns them into a build configuration.
func makeConfig(args *arguments) (*buildConfig.Config, error) {
	extraEntries := make(map[string]string)
	for _, entry := range args.entries {
		name, path, err := parseEntry(entry)
		if err != nil {
			return nil, fmt.Errorf("parse entry: %w", err)
		}
		extraEntries[name] = path
	}

	var preloadFonts *regexp.Regexp
	if args.preloadFontsPattern != "" {
		var err error
		preloadFonts, err = regexp.Compile(args.preloadFontsPattern)
		if err != nil {
			return nil, altshiftErrors.NewWithTrace(
				fmt.Errorf("%w: regexp compile: %w", altshiftErrors.ErrParseError, err),
				args.preloadFontsPattern,
			)
		}
	}

	return &buildConfig.Config{
		SourceDirectory:   args.sourceDirectory,
		OutputDirectory:   args.outputDirectory,
		PublicPath:        args.publicPath,
		PreloadFonts:      preloadFonts,
		ExtraEntries:      extraEntries,
		Splitting:         args.splitting,
		SkipModulePreload: args.skipModulePreload,
		SourceMaps:        args.sourceMaps,
	}, nil
}

func main() {
	logger := &altshiftErrorLogger.Logger{
		Logger: slog.New(
			&altshiftLog.ContextHandler{
				Next: slog.NewJSONHandler(
					os.Stderr,
					&slog.HandlerOptions{AddSource: false, Level: slog.LevelInfo},
				),
				Extractors: []altshiftLog.ContextExtractor{
					&altshiftLog.ErrorContextExtractor{},
				},
			},
		),
	}
	slog.SetDefault(logger.Logger)

	args := &arguments{}
	parser := newParser(args, os.Stdout)
	if err := parser.Validate(); err != nil {
		logger.FatalWithExitingMessage(
			"The argument parser is invalid.",
			altshiftErrors.New(fmt.Errorf("parser validate: %w", err)),
		)
	}
	parser.ParseOrExit()

	configuration, err := makeConfig(args)
	if err != nil {
		fmt.Fprint(os.Stderr, parser.FormatError(err))
		os.Exit(exitUsage)
	}

	startTime := time.Now()

	writtenPaths, err := build.Build(configuration)
	if err != nil {
		logger.FatalWithExitingMessage(
			"An error occurred when building.",
			altshiftErrors.New(fmt.Errorf("build: %w", err)),
		)
	}

	slog.Info(
		"Build completed.",
		slog.Int("numFiles", len(writtenPaths)),
		slog.Duration("duration", time.Since(startTime)),
	)
}
