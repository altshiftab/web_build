// Package config holds the build configuration.
package config

import "regexp"

type Config struct {
	SourceDirectory string
	OutputDirectory string
	PublicPath      string
	PreloadFonts    *regexp.Regexp
	ExtraEntries    map[string]string
}
