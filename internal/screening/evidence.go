package screening

import (
	"fmt"

	"github.com/sih26/ps188-backend/internal/model"
)

// toneForRisk maps a 0.0–1.0 risk score onto the UI's three-tone scale.
func toneForRisk(risk float64) string {
	switch {
	case risk >= 0.66:
		return model.EvidenceBad
	case risk >= 0.33:
		return model.EvidenceWarn
	default:
		return model.EvidenceGood
	}
}

// DeriveEvidence builds the toned evidence list from the model's plain reasons
// and risk score, for when the model does not supply a structured one itself.
// Every reason becomes one line at the risk-band tone; with no reasons a single
// summary line is returned.
func DeriveEvidence(reasons []string, risk float64) []model.EvidenceItem {
	tone := toneForRisk(risk)
	if len(reasons) == 0 {
		return []model.EvidenceItem{{
			Tone: tone,
			Text: fmt.Sprintf("Automated screening completed — risk %d/100", riskTo100(risk)),
		}}
	}
	out := make([]model.EvidenceItem, 0, len(reasons))
	for _, r := range reasons {
		if r == "" {
			continue
		}
		out = append(out, model.EvidenceItem{Tone: tone, Text: r})
	}
	return out
}

// riskTo100 mirrors model.riskTo100 for the screening package's own use.
func riskTo100(f float64) int {
	switch {
	case f <= 0:
		return 0
	case f >= 1:
		return 100
	default:
		return int(f*100 + 0.5)
	}
}
