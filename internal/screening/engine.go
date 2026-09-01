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

// ScreenResult mirrors the FastAPI /predict response.
type ScreenResult struct {
	Verdict         model.Verdict
	RiskScore       float64
	Reasons         []string
	ExtractedFields map[string]string
	// Evidence is the engine's full explainability table, stored verbatim.
	Evidence map[string]any
}

// Engine screens one document image. Implementations must return an
// *apperr.AppError (ScreeningEngineUnavailable / ScreeningEngineBadResponse) on
// any transport or protocol failure — never a bare error.
type Engine interface {
	Screen(ctx context.Context, req ScreenRequest) (*ScreenResult, error)
}
