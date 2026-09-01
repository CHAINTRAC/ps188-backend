package screening

import (
	"context"

	"github.com/sih26/ps188-backend/internal/model"
)

// MockEngine returns a deterministic result derived from the image bytes so it
// is usable offline and in tests without touching the network. Enable it with
// SCREENING_ENGINE=mock.
type MockEngine struct {
	// Force, when set, overrides the derived verdict.
	Force model.Verdict
	// Err, when set, is returned instead of a result (simulates an outage).
	Err error
}

func (m *MockEngine) Screen(_ context.Context, req ScreenRequest) (*ScreenResult, error) {
	if m.Err != nil {
		return nil, m.Err
	}

	verdict := m.Force
	risk := 0.1
	if verdict == "" {
		// Cheap deterministic spread: checksum of the image bytes.
		var sum int
		for _, b := range req.Image {
			sum += int(b)
		}
		switch sum % 3 {
		case 0:
			verdict, risk = model.VerdictGenuine, 0.08
		case 1:
			verdict, risk = model.VerdictSuspicious, 0.42
		default:
			verdict, risk = model.VerdictFake, 0.71
		}
	}

	fields := map[string]string{}
	if req.DocNumber != "" {
		fields["document_number"] = req.DocNumber
	}
	if req.MRZLine1 != "" {
		fields["mrz_line1"] = req.MRZLine1
	}

	return &ScreenResult{
		Verdict:         verdict,
		RiskScore:       risk,
		Reasons:         []string{"mock screening engine — result derived from image bytes"},
		ExtractedFields: fields,
		Evidence: map[string]any{
			"engine":             "mock",
			"cnn_score":          0.5,
			"ela_variance":       120.0,
			"quality_assessment": map[string]any{"is_sufficient": true},
		},
	}, nil
}
