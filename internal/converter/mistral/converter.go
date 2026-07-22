// Package mistral converts PDFs with the MistralAI OCR API. It is a port of
// the Obsidian plugin's src/converters/mistralaiConverter.ts.
package mistral

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/l3-n0x/marker-cli/internal/converter"
)

// Converter implements converter.Converter against the Mistral OCR API.
type Converter struct {
	client *Client
}

// New returns a Converter authenticated with apiKey.
func New(apiKey string) *Converter {
	return &Converter{client: NewClient(apiKey)}
}

// Name implements converter.Converter.
func (c *Converter) Name() string { return "mistral" }

// TestConnection implements converter.Converter by listing files, the
// cheapest call that still exercises the credentials.
func (c *Converter) TestConnection(ctx context.Context) error {
	return c.client.ListFiles(ctx)
}

// Convert implements converter.Converter: upload the PDF, get a signed URL for
// it, run OCR, then assemble the markdown and images.
func (c *Converter) Convert(ctx context.Context, req converter.Request, progress chan<- converter.Progress) (*converter.Result, error) {
	report := func(stage converter.Stage, detail string, pct float64) {
		if progress == nil {
			return
		}
		select {
		case progress <- converter.Progress{Stage: stage, Detail: detail, Percent: pct}:
		case <-ctx.Done():
		}
	}

	report(converter.StageUpload, "sending PDF to Mistral", 0.05)
	file, err := c.client.UploadFile(ctx, req.Path)
	if err != nil {
		return nil, err
	}

	if req.DeleteRemote {
		// Runs even if OCR fails, mirroring the plugin's finally block.
		defer func() {
			report(converter.StageCleanup, "removing uploaded file", 0.97)
			if err := c.client.DeleteFile(context.WithoutCancel(ctx), file.ID); err != nil {
				report(converter.StageCleanup, "warning: "+err.Error(), 0.97)
			}
		}()
	}

	report(converter.StageSign, "requesting signed URL", 0.35)
	signedURL, err := c.client.SignedURL(ctx, file.ID, 24)
	if err != nil {
		return nil, err
	}

	report(converter.StageOCR, "this can take a few minutes", 0.45)
	ocrReq := OCRRequest{
		Model:              OCRModel,
		Document:           OCRDocument{Type: "document_url", DocumentURL: signedURL},
		IncludeImageBase64: req.Extract != converter.ExtractText,
	}
	if req.ImageLimit > 0 {
		ocrReq.ImageLimit = &req.ImageLimit
	}
	if req.ImageMinSize > 0 {
		ocrReq.ImageMinSize = &req.ImageMinSize
	}

	resp, err := c.client.OCR(ctx, ocrReq)
	if err != nil {
		return nil, err
	}
	if len(resp.Pages) == 0 {
		return nil, fmt.Errorf("OCR returned no pages")
	}

	report(converter.StageParse, fmt.Sprintf("%d pages", len(resp.Pages)), 0.9)
	return parsePages(resp.Pages, req)
}

// parsePages joins page markdown and collects images, porting
// parseOCRResults in mistralaiConverter.ts.
func parsePages(pages []OCRPage, req converter.Request) (*converter.Result, error) {
	var md strings.Builder
	images := make(map[string][]byte)

	for i, page := range pages {
		if req.Extract != converter.ExtractImages {
			if i > 0 {
				// The plugin always inserts a rule here; we honour the
				// --paginate flag the setting actually documents.
				if req.Paginate {
					md.WriteString("\n\n---\n\n")
				} else {
					md.WriteString("\n\n")
				}
			}
			md.WriteString(page.Markdown)
		}

		if req.Extract == converter.ExtractText {
			continue
		}
		for _, img := range page.Images {
			data, err := decodeImage(img.ImageBase64)
			if err != nil {
				return nil, fmt.Errorf("decoding image %s: %w", img.ID, err)
			}
			if len(data) == 0 {
				continue
			}
			images[img.ID] = data
		}
	}

	return &converter.Result{
		Markdown: md.String(),
		Images:   images,
		Metadata: map[string]any{
			"page_count": len(pages),
			"processor":  "mistralai-ocr",
		},
	}, nil
}

// decodeImage strips an optional data-URL prefix and base64-decodes the rest.
func decodeImage(s string) ([]byte, error) {
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
