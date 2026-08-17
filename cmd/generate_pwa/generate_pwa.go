package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	altshiftErrors "github.com/altshiftab/utils_go/pkg/errors"
	altshiftLog "github.com/altshiftab/utils_go/pkg/log"
	altshiftErrorLogger "github.com/altshiftab/utils_go/pkg/log/error_logger"
	"github.com/evanw/esbuild/pkg/api"
)

var ErrEsbuildTransform = errors.New("esbuild transform failed")

// The icons referenced from the web app manifest and <head>. They keep fixed
// names; the service worker version accounts for content changes.
var iconFileNames = []string{
	"icon-192.png",
	"icon-512.png",
	"icon-maskable-512.png",
	"apple-touch-icon-180.png",
}

const serviceWorkerFileName = "sw.js"

// collectPrecachePaths returns the sorted dist-relative paths to be precached:
// every emitted asset except source maps, license sidecars and the service
// worker itself.
func collectPrecachePaths(distDirectory string) ([]string, error) {
	var paths []string

	err := filepath.WalkDir(distDirectory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}

		relativePath, err := filepath.Rel(distDirectory, path)
		if err != nil {
			return altshiftErrors.NewWithTrace(fmt.Errorf("filepath rel: %w", err), path)
		}
		relativePath = filepath.ToSlash(relativePath)

		if strings.HasSuffix(relativePath, ".map") ||
			strings.HasSuffix(relativePath, ".LICENSE.txt") ||
			strings.HasSuffix(relativePath, ".LEGAL.txt") ||
			relativePath == serviceWorkerFileName {
			return nil
		}

		paths = append(paths, relativePath)
		return nil
	})
	if err != nil {
		return nil, altshiftErrors.New(fmt.Errorf("filepath walk dir: %w", err), distDirectory)
	}

	sort.Strings(paths)
	return paths, nil
}

// makePrecacheUrls maps precache paths to served URLs; the app shell is served
// at the root.
func makePrecacheUrls(precachePaths []string) []string {
	urls := make([]string, 0, len(precachePaths))
	for _, precachePath := range precachePaths {
		if precachePath == "index.html" {
			urls = append(urls, "/")
		} else {
			urls = append(urls, "/"+precachePath)
		}
	}
	return urls
}

// computeVersion hashes the precached assets' names and contents, so the
// service worker version changes whenever any precached asset changes, even
// ones with fixed names such as index.html and the icons.
func computeVersion(distDirectory string, precachePaths []string) (string, error) {
	hash := sha256.New()
	for _, precachePath := range precachePaths {
		contents, err := os.ReadFile(filepath.Join(distDirectory, filepath.FromSlash(precachePath)))
		if err != nil {
			return "", altshiftErrors.NewWithTrace(fmt.Errorf("os read file: %w", err), precachePath)
		}
		hash.Write([]byte(precachePath))
		hash.Write([]byte{0})
		hash.Write(contents)
	}
	return hex.EncodeToString(hash.Sum(nil))[:12], nil
}

// generate copies the PWA icons into the dist directory and emits the service
// worker from its template with the precache list and version filled in.
func generate(sourceDirectory string, distDirectory string) error {
	iconsDirectory := filepath.Join(distDirectory, "icons")
	if err := os.MkdirAll(iconsDirectory, 0o700); err != nil {
		return altshiftErrors.NewWithTrace(fmt.Errorf("os mkdir all: %w", err), iconsDirectory)
	}
	for _, iconFileName := range iconFileNames {
		// #nosec G703 -- reading the user's own frontend sources is the point.
		contents, err := os.ReadFile(filepath.Join(sourceDirectory, "icons", iconFileName))
		if err != nil {
			return altshiftErrors.NewWithTrace(fmt.Errorf("os read file: %w", err), iconFileName)
		}
		// #nosec G703 -- writing into the user's own dist directory is the point.
		if err := os.WriteFile(filepath.Join(iconsDirectory, iconFileName), contents, 0o600); err != nil {
			return altshiftErrors.NewWithTrace(fmt.Errorf("os write file: %w", err), iconFileName)
		}
	}

	precachePaths, err := collectPrecachePaths(distDirectory)
	if err != nil {
		return fmt.Errorf("collect precache paths: %w", err)
	}

	version, err := computeVersion(distDirectory, precachePaths)
	if err != nil {
		return fmt.Errorf("compute version: %w", err)
	}

	precacheUrlsData, err := json.Marshal(makePrecacheUrls(precachePaths))
	if err != nil {
		return altshiftErrors.NewWithTrace(fmt.Errorf("json marshal: %w", err))
	}

	templatePath := filepath.Join(sourceDirectory, "sw-template.js")
	// #nosec G703 -- reading the user's own frontend sources is the point.
	template, err := os.ReadFile(templatePath)
	if err != nil {
		return altshiftErrors.NewWithTrace(fmt.Errorf("os read file: %w", err), templatePath)
	}

	serviceWorker := strings.ReplaceAll(string(template), "__BUILD_HASH__", version)
	serviceWorker = strings.ReplaceAll(serviceWorker, "__PRECACHE_URLS__", string(precacheUrlsData))

	// The service worker script is fetched on every update check, so it is
	// minified like the rest of the emitted scripts.
	transformResult := api.Transform(serviceWorker, api.TransformOptions{
		Loader:            api.LoaderJS,
		Target:            api.ESNext,
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
	})
	if len(transformResult.Errors) != 0 {
		return altshiftErrors.NewWithTrace(fmt.Errorf(
			"%w: %s",
			ErrEsbuildTransform,
			strings.Join(api.FormatMessages(transformResult.Errors, api.FormatMessagesOptions{}), "\n"),
		))
	}
	serviceWorker = string(transformResult.Code)

	serviceWorkerPath := filepath.Join(distDirectory, serviceWorkerFileName)
	// #nosec G703 -- writing into the user's own dist directory is the point.
	if err := os.WriteFile(serviceWorkerPath, []byte(serviceWorker), 0o600); err != nil {
		return altshiftErrors.NewWithTrace(fmt.Errorf("os write file: %w", err), serviceWorkerPath)
	}

	return nil
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

	var sourceDirectory string
	flag.StringVar(&sourceDirectory, "source", "../frontend/src", "The frontend source directory.")

	var distDirectory string
	flag.StringVar(&distDirectory, "dist", "../frontend/dist", "The frontend dist directory.")

	flag.Parse()

	if err := generate(sourceDirectory, distDirectory); err != nil {
		logger.FatalWithExitingMessage(
			"An error occurred when generating the PWA assets.",
			altshiftErrors.New(fmt.Errorf("generate: %w", err)),
		)
	}
}
