// Package converter defines the backend-agnostic conversion contract. Only
// MistralAI implements it today; Datalab, self-hosted Marker and the Python
// API can be added as sibling packages without touching callers.
package converter

import "context"

// Extract selects which parts of the PDF end up in the output.
type Extract string

const (
	ExtractAll    Extract = "all"
	ExtractText   Extract = "text"
	ExtractImages Extract = "images"
)

// Valid reports whether e is one of the known extract modes.
func (e Extract) Valid() bool {
	switch e {
	case ExtractAll, ExtractText, ExtractImages:
		return true
	}
	return false
}

// Request is a single PDF conversion job.
type Request struct {
	Path         string // absolute or relative path to the source PDF
	Extract      Extract
	Paginate     bool // insert a horizontal rule between pages
	ImageLimit   int  // 0 means no limit
	ImageMinSize int  // 0 means no minimum
	DeleteRemote bool // delete the uploaded file from the provider afterwards
}

// Result is the converted document, held in memory before it is written out.
type Result struct {
	Markdown string
	Images   map[string][]byte // image id -> decoded bytes
	Metadata map[string]any
}

// Stage names a step of the conversion, used to drive progress reporting.
type Stage string

const (
	StageUpload    Stage = "Uploading"
	StageSign      Stage = "Preparing"
	StageOCR       Stage = "Running OCR"
	StageParse     Stage = "Parsing results"
	StageCleanup   Stage = "Cleaning up"
	StageCompleted Stage = "Done"
)

// Progress is emitted as a conversion advances. Percent is a 0..1 fraction of
// the whole job.
type Progress struct {
	Stage   Stage
	Detail  string
	Percent float64
}

// Converter is one OCR backend.
type Converter interface {
	// Name is the provider's short identifier, e.g. "mistral".
	Name() string

	// TestConnection verifies that the configured credentials work.
	TestConnection(ctx context.Context) error

	// Convert runs the job. If progress is non-nil the implementation sends
	// updates on it and closes nothing — the caller owns the channel.
	Convert(ctx context.Context, req Request, progress chan<- Progress) (*Result, error)
}
