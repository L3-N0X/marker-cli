// Package pythonapi converts PDFs with the Marker Python API, in either of its
// two shapes: "cloud" uploads the file, "local" points the server at a path on
// its own filesystem. It ports the Obsidian plugin's markerCloudPythonApi.ts
// and markerLocalPythonApi.ts.
package pythonapi

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

// mode selects between the two Python API shapes.
type mode int

const (
	modeCloud mode = iota
	modeLocal
)

// Converter implements converter.Converter against the Marker Python API.
type Converter struct {
	mode    mode
	baseURL string
	http    *http.Client
}

// NewCloud returns a Converter that uploads the PDF to the Python API at
// endpoint (host:port or full URL).
func NewCloud(endpoint string) *Converter { return newConverter(modeCloud, endpoint) }

// NewLocal returns a Converter that hands the Python API a filesystem path to
// read itself. The server must be able to reach that path.
func NewLocal(endpoint string) *Converter { return newConverter(modeLocal, endpoint) }

func newConverter(m mode, endpoint string) *Converter {
	return &Converter{
		mode:    m,
		baseURL: normalizeEndpoint(endpoint),
		http:    &http.Client{Timeout: 10 * time.Minute},
	}
}

// Name implements converter.Converter.
func (c *Converter) Name() string {
	if c.mode == modeLocal {
		return "python-local"
	}
	return "python-cloud"
}

// TestConnection implements converter.Converter by fetching the API root.
func (c *Converter) TestConnection(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/", nil)
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

// apiResponse is the success shape shared by both endpoints.
type apiResponse struct {
	Format   string            `json:"format"`
	Output   string            `json:"output"`
	Images   map[string]string `json:"images"`
	Metadata map[string]any    `json:"metadata"`
	Success  bool              `json:"success"`
}

// Convert implements converter.Converter.
func (c *Converter) Convert(ctx context.Context, req converter.Request, progress chan<- converter.Progress) (*converter.Result, error) {
	report := converter.Reporter(ctx, progress)
	report(converter.StageUpload, "sending to the Python API", 0.2)

	var (
		resp *apiResponse
		err  error
	)
	if c.mode == modeLocal {
		resp, err = c.convertLocal(ctx, req)
	} else {
		resp, err = c.convertCloud(ctx, req)
	}
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("python API reported the conversion failed")
	}

	report(converter.StageParse, "assembling markdown", 0.9)
	return assemble(resp, req)
}

// convertCloud uploads the file to /marker/upload.
func (c *Converter) convertCloud(ctx context.Context, req converter.Request) (*apiResponse, error) {
	content, err := os.ReadFile(req.Path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", req.Path, err)
	}
	fields := map[string]string{
		"page_range":      "",
		"force_ocr":       strconv.FormatBool(req.ForceOCR),
		"paginate_output": strconv.FormatBool(req.Paginate),
		"output_format":   "markdown",
	}
	body, contentType, err := httpx.Multipart("file", filepath.Base(req.Path), content, fields)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/marker/upload", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", contentType)
	return c.do(httpReq)
}

// convertLocal asks the server to read the file itself via an absolute path.
func (c *Converter) convertLocal(ctx context.Context, req converter.Request) (*apiResponse, error) {
	abs, err := filepath.Abs(req.Path)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", req.Path, err)
	}
	payload, err := json.Marshal(map[string]any{
		"filepath":        abs,
		"page_range":      "",
		"languages":       orDefault(req.Langs, "en"),
		"force_ocr":       req.ForceOCR,
		"paginate_output": req.Paginate,
		"output_format":   "markdown",
	})
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/marker", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	return c.do(httpReq)
}

// do sends the request and decodes a success response.
func (c *Converter) do(httpReq *http.Request) (*apiResponse, error) {
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, httpx.ParseError(resp)
	}
	var out apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding python API response: %w", err)
	}
	return &out, nil
}

// assemble builds a converter.Result, honouring the extract mode.
func assemble(resp *apiResponse, req converter.Request) (*converter.Result, error) {
	markdown := resp.Output
	if req.Extract == converter.ExtractImages {
		markdown = ""
	}
	images := make(map[string][]byte)
	if req.Extract != converter.ExtractText {
		for name, encoded := range resp.Images {
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
	metadata := resp.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["processor"] = "python-marker"
	return &converter.Result{Markdown: markdown, Images: images, Metadata: metadata}, nil
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
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
