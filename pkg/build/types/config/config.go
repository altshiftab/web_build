// Package config holds the build configuration.
package config

import "regexp"

type Config struct {
	SourceDirectory string
	OutputDirectory string
	PublicPath      string
	PreloadFonts    *regexp.Regexp
	ExtraEntries    map[string]string
	// Splitting emits ES modules with code splitting: dynamic imports become
	// lazily loaded chunks, pages preload their static import closure, and each
	// page gets an import map providing subresource integrity for the chunks it
	// can load.
	Splitting bool
	// SkipModulePreload omits the modulepreload links for the static import
	// closure. The links only save a round trip: the import map already carries
	// integrity for every chunk a page can load, so nothing but load
	// parallelism is lost. Safari 26 drops the integrity metadata of
	// preload-scanned requests and then blocks them under an Integrity-Policy
	// header, which makes skipping the links a way to keep the policy enforced.
	SkipModulePreload bool
	// SourceMaps emits linked .map files alongside the js and css outputs. Off
	// by default: builds are production builds, and source maps expose the
	// original sources to anyone who can fetch the bundles.
	SourceMaps bool
}
