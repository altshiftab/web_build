package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir all: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func writeFixture(t *testing.T, root string) (string, string) {
	t.Helper()

	sourceDirectory := filepath.Join(root, "src")
	distDirectory := filepath.Join(root, "dist")

	for _, iconFileName := range iconFileNames {
		writeTestFile(t, filepath.Join(sourceDirectory, "icons", iconFileName), "png-bytes-"+iconFileName)
	}
	writeTestFile(
		t,
		filepath.Join(sourceDirectory, "sw-template.js"),
		`const VERSION = "__BUILD_HASH__";const PRECACHE = __PRECACHE_URLS__;self.state = [VERSION, PRECACHE];`,
	)

	writeTestFile(t, filepath.Join(distDirectory, "index.html"), "<html></html>")
	writeTestFile(t, filepath.Join(distDirectory, "scripts/index-ABC123.js"), "js")
	writeTestFile(t, filepath.Join(distDirectory, "scripts/index-ABC123.js.map"), "map")
	writeTestFile(t, filepath.Join(distDirectory, "scripts/index-ABC123.js.LEGAL.txt"), "legal")
	writeTestFile(t, filepath.Join(distDirectory, "styles/index-DEF456.css"), "css")

	return sourceDirectory, distDirectory
}

func TestGenerate(t *testing.T) {
	t.Parallel()
	sourceDirectory, distDirectory := writeFixture(t, t.TempDir())

	if err := generate(sourceDirectory, distDirectory); err != nil {
		t.Fatalf("generate: %v", err)
	}

	for _, iconFileName := range iconFileNames {
		contents, err := os.ReadFile(filepath.Join(distDirectory, "icons", iconFileName))
		if err != nil {
			t.Fatalf("read icon: %v", err)
		}
		if string(contents) != "png-bytes-"+iconFileName {
			t.Errorf("unexpected icon contents for %s: %q", iconFileName, contents)
		}
	}

	serviceWorker, err := os.ReadFile(filepath.Join(distDirectory, "sw.js"))
	if err != nil {
		t.Fatalf("read service worker: %v", err)
	}
	serviceWorkerText := string(serviceWorker)

	if strings.Contains(serviceWorkerText, "__BUILD_HASH__") || strings.Contains(serviceWorkerText, "__PRECACHE_URLS__") {
		t.Errorf("expected the template placeholders to be replaced, got %q", serviceWorkerText)
	}
	for _, expected := range []string{`"/"`, `"/scripts/index-ABC123.js"`, `"/styles/index-DEF456.css"`, `"/icons/icon-192.png"`} {
		if !strings.Contains(serviceWorkerText, expected) {
			t.Errorf("expected the precache list to contain %s, got %q", expected, serviceWorkerText)
		}
	}
	for _, unexpected := range []string{".map", ".LEGAL.txt", `"/sw.js"`} {
		if strings.Contains(serviceWorkerText, unexpected) {
			t.Errorf("expected the precache list not to contain %s, got %q", unexpected, serviceWorkerText)
		}
	}
}

func TestComputeVersionChangesWithContent(t *testing.T) {
	t.Parallel()
	sourceDirectory, distDirectory := writeFixture(t, t.TempDir())

	if err := generate(sourceDirectory, distDirectory); err != nil {
		t.Fatalf("generate: %v", err)
	}
	precachePaths, err := collectPrecachePaths(distDirectory)
	if err != nil {
		t.Fatalf("collect precache paths: %v", err)
	}
	firstVersion, err := computeVersion(distDirectory, precachePaths)
	if err != nil {
		t.Fatalf("compute version: %v", err)
	}
	if len(firstVersion) != 12 {
		t.Errorf("expected a 12 character version, got %q", firstVersion)
	}

	// A fixed-name asset changing content must change the version.
	writeTestFile(t, filepath.Join(distDirectory, "index.html"), "<html>changed</html>")
	secondVersion, err := computeVersion(distDirectory, precachePaths)
	if err != nil {
		t.Fatalf("compute version: %v", err)
	}
	if firstVersion == secondVersion {
		t.Error("expected the version to change with asset contents")
	}
}

func TestMakePrecacheUrls(t *testing.T) {
	t.Parallel()
	actual := makePrecacheUrls([]string{"icons/icon-192.png", "index.html", "scripts/a.js"})
	expected := []string{"/icons/icon-192.png", "/", "/scripts/a.js"}
	if !slices.Equal(actual, expected) {
		t.Errorf("expected %q, got %q", expected, actual)
	}
}
