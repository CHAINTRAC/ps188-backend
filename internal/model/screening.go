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

// EngineResult is the persisted output of the external screening model.
// Evidence keeps the engine's full explainability table verbatim.
type EngineResult struct {
	Verdict         Verdict           `bson:"verdict" json:"verdict"`
	RiskScore       float64           `bson:"risk_score" json:"risk_score"`
	Reasons         []string          `bson:"reasons" json:"reasons"`
	ExtractedFields map[string]string `bson:"extracted_fields" json:"extracted_fields"`
	Evidence        bson.M            `bson:"evidence" json:"evidence"`
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
	OfficerID    string        `bson:"officer_id"` // user id hex of the submitter
	DocType      DocType       `bson:"doc_type"`
	ImageFileID  bson.ObjectID `bson:"image_file_id"` // GridFS file id
	ImageName    string        `bson:"image_name"`

	SubmittedNumber string            `bson:"submitted_number,omitempty"`
	MRZLine1        string            `bson:"mrz_line1,omitempty"`
	MRZLine2        string            `bson:"mrz_line2,omitempty"`
	SubmittedFields map[string]string `bson:"submitted_fields,omitempty"`

	Status  ScreeningStatus `bson:"status"`
	Verdict Verdict         `bson:"verdict"`
	Risk    float64         `bson:"risk_score"`
	Engine  *EngineResult   `bson:"engine,omitempty"`
	Failure string          `bson:"failure_reason,omitempty"`

	OfficerDecision *OfficerDecision `bson:"officer_decision,omitempty"`

	CreatedAt time.Time `bson:"created_at"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// ScreeningView is the API projection.
type ScreeningView struct {
	ID              string           `json:"id"`
	ReferenceNo     string           `json:"reference_no"`
	CheckpointID    string           `json:"checkpoint_id"`
	OfficerID       string           `json:"officer_id"`
	DocType         DocType          `json:"doc_type"`
	ImageURL        string           `json:"image_url"`
	Status          ScreeningStatus  `json:"status"`
	Verdict         Verdict          `json:"verdict"`
	RiskScore       float64          `json:"risk_score"`
	Engine          *EngineResult    `json:"engine,omitempty"`
	FailureReason   string           `json:"failure_reason,omitempty"`
	OfficerDecision *OfficerDecision `json:"officer_decision,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

func (s *Screening) View() ScreeningView {
	return ScreeningView{
		ID:              s.ID.Hex(),
		ReferenceNo:     s.ReferenceNo,
		CheckpointID:    s.CheckpointID,
		OfficerID:       s.OfficerID,
		DocType:         s.DocType,
		ImageURL:        "/api/screenings/" + s.ID.Hex() + "/image",
		Status:          s.Status,
		Verdict:         s.Verdict,
		RiskScore:       s.Risk,
		Engine:          s.Engine,
		FailureReason:   s.Failure,
		OfficerDecision: s.OfficerDecision,
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
	}
}

// ScreeningFilter narrows a list query. Zero values mean "no filter".
type ScreeningFilter struct {
	Verdict      Verdict
	DocType      DocType
	Status       ScreeningStatus
	CheckpointID string
}
