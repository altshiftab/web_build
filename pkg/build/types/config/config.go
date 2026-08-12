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
	// lazily loaded chunks, pages preload their static import closure, and an
	// import map provides subresource integrity for every chunk.
	Splitting bool
}
