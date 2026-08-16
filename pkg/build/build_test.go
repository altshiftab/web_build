package build

import (
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	buildConfig "github.com/altshiftab/web_build/pkg/build/types/config"
)

func writeFixture(t *testing.T, root string) {
	t.Helper()

	files := map[string]string{
		"src/index.html": strings.Join(
			[]string{
				"<!DOCTYPE html>",
				`<html lang="en">`,
				"<head>",
				`    <meta charset="UTF-8">`,
				"    <title>Fixture</title>",
				`    <link rel="icon" href="~@test/pkg/icon.svg" type="image/svg+xml">`,
				"</head>",
				"<body>",
				`    <img src="./images/logo.svg" alt="logo">`,
				`    <div id="app"></div>`,
				"</body>",
				"</html>",
			},
			"\n",
		),
		"src/scripts/modules/shared.ts":     `export const sharedValue = "shared-code-marker";`,
		"src/scripts/other.ts":              `import {sharedValue} from "./modules/shared";` + "\n" + `console.log("other-entry", sharedValue);`,
		"src/scripts/modules/lazy_extra.ts": `export const lazyValue = "lazy-extra-marker";`,
		"src/scripts/index.ts": strings.Join(
			[]string{
				"/**",
				" * @license Fixture-JS-License",
				" */",
				`import "../styles/main.css";`,
				`import {sharedValue} from "./modules/shared";`,
				"",
				`void import("./modules/lazy_extra").then(module => console.log(sharedValue, module.lazyValue));`,
				"",
				"type TemplateTag = (strings: TemplateStringsArray, ...values: unknown[]) => string;",
				"",
				"const joinTemplate: TemplateTag = (strings, ...values) =>",
				`    strings.reduce((out, part, i) => out + part + String(values[i] ?? ""), "");`,
				"",
				"const css: TemplateTag = joinTemplate;",
				"const html: TemplateTag = joinTemplate;",
				"",
				"const styles = css`",
				"  .lit-element {",
				"    color: red;",
				"  }",
				"`;",
				"",
				"const template = html`",
				"  <div>",
				"    Fixture content",
				"  </div>",
				"`;",
				"",
				`console.log("fixture-entry", styles, template);`,
			},
			"\n",
		),
		"src/styles/main.css": strings.Join(
			[]string{
				"/*! Fixture-CSS-License */",
				"@font-face {",
				`    font-family: "Body";`,
				`    src: url("../fonts/body.woff2") format("woff2");`,
				"}",
				"",
				".app {",
				`    font-family: "Body", sans-serif;`,
				"    margin: 0px;",
				"    color: red;",
				"}",
			},
			"\n",
		),
		"src/styles/only.css":             ".styles-only-entry {\n    color: blue;\n}",
		"src/fonts/body.woff2":            "wOF2fake-font-bytes-for-testing",
		"node_modules/@test/pkg/icon.svg": `<svg viewBox="0 0 2 2"><circle r="1"/></svg>`,
		"src/images/logo.svg": strings.Join(
			[]string{
				`<?xml version="1.0" encoding="UTF-8"?>`,
				"<!-- This comment should be removed. -->",
				`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">`,
				`    <rect width="10" height="10" fill="#ff0000"/>`,
				"</svg>",
			},
			"\n",
		),
	}

	for name, contents := range files {
		fullPath := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir all: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(contents), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
}

func TestBuild(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root)

	outputDirectory := filepath.Join(root, "dist")
	writtenPaths, err := Build(
		&buildConfig.Config{
			SourceDirectory: filepath.Join(root, "src"),
			OutputDirectory: outputDirectory,
			PublicPath:      "/",
			PreloadFonts:    regexp.MustCompile(`^fonts/`),
			ExtraEntries:    map[string]string{"styles": filepath.Join(root, "src/styles/only.css")},
		},
	)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	readOutput := func(pattern string) (string, string) {
		t.Helper()
		compiledPattern := regexp.MustCompile(pattern)
		for _, writtenPath := range writtenPaths {
			if compiledPattern.MatchString(writtenPath) {
				contents, err := os.ReadFile(filepath.Join(outputDirectory, filepath.FromSlash(writtenPath)))
				if err != nil {
					t.Fatalf("read output: %v", err)
				}
				return writtenPath, string(contents)
			}
		}
		t.Fatalf("no output matching %s in %v", pattern, writtenPaths)
		return "", ""
	}

	t.Run("emits hashed assets of every type", func(t *testing.T) {
		t.Parallel()
		for _, pattern := range []string{
			`^scripts/index-[A-Z0-9]+\.js$`,
			`^styles/index-[A-Z0-9]+\.css$`,
			`^images/logo\.[0-9a-f]+\.svg$`,
			`^fonts/body-[A-Z0-9]+\.woff2$`,
			`^index\.html$`,
		} {
			readOutput(pattern)
		}
	})

	t.Run("emits no source maps by default", func(t *testing.T) {
		t.Parallel()
		for _, writtenPath := range writtenPaths {
			if strings.HasSuffix(writtenPath, ".map") {
				t.Errorf("unexpected source map output: %s", writtenPath)
			}
		}

		_, script := readOutput(`^scripts/index-[A-Z0-9]+\.js$`)
		if strings.Contains(script, "sourceMappingURL") {
			t.Error("unexpected sourceMappingURL comment in the script output")
		}
	})

	t.Run("emits no script for the style-only entry", func(t *testing.T) {
		t.Parallel()
		readOutput(`^styles/styles-[A-Z0-9]+\.css$`)
		for _, writtenPath := range writtenPaths {
			if strings.HasPrefix(writtenPath, "scripts/styles-") {
				t.Errorf("unexpected script for style-only entry: %s", writtenPath)
			}
		}
	})

	t.Run("minifies the emitted javascript including tagged templates", func(t *testing.T) {
		t.Parallel()
		_, script := readOutput(`^scripts/index-[A-Z0-9]+\.js$`)
		for _, expected := range []string{"fixture-entry", ".lit-element{color:red}", "<div> Fixture content </div>"} {
			if !strings.Contains(script, expected) {
				t.Errorf("expected script to contain %q", expected)
			}
		}
		if strings.Contains(script, "\n    ") {
			t.Error("expected minified script output")
		}
	})

	t.Run("minifies the extracted css and rewrites the font url", func(t *testing.T) {
		t.Parallel()
		_, style := readOutput(`^styles/index-[A-Z0-9]+\.css$`)
		if !strings.Contains(style, ".app{") {
			t.Errorf("expected minified css, got %q", style)
		}
		if !regexp.MustCompile(`url\(["']?/fonts/body-[A-Z0-9]+\.woff2["']?\)`).MatchString(style) {
			t.Errorf("expected a rewritten font url, got %q", style)
		}
	})

	t.Run("cleans up the svg", func(t *testing.T) {
		t.Parallel()
		_, image := readOutput(`^images/logo\.[0-9a-f]+\.svg$`)
		if strings.Contains(image, "<!--") || strings.Contains(image, "<?xml") {
			t.Errorf("expected cleaned svg, got %q", image)
		}
		if !strings.Contains(image, `viewBox="0 0 10 10"`) {
			t.Errorf("expected the viewBox to be kept, got %q", image)
		}
	})

	t.Run("emits html with valid subresource integrity", func(t *testing.T) {
		t.Parallel()
		_, page := readOutput(`^index\.html$`)

		references := regexp.MustCompile(
			`(?:src|href)="(/(?:scripts|styles)/[^"]+)"[^>]*integrity="(sha384-[^"]+)"`,
		).FindAllStringSubmatch(page, -1)
		if len(references) < 2 {
			t.Fatalf("expected integrity on at least the script and stylesheet, got %q", page)
		}

		for _, reference := range references {
			contents, err := os.ReadFile(filepath.Join(outputDirectory, filepath.FromSlash(reference[1][1:])))
			if err != nil {
				t.Fatalf("read referenced asset: %v", err)
			}
			digest := sha512.Sum384(contents)
			expected := "sha384-" + base64.StdEncoding.EncodeToString(digest[:])
			if reference[2] != expected {
				t.Errorf("integrity mismatch for %s: %s != %s", reference[1], reference[2], expected)
			}
		}
	})

	t.Run("extracts license comments into sidecar files", func(t *testing.T) {
		t.Parallel()
		_, scriptLicenses := readOutput(`^scripts/index-[A-Z0-9]+\.js\.LEGAL\.txt$`)
		if !strings.Contains(scriptLicenses, "Fixture-JS-License") {
			t.Errorf("expected the js license text, got %q", scriptLicenses)
		}

		// The sidecar of a relocated css bundle must be relocated with it.
		_, styleLicenses := readOutput(`^styles/index-[A-Z0-9]+\.css\.LEGAL\.txt$`)
		if !strings.Contains(styleLicenses, "Fixture-CSS-License") {
			t.Errorf("expected the css license text, got %q", styleLicenses)
		}

		_, script := readOutput(`^scripts/index-[A-Z0-9]+\.js$`)
		if strings.Contains(script, "Fixture-JS-License") {
			t.Error("expected the license text to be moved out of the bundle")
		}
	})

	t.Run("resolves tilde references against node_modules", func(t *testing.T) {
		t.Parallel()
		iconPath, icon := readOutput(`^images/icon\.[0-9a-f]+\.svg$`)
		if !strings.Contains(icon, `viewBox="0 0 2 2"`) {
			t.Errorf("expected the module icon contents, got %q", icon)
		}

		_, page := readOutput(`^index\.html$`)
		if !strings.Contains(page, "/"+iconPath) {
			t.Errorf("expected the page to reference %s, got %q", iconPath, page)
		}
	})

	t.Run("preloads fonts and references the hashed image", func(t *testing.T) {
		t.Parallel()
		_, page := readOutput(`^index\.html$`)

		preloadMatch := regexp.MustCompile(`<link[^>]*rel="preload"[^>]*>`).FindString(page)
		if preloadMatch == "" {
			t.Fatalf("expected a preload link, got %q", page)
		}
		for _, expected := range []string{`as="font"`, `type="font/woff2"`, "/fonts/body-"} {
			if !strings.Contains(preloadMatch, expected) {
				t.Errorf("expected preload link to contain %q, got %q", expected, preloadMatch)
			}
		}

		imagePath, _ := readOutput(`^images/logo\.[0-9a-f]+\.svg$`)
		if !strings.Contains(page, "/"+imagePath) {
			t.Errorf("expected the page to reference %s, got %q", imagePath, page)
		}

		if strings.Contains(page, "\n") {
			t.Error("expected minified html output")
		}
	})
}

func TestBuildSplitting(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root)

	outputDirectory := filepath.Join(root, "dist")
	writtenPaths, err := Build(
		&buildConfig.Config{
			SourceDirectory: filepath.Join(root, "src"),
			OutputDirectory: outputDirectory,
			PublicPath:      "/",
			Splitting:       true,
		},
	)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	readOutput := func(path string) string {
		t.Helper()
		contents, err := os.ReadFile(filepath.Join(outputDirectory, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("read output: %v", err)
		}
		return string(contents)
	}

	findOutput := func(condition func(string) bool) string {
		t.Helper()
		for _, writtenPath := range writtenPaths {
			if condition(writtenPath) {
				return writtenPath
			}
		}
		return ""
	}

	entryPattern := regexp.MustCompile(`^scripts/index-[A-Z0-9]+\.js$`)
	chunkPattern := regexp.MustCompile(`^scripts/chunk-[A-Z0-9]+\.js$`)

	entryPath := findOutput(entryPattern.MatchString)
	if entryPath == "" {
		t.Fatalf("no entry output in %v", writtenPaths)
	}

	var lazyChunkPath, sharedChunkPath string
	for _, writtenPath := range writtenPaths {
		if !chunkPattern.MatchString(writtenPath) {
			continue
		}
		contents := readOutput(writtenPath)
		if strings.Contains(contents, "lazy-extra-marker") {
			lazyChunkPath = writtenPath
		}
		if strings.Contains(contents, "shared-code-marker") {
			sharedChunkPath = writtenPath
		}
	}

	t.Run("splits lazy and shared code out of the entry", func(t *testing.T) {
		t.Parallel()
		if lazyChunkPath == "" {
			t.Fatalf("no lazy chunk in %v", writtenPaths)
		}
		if sharedChunkPath == "" {
			t.Fatalf("no shared chunk in %v", writtenPaths)
		}
		entry := readOutput(entryPath)
		if strings.Contains(entry, "lazy-extra-marker") {
			t.Error("expected the lazily imported code to be split out of the entry")
		}
	})

	page := readOutput("index.html")

	t.Run("emits a module script", func(t *testing.T) {
		t.Parallel()
		scriptPattern := regexp.MustCompile(`<script type="module" src="/` + regexp.QuoteMeta(entryPath) + `" integrity="sha384-[^"]+" crossorigin="anonymous">`)
		if !scriptPattern.MatchString(page) {
			t.Errorf("expected a module script tag, got %q", page)
		}
	})

	t.Run("scopes import map integrity to the page's import closure", func(t *testing.T) {
		t.Parallel()
		importMapMatch := regexp.MustCompile(`<script type="importmap">(.*?)</script>`).FindStringSubmatch(page)
		if importMapMatch == nil {
			t.Fatalf("no import map in %q", page)
		}

		var importMap struct {
			Integrity map[string]string `json:"integrity"`
		}
		if err := json.Unmarshal([]byte(importMapMatch[1]), &importMap); err != nil {
			t.Fatalf("json unmarshal import map: %v", err)
		}

		expectedPaths := []string{entryPath, sharedChunkPath, lazyChunkPath}
		for _, expectedPath := range expectedPaths {
			expected := integrityAttribute([]byte(readOutput(expectedPath)))
			if importMap.Integrity["/"+expectedPath] != expected {
				t.Errorf("import map integrity mismatch for %s", expectedPath)
			}
		}
		if len(importMap.Integrity) != len(expectedPaths) {
			t.Errorf("expected %d import map entries, got %v", len(expectedPaths), importMap.Integrity)
		}

		otherEntryPath := findOutput(regexp.MustCompile(`^scripts/other-[A-Z0-9]+\.js$`).MatchString)
		if otherEntryPath == "" {
			t.Fatalf("no other entry output in %v", writtenPaths)
		}
		if _, ok := importMap.Integrity["/"+otherEntryPath]; ok {
			t.Error("expected the other entry to be absent from the page's import map")
		}

		if page[strings.Index(page, "importmap")] == 0 || strings.Index(page, "importmap") > strings.Index(page, "<script type=\"module\"") {
			t.Error("expected the import map before the module script")
		}
	})

	t.Run("preloads the static import closure but not lazy chunks", func(t *testing.T) {
		t.Parallel()
		preloadPattern := regexp.MustCompile(`<link rel="modulepreload" href="([^"]+)" integrity="(sha384-[^"]+)" crossorigin="anonymous"/?>`)
		preloadedPaths := make(map[string]string)
		for _, match := range preloadPattern.FindAllStringSubmatch(page, -1) {
			preloadedPaths[match[1]] = match[2]
		}

		sharedIntegrity, ok := preloadedPaths["/"+sharedChunkPath]
		if !ok {
			t.Fatalf("expected a modulepreload for the shared chunk, got %v", preloadedPaths)
		}
		if sharedIntegrity != integrityAttribute([]byte(readOutput(sharedChunkPath))) {
			t.Error("modulepreload integrity mismatch for the shared chunk")
		}

		if _, ok := preloadedPaths["/"+lazyChunkPath]; ok {
			t.Error("expected the lazy chunk not to be preloaded")
		}
	})
}

func TestBuildSplittingSkipModulePreload(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root)

	outputDirectory := filepath.Join(root, "dist")
	writtenPaths, err := Build(
		&buildConfig.Config{
			SourceDirectory:   filepath.Join(root, "src"),
			OutputDirectory:   outputDirectory,
			PublicPath:        "/",
			Splitting:         true,
			SkipModulePreload: true,
		},
	)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	readOutput := func(path string) string {
		t.Helper()
		contents, err := os.ReadFile(filepath.Join(outputDirectory, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("read output: %v", err)
		}
		return string(contents)
	}

	entryPattern := regexp.MustCompile(`^scripts/index-[A-Z0-9]+\.js$`)
	chunkPattern := regexp.MustCompile(`^scripts/chunk-[A-Z0-9]+\.js$`)

	var entryPath string
	var chunkPaths []string
	for _, writtenPath := range writtenPaths {
		if entryPattern.MatchString(writtenPath) {
			entryPath = writtenPath
		}
		if chunkPattern.MatchString(writtenPath) {
			chunkPaths = append(chunkPaths, writtenPath)
		}
	}
	if entryPath == "" {
		t.Fatalf("no entry output in %v", writtenPaths)
	}
	if len(chunkPaths) == 0 {
		t.Fatalf("no chunk outputs in %v", writtenPaths)
	}

	page := readOutput("index.html")

	t.Run("emits no modulepreload links", func(t *testing.T) {
		t.Parallel()
		if strings.Contains(page, "modulepreload") {
			t.Errorf("expected no modulepreload link, got %q", page)
		}
	})

	t.Run("still emits the module script with integrity", func(t *testing.T) {
		t.Parallel()
		scriptPattern := regexp.MustCompile(
			`<script type="module" src="/` + regexp.QuoteMeta(entryPath) + `" integrity="sha384-[^"]+" crossorigin="anonymous">`,
		)
		if !scriptPattern.MatchString(page) {
			t.Errorf("expected a module script tag, got %q", page)
		}
	})

	// Skipping the links must not cost integrity coverage: it is the import map
	// that the chunks are meant to get their integrity from.
	t.Run("keeps import map integrity for the entry and every chunk", func(t *testing.T) {
		t.Parallel()
		importMapMatch := regexp.MustCompile(`<script type="importmap">(.*?)</script>`).FindStringSubmatch(page)
		if importMapMatch == nil {
			t.Fatalf("no import map in %q", page)
		}

		var importMap struct {
			Integrity map[string]string `json:"integrity"`
		}
		if err := json.Unmarshal([]byte(importMapMatch[1]), &importMap); err != nil {
			t.Fatalf("json unmarshal import map: %v", err)
		}

		for _, expectedPath := range append([]string{entryPath}, chunkPaths...) {
			expected := integrityAttribute([]byte(readOutput(expectedPath)))
			if importMap.Integrity["/"+expectedPath] != expected {
				t.Errorf("import map integrity mismatch for %s", expectedPath)
			}
		}
	})
}

func TestBuildSourceMaps(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root)

	outputDirectory := filepath.Join(root, "dist")
	writtenPaths, err := Build(
		&buildConfig.Config{
			SourceDirectory: filepath.Join(root, "src"),
			OutputDirectory: outputDirectory,
			SourceMaps:      true,
		},
	)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	for _, pattern := range []string{
		`^scripts/index-[A-Z0-9]+\.js\.map$`,
		`^styles/index-[A-Z0-9]+\.css\.map$`,
	} {
		compiledPattern := regexp.MustCompile(pattern)
		found := false
		for _, writtenPath := range writtenPaths {
			if compiledPattern.MatchString(writtenPath) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no output matching %s in %v", pattern, writtenPaths)
		}
	}
}

func TestBuildRelativeDirectories(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root)

	// esbuild reports absolute output paths, which must not break relative
	// source and output directory configurations.
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	relativeRoot, err := filepath.Rel(workingDirectory, root)
	if err != nil {
		t.Skipf("cannot make %s relative to %s: %v", root, workingDirectory, err)
	}

	writtenPaths, err := Build(
		&buildConfig.Config{
			SourceDirectory: filepath.Join(relativeRoot, "src"),
			OutputDirectory: filepath.Join(relativeRoot, "dist"),
		},
	)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	for _, pattern := range []string{`^scripts/index-[A-Z0-9]+\.js$`, `^index\.html$`} {
		compiledPattern := regexp.MustCompile(pattern)
		found := false
		for _, writtenPath := range writtenPaths {
			if compiledPattern.MatchString(writtenPath) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no output matching %s in %v", pattern, writtenPaths)
		}
	}
}

func TestBuildValidation(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name          string
		configuration *buildConfig.Config
	}{
		{name: "nil configuration", configuration: nil},
		{name: "empty source directory", configuration: &buildConfig.Config{OutputDirectory: "dist"}},
		{name: "empty output directory", configuration: &buildConfig.Config{SourceDirectory: "src"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Build(testCase.configuration); err == nil {
				t.Error("expected an error")
			}
		})
	}
}
