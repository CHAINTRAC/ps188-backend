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

func TestScreeningRepository_SetDecision_SingleWriter(t *testing.T) {
	db := testsupport.RequireMongo(t)
	repo := repository.NewScreeningRepository(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, newScreening("SCR-D"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := created.ID.Hex()
	dec := model.OfficerDecision{Decision: model.DecisionClear, Reason: "ok", DecidedBy: "officer-1"}

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
