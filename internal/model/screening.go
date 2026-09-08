package model

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// CollScreenings is the MongoDB collection name for screening cases.
const CollScreenings = "screenings"

// DocType is the kind of identity/travel document presented at the checkpoint.
type DocType string

const (
	DocPassport       DocType = "passport"
	DocVisa           DocType = "visa"
	DocNationalID     DocType = "national_id"
	DocDrivingLicense DocType = "driving_license"
	DocPermit         DocType = "permit"
)

func (d DocType) Valid() bool {
	switch d {
	case DocPassport, DocVisa, DocNationalID, DocDrivingLicense, DocPermit:
		return true
	}
	return false
}

// Verdict is the screening engine's explainable conclusion.
type Verdict string

const (
	VerdictGenuine      Verdict = "GENUINE"
	VerdictSuspicious   Verdict = "SUSPICIOUS"
	VerdictFake         Verdict = "FAKE"
	VerdictInsufficient Verdict = "INSUFFICIENT_IMAGE_QUALITY"
	VerdictPending      Verdict = "PENDING" // set while status == processing/failed
)

// Band collapses a verdict to the three states a UI control that only knows
// genuine / suspicious / fake can render. INSUFFICIENT_IMAGE_QUALITY and PENDING
// map to SUSPICIOUS — both mean "a human still needs to look". The frontend
// SHOULD show INSUFFICIENT_IMAGE_QUALITY as its own "retake photo" badge when it
// has one and fall back to the SUSPICIOUS styling otherwise (see ScreeningView
// which exposes both `verdict` and `verdict_band`).
func (v Verdict) Band() Verdict {
	switch v {
	case VerdictGenuine, VerdictFake:
		return v
	default:
		return VerdictSuspicious
	}
}

// ScreeningStatus tracks the async lifecycle of a case.
type ScreeningStatus string

const (
	StatusProcessing ScreeningStatus = "processing"
	StatusCompleted  ScreeningStatus = "completed"
	StatusFailed     ScreeningStatus = "failed"
)

// Decision is the officer's manual call on top of the machine verdict. Values
// match the operator-facing UI: accept the document, escalate for review, or
// reject it.
type Decision string

const (
	DecisionAccept   Decision = "accept"
	DecisionEscalate Decision = "escalate"
	DecisionReject   Decision = "reject"
)

func (d Decision) Valid() bool {
	switch d {
	case DecisionAccept, DecisionEscalate, DecisionReject:
		return true
	}
	return false
}

// Advisory screening flags. They surface risk for the officer and never block —
// the officer still records the decision.
const (
	FlagBlacklistHit     = "blacklist_hit"
	FlagExpiredDocument  = "expired_document"
	FlagMultipleIdentity = "multiple_identity"
	FlagFaceMismatch     = "face_mismatch"
)

// FaceMatchResult is separate from EngineResult — a distinct external call
// with its own inputs (live selfie vs. the document's own photo).
type FaceMatchResult struct {
	IsMatch         bool    `bson:"is_match" json:"is_match"`
	SimilarityScore float64 `bson:"similarity_score" json:"similarity_score"`
	Threshold       float64 `bson:"threshold" json:"threshold"`
	Message         string  `bson:"message,omitempty" json:"message,omitempty"`
}

// BlacklistMatch is a compact record of one blacklist entry a screening matched,
// embedded on the screening for the officer's review and the audit trail.
type BlacklistMatch struct {
	EntryID   string        `bson:"entry_id" json:"entry_id"`
	Kind      BlacklistKind `bson:"kind" json:"kind"`
	DocNumber string        `bson:"doc_number,omitempty" json:"doc_number,omitempty"`
	Name      string        `bson:"name,omitempty" json:"name,omitempty"`
	Reason    string        `bson:"reason" json:"reason"`
	Source    string        `bson:"source,omitempty" json:"source,omitempty"`
}

// Evidence tone — the UI colour-codes each explainability line.
const (
	EvidenceGood = "good"
	EvidenceWarn = "warn"
	EvidenceBad  = "bad"
)

// ExtractedField is one OCR'd document field with a per-field confidence
// (0.0–1.0; 0 means the engine gave no confidence). The UI renders a badge per
// row.
type ExtractedField struct {
	Label      string  `bson:"label" json:"label"`
	Value      string  `bson:"value" json:"value"`
	Confidence float64 `bson:"confidence" json:"confidence"`
}

// EvidenceItem is one toned explainability line for the UI evidence panel.
type EvidenceItem struct {
	Tone string `bson:"tone" json:"tone"` // good | warn | bad
	Text string `bson:"text" json:"text"`
}

// EngineResult is the persisted output of the external screening model.
// RiskScore is stored on the model's native 0.0–1.0 scale — every API projection
// converts to an integer 0–100 (see riskTo100). RawEvidence keeps the engine's
// full explainability table verbatim as a fallback when structured Evidence /
// ExtractedFields are absent.
type EngineResult struct {
	Verdict         Verdict          `bson:"verdict" json:"verdict"`
	RiskScore       float64          `bson:"risk_score" json:"risk_score"`
	Reasons         []string         `bson:"reasons" json:"reasons"`
	ExtractedFields []ExtractedField `bson:"extracted_fields" json:"extracted_fields"`
	Evidence        []EvidenceItem   `bson:"evidence_items" json:"evidence"`
	RawEvidence     bson.M           `bson:"evidence" json:"raw_evidence,omitempty"`
}

// EngineView is the API projection of an engine result: risk on the 0–100 scale
// and never-null slices.
type EngineView struct {
	Verdict         Verdict          `json:"verdict"`
	VerdictBand     Verdict          `json:"verdict_band"`
	RiskScore       int              `json:"risk_score"` // 0–100
	Reasons         []string         `json:"reasons"`
	ExtractedFields []ExtractedField `json:"extracted_fields"`
	Evidence        []EvidenceItem   `json:"evidence"`
	RawEvidence     bson.M           `json:"raw_evidence,omitempty"`
}

func (e *EngineResult) View() EngineView {
	reasons := e.Reasons
	if reasons == nil {
		reasons = []string{}
	}
	fields := e.ExtractedFields
	if fields == nil {
		fields = []ExtractedField{}
	}
	evidence := e.Evidence
	if evidence == nil {
		evidence = []EvidenceItem{}
	}
	return EngineView{
		Verdict:         e.Verdict,
		VerdictBand:     e.Verdict.Band(),
		RiskScore:       riskTo100(e.RiskScore),
		Reasons:         reasons,
		ExtractedFields: fields,
		Evidence:        evidence,
		RawEvidence:     e.RawEvidence,
	}
}

// riskTo100 converts the stored 0.0–1.0 risk score to the integer 0–100 scale
// every client (UI RiskGauge, history list) expects. This is the one place the
// conversion lives.
func riskTo100(f float64) int {
	if f <= 0 {
		return 0
	}
	if f >= 1 {
		return 100
	}
	return int(f*100 + 0.5)
}

// OfficerDecision is embedded once, when an officer records their call.
type OfficerDecision struct {
	Decision  Decision  `bson:"decision" json:"decision"`
	Reason    string    `bson:"reason" json:"reason"`
	DecidedBy string    `bson:"decided_by" json:"decided_by"` // user id hex
	DecidedAt time.Time `bson:"decided_at" json:"decided_at"`
}

// Screening is one document-screening case.
type Screening struct {
	ID           bson.ObjectID `bson:"_id,omitempty"`
	ReferenceNo  string        `bson:"reference_no"`
	CheckpointID string        `bson:"checkpoint_id"`
	Region       string        `bson:"region,omitempty"` // denormalised from the officer's checkpoint at submit time
	OfficerID    string        `bson:"officer_id"`       // user id hex of the submitter
	DocType      DocType       `bson:"doc_type"`
	ImageFileID  bson.ObjectID `bson:"image_file_id"` // GridFS file id
	ImageName    string        `bson:"image_name"`
	SelfieFileID bson.ObjectID `bson:"selfie_file_id,omitempty"`
	SelfieName   string        `bson:"selfie_name,omitempty"`

	// Flags are advisory markers raised during screening (e.g. "blacklist_hit",
	// "expired_document", "face_mismatch"). Never auto-blocking — the officer decides.
	Flags            []string         `bson:"flags,omitempty"`
	BlacklistMatches []BlacklistMatch `bson:"blacklist_matches,omitempty"`

	SubmittedNumber string            `bson:"submitted_number,omitempty"`
	MRZLine1        string            `bson:"mrz_line1,omitempty"`
	MRZLine2        string            `bson:"mrz_line2,omitempty"`
	SubmittedFields map[string]string `bson:"submitted_fields,omitempty"`

	Status    ScreeningStatus  `bson:"status"`
	Verdict   Verdict          `bson:"verdict"`
	Risk      float64          `bson:"risk_score"`
	Engine    *EngineResult    `bson:"engine,omitempty"`
	FaceMatch *FaceMatchResult `bson:"face_match,omitempty"`
	Failure   string           `bson:"failure_reason,omitempty"`

	OfficerDecision *OfficerDecision `bson:"officer_decision,omitempty"`

	CreatedAt time.Time `bson:"created_at"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// ScreeningView is the API projection. RiskScore is an integer 0–100 (converted
// from the stored 0.0–1.0). `verdict` is the engine's raw conclusion;
// `verdict_band` collapses it to genuine/suspicious/fake for simple UI controls.
type ScreeningView struct {
	ID               string           `json:"id"`
	ReferenceNo      string           `json:"reference_no"`
	CheckpointID     string           `json:"checkpoint_id"`
	Region           string           `json:"region"`
	OfficerID        string           `json:"officer_id"`
	DocType          DocType          `json:"doc_type"`
	ImageURL         string           `json:"image_url"`
	SelfieURL        string           `json:"selfie_url,omitempty"`
	Flags            []string         `json:"flags"`
	BlacklistMatches []BlacklistMatch `json:"blacklist_matches"`
	Status           ScreeningStatus  `json:"status"`
	Verdict          Verdict          `json:"verdict"`
	VerdictBand      Verdict          `json:"verdict_band"`
	RiskScore        int              `json:"risk_score"` // 0–100
	Engine           *EngineView      `json:"engine,omitempty"`
	FaceMatch        *FaceMatchResult `json:"face_match,omitempty"`
	FailureReason    string           `json:"failure_reason,omitempty"`
	OfficerDecision  *OfficerDecision `json:"officer_decision,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
}

func (s *Screening) View() ScreeningView {
	flags := s.Flags
	if flags == nil {
		flags = []string{}
	}
	matches := s.BlacklistMatches
	if matches == nil {
		matches = []BlacklistMatch{}
	}
	var engine *EngineView
	if s.Engine != nil {
		v := s.Engine.View()
		engine = &v
	}
	var selfieURL string
	if !s.SelfieFileID.IsZero() {
		selfieURL = "/api/screenings/" + s.ID.Hex() + "/selfie"
	}
	return ScreeningView{
		ID:               s.ID.Hex(),
		ReferenceNo:      s.ReferenceNo,
		CheckpointID:     s.CheckpointID,
		Region:           s.Region,
		OfficerID:        s.OfficerID,
		DocType:          s.DocType,
		ImageURL:         "/api/screenings/" + s.ID.Hex() + "/image",
		SelfieURL:        selfieURL,
		Flags:            flags,
		BlacklistMatches: matches,
		Status:           s.Status,
		Verdict:          s.Verdict,
		VerdictBand:      s.Verdict.Band(),
		RiskScore:        riskTo100(s.Risk),
		Engine:           engine,
		FaceMatch:        s.FaceMatch,
		FailureReason:    s.Failure,
		OfficerDecision:  s.OfficerDecision,
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
	}
}

// ScreeningFilter narrows a list query. Zero values mean "no filter".
type ScreeningFilter struct {
	Verdict      Verdict
	DocType      DocType
	Status       ScreeningStatus
	CheckpointID string
	OfficerID    string
	Region       string
	// Decided filters on whether an officer decision has been recorded:
	// a pointer to true → only decided, to false → only undecided, nil → both.
	Decided *bool
	// DecisionValue filters on the recorded decision (accept|escalate|reject).
	DecisionValue Decision
}
