package pythonapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/l3-n0x/marker-cli/internal/converter"
)

func successBody(img string) map[string]any {
	return map[string]any{
		"format":   "markdown",
		"output":   "# Doc\n\n![](p.png)",
		"images":   map[string]string{"p.png": img},
		"metadata": map[string]any{"pages": 1},
		"success":  true,
	}
}

func TestCloudUploads(t *testing.T) {
	img := base64.StdEncoding.EncodeToString([]byte("bytes"))
	var gotPaginate string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/":
			w.WriteHeader(http.StatusOK)
		case "/marker/upload":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("parsing multipart: %v", err)
			}
			if _, ok := r.MultipartForm.File["file"]; !ok {
				t.Error("expected a 'file' part in the upload")
			}
			gotPaginate = r.FormValue("paginate_output")
			json.NewEncoder(w).Encode(successBody(img))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	pdf := filepath.Join(t.TempDir(), "doc.pdf")
	os.WriteFile(pdf, []byte("%PDF"), 0o644)

	conv := NewCloud(srv.URL)
	if conv.Name() != "python-cloud" {
		t.Errorf("Name = %q, want python-cloud", conv.Name())
	}
	if err := conv.TestConnection(context.Background()); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}

	res, err := conv.Convert(context.Background(), converter.Request{Path: pdf, Extract: converter.ExtractAll, Paginate: true}, nil)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if gotPaginate != "true" {
		t.Errorf("paginate_output = %q, want true", gotPaginate)
	}
	if string(res.Images["p.png"]) != "bytes" {
		t.Errorf("image bytes = %q", res.Images["p.png"])
	}
}

func TestLocalSendsFilepath(t *testing.T) {
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/marker" {
			body, _ := io.ReadAll(r.Body)
			var payload map[string]any
			json.Unmarshal(body, &payload)
			gotPath, _ = payload["filepath"].(string)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(successBody(""))
		}
	}))
	defer srv.Close()

	pdf := filepath.Join(t.TempDir(), "doc.pdf")
	os.WriteFile(pdf, []byte("%PDF"), 0o644)

	conv := NewLocal(srv.URL)
	if conv.Name() != "python-local" {
		t.Errorf("Name = %q, want python-local", conv.Name())
	}

	if _, err := conv.Convert(context.Background(), converter.Request{Path: pdf, Extract: converter.ExtractAll}, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !filepath.IsAbs(gotPath) {
		t.Errorf("filepath sent = %q, want an absolute path", gotPath)
	}
}
