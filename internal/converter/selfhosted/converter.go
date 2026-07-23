// Package selfhosted converts PDFs with a self-hosted Marker API (the Docker
// image). It is a port of the Obsidian plugin's src/converters/markerApiDocker.ts.
package selfhosted

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/l3-n0x/marker-cli/internal/converter"
	"github.com/l3-n0x/marker-cli/internal/httpx"
)

// Converter implements converter.Converter against a self-hosted Marker API.
type Converter struct {
	baseURL string
	http    *http.Client
}

// New returns a Converter talking to the Marker API at endpoint, given as a
// bare host:port (e.g. "localhost:8000") or a full URL.
func New(endpoint string) *Converter {
	return &Converter{
		baseURL: normalizeEndpoint(endpoint),
		http:    &http.Client{Timeout: 10 * time.Minute},
	}
}

// Name implements converter.Converter.
func (c *Converter) Name() string { return "selfhosted" }

// TestConnection implements converter.Converter by hitting the /health endpoint.
func (c *Converter) TestConnection(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return httpx.ParseError(resp)
	}
	return nil
}

// convertResponse is the /convert response shape.
type convertResponse struct {
	Status string `json:"status"`
	Result *struct {
		Filename string            `json:"filename"`
		Markdown string            `json:"markdown"`
		Metadata map[string]any    `json:"metadata"`
		Images   map[string]string `json:"images"`
		Status   string            `json:"status"`
	} `json:"result"`
}

// Convert implements converter.Converter.
func (c *Converter) Convert(ctx context.Context, req converter.Request, progress chan<- converter.Progress) (*converter.Result, error) {
	report := converter.Reporter(ctx, progress)

	content, err := os.ReadFile(req.Path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", req.Path, err)
	}

	report(converter.StageUpload, "sending PDF to the Marker API", 0.2)

	// Older and newer builds of the image name the file field differently, so
	// fall back to "document_file" when "pdf_file" is rejected as missing.
	resp, err := c.attempt(ctx, req, content, "pdf_file")
	if err != nil && missingField(err, "document_file") {
		resp, err = c.attempt(ctx, req, content, "document_file")
	}
	if err != nil {
		return nil, err
	}

	if resp.Status != "Success" || resp.Result == nil {
		return nil, fmt.Errorf("conversion failed with status %q", resp.Status)
	}

	report(converter.StageParse, "assembling markdown", 0.9)
	return assemble(resp.Result.Markdown, resp.Result.Images, resp.Result.Metadata, req)
}

// attempt performs one /convert request using fieldName for the file part.
func (c *Converter) attempt(ctx context.Context, req converter.Request, content []byte, fieldName string) (*convertResponse, error) {
	fields := map[string]string{
		"extract_images": strconv.FormatBool(req.Extract != converter.ExtractText),
	}
	body, contentType, err := httpx.Multipart(fieldName, "document.pdf", content, fields)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/convert", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", contentType)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, httpx.ParseError(resp)
	}

	var out convertResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding convert response: %w", err)
	}
	return &out, nil
}

// assemble builds a converter.Result, honouring the extract mode.
func assemble(markdown string, rawImages map[string]string, metadata map[string]any, req converter.Request) (*converter.Result, error) {
	if req.Extract == converter.ExtractImages {
		markdown = ""
	}
	images := make(map[string][]byte)
	if req.Extract != converter.ExtractText {
		for name, encoded := range rawImages {
			data, err := httpx.DecodeImage(encoded)
			if err != nil {
				return nil, fmt.Errorf("decoding image %s: %w", name, err)
			}
			if len(data) == 0 {
				continue
			}
			images[name] = data
		}
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["processor"] = "selfhosted-marker"
	return &converter.Result{Markdown: markdown, Images: images, Metadata: metadata}, nil
}

// missingField reports whether err looks like the API complaining that a
// required form field (name) was not supplied.
func missingField(err error, name string) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "missing") && strings.Contains(msg, name)
}

// normalizeEndpoint turns a bare host:port into an http:// URL and trims any
// trailing slash, leaving an explicit scheme untouched.
func normalizeEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if !strings.Contains(endpoint, "://") {
		endpoint = "http://" + endpoint
	}
	return strings.TrimRight(endpoint, "/")
}
