package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"

	motmedelErrors "github.com/Motmedel/utils_go/pkg/errors"
	motmedelLog "github.com/Motmedel/utils_go/pkg/log"
	motmedelErrorLogger "github.com/Motmedel/utils_go/pkg/log/error_logger"
	"github.com/altshiftab/web_build/pkg/build"
	buildConfig "github.com/altshiftab/web_build/pkg/build/types/config"
)

var ErrInvalidEntryFormat = errors.New("invalid entry format (expected name=path)")

func parseEntry(value string) (string, string, error) {
	name, path, found := strings.Cut(value, "=")
	if !found || name == "" || path == "" {
		return "", "", fmt.Errorf("%w: %s", ErrInvalidEntryFormat, value)
	}
	return name, path, nil
}

func main() {
	logger := &motmedelErrorLogger.Logger{
		Logger: slog.New(
			&motmedelLog.ContextHandler{
				Next: slog.NewJSONHandler(
					os.Stderr,
					&slog.HandlerOptions{AddSource: false, Level: slog.LevelInfo},
				),
				Extractors: []motmedelLog.ContextExtractor{
					&motmedelLog.ErrorContextExtractor{},
				},
			},
		),
	}
	slog.SetDefault(logger.Logger)

	var sourceDirectory string
	flag.StringVar(&sourceDirectory, "source", "src", "The source directory.")

	var outputDirectory string
	flag.StringVar(&outputDirectory, "output", "dist", "The output directory.")

	var publicPath string
	flag.StringVar(&publicPath, "public-path", "/", "The public path prefix for emitted URLs.")

	var preloadFontsPattern string
	flag.StringVar(&preloadFontsPattern, "preload-fonts", "", "A regular expression matching font assets to preload.")

	var splitting bool
	flag.BoolVar(&splitting, "splitting", false, "Emit ES modules with code splitting: dynamic imports become lazy chunks with import map integrity.")

	extraEntries := make(map[string]string)
	flag.Func("entry", "An extra entry point as name=path. Can be repeated.", func(value string) error {
		name, path, err := parseEntry(value)
		if err != nil {
			return fmt.Errorf("parse entry: %w", err)
		}
		extraEntries[name] = path
		return nil
	})

	flag.Parse()

	var preloadFonts *regexp.Regexp
	if preloadFontsPattern != "" {
		var err error
		preloadFonts, err = regexp.Compile(preloadFontsPattern)
		if err != nil {
			logger.FatalWithExitingMessage(
				"The preload fonts pattern could not be compiled.",
				motmedelErrors.NewWithTrace(fmt.Errorf("regexp compile: %w", err), preloadFontsPattern),
			)
		}
	}

	startTime := time.Now()

	writtenPaths, err := build.Build(
		&buildConfig.Config{
			SourceDirectory: sourceDirectory,
			OutputDirectory: outputDirectory,
			PublicPath:      publicPath,
			PreloadFonts:    preloadFonts,
			ExtraEntries:    extraEntries,
			Splitting:       splitting,
		},
	)
	if err != nil {
		logger.FatalWithExitingMessage(
			"An error occurred when building.",
			motmedelErrors.New(fmt.Errorf("build: %w", err)),
		)
	}

	slog.Info(
		"Build completed.",
		slog.Int("numFiles", len(writtenPaths)),
		slog.Duration("duration", time.Since(startTime)),
	)
}
