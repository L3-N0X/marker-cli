// Package httpx holds HTTP helpers shared by the converter backends that talk
// to a Marker-style REST API: building multipart uploads, decoding the base64
// images those APIs return, and turning a non-2xx response into a readable
// error. The MistralAI client predates this package and keeps its own
// equivalents.
package httpx

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sort"
	"strings"
)

// Multipart builds a multipart/form-data body with one file part named
// fileField plus the given text fields. It returns the body and the matching
// Content-Type header (which carries the boundary). The body is buffered whole,
// which is fine for the local PDFs these backends convert.
//
// It is a port of MarkerMultipartRequest.build in the Obsidian plugin's
// src/utils/multipartUtils.ts.
func Multipart(fileField, filename string, content []byte, fields map[string]string) ([]byte, string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	part, err := mw.CreateFormFile(fileField, filename)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(content); err != nil {
		return nil, "", err
	}

	// Sort keys so the body is deterministic, which keeps tests stable.
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := mw.WriteField(k, fields[k]); err != nil {
			return nil, "", err
		}
	}

	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), mw.FormDataContentType(), nil
}

// DecodeImage strips an optional data-URL prefix (e.g. "data:image/png;base64,")
// and base64-decodes the rest. An empty string decodes to no bytes.
func DecodeImage(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	if strings.HasPrefix(s, "data:") {
		_, after, found := strings.Cut(s, ",")
		if !found {
			return nil, fmt.Errorf("malformed data URL")
		}
		s = after
	}
	return base64.StdEncoding.DecodeString(s)
}

// APIError is a non-2xx response from a backend.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("API returned %d %s", e.StatusCode, http.StatusText(e.StatusCode))
	}
	return fmt.Sprintf("API returned %d: %s", e.StatusCode, e.Message)
}

// ParseError reads an error response and pulls out the most human-readable
// message it can find, so users see the API's own wording rather than a bare
// status code. It handles the common {"message":...}, {"error":{"message":...}}
// and FastAPI {"detail":[...]} shapes.
func ParseError(resp *http.Response) error {
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
			msg = detailMessage(envelope.Detail)
		}
	}
	if msg == "" {
		msg = strings.TrimSpace(string(body))
	}
	return &APIError{StatusCode: resp.StatusCode, Message: msg}
}

// detailMessage flattens a FastAPI "detail" field, which is either a string or a
// list of {msg, loc, type} validation errors.
func detailMessage(detail any) string {
	switch d := detail.(type) {
	case string:
		return d
	case []any:
		msgs := make([]string, 0, len(d))
		for _, item := range d {
			if m, ok := item.(map[string]any); ok {
				if s, ok := m["msg"].(string); ok {
					msgs = append(msgs, s)
					continue
				}
			}
			msgs = append(msgs, fmt.Sprint(item))
		}
		return strings.Join(msgs, "; ")
	default:
		return fmt.Sprint(detail)
	}
}
