package datalab

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/l3-n0x/marker-cli/internal/converter"
)

// TestConvertPolls exercises submit → poll(processing) → poll(complete) and the
// image decoding, against a stand-in for the Datalab API.
func TestConvertPolls(t *testing.T) {
	pollInterval = time.Millisecond // don't wait 2s between polls in the test

	img := base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	var gotLangs, gotForceOCR string
	polls := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Api-Key"); got != "test-key" {
			t.Errorf("X-Api-Key = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/marker":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("parsing multipart: %v", err)
			}
			gotLangs = r.FormValue("langs")
			gotForceOCR = r.FormValue("force_ocr")
			json.NewEncoder(w).Encode(submitResponse{
				Success:         true,
				RequestCheckURL: "http://" + r.Host + "/check",
			})

		case r.URL.Path == "/check":
			polls++
			if polls < 2 {
				json.NewEncoder(w).Encode(pollResponse{Status: "processing"})
				return
			}
			json.NewEncoder(w).Encode(pollResponse{
				Status:    "complete",
				Markdown:  "# Title\n\n![](fig.png)",
				Images:    map[string]string{"fig.png": img},
				Metadata:  map[string]any{"pages": 3},
				PageCount: 3,
			})

		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	t.Setenv("DATALAB_BASE_URL", srv.URL)

	pdf := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-1.4 fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	conv := New("test-key")
	res, err := conv.Convert(context.Background(), converter.Request{
		Path:     pdf,
		Extract:  converter.ExtractAll,
		Langs:    "en,de",
		ForceOCR: true,
	}, nil)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	if gotLangs != "en,de" {
		t.Errorf("langs field = %q, want en,de", gotLangs)
	}
	if gotForceOCR != "true" {
		t.Errorf("force_ocr field = %q, want true", gotForceOCR)
	}
	if polls < 2 {
		t.Errorf("expected the converter to poll until complete, got %d polls", polls)
	}
	if res.Markdown != "# Title\n\n![](fig.png)" {
		t.Errorf("markdown = %q", res.Markdown)
	}
	if string(res.Images["fig.png"]) != "png-bytes" {
		t.Errorf("image bytes = %q", res.Images["fig.png"])
	}
}

func TestTestConnection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/user_health" {
			t.Errorf("path = %q", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	t.Setenv("DATALAB_BASE_URL", srv.URL)
	if err := New("test-key").TestConnection(context.Background()); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
}
