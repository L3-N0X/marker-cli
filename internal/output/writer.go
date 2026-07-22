// Package output turns a conversion Result into files on disk. It is a port
// of the Obsidian plugin's src/utils/fileUtils.ts, with vault operations
// replaced by ordinary filesystem calls.
package output

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/l3-n0x/marker-cli/internal/converter"
)

// Options control where and how the result is written.
type Options struct {
	// Output is the raw -o value: a path ending in .md means "write exactly
	// this file", anything else is treated as a parent directory.
	Output string

	Extract         converter.Extract
	AssetsSubfolder bool
	Metadata        bool
	MovePDF         bool
	DeleteOriginal  bool
	Force           bool
}

// Layout is the resolved set of destination paths for one conversion.
type Layout struct {
	MarkdownPath string // the .md file to write
	AssetsDir    string // where images go; equals the markdown's dir when no subfolder
	AssetsPrefix string // path prefix used in markdown image links, "" or "assets/"
}

// Written reports what a conversion produced, for the CLI summary.
type Written struct {
	MarkdownPath string
	ImagePaths   []string
}

// mdImage matches a markdown image link: ![alt](target)
var mdImage = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]+)([^)]*)\)`)

// ResolveLayout works out the destination paths for pdfPath under opts.
//
//	-o out.md   -> out.md          + out_assets/   (prefix "out_assets/")
//	-o dir/     -> dir/<stem>/<stem>.md + dir/<stem>/assets/
//
// With AssetsSubfolder disabled, images land next to the markdown file.
func ResolveLayout(pdfPath string, opts Options) Layout {
	stem := Stem(pdfPath)

	if strings.EqualFold(filepath.Ext(opts.Output), ".md") {
		mdPath := opts.Output
		dir := filepath.Dir(mdPath)
		if !opts.AssetsSubfolder {
			return Layout{MarkdownPath: mdPath, AssetsDir: dir}
		}
		name := strings.TrimSuffix(filepath.Base(mdPath), filepath.Ext(mdPath)) + "_assets"
		return Layout{
			MarkdownPath: mdPath,
			AssetsDir:    filepath.Join(dir, name),
			AssetsPrefix: name + "/",
		}
	}

	base := opts.Output
	if base == "" {
		base = "."
	}
	docDir := filepath.Join(base, stem)
	mdPath := filepath.Join(docDir, stem+".md")
	if !opts.AssetsSubfolder {
		return Layout{MarkdownPath: mdPath, AssetsDir: docDir}
	}
	return Layout{
		MarkdownPath: mdPath,
		AssetsDir:    filepath.Join(docDir, "assets"),
		AssetsPrefix: "assets/",
	}
}

// Stem is the PDF's base name without its extension, with dots replaced by
// dashes so the name is safe to use as a directory.
func Stem(pdfPath string) string {
	base := filepath.Base(pdfPath)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return strings.ReplaceAll(base, ".", "-")
}

// CheckDestination reports whether writing pdfPath under opts would clobber an
// existing markdown file. Callers run this before conversion so a refusal
// costs no API calls.
func CheckDestination(pdfPath string, opts Options) error {
	if opts.Force || opts.Extract == converter.ExtractImages {
		return nil
	}
	path := ResolveLayout(pdfPath, opts).MarkdownPath
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists (pass --force to overwrite)", path)
	}
	return nil
}

// Write persists the result and returns what it created.
func Write(pdfPath string, res *converter.Result, opts Options) (*Written, error) {
	layout := ResolveLayout(pdfPath, opts)
	stem := Stem(pdfPath)

	if err := CheckDestination(pdfPath, opts); err != nil {
		return nil, err
	}

	// imageNames maps each image id to the file name it is written under, so
	// markdown links can be rewritten to point at the real file.
	imageNames := make(map[string]string, len(res.Images))
	for id := range res.Images {
		name := id
		if opts.AssetsSubfolder {
			name = stem + "_" + id
		}
		imageNames[id] = name
	}

	written := &Written{MarkdownPath: layout.MarkdownPath}

	if opts.Extract != converter.ExtractText && len(res.Images) > 0 {
		if err := os.MkdirAll(layout.AssetsDir, 0o755); err != nil {
			return nil, fmt.Errorf("creating %s: %w", layout.AssetsDir, err)
		}
		// Sort for deterministic output and a stable summary listing.
		ids := make([]string, 0, len(res.Images))
		for id := range res.Images {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			path := filepath.Join(layout.AssetsDir, imageNames[id])
			if err := os.WriteFile(path, res.Images[id], 0o644); err != nil {
				return nil, fmt.Errorf("writing %s: %w", path, err)
			}
			written.ImagePaths = append(written.ImagePaths, path)
		}
	}

	if opts.Extract != converter.ExtractImages {
		markdown := res.Markdown
		if opts.Extract == converter.ExtractText {
			markdown = StripImages(markdown)
		} else {
			markdown = RewriteImageLinks(markdown, imageNames, layout.AssetsPrefix)
		}
		if opts.Metadata {
			markdown = Frontmatter(res.Metadata) + markdown
		}

		if err := os.MkdirAll(filepath.Dir(layout.MarkdownPath), 0o755); err != nil {
			return nil, fmt.Errorf("creating %s: %w", filepath.Dir(layout.MarkdownPath), err)
		}
		if err := os.WriteFile(layout.MarkdownPath, []byte(markdown), 0o644); err != nil {
			return nil, fmt.Errorf("writing %s: %w", layout.MarkdownPath, err)
		}
	}

	if err := handleOriginal(pdfPath, layout, opts); err != nil {
		return nil, err
	}
	return written, nil
}

// RewriteImageLinks points every markdown image whose target is a known image
// id at the file that image was written to.
func RewriteImageLinks(markdown string, imageNames map[string]string, prefix string) string {
	if len(imageNames) == 0 {
		return markdown
	}
	return mdImage.ReplaceAllStringFunc(markdown, func(match string) string {
		groups := mdImage.FindStringSubmatch(match)
		alt, target, rest := groups[1], groups[2], groups[3]
		name, ok := imageNames[target]
		if !ok {
			return match
		}
		return fmt.Sprintf("![%s](%s%s%s)", alt, prefix, escapePath(name), rest)
	})
}

// StripImages removes every markdown image link, used for --extract text.
func StripImages(markdown string) string {
	return mdImage.ReplaceAllString(markdown, "")
}

// Frontmatter renders metadata as a YAML block for the top of the file.
func Frontmatter(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	keys := make([]string, 0, len(metadata))
	for k := range metadata {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("---\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s: %v\n", k, metadata[k])
	}
	b.WriteString("---\n\n")
	return b.String()
}

// handleOriginal moves or deletes the source PDF when asked to.
func handleOriginal(pdfPath string, layout Layout, opts Options) error {
	if opts.DeleteOriginal {
		if err := os.Remove(pdfPath); err != nil {
			return fmt.Errorf("deleting original PDF: %w", err)
		}
		return nil
	}
	if !opts.MovePDF {
		return nil
	}

	dest := filepath.Join(filepath.Dir(layout.MarkdownPath), filepath.Base(pdfPath))
	absSrc, err := filepath.Abs(pdfPath)
	if err != nil {
		return fmt.Errorf("resolving original PDF path: %w", err)
	}
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return fmt.Errorf("resolving destination path: %w", err)
	}
	if absSrc == absDest {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(dest), err)
	}
	if err := os.Rename(absSrc, absDest); err != nil {
		return fmt.Errorf("moving original PDF: %w", err)
	}
	return nil
}

// escapePath percent-encodes a file name for use inside a markdown link,
// which is mainly about spaces.
func escapePath(name string) string {
	return (&url.URL{Path: name}).EscapedPath()
}
