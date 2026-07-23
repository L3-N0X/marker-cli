// Package datalab converts PDFs with the hosted Datalab Marker API. It is a
// port of the Obsidian plugin's src/converters/datalabConverter.ts.
package datalab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/l3-n0x/marker-cli/internal/converter"
	"github.com/l3-n0x/marker-cli/internal/httpx"
)

// DefaultBaseURL is the Datalab API root. Set DATALAB_BASE_URL to point at a
// proxy or a test server instead.
const DefaultBaseURL = "https://www.datalab.to"

// maxPolls caps the total wait, mirroring the plugin's 300 × 2s budget.
const maxPolls = 300

// pollInterval is how long we wait between checks of an in-flight conversion. It
// is a var so tests can shorten it.
var pollInterval = 2 * time.Second

// Converter implements converter.Converter against the Datalab Marker API.
type Converter struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

// New returns a Converter authenticated with apiKey.
func New(apiKey string) *Converter {
	baseURL := DefaultBaseURL
	if override := os.Getenv("DATALAB_BASE_URL"); override != "" {
		baseURL = strings.TrimRight(override, "/")
	}
	return &Converter{
		apiKey:  apiKey,
		baseURL: baseURL,
		http:    &http.Client{Timeout: 10 * time.Minute},
	}
}

// Name implements converter.Converter.
func (c *Converter) Name() string { return "datalab" }

// TestConnection implements converter.Converter by hitting the user-health
// endpoint, which only needs the API key.
func (c *Converter) TestConnection(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/user_health", nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return httpx.ParseError(resp)
	}
	var out struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("decoding health response: %w", err)
	}
	if out.Status != "ok" {
		return fmt.Errorf("datalab reported status %q", out.Status)
	}
	return nil
}

// submitResponse is the initial response from POST /api/v1/marker.
type submitResponse struct {
	Success         bool   `json:"success"`
	Error           string `json:"error"`
	RequestCheckURL string `json:"request_check_url"`
}

// pollResponse is the shape of the request-check endpoint.
type pollResponse struct {
	Status    string            `json:"status"`
	Markdown  string            `json:"markdown"`
	Images    map[string]string `json:"images"`
	Metadata  map[string]any    `json:"metadata"`
	Error     string            `json:"error"`
	PageCount int               `json:"page_count"`
}

// Convert implements converter.Converter: submit the PDF, then poll until the
// conversion completes.
func (c *Converter) Convert(ctx context.Context, req converter.Request, progress chan<- converter.Progress) (*converter.Result, error) {
	report := converter.Reporter(ctx, progress)

	if c.apiKey == "" {
		return nil, fmt.Errorf("no API key configured")
	}

	content, err := os.ReadFile(req.Path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", req.Path, err)
	}

	report(converter.StageUpload, "submitting to Datalab", 0.1)
	checkURL, err := c.submit(ctx, req, content)
	if err != nil {
		return nil, err
	}

	report(converter.StageOCR, "this can take a few minutes", 0.4)
	poll, err := c.poll(ctx, checkURL, report)
	if err != nil {
		return nil, err
	}

	report(converter.StageParse, fmt.Sprintf("%d pages", poll.PageCount), 0.95)
	return assemble(poll, req)
}

// submit sends the conversion request and returns the URL to poll for the
// result.
func (c *Converter) submit(ctx context.Context, req converter.Request, content []byte) (string, error) {
	fields := map[string]string{
		"langs":                    orDefault(req.Langs, "en"),
		"force_ocr":                strconv.FormatBool(req.ForceOCR),
		"paginate":                 strconv.FormatBool(req.Paginate),
		"disable_image_extraction": strconv.FormatBool(req.Extract == converter.ExtractText),
		"output_format":            "markdown",
		"strip_existing_ocr":       strconv.FormatBool(req.StripExistingOCR),
		"use_llm":                  strconv.FormatBool(req.UseLLM),
		"skip_cache":               strconv.FormatBool(req.SkipCache),
	}
	if req.MaxPages > 0 {
		fields["max_pages"] = strconv.Itoa(req.MaxPages)
	}

	body, contentType, err := httpx.Multipart("file", filepath.Base(req.Path), content, fields)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/marker", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", contentType)
	httpReq.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", httpx.ParseError(resp)
	}

	var out submitResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decoding submit response: %w", err)
	}
	if out.RequestCheckURL == "" {
		if out.Error != "" {
			return "", fmt.Errorf("datalab: %s", out.Error)
		}
		return "", fmt.Errorf("datalab returned no request_check_url")
	}
	return out.RequestCheckURL, nil
}

// poll repeatedly checks checkURL until the conversion completes, fails or the
// budget runs out.
func (c *Converter) poll(ctx context.Context, checkURL string, report func(converter.Stage, string, float64)) (*pollResponse, error) {
	for i := 0; i < maxPolls; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-Api-Key", c.apiKey)

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		var out pollResponse
		decodeErr := json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("decoding poll response: %w", decodeErr)
		}
		if out.Error != "" {
			return nil, fmt.Errorf("datalab: %s", out.Error)
		}
		if out.Status == "complete" {
			return &out, nil
		}

		report(converter.StageOCR, fmt.Sprintf("converting… (%d/%d)", i+1, maxPolls), 0.4)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
	return nil, fmt.Errorf("datalab conversion timed out after %d checks", maxPolls)
}

// assemble turns a completed poll response into a converter.Result.
func assemble(poll *pollResponse, req converter.Request) (*converter.Result, error) {
	markdown := poll.Markdown
	if req.Extract == converter.ExtractImages {
		markdown = ""
	}

	images := make(map[string][]byte)
	if req.Extract != converter.ExtractText {
		for name, encoded := range poll.Images {
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

	metadata := poll.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["processor"] = "datalab-marker"

	return &converter.Result{Markdown: markdown, Images: images, Metadata: metadata}, nil
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
