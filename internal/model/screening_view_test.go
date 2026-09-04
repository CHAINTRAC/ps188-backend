package model_test

import (
	"testing"

	"github.com/sih26/ps188-backend/internal/model"
)

func TestVerdict_Band(t *testing.T) {
	cases := map[model.Verdict]model.Verdict{
		model.VerdictGenuine:      model.VerdictGenuine,
		model.VerdictFake:         model.VerdictFake,
		model.VerdictSuspicious:   model.VerdictSuspicious,
		model.VerdictInsufficient: model.VerdictSuspicious,
		model.VerdictPending:      model.VerdictSuspicious,
	}
	for in, want := range cases {
		if got := in.Band(); got != want {
			t.Errorf("%q.Band() = %q, want %q", in, got, want)
		}
	}
}

func TestScreeningView_RiskScaleAndEngine(t *testing.T) {
	s := &model.Screening{
		Risk:    0.42,
		Verdict: model.VerdictInsufficient,
		Engine: &model.EngineResult{
			Verdict:   model.VerdictInsufficient,
			RiskScore: 0.426, // rounds to 43
			Reasons:   nil,
		},
	}
	v := s.View()

	if v.RiskScore != 42 {
		t.Fatalf("view risk_score = %d, want 42", v.RiskScore)
	}
	if v.VerdictBand != model.VerdictSuspicious {
		t.Fatalf("verdict_band = %q, want SUSPICIOUS", v.VerdictBand)
	}
	if v.Engine == nil || v.Engine.RiskScore != 43 {
		t.Fatalf("engine view risk_score = %+v, want 43", v.Engine)
	}
	if v.Engine.VerdictBand != model.VerdictSuspicious {
		t.Fatalf("engine verdict_band = %q", v.Engine.VerdictBand)
	}
	// never-null slices
	if v.Engine.Reasons == nil || v.Engine.ExtractedFields == nil || v.Engine.Evidence == nil {
		t.Fatalf("engine view slices must be non-nil: %+v", v.Engine)
	}
	if v.Flags == nil || v.BlacklistMatches == nil {
		t.Fatalf("view slices must be non-nil")
	}
}

func TestScreeningView_NoEngine(t *testing.T) {
	s := &model.Screening{Risk: 0, Verdict: model.VerdictPending}
	v := s.View()
	if v.Engine != nil {
		t.Fatalf("engine should be nil when unset")
	}
	if v.RiskScore != 0 {
		t.Fatalf("risk = %d", v.RiskScore)
	}
}
