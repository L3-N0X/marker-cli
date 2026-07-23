package selfhosted

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/l3-n0x/marker-cli/internal/converter"
)

func TestConvert(t *testing.T) {
	img := base64.StdEncoding.EncodeToString([]byte("img-bytes"))
	var gotField, gotExtractImages string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/convert":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("parsing multipart: %v", err)
			}
			if _, ok := r.MultipartForm.File["pdf_file"]; ok {
				gotField = "pdf_file"
			}
			gotExtractImages = r.FormValue("extract_images")
			json.NewEncoder(w).Encode(map[string]any{
				"status": "Success",
				"result": map[string]any{
					"markdown": "# Doc\n\n![](x.png)",
					"images":   map[string]string{"x.png": img},
					"metadata": map[string]any{"pages": 1},
				},
			})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	pdf := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-1.4 fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	conv := New(srv.URL)
	if err := conv.TestConnection(context.Background()); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}

	res, err := conv.Convert(context.Background(), converter.Request{Path: pdf, Extract: converter.ExtractAll}, nil)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	if gotField != "pdf_file" {
		t.Errorf("file field = %q, want pdf_file", gotField)
	}
	if gotExtractImages != "true" {
		t.Errorf("extract_images = %q, want true", gotExtractImages)
	}
	if res.Markdown != "# Doc\n\n![](x.png)" {
		t.Errorf("markdown = %q", res.Markdown)
	}
	if string(res.Images["x.png"]) != "img-bytes" {
		t.Errorf("image bytes = %q", res.Images["x.png"])
	}
}

func TestExtractTextDisablesImages(t *testing.T) {
	var gotExtractImages string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/convert" {
			r.ParseMultipartForm(1 << 20)
			gotExtractImages = r.FormValue("extract_images")
			json.NewEncoder(w).Encode(map[string]any{
				"status": "Success",
				"result": map[string]any{"markdown": "text", "images": map[string]string{}},
			})
		}
	}))
	defer srv.Close()

	pdf := filepath.Join(t.TempDir(), "doc.pdf")
	os.WriteFile(pdf, []byte("%PDF"), 0o644)

	if _, err := New(srv.URL).Convert(context.Background(), converter.Request{Path: pdf, Extract: converter.ExtractText}, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if gotExtractImages != "false" {
		t.Errorf("extract_images = %q, want false for --extract text", gotExtractImages)
	}
}
