package repository_test

import (
	"context"
	"testing"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/repository"
	"github.com/sih26/ps188-backend/internal/testsupport"
)

func docEntry(num string) *model.BlacklistEntry {
	return &model.BlacklistEntry{
		Kind:      model.BlacklistDocument,
		DocNumber: model.NormalizeBlacklistKey(num),
		Reason:    "stolen booklet",
		AddedBy:   "admin-1",
		Active:    true,
	}
}

func TestBlacklistRepository_CreateAndFind(t *testing.T) {
	db := testsupport.RequireMongo(t)
	repo := repository.NewBlacklistRepository(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, docEntry("Z1234567"))
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
	if got.DocNumber != "Z1234567" {
		t.Fatalf("doc_number = %q", got.DocNumber)
	}
}

func TestBlacklistRepository_FindByID_NotFound(t *testing.T) {
	db := testsupport.RequireMongo(t)
	repo := repository.NewBlacklistRepository(db)

	_, err := repo.FindByID(context.Background(), "64b7f9b0c0000000000000aa")
	if ae := apperr.From(err); ae == nil || ae.Code != apperr.ERRORS.BlacklistEntryNotFound.Code {
		t.Fatalf("want BlacklistEntryNotFound, got %v", err)
	}
}

func TestBlacklistRepository_LookupByDocNumber_CaseInsensitiveActiveOnly(t *testing.T) {
	db := testsupport.RequireMongo(t)
	repo := repository.NewBlacklistRepository(db)
	ctx := context.Background()

	if _, err := repo.Create(ctx, docEntry("z1234567")); err != nil {
		t.Fatalf("seed: %v", err)
	}

	hits, err := repo.LookupByDocNumber(ctx, "  Z1234567 ")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("want 1 hit, got %d", len(hits))
	}

	if _, err := repo.Deactivate(ctx, hits[0].ID.Hex()); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	hits, err = repo.LookupByDocNumber(ctx, "Z1234567")
	if err != nil {
		t.Fatalf("lookup after deactivate: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("want 0 hits after deactivate, got %d", len(hits))
	}
}

func TestBlacklistRepository_LookupByIdentity_NarrowsOnDOB(t *testing.T) {
	db := testsupport.RequireMongo(t)
	repo := repository.NewBlacklistRepository(db)
	ctx := context.Background()

	_, err := repo.Create(ctx, &model.BlacklistEntry{
		Kind:    model.BlacklistIdentity,
		Name:    model.NormalizeBlacklistKey("John Doe"),
		DOB:     "1985-01-01",
		Reason:  "known alias",
		AddedBy: "admin-1",
		Active:  true,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Matching name + DOB.
	hits, err := repo.LookupByIdentity(ctx, "JOHN DOE", "1985-01-01", "")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("want 1 hit, got %d", len(hits))
	}

	// Same name, different DOB -> no match.
	hits, err = repo.LookupByIdentity(ctx, "JOHN DOE", "1990-12-31", "")
	if err != nil {
		t.Fatalf("lookup 2: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("want 0 hits for wrong DOB, got %d", len(hits))
	}
}

func TestBlacklistRepository_List_ActiveFilterAndPagination(t *testing.T) {
	db := testsupport.RequireMongo(t)
	repo := repository.NewBlacklistRepository(db)
	ctx := context.Background()

	for _, n := range []string{"A1", "A2", "A3", "A4"} {
		if _, err := repo.Create(ctx, docEntry(n)); err != nil {
			t.Fatalf("seed %s: %v", n, err)
		}
	}
	page1, err := repo.List(ctx, model.BlacklistFilter{}, "", 2)
	if err != nil {
		t.Fatalf("list p1: %v", err)
	}
	if len(page1.Data) != 2 || !page1.Info.HasNext {
		t.Fatalf("page1 = %+v", page1.Info)
	}

	active := true
	all, err := repo.List(ctx, model.BlacklistFilter{Active: &active}, "", 100)
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(all.Data) != 4 {
		t.Fatalf("want 4 active, got %d", len(all.Data))
	}
}
