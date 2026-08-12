// Package build orchestrates esbuild and HTML page emission into a production
// site build: TypeScript entries are bundled and minified with hashed names,
// CSS is extracted and minified, assets referenced from CSS and HTML are hashed
// into per-type directories, css/html tagged templates are minified, and each
// HTML page gets stylesheet/script tags with subresource integrity plus
// optional font preload links.
package build

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	motmedelErrors "github.com/Motmedel/utils_go/pkg/errors"
	"github.com/Motmedel/utils_go/pkg/errors/types/empty_error"
	"github.com/Motmedel/utils_go/pkg/errors/types/nil_error"
	buildConfig "github.com/altshiftab/web_build/pkg/build/types/config"
	"github.com/altshiftab/web_build/pkg/html_minifier"
	"github.com/altshiftab/web_build/pkg/lit_minifier"
	"github.com/altshiftab/web_build/pkg/svg_minifier"
	"github.com/evanw/esbuild/pkg/api"
	"golang.org/x/net/html"
)

const (
	scriptsDirectoryName   = "scripts"
	stylesDirectoryName    = "styles"
	fontsDirectoryName     = "fonts"
	imagesDirectoryName    = "images"
	documentsDirectoryName = "documents"
	temporaryAssetsPrefix  = "assets/"

	woff2Extension = ".woff2"
	woffExtension  = ".woff"

	crossOriginAnonymous     = "anonymous"
	crossOriginAttributeName = "crossorigin"
)

var (
	ErrNoEntrypointForChunk = errors.New("no entrypoint found for chunk name")
	ErrNoHeadElement        = errors.New("no head element in template")
	ErrEsbuildTransform     = errors.New("esbuild transform failed")
	ErrEsbuildBuild         = errors.New("esbuild build failed")
	ErrModuleAssetNotFound  = errors.New("module asset not found in any node_modules directory")
)

const transformTsconfigRaw = `{"compilerOptions":{"experimentalDecorators":true,"useDefineForClassFields":false}}`

var extensionDirectories = map[string]string{
	woff2Extension: fontsDirectoryName,
	woffExtension:  fontsDirectoryName,
	".ttf":         fontsDirectoryName,
	".otf":         fontsDirectoryName,
	".png":         imagesDirectoryName,
	".svg":         imagesDirectoryName,
	".jpg":         imagesDirectoryName,
	".jpeg":        imagesDirectoryName,
	".gif":         imagesDirectoryName,
	".avif":        imagesDirectoryName,
	".pdf":         documentsDirectoryName,
}

var fontMimeTypes = map[string]string{
	woff2Extension: "font/woff2",
	woffExtension:  "font/woff",
	".ttf":         "font/ttf",
	".otf":         "font/otf",
}

var iconRelValues = map[string]struct{}{
	"icon":             {},
	"mask-icon":        {},
	"apple-touch-icon": {},
}

var pdfHrefRegexp = regexp.MustCompile(`(?i)\.pdf($|\?)`)

var transformLoaders = map[string]api.Loader{
	".ts":  api.LoaderTS,
	".mts": api.LoaderTS,
	".cts": api.LoaderTS,
}

type page struct {
	templatePath string
	outputName   string
	chunkName    string
}

type metafileOutput struct {
	EntryPoint string `json:"entryPoint,omitzero"`
	CssBundle  string `json:"cssBundle,omitzero"`
}

type metafile struct {
	Outputs map[string]*metafileOutput `json:"outputs"`
}

// litPlugin transpiles TypeScript and minifies css/html tagged templates before
// esbuild parses each module, mirroring the esbuild-loader + minify-lit chain.
func litPlugin() api.Plugin {
	return api.Plugin{
		Name: "minify_lit",
		Setup: func(pluginBuild api.PluginBuild) {
			pluginBuild.OnLoad(
				api.OnLoadOptions{Filter: `\.(ts|mts|cts|js|mjs|cjs)$`},
				func(args api.OnLoadArgs) (api.OnLoadResult, error) {
					data, err := os.ReadFile(args.Path)
					if err != nil {
						return api.OnLoadResult{}, motmedelErrors.NewWithTrace(
							fmt.Errorf("os read file: %w", err),
							args.Path,
						)
					}
					source := string(data)

					if loader, ok := transformLoaders[filepath.Ext(args.Path)]; ok && !strings.HasSuffix(args.Path, ".d.ts") {
						result := api.Transform(source, api.TransformOptions{
							Loader:      loader,
							Target:      api.ESNext,
							Sourcefile:  args.Path,
							TsconfigRaw: transformTsconfigRaw,
						})
						if len(result.Errors) != 0 {
							return api.OnLoadResult{}, motmedelErrors.NewWithTrace(
								fmt.Errorf(
									"%w: %s",
									ErrEsbuildTransform,
									strings.Join(api.FormatMessages(result.Errors, api.FormatMessagesOptions{}), "\n"),
								),
								args.Path,
							)
						}
						source = string(result.Code)
					}

					minified, err := lit_minifier.Minify(source)
					if err != nil {
						return api.OnLoadResult{}, motmedelErrors.New(
							fmt.Errorf("lit minifier minify: %w", err),
							args.Path,
						)
					}

					return api.OnLoadResult{
						Contents:   &minified,
						Loader:     api.LoaderJS,
						ResolveDir: filepath.Dir(args.Path),
					}, nil
				},
			)
		},
	}
}

func discoverEntryPoints(configuration *buildConfig.Config) ([]api.EntryPoint, error) {
	pattern := filepath.Join(configuration.SourceDirectory, scriptsDirectoryName, "*.ts")
	scriptPaths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, motmedelErrors.NewWithTrace(fmt.Errorf("filepath glob: %w", err), pattern)
	}

	var entryPoints []api.EntryPoint
	for _, scriptPath := range scriptPaths {
		if strings.HasSuffix(scriptPath, ".d.ts") {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(scriptPath), ".ts")
		entryPoints = append(entryPoints, api.EntryPoint{
			InputPath:  scriptPath,
			OutputPath: scriptsDirectoryName + "/" + name,
		})
	}

	extraEntryNames := slices.Sorted(maps.Keys(configuration.ExtraEntries))
	for _, name := range extraEntryNames {
		inputPath := configuration.ExtraEntries[name]
		outputDirectory := scriptsDirectoryName
		if strings.HasSuffix(inputPath, ".css") {
			outputDirectory = stylesDirectoryName
		}
		entryPoints = append(entryPoints, api.EntryPoint{
			InputPath:  inputPath,
			OutputPath: outputDirectory + "/" + name,
		})
	}

	return entryPoints, nil
}

func discoverPages(configuration *buildConfig.Config) ([]*page, error) {
	var pages []*page

	err := filepath.WalkDir(configuration.SourceDirectory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}

		relativePath, err := filepath.Rel(configuration.SourceDirectory, path)
		if err != nil {
			return motmedelErrors.NewWithTrace(fmt.Errorf("filepath rel: %w", err), path)
		}

		pages = append(pages, &page{
			templatePath: path,
			outputName:   filepath.ToSlash(relativePath),
			chunkName: strings.ReplaceAll(
				filepath.ToSlash(strings.TrimSuffix(relativePath, ".html")),
				"/",
				"_",
			),
		})
		return nil
	})
	if err != nil {
		return nil, motmedelErrors.New(fmt.Errorf("filepath walk dir: %w", err), configuration.SourceDirectory)
	}

	return pages, nil
}

func integrityAttribute(contents []byte) string {
	digest := sha512.Sum384(contents)
	return "sha384-" + base64.StdEncoding.EncodeToString(digest[:])
}

func contentHash(contents []byte) string {
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])[:20]
}

func makeElement(tagName string, attributes ...html.Attribute) *html.Node {
	return &html.Node{Type: html.ElementNode, Data: tagName, Attr: attributes}
}

func findElement(node *html.Node, tagName string) *html.Node {
	if node.Type == html.ElementNode && node.Data == tagName {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findElement(child, tagName); found != nil {
			return found
		}
	}
	return nil
}

func attributeValue(node *html.Node, name string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return attribute.Val
		}
	}
	return ""
}

func setAttributeValue(node *html.Node, name string, value string) {
	for i := range node.Attr {
		if node.Attr[i].Key == name {
			node.Attr[i].Val = value
			return
		}
	}
	node.Attr = append(node.Attr, html.Attribute{Key: name, Val: value})
}

func isProcessableReference(value string) bool {
	if value == "" {
		return false
	}
	for _, prefix := range []string{"http://", "https://", "//", "/", "data:", "mailto:", "tel:", "#"} {
		if strings.HasPrefix(value, prefix) {
			return false
		}
	}
	return true
}

type builder struct {
	configuration *buildConfig.Config
	publicPath    string
	outputs       map[string][]byte
	// emittedAssets maps source file paths of HTML-referenced assets to their
	// output paths, deduplicating across pages.
	emittedAssets map[string]string
}

// emitFileAsset hashes and stores a file referenced from an HTML template,
// returning its output path.
func (b *builder) emitFileAsset(sourcePath string) (string, error) {
	if outputPath, ok := b.emittedAssets[sourcePath]; ok {
		return outputPath, nil
	}

	// #nosec G703 -- reading files referenced from the user's own templates is the point.
	contents, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", motmedelErrors.NewWithTrace(fmt.Errorf("os read file: %w", err), sourcePath)
	}

	extension := strings.ToLower(filepath.Ext(sourcePath))
	if extension == ".svg" {
		contents = []byte(svg_minifier.Minify(string(contents)))
	}

	directory, ok := extensionDirectories[extension]
	if !ok {
		directory = imagesDirectoryName
	}

	name := strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath))
	outputPath := directory + "/" + name + "." + contentHash(contents) + extension

	b.outputs[outputPath] = contents
	b.emittedAssets[sourcePath] = outputPath
	return outputPath, nil
}

// resolveReference resolves a template asset reference to a file path. A "~"
// prefix resolves against the nearest node_modules directory (html-loader
// compatibility); anything else is relative to the template.
func (b *builder) resolveReference(value string, templateDirectory string) (string, error) {
	if !strings.HasPrefix(value, "~") {
		return filepath.Join(templateDirectory, value), nil
	}

	modulePath := strings.TrimPrefix(value, "~")
	directory, err := filepath.Abs(templateDirectory)
	if err != nil {
		return "", motmedelErrors.NewWithTrace(fmt.Errorf("filepath abs: %w", err), templateDirectory)
	}

	for {
		candidate := filepath.Join(directory, "node_modules", filepath.FromSlash(modulePath))
		// #nosec G703 -- probing for files referenced from the user's own templates is the point.
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", motmedelErrors.NewWithTrace(fmt.Errorf("%w: %s", ErrModuleAssetNotFound, value))
		}
		directory = parent
	}
}

// rewriteReferences rewrites relative asset references in a parsed template:
// img[src], link[href] for icon-like rel values, and a[href] for pdf targets.
func (b *builder) rewriteReferences(node *html.Node, templateDirectory string) error {
	if node.Type == html.ElementNode {
		var attributeName string
		switch node.Data {
		case "img":
			attributeName = "src"
		case "link":
			if _, ok := iconRelValues[attributeValue(node, "rel")]; ok {
				attributeName = "href"
			}
		case "a":
			if pdfHrefRegexp.MatchString(attributeValue(node, "href")) {
				attributeName = "href"
			}
		}

		if attributeName != "" {
			if value := attributeValue(node, attributeName); isProcessableReference(value) {
				sourcePath, err := b.resolveReference(value, templateDirectory)
				if err != nil {
					return fmt.Errorf("resolve reference: %w", err)
				}
				outputPath, err := b.emitFileAsset(sourcePath)
				if err != nil {
					return fmt.Errorf("emit file asset: %w", err)
				}
				setAttributeValue(node, attributeName, b.publicPath+outputPath)
			}
		}
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if err := b.rewriteReferences(child, templateDirectory); err != nil {
			return err
		}
	}
	return nil
}

func (b *builder) preloadTags() []*html.Node {
	preloadFonts := b.configuration.PreloadFonts
	if preloadFonts == nil {
		return nil
	}

	// The emitted names are sorted to keep the output deterministic.
	outputPaths := slices.Sorted(maps.Keys(b.outputs))

	var tags []*html.Node
	for _, outputPath := range outputPaths {
		if !preloadFonts.MatchString(outputPath) {
			continue
		}
		mimeType, ok := fontMimeTypes[strings.ToLower(filepath.Ext(outputPath))]
		if !ok {
			continue
		}
		tags = append(tags, makeElement(
			"link",
			html.Attribute{Key: "rel", Val: "preload"},
			html.Attribute{Key: "as", Val: "font"},
			html.Attribute{Key: "type", Val: mimeType},
			html.Attribute{Key: "href", Val: b.publicPath + outputPath},
			html.Attribute{Key: crossOriginAttributeName, Val: crossOriginAnonymous},
		))
	}
	return tags
}

// pageChunkFiles returns the js and css output paths of the entry named by the
// page's chunk name.
func (b *builder) pageChunkFiles(chunkName string, parsedMetafile *metafile) (string, string, error) {
	scriptPattern := regexp.MustCompile(
		"^" + scriptsDirectoryName + "/" + regexp.QuoteMeta(chunkName) + `-[A-Z0-9]+\.js$`,
	)

	for outputPath, output := range parsedMetafile.Outputs {
		if output.EntryPoint != "" && scriptPattern.MatchString(outputPath) {
			return outputPath, output.CssBundle, nil
		}
	}

	return "", "", motmedelErrors.NewWithTrace(fmt.Errorf("%w: %s", ErrNoEntrypointForChunk, chunkName))
}

func (b *builder) processPage(currentPage *page, parsedMetafile *metafile) error {
	templateContents, err := os.ReadFile(currentPage.templatePath)
	if err != nil {
		return motmedelErrors.NewWithTrace(fmt.Errorf("os read file: %w", err), currentPage.templatePath)
	}

	document, err := html.Parse(bytes.NewReader(templateContents))
	if err != nil {
		return motmedelErrors.NewWithTrace(fmt.Errorf("html parse: %w", err), currentPage.templatePath)
	}

	if err := b.rewriteReferences(document, filepath.Dir(currentPage.templatePath)); err != nil {
		return fmt.Errorf("rewrite references: %w", err)
	}

	headElement := findElement(document, "head")
	if headElement == nil {
		return motmedelErrors.NewWithTrace(fmt.Errorf("%w: %s", ErrNoHeadElement, currentPage.templatePath))
	}

	scriptPath, cssPath, err := b.pageChunkFiles(currentPage.chunkName, parsedMetafile)
	if err != nil {
		return fmt.Errorf("page chunk files (page %s): %w", currentPage.outputName, err)
	}

	var injectedTags []*html.Node
	injectedTags = append(injectedTags, b.preloadTags()...)

	if cssPath != "" {
		injectedTags = append(injectedTags, makeElement(
			"link",
			html.Attribute{Key: "rel", Val: "stylesheet"},
			html.Attribute{Key: "href", Val: b.publicPath + cssPath},
			html.Attribute{Key: "integrity", Val: integrityAttribute(b.outputs[cssPath])},
			html.Attribute{Key: crossOriginAttributeName, Val: crossOriginAnonymous},
		))
	}

	scriptElement := makeElement(
		"script",
		html.Attribute{Key: "defer", Val: ""},
		html.Attribute{Key: "src", Val: b.publicPath + scriptPath},
		html.Attribute{Key: "integrity", Val: integrityAttribute(b.outputs[scriptPath])},
		html.Attribute{Key: crossOriginAttributeName, Val: crossOriginAnonymous},
	)
	injectedTags = append(injectedTags, scriptElement)

	for _, tag := range injectedTags {
		headElement.AppendChild(tag)
	}

	var renderedDocument bytes.Buffer
	if err := html.Render(&renderedDocument, document); err != nil {
		return motmedelErrors.NewWithTrace(fmt.Errorf("html render: %w", err), currentPage.outputName)
	}

	b.outputs[currentPage.outputName] = []byte(html_minifier.Minify(renderedDocument.String()))
	return nil
}

// Build performs a production build and returns the relative paths of the
// written files.
func Build(configuration *buildConfig.Config) ([]string, error) {
	if configuration == nil {
		return nil, nil_error.New("configuration")
	}
	if configuration.SourceDirectory == "" {
		return nil, empty_error.New("source directory")
	}
	if configuration.OutputDirectory == "" {
		return nil, empty_error.New("output directory")
	}

	publicPath := configuration.PublicPath
	if publicPath == "" {
		publicPath = "/"
	}
	if !strings.HasSuffix(publicPath, "/") {
		publicPath += "/"
	}

	entryPoints, err := discoverEntryPoints(configuration)
	if err != nil {
		return nil, fmt.Errorf("discover entry points: %w", err)
	}
	if len(entryPoints) == 0 {
		return nil, empty_error.New("entry points")
	}

	pages, err := discoverPages(configuration)
	if err != nil {
		return nil, fmt.Errorf("discover pages: %w", err)
	}

	buildResult := api.Build(api.BuildOptions{
		EntryPointsAdvanced: entryPoints,
		Bundle:              true,
		Outdir:              configuration.OutputDirectory,
		EntryNames:          "[dir]/[name]-[hash]",
		AssetNames:          temporaryAssetsPrefix + "[name]-[hash]",
		PublicPath:          publicPath,
		Format:              api.FormatIIFE,
		Platform:            api.PlatformBrowser,
		Target:              api.ESNext,
		MinifyWhitespace:    true,
		MinifyIdentifiers:   true,
		MinifySyntax:        true,
		Sourcemap:           api.SourceMapLinked,
		LegalComments:       api.LegalCommentsLinked,
		Metafile:            true,
		Write:               false,
		Loader: map[string]api.Loader{
			".woff2": api.LoaderFile,
			".woff":  api.LoaderFile,
			".ttf":   api.LoaderFile,
			".otf":   api.LoaderFile,
			".png":   api.LoaderFile,
			".svg":   api.LoaderFile,
			".jpg":   api.LoaderFile,
			".jpeg":  api.LoaderFile,
			".gif":   api.LoaderFile,
			".avif":  api.LoaderFile,
			".pdf":   api.LoaderFile,
		},
		Plugins: []api.Plugin{litPlugin()},
	})
	if len(buildResult.Errors) != 0 {
		return nil, motmedelErrors.NewWithTrace(fmt.Errorf(
			"%w: %s",
			ErrEsbuildBuild,
			strings.Join(api.FormatMessages(buildResult.Errors, api.FormatMessagesOptions{}), "\n"),
		))
	}
	for _, warning := range api.FormatMessages(buildResult.Warnings, api.FormatMessagesOptions{}) {
		slog.Warn("An esbuild warning occurred.", slog.String("warning", warning))
	}

	var parsedMetafile metafile
	if err := json.Unmarshal([]byte(buildResult.Metafile), &parsedMetafile); err != nil {
		return nil, motmedelErrors.NewWithTrace(fmt.Errorf("json unmarshal metafile: %w", err))
	}

	b := &builder{
		configuration: configuration,
		publicPath:    publicPath,
		outputs:       make(map[string][]byte),
		emittedAssets: make(map[string]string),
	}

	for _, file := range buildResult.OutputFiles {
		relativePath, err := filepath.Rel(configuration.OutputDirectory, file.Path)
		if err != nil {
			return nil, motmedelErrors.NewWithTrace(fmt.Errorf("filepath rel: %w", err), file.Path)
		}
		b.outputs[filepath.ToSlash(relativePath)] = file.Contents
	}

	// The metafile references outputs by paths relative to the working directory.
	absoluteOutputDirectory, err := filepath.Abs(configuration.OutputDirectory)
	if err != nil {
		return nil, motmedelErrors.NewWithTrace(fmt.Errorf("filepath abs: %w", err), configuration.OutputDirectory)
	}
	makeOutputRelative := func(metafilePath string) (string, error) {
		absolutePath, err := filepath.Abs(metafilePath)
		if err != nil {
			return "", motmedelErrors.NewWithTrace(fmt.Errorf("filepath abs: %w", err), metafilePath)
		}
		relativePath, err := filepath.Rel(absoluteOutputDirectory, absolutePath)
		if err != nil {
			return "", motmedelErrors.NewWithTrace(fmt.Errorf("filepath rel: %w", err), absolutePath)
		}
		return filepath.ToSlash(relativePath), nil
	}

	normalizedOutputs := make(map[string]*metafileOutput)
	for outputPath, output := range parsedMetafile.Outputs {
		relativePath, err := makeOutputRelative(outputPath)
		if err != nil {
			return nil, fmt.Errorf("make output relative: %w", err)
		}
		if output.CssBundle != "" {
			if output.CssBundle, err = makeOutputRelative(output.CssBundle); err != nil {
				return nil, fmt.Errorf("make output relative: %w", err)
			}
		}
		normalizedOutputs[relativePath] = output
	}
	parsedMetafile.Outputs = normalizedOutputs

	if err := b.relocateOutputs(&parsedMetafile); err != nil {
		return nil, fmt.Errorf("relocate outputs: %w", err)
	}

	// SVG assets loaded through esbuild are minified before hashing is settled
	// elsewhere, so only content is updated here; their names keep esbuild's hash.
	for outputPath, contents := range b.outputs {
		if strings.HasSuffix(outputPath, ".svg") {
			b.outputs[outputPath] = []byte(svg_minifier.Minify(string(contents)))
		}
	}

	for _, currentPage := range pages {
		if err := b.processPage(currentPage, &parsedMetafile); err != nil {
			return nil, fmt.Errorf("process page: %w", err)
		}
	}

	if err := os.RemoveAll(configuration.OutputDirectory); err != nil {
		return nil, motmedelErrors.NewWithTrace(fmt.Errorf("os remove all: %w", err), configuration.OutputDirectory)
	}
	writtenPaths := make([]string, 0, len(b.outputs))
	for outputPath, contents := range b.outputs {
		fullPath := filepath.Join(configuration.OutputDirectory, filepath.FromSlash(outputPath))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
			return nil, motmedelErrors.NewWithTrace(fmt.Errorf("os mkdir all: %w", err), fullPath)
		}
		if err := os.WriteFile(fullPath, contents, 0o600); err != nil {
			return nil, motmedelErrors.NewWithTrace(fmt.Errorf("os write file: %w", err), fullPath)
		}
		writtenPaths = append(writtenPaths, outputPath)
	}

	sort.Strings(writtenPaths)
	return writtenPaths, nil
}

// relocateOutputs moves css bundles of script entries from scripts/ to styles/
// and routes esbuild file-loader assets into per-type directories, rewriting
// references in text outputs accordingly.
func (b *builder) relocateOutputs(parsedMetafile *metafile) error {
	renames := make(map[string]string)

	for outputPath := range b.outputs {
		switch {
		case strings.HasPrefix(outputPath, scriptsDirectoryName+"/") &&
			(strings.HasSuffix(outputPath, ".css") ||
				strings.HasSuffix(outputPath, ".css.map") ||
				strings.HasSuffix(outputPath, ".css.LEGAL.txt")):
			renames[outputPath] = stylesDirectoryName + "/" + strings.TrimPrefix(outputPath, scriptsDirectoryName+"/")
		case strings.HasPrefix(outputPath, temporaryAssetsPrefix):
			extension := strings.ToLower(filepath.Ext(outputPath))
			directory, ok := extensionDirectories[extension]
			if !ok {
				directory = imagesDirectoryName
			}
			renames[outputPath] = directory + "/" + strings.TrimPrefix(outputPath, temporaryAssetsPrefix)
		}
	}

	for oldPath, newPath := range renames {
		b.outputs[newPath] = b.outputs[oldPath]
		delete(b.outputs, oldPath)

		for _, output := range parsedMetafile.Outputs {
			if output.CssBundle == oldPath {
				output.CssBundle = newPath
			}
		}
	}

	// References in text outputs use the public path prefix.
	for outputPath, contents := range b.outputs {
		if !strings.HasSuffix(outputPath, ".js") && !strings.HasSuffix(outputPath, ".css") {
			continue
		}
		for oldPath, newPath := range renames {
			contents = bytes.ReplaceAll(
				contents,
				[]byte(b.publicPath+oldPath),
				[]byte(b.publicPath+newPath),
			)
		}
		b.outputs[outputPath] = contents
	}

	return nil
}
