package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/repository"
	"github.com/sih26/ps188-backend/internal/response"
	"github.com/sih26/ps188-backend/internal/screening"
	"github.com/sih26/ps188-backend/internal/storage"
)

// Risk-score bumps applied when a post-engine check raises a flag. The verdict is
// never changed and the screening never auto-blocks — these only nudge the score
// the officer sees. Scale is 0.0–1.0.
const (
	riskBumpBlacklist = 0.25
	riskBumpExpired   = 0.15
)

// SubmitInput is the validated payload for a new screening.
type SubmitInput struct {
	OfficerID    string
	IP           string
	CheckpointID string
	Region       string // denormalised from the officer's checkpoint (see the handler)
	DocType      model.DocType
	DocNumber    string
	MRZLine1     string
	MRZLine2     string
	// Optional holder details the officer may enter — used for the blacklist
	// identity check and the expiry check. Missing values fall back to the
	// engine's extracted fields.
	HolderName  string
	DOB         string
	Nationality string
	ExpiryDate  string
	ImageName   string
	Image       []byte
}

// ScreeningService orchestrates: store image -> call engine -> blacklist/expiry
// checks -> persist result, then list/get/decision.
type ScreeningService struct {
	repo      repository.ScreeningRepository
	audit     repository.AuditRepository
	files     storage.FileStore
	engine    screening.Engine
	blacklist *BlacklistService
	log       *slog.Logger
}

func NewScreeningService(
	repo repository.ScreeningRepository,
	audit repository.AuditRepository,
	files storage.FileStore,
	engine screening.Engine,
	blacklist *BlacklistService,
	log *slog.Logger,
) *ScreeningService {
	return &ScreeningService{repo: repo, audit: audit, files: files, engine: engine, blacklist: blacklist, log: log}
}

func (s *ScreeningService) Submit(ctx context.Context, in SubmitInput) (model.ScreeningView, error) {
	var zero model.ScreeningView
	if !in.DocType.Valid() {
		return zero, apperr.ERRORS.InvalidDocType
	}
	if len(in.Image) == 0 {
		return zero, apperr.ERRORS.FileRequired
	}

	fileID, err := s.files.Put(ctx, in.ImageName, bytes.NewReader(in.Image))
	if err != nil {
		return zero, apperr.ERRORS.StorageFailed.Wrap(err)
	}
	imageOID, _ := bson.ObjectIDFromHex(fileID)

	day := time.Now().UTC().Format("20060102")
	seq, err := s.repo.NextSequence(ctx, day)
	if err != nil {
		_ = s.files.Delete(ctx, fileID)
		return zero, err
	}
	ref := fmt.Sprintf("SCR-%s-%05d", day, seq)

	sc := &model.Screening{
		ReferenceNo:     ref,
		CheckpointID:    in.CheckpointID,
		Region:          in.Region,
		OfficerID:       in.OfficerID,
		DocType:         in.DocType,
		ImageFileID:     imageOID,
		ImageName:       in.ImageName,
		SubmittedNumber: in.DocNumber,
		MRZLine1:        in.MRZLine1,
		MRZLine2:        in.MRZLine2,
		Status:          model.StatusProcessing,
		Verdict:         model.VerdictPending,
	}
	sc, err = s.repo.Create(ctx, sc)
	if err != nil {
		_ = s.files.Delete(ctx, fileID)
		return zero, err
	}

	// Call the external model. A failure here is persisted, not fatal — the case
	// still exists and an officer can act on it manually.
	result, engErr := s.engine.Screen(ctx, screening.ScreenRequest{
		DocType:   in.DocType,
		DocNumber: in.DocNumber,
		MRZLine1:  in.MRZLine1,
		MRZLine2:  in.MRZLine2,
		Filename:  in.ImageName,
		Image:     in.Image,
	})

	var updated *model.Screening
	if engErr != nil {
		ae := apperr.From(engErr)
		s.log.WarnContext(ctx, "screening engine failed",
			slog.String("screening_id", sc.ID.Hex()), slog.String("error", ae.Error()))
		updated, err = s.repo.SetResult(ctx, sc.ID.Hex(),
			model.StatusFailed, model.VerdictPending, 0, nil, ae.Message)
	} else {
		eng := &model.EngineResult{
			Verdict:         result.Verdict,
			RiskScore:       result.RiskScore,
			Reasons:         result.Reasons,
			ExtractedFields: result.ExtractedFields,
			Evidence:        result.EvidenceItems,
			RawEvidence:     bson.M(result.RawEvidence),
		}
		updated, err = s.repo.SetResult(ctx, sc.ID.Hex(),
			model.StatusCompleted, result.Verdict, result.RiskScore, eng, "")
	}
	if err != nil {
		return zero, err
	}

	// Post-engine checks: blacklist (document number + identity) and document
	// expiry. A hit raises an advisory flag and bumps the risk score — it never
	// changes the verdict or blocks the officer.
	flags, matches, extraReasons, bump := s.postEngineChecks(ctx, in, result)
	if len(flags) > 0 {
		appendReasons := extraReasons
		if updated.Engine == nil {
			appendReasons = nil // nothing to append reasons to on a failed engine run
		}
		checked, cerr := s.repo.SetChecks(ctx, updated.ID.Hex(), flags, matches,
			clampRisk(updated.Risk+bump), appendReasons)
		if cerr != nil {
			s.log.WarnContext(ctx, "persisting screening checks failed",
				slog.String("screening_id", updated.ID.Hex()), slog.String("error", cerr.Error()))
		} else {
			updated = checked
		}
	}

	_ = s.audit.Insert(ctx, model.AuditLog{
		UserID:        in.OfficerID,
		Action:        model.ActionScreeningSubmitted,
		Region:        updated.Region,
		ReferenceType: "screening",
		ReferenceID:   updated.ID.Hex(),
		NewData: bson.M{
			"reference_no": updated.ReferenceNo,
			"status":       updated.Status,
			"verdict":      updated.Verdict,
			"region":       updated.Region,
			"flags":        updated.Flags,
		},
		IPAddress: in.IP,
		CreatedAt: time.Now().UTC(),
	})
	return updated.View(), nil
}

// postEngineChecks runs the blacklist and expiry checks and returns the flags,
// blacklist matches, extra evidence reasons, and the total risk-score bump.
func (s *ScreeningService) postEngineChecks(ctx context.Context, in SubmitInput, res *screening.ScreenResult) (flags []string, matches []model.BlacklistMatch, reasons []string, bump float64) {
	var extracted []model.ExtractedField
	if res != nil {
		extracted = res.ExtractedFields
	}

	docNumber := pick(in.DocNumber, extractedValue(extracted, "document_number", "passport_number", "doc_number", "id_number"))
	name := pick(in.HolderName, extractedValue(extracted, "name", "full_name"))
	if name == "" {
		surname := extractedValue(extracted, "surname", "last_name")
		given := extractedValue(extracted, "given_name", "given_names", "first_name")
		name = strings.TrimSpace(given + " " + surname)
	}
	dob := pick(in.DOB, extractedValue(extracted, "date_of_birth", "dob", "birth_date"))
	nationality := pick(in.Nationality, extractedValue(extracted, "nationality", "country"))
	expiry := pick(in.ExpiryDate, extractedValue(extracted, "date_of_expiry", "expiry_date", "expiration_date", "expiry"))

	if s.blacklist != nil && (docNumber != "" || name != "") {
		hit, err := s.blacklist.Check(ctx, model.BlacklistProbe{
			DocNumber: docNumber, Name: name, DOB: dob, Nationality: nationality,
		})
		if err != nil {
			s.log.WarnContext(ctx, "blacklist check failed during screening", slog.String("error", err.Error()))
		} else if hit.Hit {
			flags = append(flags, model.FlagBlacklistHit)
			bump += riskBumpBlacklist
			for _, m := range hit.Matches {
				matches = append(matches, model.BlacklistMatch{
					EntryID:   m.ID,
					Kind:      m.Kind,
					DocNumber: m.DocNumber,
					Name:      m.Name,
					Reason:    m.Reason,
					Source:    m.Source,
				})
				reasons = append(reasons, "Blacklist hit ("+string(m.Kind)+"): "+m.Reason)
			}
		}
	}

	if t, ok := parseExpiry(expiry); ok && t.Before(startOfUTCDay(time.Now())) {
		flags = append(flags, model.FlagExpiredDocument)
		bump += riskBumpExpired
		reasons = append(reasons, "Document expired on "+t.Format("2006-01-02"))
	}

	return flags, matches, reasons, bump
}

func (s *ScreeningService) Get(ctx context.Context, id string) (model.ScreeningView, error) {
	sc, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return model.ScreeningView{}, err
	}
	return sc.View(), nil
}

func (s *ScreeningService) List(ctx context.Context, f model.ScreeningFilter, cursor string, limit int64) (response.Page[model.ScreeningView], error) {
	return s.repo.List(ctx, f, cursor, limit)
}

// StreamImage writes the stored document image for id into w.
func (s *ScreeningService) StreamImage(ctx context.Context, id string, w io.Writer) (string, error) {
	sc, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return "", err
	}
	if err := s.files.Get(ctx, sc.ImageFileID.Hex(), w); err != nil {
		return "", apperr.ERRORS.StorageFailed.Wrap(err)
	}
	return sc.ImageName, nil
}

// Decide records the officer's manual call. Exactly one decision per screening.
func (s *ScreeningService) Decide(ctx context.Context, id, actorID, ip string, decision model.Decision, reason string) (model.ScreeningView, error) {
	var zero model.ScreeningView
	if !decision.Valid() {
		return zero, apperr.ERRORS.InvalidDecision
	}
	sc, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return zero, err
	}
	if sc.Status == model.StatusProcessing {
		return zero, apperr.ERRORS.ScreeningNotCompleted
	}
	if sc.OfficerDecision != nil {
		return zero, apperr.ERRORS.AlreadyDecided
	}

	d := model.OfficerDecision{
		Decision:  decision,
		Reason:    reason,
		DecidedBy: actorID,
		DecidedAt: time.Now().UTC(),
	}
	updated, err := s.repo.SetDecision(ctx, id, d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return zero, apperr.ERRORS.AlreadyDecided
	}
	if err != nil {
		return zero, err
	}

	_ = s.audit.Insert(ctx, model.AuditLog{
		UserID:        actorID,
		Action:        model.ActionScreeningDecided,
		Region:        updated.Region,
		ReferenceType: "screening",
		ReferenceID:   id,
		OldData:       bson.M{"verdict": sc.Verdict, "risk_score": sc.Risk},
		NewData: bson.M{
			"decision":     decision,
			"reason":       reason,
			"reference_no": sc.ReferenceNo,
			"detail":       model.ActionScreeningDecided + " · " + strings.ToUpper(string(decision)),
		},
		IPAddress: ip,
		CreatedAt: time.Now().UTC(),
	})
	return updated.View(), nil
}

// pick returns the first non-blank value.
func pick(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// extractedValue returns the first non-blank value among the OCR fields whose
// label case-insensitively matches any of labels.
func extractedValue(fields []model.ExtractedField, labels ...string) string {
	for _, want := range labels {
		for _, f := range fields {
			if strings.EqualFold(strings.TrimSpace(f.Label), want) {
				if s := strings.TrimSpace(f.Value); s != "" {
					return s
				}
			}
		}
	}
	return ""
}

// parseExpiry parses a document expiry date in the common formats an officer or
// the OCR layer might supply (including the MRZ YYMMDD form).
func parseExpiry(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		"2006-01-02", "2006/01/02", "02-01-2006", "02/01/2006", "02.01.2006",
		"02 Jan 2006", time.RFC3339, "20060102", "060102",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func startOfUTCDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func clampRisk(r float64) float64 {
	switch {
	case r > 1:
		return 1
	case r < 0:
		return 0
	default:
		return r
	}
}
