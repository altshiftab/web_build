package build

import (
	"crypto/sha512"
	"encoding/base64"
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
		"src/scripts/index.ts": strings.Join(
			[]string{
				"/**",
				" * @license Fixture-JS-License",
				" */",
				`import "../styles/main.css";`,
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
