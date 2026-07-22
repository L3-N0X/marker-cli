package mistral

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/l3-n0x/marker-cli/internal/converter"
)

func TestDecodeImage(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString([]byte("hello"))

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain base64", raw, "hello"},
		{"data URL prefix is stripped", "data:image/jpeg;base64," + raw, "hello"},
		{"empty stays empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeImage(tt.input)
			if err != nil {
				t.Fatalf("decodeImage: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParsePages(t *testing.T) {
	img := base64.StdEncoding.EncodeToString([]byte("jpeg-bytes"))
	pages := []OCRPage{
		{Index: 0, Markdown: "page one", Images: []OCRImage{{ID: "img-0.jpeg", ImageBase64: img}}},
		{Index: 1, Markdown: "page two"},
	}

	t.Run("paginate inserts a rule between pages", func(t *testing.T) {
		res, err := parsePages(pages, converter.Request{Extract: converter.ExtractAll, Paginate: true})
		if err != nil {
			t.Fatal(err)
		}
		if want := "page one\n\n---\n\npage two"; res.Markdown != want {
			t.Errorf("markdown = %q, want %q", res.Markdown, want)
		}
		if len(res.Images) != 1 {
			t.Errorf("expected 1 image, got %d", len(res.Images))
		}
		if res.Metadata["page_count"] != 2 {
			t.Errorf("page_count = %v, want 2", res.Metadata["page_count"])
		}
	})

	t.Run("without paginate pages are joined by a blank line", func(t *testing.T) {
		res, err := parsePages(pages, converter.Request{Extract: converter.ExtractAll})
		if err != nil {
			t.Fatal(err)
		}
		if want := "page one\n\npage two"; res.Markdown != want {
			t.Errorf("markdown = %q, want %q", res.Markdown, want)
		}
	})

	t.Run("text-only skips images", func(t *testing.T) {
		res, err := parsePages(pages, converter.Request{Extract: converter.ExtractText})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Images) != 0 {
			t.Errorf("expected no images, got %d", len(res.Images))
		}
	})

	t.Run("images-only skips markdown", func(t *testing.T) {
		res, err := parsePages(pages, converter.Request{Extract: converter.ExtractImages})
		if err != nil {
			t.Fatal(err)
		}
		if res.Markdown != "" {
			t.Errorf("expected empty markdown, got %q", res.Markdown)
		}
		if len(res.Images) != 1 {
			t.Errorf("expected 1 image, got %d", len(res.Images))
		}
	})
}

// TestConvertAgainstStubAPI exercises the whole upload -> sign -> OCR ->
// delete flow against a stand-in for the Mistral API.
func TestConvertAgainstStubAPI(t *testing.T) {
	var gotPurpose, gotDocURL string
	var deleted bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/files":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("parsing multipart body: %v", err)
			}
			gotPurpose = r.FormValue("purpose")
			json.NewEncoder(w).Encode(File{ID: "file-123"})

		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/url"):
			if got := r.URL.Query().Get("expiry"); got != "24" {
				t.Errorf("expiry = %q, want 24", got)
			}
			json.NewEncoder(w).Encode(map[string]string{"url": "https://signed.example/doc.pdf"})

		case r.Method == http.MethodPost && r.URL.Path == "/ocr":
			var req OCRRequest
			json.NewDecoder(r.Body).Decode(&req)
			gotDocURL = req.Document.DocumentURL
			img := base64.StdEncoding.EncodeToString([]byte("jpeg"))
			json.NewEncoder(w).Encode(OCRResponse{Pages: []OCRPage{
				{Index: 0, Markdown: "# Hello\n\n![](img-0.jpeg)", Images: []OCRImage{{ID: "img-0.jpeg", ImageBase64: img}}},
			}})

		case r.Method == http.MethodDelete && r.URL.Path == "/files/file-123":
			deleted = true
			json.NewEncoder(w).Encode(map[string]bool{"deleted": true})

		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	pdf := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-1.4 fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	conv := New("test-key")
	conv.client.BaseURL = srv.URL

	res, err := conv.Convert(context.Background(), converter.Request{
		Path:         pdf,
		Extract:      converter.ExtractAll,
		DeleteRemote: true,
	}, nil)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	if gotPurpose != "ocr" {
		t.Errorf("upload purpose = %q, want ocr", gotPurpose)
	}
	if gotDocURL != "https://signed.example/doc.pdf" {
		t.Errorf("OCR document_url = %q", gotDocURL)
	}
	if !deleted {
		t.Error("--delete-remote should have deleted the uploaded file")
	}
	if res.Markdown != "# Hello\n\n![](img-0.jpeg)" {
		t.Errorf("markdown = %q", res.Markdown)
	}
	if string(res.Images["img-0.jpeg"]) != "jpeg" {
		t.Errorf("image bytes = %q", res.Images["img-0.jpeg"])
	}
}

// TestAPIErrorMessage checks that the provider's message reaches the user
// instead of a bare status code.
func TestAPIErrorMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Unauthorized: invalid api key"}`))
	}))
	defer srv.Close()

	conv := New("bad-key")
	conv.client.BaseURL = srv.URL

	err := conv.TestConnection(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("error should carry the API message, got %q", err)
	}
}
