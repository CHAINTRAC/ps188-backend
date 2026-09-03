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
	ImageName    string
	Image        []byte
}

// ScreeningService orchestrates: store image -> call engine -> persist result,
// then list/get/decision.
type ScreeningService struct {
	repo   repository.ScreeningRepository
	audit  repository.AuditRepository
	files  storage.FileStore
	engine screening.Engine
	log    *slog.Logger
}

func NewScreeningService(
	repo repository.ScreeningRepository,
	audit repository.AuditRepository,
	files storage.FileStore,
	engine screening.Engine,
	log *slog.Logger,
) *ScreeningService {
	return &ScreeningService{repo: repo, audit: audit, files: files, engine: engine, log: log}
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
			Evidence:        bson.M(result.Evidence),
		}
		updated, err = s.repo.SetResult(ctx, sc.ID.Hex(),
			model.StatusCompleted, result.Verdict, result.RiskScore, eng, "")
	}
	if err != nil {
		return zero, err
	}

	_ = s.audit.Insert(ctx, model.AuditLog{
		UserID:        in.OfficerID,
		Action:        model.ActionScreeningSubmitted,
		ReferenceType: "screening",
		ReferenceID:   updated.ID.Hex(),
		NewData: bson.M{
			"reference_no": updated.ReferenceNo,
			"status":       updated.Status,
			"verdict":      updated.Verdict,
			"region":       updated.Region,
		},
		IPAddress: in.IP,
		CreatedAt: time.Now().UTC(),
	})
	return updated.View(), nil
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
		ReferenceType: "screening",
		ReferenceID:   id,
		OldData:       bson.M{"verdict": sc.Verdict, "risk_score": sc.Risk},
		NewData: bson.M{
			"decision": decision,
			"reason":   reason,
			"detail":   model.ActionScreeningDecided + " · " + strings.ToUpper(string(decision)),
		},
		IPAddress: ip,
		CreatedAt: time.Now().UTC(),
	})
	return updated.View(), nil
}
