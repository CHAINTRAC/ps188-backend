package service_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/repository"
	"github.com/sih26/ps188-backend/internal/screening"
	"github.com/sih26/ps188-backend/internal/service"
	"github.com/sih26/ps188-backend/internal/storage"
	"github.com/sih26/ps188-backend/internal/testsupport"
)

func newScreeningSvc(t *testing.T, engine screening.Engine) *service.ScreeningService {
	t.Helper()
	db := testsupport.RequireMongo(t)
	return service.NewScreeningService(
		repository.NewScreeningRepository(db),
		repository.NewAuditRepository(db),
		storage.NewGridFS(db),
		engine,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}

var pngPixel = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89,
}

func submitInput() service.SubmitInput {
	return service.SubmitInput{
		OfficerID:    "officer-1",
		IP:           "127.0.0.1",
		CheckpointID: "CP-1",
		DocType:      model.DocPassport,
		DocNumber:    "Z1234567",
		ImageName:    "passport.png",
		Image:        pngPixel,
	}
}

func TestScreeningService_Submit_Completed(t *testing.T) {
	svc := newScreeningSvc(t, &screening.MockEngine{Force: model.VerdictSuspicious})

	view, err := svc.Submit(context.Background(), submitInput())
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if view.Status != model.StatusCompleted {
		t.Fatalf("status = %q", view.Status)
	}
	if view.Verdict != model.VerdictSuspicious {
		t.Fatalf("verdict = %q", view.Verdict)
	}
	if view.Engine == nil || view.ReferenceNo == "" {
		t.Fatalf("missing engine result / reference_no: %+v", view)
	}
}

func TestScreeningService_Submit_EngineDown_PersistsFailed(t *testing.T) {
	svc := newScreeningSvc(t, &screening.MockEngine{
		Err: apperr.ERRORS.ScreeningEngineUnavailable,
	})

	view, err := svc.Submit(context.Background(), submitInput())
	if err != nil {
		t.Fatalf("submit should not fail hard: %v", err)
	}
	if view.Status != model.StatusFailed {
		t.Fatalf("status = %q, want failed", view.Status)
	}
	if view.FailureReason == "" {
		t.Fatal("expected a failure reason")
	}

	// The case is retrievable and carries the failure.
	got, err := svc.Get(context.Background(), view.ID)
	if err != nil || got.Status != model.StatusFailed {
		t.Fatalf("get: %v / %q", err, got.Status)
	}
}

func TestScreeningService_Submit_InvalidDocType(t *testing.T) {
	svc := newScreeningSvc(t, &screening.MockEngine{})
	in := submitInput()
	in.DocType = "hologram"

	_, err := svc.Submit(context.Background(), in)
	if ae := apperr.From(err); ae == nil || ae.Code != apperr.ERRORS.InvalidDocType.Code {
		t.Fatalf("want InvalidDocType, got %v", err)
	}
}

func TestScreeningService_Decide_OncePerScreening(t *testing.T) {
	svc := newScreeningSvc(t, &screening.MockEngine{Force: model.VerdictFake})
	ctx := context.Background()

	view, err := svc.Submit(ctx, submitInput())
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	decided, err := svc.Decide(ctx, view.ID, "officer-1", "127.0.0.1", model.DecisionDetain, "MRZ mismatch")
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if decided.OfficerDecision == nil || decided.OfficerDecision.Decision != model.DecisionDetain {
		t.Fatalf("decision not embedded: %+v", decided.OfficerDecision)
	}

	_, err = svc.Decide(ctx, view.ID, "officer-2", "127.0.0.1", model.DecisionClear, "override")
	if ae := apperr.From(err); ae == nil || ae.Code != apperr.ERRORS.AlreadyDecided.Code {
		t.Fatalf("want AlreadyDecided, got %v", err)
	}
}

func TestScreeningService_Decide_InvalidDecision(t *testing.T) {
	svc := newScreeningSvc(t, &screening.MockEngine{})
	ctx := context.Background()
	view, err := svc.Submit(ctx, submitInput())
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	_, err = svc.Decide(ctx, view.ID, "officer-1", "127.0.0.1", model.Decision("arrest"), "x")
	if ae := apperr.From(err); ae == nil || ae.Code != apperr.ERRORS.InvalidDecision.Code {
		t.Fatalf("want InvalidDecision, got %v", err)
	}
}
