package service_test

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"strings"
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
	svc, _ := newScreeningSvcWithBlacklist(t, engine)
	return svc
}

func newScreeningSvcWithBlacklist(t *testing.T, engine screening.Engine) (*service.ScreeningService, *service.BlacklistService) {
	t.Helper()
	db := testsupport.RequireMongo(t)
	// Local storage (the production default) — t.TempDir() self-cleans.
	files, err := storage.NewLocalFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("local file store: %v", err)
	}
	bl := service.NewBlacklistService(
		repository.NewBlacklistRepository(db),
		repository.NewAuditRepository(db),
		files,
	)
	scr := service.NewScreeningService(
		repository.NewScreeningRepository(db),
		repository.NewAuditRepository(db),
		files,
		engine,
		bl,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	return scr, bl
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

func TestScreeningService_Submit_StampsRegion(t *testing.T) {
	svc := newScreeningSvc(t, &screening.MockEngine{Force: model.VerdictGenuine})
	in := submitInput()
	in.Region = "north"

	view, err := svc.Submit(context.Background(), in)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if view.Region != "north" {
		t.Fatalf("region = %q, want north", view.Region)
	}

	got, err := svc.List(context.Background(), model.ScreeningFilter{Region: "north"}, "", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got.Data) != 1 || got.Data[0].ID != view.ID {
		t.Fatalf("region list = %+v", got.Data)
	}
}

func TestScreeningService_Submit_BlacklistHit_RaisesFlag(t *testing.T) {
	svc, bl := newScreeningSvcWithBlacklist(t, &screening.MockEngine{Force: model.VerdictGenuine})
	ctx := context.Background()

	if _, err := bl.Add(ctx, "admin-1", "127.0.0.1", model.CreateBlacklistInput{
		Kind:      model.BlacklistDocument,
		DocNumber: "Z1234567", // matches submitInput().DocNumber
		Reason:    "reported stolen",
	}); err != nil {
		t.Fatalf("seed blacklist: %v", err)
	}

	view, err := svc.Submit(ctx, submitInput())
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if !slices.Contains(view.Flags, model.FlagBlacklistHit) {
		t.Fatalf("flags = %v, want blacklist_hit", view.Flags)
	}
	if len(view.BlacklistMatches) != 1 || view.BlacklistMatches[0].Reason != "reported stolen" {
		t.Fatalf("blacklist_matches = %+v", view.BlacklistMatches)
	}
	if view.RiskScore <= 10 { // 0.10 engine baseline → 10/100
		t.Fatalf("risk_score = %v, expected a bump over the 10/100 engine baseline", view.RiskScore)
	}
	if view.Verdict != model.VerdictSuspicious {
		t.Fatalf("verdict = %v, want the blacklist bump to upgrade GENUINE to SUSPICIOUS", view.Verdict)
	}
	// The reason is surfaced on the engine evidence.
	if view.Engine == nil || !slices.ContainsFunc(view.Engine.Reasons, func(r string) bool {
		return strings.Contains(r, "Blacklist hit")
	}) {
		t.Fatalf("engine reasons missing blacklist note: %+v", view.Engine)
	}

	// Persisted, not just returned.
	got, err := svc.Get(ctx, view.ID)
	if err != nil || !slices.Contains(got.Flags, model.FlagBlacklistHit) {
		t.Fatalf("get: %v / flags %v", err, got.Flags)
	}
}

func TestScreeningService_Submit_Clean_NoFlag(t *testing.T) {
	svc := newScreeningSvc(t, &screening.MockEngine{Force: model.VerdictGenuine})

	view, err := svc.Submit(context.Background(), submitInput())
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(view.Flags) != 0 {
		t.Fatalf("flags = %v, want none", view.Flags)
	}
	if len(view.BlacklistMatches) != 0 {
		t.Fatalf("blacklist_matches = %+v, want none", view.BlacklistMatches)
	}
	if view.RiskScore != 10 {
		t.Fatalf("risk_score = %v, want unchanged 10/100", view.RiskScore)
	}
}

func TestScreeningService_Submit_ExpiredDocument_RaisesFlag(t *testing.T) {
	svc := newScreeningSvc(t, &screening.MockEngine{Force: model.VerdictGenuine})

	in := submitInput()
	in.ExpiryDate = "2000-01-01"
	view, err := svc.Submit(context.Background(), in)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if !slices.Contains(view.Flags, model.FlagExpiredDocument) {
		t.Fatalf("flags = %v, want expired_document", view.Flags)
	}
	if view.RiskScore <= 10 {
		t.Fatalf("risk_score = %v, expected an expiry bump", view.RiskScore)
	}

	// A future expiry date does not flag.
	in2 := submitInput()
	in2.ExpiryDate = "2999-12-31"
	view2, err := svc.Submit(context.Background(), in2)
	if err != nil {
		t.Fatalf("submit 2: %v", err)
	}
	if len(view2.Flags) != 0 {
		t.Fatalf("future-expiry flags = %v, want none", view2.Flags)
	}
}

func TestScreeningService_Submit_EngineDown_PersistsFailed(t *testing.T) {
	svc := newScreeningSvc(t, &screening.MockEngine{
		Err: apperr.ERRORS.ScreeningEngineUnavailable,
	})

	in := submitInput()
	in.DocType = "" // officer left it to the model, which is now unreachable
	view, err := svc.Submit(context.Background(), in)
	if err != nil {
		t.Fatalf("submit should not fail hard: %v", err)
	}
	if view.Status != model.StatusFailed {
		t.Fatalf("status = %q, want failed", view.Status)
	}
	if view.FailureReason == "" {
		t.Fatal("expected a failure reason")
	}
	if !view.DocType.Valid() {
		t.Fatalf("doc_type must never be blank, even on a failed run — got %q", view.DocType)
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

func TestScreeningService_Submit_AutoDocType(t *testing.T) {
	// No doc type from the officer — the engine classifies one and it is persisted.
	svc := newScreeningSvc(t, &screening.MockEngine{Force: model.VerdictGenuine})
	in := submitInput()
	in.DocType = ""

	view, err := svc.Submit(context.Background(), in)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if view.Status != model.StatusCompleted {
		t.Fatalf("status = %q", view.Status)
	}
	if !view.DocType.Valid() {
		t.Fatalf("doc_type not classified: %q", view.DocType)
	}
}

func TestScreeningService_Decide_OncePerScreening(t *testing.T) {
	svc := newScreeningSvc(t, &screening.MockEngine{Force: model.VerdictFake})
	ctx := context.Background()

	view, err := svc.Submit(ctx, submitInput())
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	decided, err := svc.Decide(ctx, view.ID, "officer-1", "127.0.0.1", model.DecisionReject, "MRZ mismatch")
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if decided.OfficerDecision == nil || decided.OfficerDecision.Decision != model.DecisionReject {
		t.Fatalf("decision not embedded: %+v", decided.OfficerDecision)
	}

	_, err = svc.Decide(ctx, view.ID, "officer-2", "127.0.0.1", model.DecisionAccept, "override")
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
