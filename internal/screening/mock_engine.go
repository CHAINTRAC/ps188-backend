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

	var fields []model.ExtractedField
	if req.DocNumber != "" {
		fields = append(fields, model.ExtractedField{Label: "document_number", Value: req.DocNumber, Confidence: 0.98})
	}
	if req.MRZLine1 != "" {
		fields = append(fields, model.ExtractedField{Label: "mrz_line1", Value: req.MRZLine1, Confidence: 0.9})
	}

	// Echo the requested type, or "classify" one deterministically when the
	// caller left it to the engine (auto).
	docType := req.DocType
	if docType == "" {
		if len(req.Image)%2 == 0 {
			docType = model.DocPassport
		} else {
			docType = model.DocNationalID
		}
	}

	reasons := []string{"mock screening engine — result derived from image bytes"}
	return &ScreenResult{
		Verdict:         verdict,
		DocType:         docType,
		RiskScore:       risk,
		Reasons:         reasons,
		ExtractedFields: fields,
		EvidenceItems:   DeriveEvidence(reasons, risk),
		RawEvidence: map[string]any{
			"engine":             "mock",
			"cnn_score":          0.5,
			"ela_variance":       120.0,
			"quality_assessment": map[string]any{"is_sufficient": true},
		},
	}, nil
}

func (m *MockEngine) MatchFace(_ context.Context, req FaceMatchRequest) (*FaceMatchResult, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	var sum int
	for _, b := range req.Selfie {
		sum += int(b)
	}
	for _, b := range req.DocImage {
		sum += int(b)
	}
	isMatch := sum%3 != 0
	score := 0.82
	if !isMatch {
		score = 0.31
	}
	msg := "mock face match engine — result derived from image bytes"
	return &FaceMatchResult{
		IsMatch:         isMatch,
		SimilarityScore: score,
		Threshold:       0.6,
		Message:         msg,
	}, nil
}
