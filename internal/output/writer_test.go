package output

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/l3-n0x/marker-cli/internal/converter"
)

func TestResolveLayout(t *testing.T) {
	tests := []struct {
		name    string
		pdf     string
		opts    Options
		wantMD  string
		wantDir string
		wantPfx string
	}{
		{
			name:    "directory output creates a document folder",
			pdf:     "papers/My Paper.pdf",
			opts:    Options{Output: "out", AssetsSubfolder: true},
			wantMD:  filepath.Join("out", "My Paper", "My Paper.md"),
			wantDir: filepath.Join("out", "My Paper", "assets"),
			wantPfx: "assets/",
		},
		{
			name:    "explicit .md output writes that exact file",
			pdf:     "papers/paper.pdf",
			opts:    Options{Output: filepath.Join("notes", "read me.md"), AssetsSubfolder: true},
			wantMD:  filepath.Join("notes", "read me.md"),
			wantDir: filepath.Join("notes", "read me_assets"),
			wantPfx: "read me_assets/",
		},
		{
			name:    "without an assets subfolder images sit beside the markdown",
			pdf:     "paper.pdf",
			opts:    Options{Output: "out", AssetsSubfolder: false},
			wantMD:  filepath.Join("out", "paper", "paper.md"),
			wantDir: filepath.Join("out", "paper"),
			wantPfx: "",
		},
		{
			name:    "dots in the name become dashes so the folder is safe",
			pdf:     "v1.2.report.pdf",
			opts:    Options{Output: ".", AssetsSubfolder: true},
			wantMD:  filepath.Join("v1-2-report", "v1-2-report.md"),
			wantDir: filepath.Join("v1-2-report", "assets"),
			wantPfx: "assets/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveLayout(tt.pdf, tt.opts)
			if got.MarkdownPath != tt.wantMD {
				t.Errorf("MarkdownPath = %q, want %q", got.MarkdownPath, tt.wantMD)
			}
			if got.AssetsDir != tt.wantDir {
				t.Errorf("AssetsDir = %q, want %q", got.AssetsDir, tt.wantDir)
			}
			if got.AssetsPrefix != tt.wantPfx {
				t.Errorf("AssetsPrefix = %q, want %q", got.AssetsPrefix, tt.wantPfx)
			}
		})
	}
}

func TestRewriteImageLinks(t *testing.T) {
	names := map[string]string{
		"img-0.jpeg": "my doc_img-0.jpeg",
		"img-1.jpeg": "my doc_img-1.jpeg",
	}

	tests := []struct {
		name     string
		markdown string
		prefix   string
		want     string
	}{
		{
			name:     "known ids are rewritten and spaces escaped",
			markdown: "text\n\n![](img-0.jpeg)\n",
			prefix:   "assets/",
			want:     "text\n\n![](assets/my%20doc_img-0.jpeg)\n",
		},
		{
			name:     "alt text is preserved",
			markdown: "![a figure](img-1.jpeg)",
			prefix:   "assets/",
			want:     "![a figure](assets/my%20doc_img-1.jpeg)",
		},
		{
			name:     "unknown targets are left alone",
			markdown: "![](https://example.com/x.png)",
			prefix:   "assets/",
			want:     "![](https://example.com/x.png)",
		},
		{
			name:     "an empty prefix still rewrites the file name",
			markdown: "![](img-0.jpeg)",
			prefix:   "",
			want:     "![](my%20doc_img-0.jpeg)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RewriteImageLinks(tt.markdown, names, tt.prefix); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStripImages(t *testing.T) {
	got := StripImages("before ![alt](img-0.jpeg) after")
	if want := "before  after"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFrontmatter(t *testing.T) {
	got := Frontmatter(map[string]any{"page_count": 3, "processor": "mistralai-ocr"})
	want := "---\npage_count: 3\nprocessor: mistralai-ocr\n---\n\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if Frontmatter(nil) != "" {
		t.Error("nil metadata should render nothing")
	}
}

func TestWrite(t *testing.T) {
	dir := t.TempDir()
	pdf := filepath.Join(dir, "paper.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := &converter.Result{
		Markdown: "# Title\n\n![](img-0.jpeg)\n",
		Images:   map[string][]byte{"img-0.jpeg": []byte("fake-jpeg")},
		Metadata: map[string]any{"page_count": 1},
	}
	opts := Options{
		Output:          filepath.Join(dir, "out"),
		Extract:         converter.ExtractAll,
		AssetsSubfolder: true,
		Metadata:        true,
	}

	written, err := Write(pdf, res, opts)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	markdown, err := os.ReadFile(written.MarkdownPath)
	if err != nil {
		t.Fatalf("reading markdown: %v", err)
	}
	wantMD := "---\npage_count: 1\n---\n\n# Title\n\n![](assets/paper_img-0.jpeg)\n"
	if string(markdown) != wantMD {
		t.Errorf("markdown = %q, want %q", markdown, wantMD)
	}

	// Every image link must resolve to a file that actually exists.
	imgPath := filepath.Join(filepath.Dir(written.MarkdownPath), "assets", "paper_img-0.jpeg")
	if _, err := os.Stat(imgPath); err != nil {
		t.Errorf("expected image at %s: %v", imgPath, err)
	}

	// A second write must refuse to clobber unless forced.
	if _, err := Write(pdf, res, opts); err == nil {
		t.Error("expected an error when overwriting without --force")
	}
	opts.Force = true
	if _, err := Write(pdf, res, opts); err != nil {
		t.Errorf("--force should allow overwriting: %v", err)
	}
}

func TestWriteTextOnly(t *testing.T) {
	dir := t.TempDir()
	pdf := filepath.Join(dir, "paper.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := &converter.Result{
		Markdown: "# Title\n\n![](img-0.jpeg)\n",
		Images:   map[string][]byte{"img-0.jpeg": []byte("fake-jpeg")},
	}
	written, err := Write(pdf, res, Options{
		Output:          filepath.Join(dir, "out"),
		Extract:         converter.ExtractText,
		AssetsSubfolder: true,
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	markdown, err := os.ReadFile(written.MarkdownPath)
	if err != nil {
		t.Fatal(err)
	}
	if want := "# Title\n\n\n"; string(markdown) != want {
		t.Errorf("markdown = %q, want %q", markdown, want)
	}
	if len(written.ImagePaths) != 0 {
		t.Errorf("text-only should write no images, got %v", written.ImagePaths)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(written.MarkdownPath), "assets")); err == nil {
		t.Error("text-only should not create an assets folder")
	}
}

func TestWriteMovePDF(t *testing.T) {
	dir := t.TempDir()
	pdf := filepath.Join(dir, "paper.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}

	written, err := Write(pdf, &converter.Result{Markdown: "# Title"}, Options{
		Output:  filepath.Join(dir, "out"),
		Extract: converter.ExtractAll,
		MovePDF: true,
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(pdf); err == nil {
		t.Error("original PDF should have been moved")
	}
	moved := filepath.Join(filepath.Dir(written.MarkdownPath), "paper.pdf")
	if _, err := os.Stat(moved); err != nil {
		t.Errorf("expected the PDF at %s: %v", moved, err)
	}
}
