package repository_test

import (
	"context"
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/repository"
	"github.com/sih26/ps188-backend/internal/testsupport"
)

func newScreening(ref string) *model.Screening {
	return &model.Screening{
		ReferenceNo:  ref,
		CheckpointID: "CP-1",
		OfficerID:    "officer-1",
		DocType:      model.DocPassport,
		Status:       model.StatusProcessing,
		Verdict:      model.VerdictPending,
	}
}

func TestScreeningRepository_CreateAndFind(t *testing.T) {
	db := testsupport.RequireMongo(t)
	repo := repository.NewScreeningRepository(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, newScreening("SCR-1"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID.IsZero() {
		t.Fatal("expected an assigned _id")
	}

	got, err := repo.FindByID(ctx, created.ID.Hex())
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.ReferenceNo != "SCR-1" {
		t.Fatalf("reference_no = %q", got.ReferenceNo)
	}
}

func TestScreeningRepository_FindByID_NotFound(t *testing.T) {
	db := testsupport.RequireMongo(t)
	repo := repository.NewScreeningRepository(db)

	_, err := repo.FindByID(context.Background(), "64b7f9b0c0000000000000aa")
	if ae := apperr.From(err); ae == nil || ae.Code != apperr.ERRORS.ScreeningNotFound.Code {
		t.Fatalf("want ScreeningNotFound, got %v", err)
	}
}

func TestScreeningRepository_ListPagination(t *testing.T) {
	db := testsupport.RequireMongo(t)
	repo := repository.NewScreeningRepository(db)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := repo.Create(ctx, newScreening("SCR-P-"+string(rune('a'+i)))); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	page1, err := repo.List(ctx, model.ScreeningFilter{}, "", 2)
	if err != nil {
		t.Fatalf("list p1: %v", err)
	}
	if len(page1.Data) != 2 || !page1.Info.HasNext || page1.Info.NextCursor == "" {
		t.Fatalf("page1 = %+v", page1.Info)
	}

	page2, err := repo.List(ctx, model.ScreeningFilter{}, page1.Info.NextCursor, 2)
	if err != nil {
		t.Fatalf("list p2: %v", err)
	}
	if len(page2.Data) != 2 {
		t.Fatalf("page2 len = %d", len(page2.Data))
	}
	if page1.Data[0].ID == page2.Data[0].ID {
		t.Fatal("page2 overlaps page1")
	}
}

func TestScreeningRepository_ListFilters(t *testing.T) {
	db := testsupport.RequireMongo(t)
	repo := repository.NewScreeningRepository(db)
	ctx := context.Background()

	mk := func(ref, officer, region string, decided bool, decision model.Decision) {
		s := newScreening(ref)
		s.OfficerID = officer
		s.Region = region
		s.Status = model.StatusCompleted
		if decided {
			s.OfficerDecision = &model.OfficerDecision{Decision: decision, Reason: "x", DecidedBy: officer}
		}
		if _, err := repo.Create(ctx, s); err != nil {
			t.Fatalf("seed %s: %v", ref, err)
		}
	}
	mk("SCR-F1", "off-A", "north", false, "")
	mk("SCR-F2", "off-A", "north", true, model.DecisionAccept)
	mk("SCR-F3", "off-B", "south", true, model.DecisionReject)
	mk("SCR-F4", "off-B", "north", false, "")

	count := func(f model.ScreeningFilter) int {
		page, err := repo.List(ctx, f, "", 100)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		return len(page.Data)
	}

	if n := count(model.ScreeningFilter{OfficerID: "off-A"}); n != 2 {
		t.Fatalf("officer filter = %d, want 2", n)
	}
	if n := count(model.ScreeningFilter{Region: "north"}); n != 3 {
		t.Fatalf("region filter = %d, want 3", n)
	}
	no := false
	if n := count(model.ScreeningFilter{Decided: &no}); n != 2 {
		t.Fatalf("undecided filter = %d, want 2", n)
	}
	yes := true
	if n := count(model.ScreeningFilter{Decided: &yes, Region: "north"}); n != 1 {
		t.Fatalf("decided+region filter = %d, want 1", n)
	}
	if n := count(model.ScreeningFilter{DecisionValue: model.DecisionReject}); n != 1 {
		t.Fatalf("decision-value filter = %d, want 1", n)
	}
	// "Flagged for review": suspicious/fake + undecided + region — via filters, no new route.
	if n := count(model.ScreeningFilter{Region: "north", Decided: &no}); n != 2 {
		t.Fatalf("flagged-for-review shape = %d, want 2", n)
	}
}

func TestScreeningRepository_SetChecks(t *testing.T) {
	db := testsupport.RequireMongo(t)
	repo := repository.NewScreeningRepository(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, newScreening("SCR-CHK"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := created.ID.Hex()

	// Give it an engine result so the reason append has somewhere to land.
	eng := &model.EngineResult{Verdict: model.VerdictGenuine, RiskScore: 0.1, Reasons: []string{"looks clean"}}
	if _, err := repo.SetResult(ctx, id, model.StatusCompleted, model.VerdictGenuine, 0.1, eng, ""); err != nil {
		t.Fatalf("set result: %v", err)
	}

	matches := []model.BlacklistMatch{{EntryID: "e1", Kind: model.BlacklistDocument, DocNumber: "Z1", Reason: "stolen"}}
	updated, err := repo.SetChecks(ctx, id, []string{model.FlagBlacklistHit}, matches, 0.35,
		[]string{"Blacklist hit (document): stolen"})
	if err != nil {
		t.Fatalf("set checks: %v", err)
	}
	if len(updated.Flags) != 1 || updated.Flags[0] != model.FlagBlacklistHit {
		t.Fatalf("flags = %v", updated.Flags)
	}
	if len(updated.BlacklistMatches) != 1 || updated.BlacklistMatches[0].EntryID != "e1" {
		t.Fatalf("matches = %+v", updated.BlacklistMatches)
	}
	if updated.Risk != 0.35 {
		t.Fatalf("risk = %v", updated.Risk)
	}
	if updated.Engine == nil || len(updated.Engine.Reasons) != 2 {
		t.Fatalf("engine reasons not appended: %+v", updated.Engine)
	}
	if updated.Verdict != model.VerdictGenuine {
		t.Fatalf("verdict changed to %q", updated.Verdict)
	}
}

func TestScreeningRepository_SetDecision_SingleWriter(t *testing.T) {
	db := testsupport.RequireMongo(t)
	repo := repository.NewScreeningRepository(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, newScreening("SCR-D"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := created.ID.Hex()
	dec := model.OfficerDecision{Decision: model.DecisionAccept, Reason: "ok", DecidedBy: "officer-1"}

	if _, err := repo.SetDecision(ctx, id, dec); err != nil {
		t.Fatalf("first decision: %v", err)
	}
	_, err = repo.SetDecision(ctx, id, dec)
	if !errors.Is(err, mongo.ErrNoDocuments) {
		t.Fatalf("second decision: want ErrNoDocuments, got %v", err)
	}
}

func TestScreeningRepository_NextSequence(t *testing.T) {
	db := testsupport.RequireMongo(t)
	repo := repository.NewScreeningRepository(db)
	ctx := context.Background()

	a, err := repo.NextSequence(ctx, "20260101")
	if err != nil {
		t.Fatalf("seq a: %v", err)
	}
	b, err := repo.NextSequence(ctx, "20260101")
	if err != nil {
		t.Fatalf("seq b: %v", err)
	}
	if b != a+1 {
		t.Fatalf("want %d, got %d", a+1, b)
	}
}
