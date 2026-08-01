// Package artifact resolves and inspects bounded files referenced by Agent
// conversation context without exposing provider keys or filesystem paths to
// the model.
package artifact

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	MaxFileSize        int64 = 20 << 20
	MaxTextBytes             = 512 << 10
	MaxTextOutputBytes       = 48 << 10
	MaxTableRows             = 100_000
	MaxWorkbookSheets        = 50
	MaxSampleRows            = 20
	MaxColumns               = 100
	MaxCellRunes             = 500
)

var (
	ErrNotFound            = errors.New("conversation artifact was not found")
	ErrScope               = errors.New("conversation artifact is outside the Run scope")
	ErrUnsupported         = errors.New("conversation artifact type is unsupported")
	ErrTooLarge            = errors.New("conversation artifact exceeds the size limit")
	ErrInvalid             = errors.New("conversation artifact is invalid")
	ErrAnalysisUnavailable = errors.New("conversation artifact analysis is unavailable")
)

type Kind string

const (
	KindImage       Kind = "image"
	KindMarkdown    Kind = "markdown"
	KindCSV         Kind = "csv"
	KindSpreadsheet Kind = "spreadsheet"
	KindText        Kind = "text"
	KindFile        Kind = "file"
)

type Availability string

const (
	AvailabilityAvailable   Availability = "available"
	AvailabilityUnavailable Availability = "unavailable"
)

// Reference is safe to expose to the model. Provider resource keys and local
// paths belong only to Resolver implementations.
type Reference struct {
	ID           string       `json:"id"`
	Kind         Kind         `json:"kind"`
	Name         string       `json:"name,omitempty"`
	MediaType    string       `json:"media_type,omitempty"`
	Availability Availability `json:"availability"`
}

type Scope struct {
	RunID          uuid.UUID
	TenantID       string
	ConversationID string
	// NotBefore and NotAfter form an inclusive artifact creation-time window.
	// The inclusive upper bound permits artifacts attached to the trigger itself.
	NotBefore time.Time
	NotAfter  time.Time
}

type LocalFile struct {
	Reference Reference
	Path      string
	Cleanup   func() error
}

type Resolver interface {
	Resolve(context.Context, Scope, string) (LocalFile, error)
}

type Status string

const (
	StatusReadable            Status = "readable"
	StatusTruncated           Status = "truncated"
	StatusAnalysisUnavailable Status = "analysis_unavailable"
)

type Digest struct {
	ArtifactID string          `json:"artifact_id"`
	Kind       Kind            `json:"kind"`
	Name       string          `json:"name,omitempty"`
	MediaType  string          `json:"media_type"`
	SizeBytes  int64           `json:"size_bytes"`
	Status     Status          `json:"status"`
	Warnings   []string        `json:"warnings,omitempty"`
	Text       *TextDigest     `json:"text,omitempty"`
	Table      *TableDigest    `json:"table,omitempty"`
	Workbook   *WorkbookDigest `json:"workbook,omitempty"`
	Image      *ImageDigest    `json:"image,omitempty"`
}

type TextDigest struct {
	Content   string   `json:"content"`
	Headings  []string `json:"headings,omitempty"`
	Truncated bool     `json:"truncated"`
}

type TableDigest struct {
	Columns    []string   `json:"columns,omitempty"`
	RowCount   int        `json:"row_count"`
	SampleRows [][]string `json:"sample_rows,omitempty"`
	Truncated  bool       `json:"truncated"`
}

type WorkbookDigest struct {
	Sheets    []SheetDigest `json:"sheets"`
	Truncated bool          `json:"truncated"`
}

type SheetDigest struct {
	Name         string        `json:"name"`
	RowCount     int           `json:"row_count"`
	ColumnCount  int           `json:"column_count"`
	SampleRows   [][]string    `json:"sample_rows,omitempty"`
	FormulaCells []FormulaCell `json:"formula_cells,omitempty"`
	Truncated    bool          `json:"truncated"`
}

type FormulaCell struct {
	Cell    string `json:"cell"`
	Formula string `json:"formula"`
	Value   string `json:"value,omitempty"`
}

type ImageDigest struct {
	Width         int           `json:"width"`
	Height        int           `json:"height"`
	Format        string        `json:"format"`
	Summary       string        `json:"summary,omitempty"`
	VisibleText   []string      `json:"visible_text,omitempty"`
	Tables        []TableDigest `json:"tables,omitempty"`
	Uncertainties []string      `json:"uncertainties,omitempty"`
	Confidence    string        `json:"confidence,omitempty"`
}

type VisionAnalyzer interface {
	Describe(context.Context, string, string, string) (string, error)
}

// Describer converts one bounded artifact into a goal-specific natural-language
// description. The parent Agent never receives parser output.
type Describer interface {
	Describe(context.Context, LocalFile, string) (string, error)
}
