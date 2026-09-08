// Package screening is the client for the external Python/FastAPI screening
// model. It is the project's single external service boundary — the one thing
// stubbed in tests (see mock_engine.go). Everything else in a test runs for real.
package screening

import (
	"context"

	"github.com/sih26/ps188-backend/internal/model"
)

// ScreenRequest is what the caller hands the engine.
type ScreenRequest struct {
	DocType   model.DocType
	DocNumber string
	MRZLine1  string
	MRZLine2  string
	Filename  string
	Image     []byte
}

// ScreenResult mirrors the FastAPI verify response. RiskScore stays on the
// model's native 0.0–1.0 scale; the API layer converts to 0–100.
type ScreenResult struct {
	Verdict model.Verdict
	// DocType is the document type the model identified (empty if it did not
	// classify one). The caller uses it when the officer did not pick a type.
	DocType         model.DocType
	RiskScore       float64
	Reasons         []string
	ExtractedFields []model.ExtractedField
	// EvidenceItems is the toned explainability list for the UI — supplied by the
	// model when it can, otherwise derived from Reasons + RiskScore.
	EvidenceItems []model.EvidenceItem
	// RawEvidence is the engine's full explainability table, stored verbatim.
	RawEvidence map[string]any
}

type FaceMatchRequest struct {
	DocImage       []byte
	DocFilename    string
	Selfie         []byte
	SelfieFilename string
}

type FaceMatchResult struct {
	IsMatch         bool
	SimilarityScore float64
	Threshold       float64
	Message         string
}

// Engine screens one document image and, separately, compares a live selfie
// against it. Implementations must return an *apperr.AppError on any
// transport or protocol failure — never a bare error.
type Engine interface {
	Screen(ctx context.Context, req ScreenRequest) (*ScreenResult, error)
	MatchFace(ctx context.Context, req FaceMatchRequest) (*FaceMatchResult, error)
}
