package mistral

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultBaseURL is the Mistral API root.
const DefaultBaseURL = "https://api.mistral.ai/v1"

// OCRModel is the OCR model the plugin uses.
const OCRModel = "mistral-ocr-latest"

// Client is a minimal REST client for the handful of Mistral endpoints we
// need. The official SDK is a thin wrapper over the same calls, so we avoid
// the dependency.
type Client struct {
	APIKey  string
	BaseURL string
	HTTP    *http.Client
}

// NewClient returns a Client with a timeout generous enough for OCR on large
// PDFs, which routinely takes minutes. Set MISTRAL_BASE_URL to point at a
// proxy or a test server instead of the public API.
func NewClient(apiKey string) *Client {
	baseURL := DefaultBaseURL
	if override := os.Getenv("MISTRAL_BASE_URL"); override != "" {
		baseURL = strings.TrimRight(override, "/")
	}
	return &Client{
		APIKey:  apiKey,
		BaseURL: baseURL,
		HTTP:    &http.Client{Timeout: 10 * time.Minute},
	}
}

// APIError is a non-2xx response from the Mistral API.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("mistral API returned %d %s", e.StatusCode, http.StatusText(e.StatusCode))
	}
	return fmt.Sprintf("mistral API returned %d: %s", e.StatusCode, e.Message)
}

// File is an uploaded file record.
type File struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Bytes    int64  `json:"bytes"`
	Purpose  string `json:"purpose"`
}

// UploadFile streams path to the API with purpose "ocr" and returns the
// created file record.
func (c *Client) UploadFile(ctx context.Context, path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	// Stream the body through a pipe so a large PDF is never buffered whole.
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		// Any error here is surfaced to the reader (and thus to http.Do)
		// via CloseWithError.
		err := func() error {
			if err := mw.WriteField("purpose", "ocr"); err != nil {
				return err
			}
			part, err := mw.CreateFormFile("file", filepath.Base(path))
			if err != nil {
				return err
			}
			if _, err := io.Copy(part, f); err != nil {
				return err
			}
			return mw.Close()
		}()
		pw.CloseWithError(err)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/files", pr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	var out File
	if err := c.do(req, &out); err != nil {
		return nil, fmt.Errorf("uploading file: %w", err)
	}
	if out.ID == "" {
		return nil, fmt.Errorf("uploading file: API returned no file id")
	}
	return &out, nil
}

// SignedURL returns a temporary URL the OCR endpoint can read the file from.
// expiryHours mirrors the API's `expiry` query parameter.
func (c *Client) SignedURL(ctx context.Context, fileID string, expiryHours int) (string, error) {
	endpoint := fmt.Sprintf("%s/files/%s/url?expiry=%d", c.BaseURL, url.PathEscape(fileID), expiryHours)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}

	var out struct {
		URL string `json:"url"`
	}
	if err := c.do(req, &out); err != nil {
		return "", fmt.Errorf("getting signed URL: %w", err)
	}
	if out.URL == "" {
		return "", fmt.Errorf("getting signed URL: API returned an empty URL")
	}
	return out.URL, nil
}

// OCRRequest is the body of POST /ocr.
type OCRRequest struct {
	Model              string      `json:"model"`
	Document           OCRDocument `json:"document"`
	IncludeImageBase64 bool        `json:"include_image_base64"`
	ImageLimit         *int        `json:"image_limit,omitempty"`
	ImageMinSize       *int        `json:"image_min_size,omitempty"`
}

// OCRDocument points the OCR endpoint at the document to read.
type OCRDocument struct {
	Type        string `json:"type"`
	DocumentURL string `json:"document_url,omitempty"`
	ImageURL    string `json:"image_url,omitempty"`
}

// OCRResponse is the OCR result: one entry per page.
type OCRResponse struct {
	Pages []OCRPage `json:"pages"`
}

// OCRPage is a single converted page.
type OCRPage struct {
	Index    int        `json:"index"`
	Markdown string     `json:"markdown"`
	Images   []OCRImage `json:"images"`
}

// OCRImage is an image extracted from a page. ImageBase64 may carry a
// `data:image/png;base64,` prefix, which the parser strips.
type OCRImage struct {
	ID          string `json:"id"`
	ImageBase64 string `json:"image_base64"`
}

// OCR runs OCR over a document that is already reachable at a URL.
func (c *Client) OCR(ctx context.Context, in OCRRequest) (*OCRResponse, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("encoding OCR request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/ocr", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	var out OCRResponse
	if err := c.do(req, &out); err != nil {
		return nil, fmt.Errorf("running OCR: %w", err)
	}
	return &out, nil
}

// DeleteFile removes an uploaded file from Mistral's servers.
func (c *Client) DeleteFile(ctx context.Context, fileID string) error {
	endpoint := c.BaseURL + "/files/" + url.PathEscape(fileID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}

	var out struct {
		Deleted bool `json:"deleted"`
	}
	if err := c.do(req, &out); err != nil {
		return fmt.Errorf("deleting remote file: %w", err)
	}
	if !out.Deleted {
		return fmt.Errorf("deleting remote file %s: API reported it was not deleted", fileID)
	}
	return nil
}

// ListFiles is the cheapest authenticated call available; it doubles as the
// connection test.
func (c *Client) ListFiles(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/files", nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

// do sends req with auth applied and decodes a successful JSON body into out
// (which may be nil to discard it).
func (c *Client) do(req *http.Request, out any) error {
	if c.APIKey == "" {
		return fmt.Errorf("no API key configured")
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseAPIError(resp)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	return nil
}

// parseAPIError pulls the human-readable message out of an error response so
// users see "Unauthorized: invalid api key" rather than a bare 401.
func parseAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))

	var envelope struct {
		Message string `json:"message"`
		Detail  any    `json:"detail"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	msg := ""
	if err := json.Unmarshal(body, &envelope); err == nil {
		switch {
		case envelope.Message != "":
			msg = envelope.Message
		case envelope.Error.Message != "":
			msg = envelope.Error.Message
		case envelope.Detail != nil:
			msg = fmt.Sprint(envelope.Detail)
		}
	}
	if msg == "" {
		msg = strings.TrimSpace(string(body))
	}

	return &APIError{StatusCode: resp.StatusCode, Message: msg}
}
