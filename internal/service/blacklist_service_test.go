package service_test

import (
	"context"
	"testing"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/repository"
	"github.com/sih26/ps188-backend/internal/service"
	"github.com/sih26/ps188-backend/internal/storage"
	"github.com/sih26/ps188-backend/internal/testsupport"
)

func newBlacklistSvc(t *testing.T) *service.BlacklistService {
	t.Helper()
	db := testsupport.RequireMongo(t)
	files, err := storage.NewLocalFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("local file store: %v", err)
	}
	return service.NewBlacklistService(
		repository.NewBlacklistRepository(db),
		repository.NewAuditRepository(db),
		files,
	)
}

func TestBlacklistService_AddDocument_ThenCheckHits(t *testing.T) {
	svc := newBlacklistSvc(t)
	ctx := context.Background()

	view, err := svc.Add(ctx, "admin-1", "127.0.0.1", model.CreateBlacklistInput{
		Kind:      model.BlacklistDocument,
		DocNumber: "z1234567",
		Reason:    "reported stolen",
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if !view.Active || view.DocNumber != "Z1234567" {
		t.Fatalf("view = %+v", view)
	}

	res, err := svc.Check(ctx, model.BlacklistProbe{DocNumber: "Z1234567"})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !res.Hit || len(res.Matches) != 1 {
		t.Fatalf("check result = %+v", res)
	}

	// Deactivating clears the hit.
	if _, err := svc.Deactivate(ctx, "admin-1", "127.0.0.1", view.ID); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	res, err = svc.Check(ctx, model.BlacklistProbe{DocNumber: "Z1234567"})
	if err != nil {
		t.Fatalf("check 2: %v", err)
	}
	if res.Hit {
		t.Fatalf("expected no hit after deactivate, got %+v", res)
	}
}

func TestBlacklistService_AddDuplicateDocument_Rejected(t *testing.T) {
	svc := newBlacklistSvc(t)
	ctx := context.Background()
	in := model.CreateBlacklistInput{Kind: model.BlacklistDocument, DocNumber: "X99", Reason: "dupe"}

	if _, err := svc.Add(ctx, "admin-1", "127.0.0.1", in); err != nil {
		t.Fatalf("first add: %v", err)
	}
	_, err := svc.Add(ctx, "admin-1", "127.0.0.1", in)
	if ae := apperr.From(err); ae == nil || ae.Code != apperr.ERRORS.BlacklistEntryExists.Code {
		t.Fatalf("want BlacklistEntryExists, got %v", err)
	}
}

func TestBlacklistService_Add_Validation(t *testing.T) {
	svc := newBlacklistSvc(t)
	ctx := context.Background()

	_, err := svc.Add(ctx, "admin-1", "127.0.0.1", model.CreateBlacklistInput{
		Kind: "hologram", Reason: "x",
	})
	if ae := apperr.From(err); ae == nil || ae.Code != apperr.ERRORS.InvalidBlacklistKind.Code {
		t.Fatalf("want InvalidBlacklistKind, got %v", err)
	}

	_, err = svc.Add(ctx, "admin-1", "127.0.0.1", model.CreateBlacklistInput{
		Kind: model.BlacklistDocument, Reason: "no number",
	})
	if ae := apperr.From(err); ae == nil || ae.Code != apperr.ERRORS.BlacklistFieldsMissing.Code {
		t.Fatalf("want BlacklistFieldsMissing, got %v", err)
	}
}

func TestBlacklistService_CheckIdentity(t *testing.T) {
	svc := newBlacklistSvc(t)
	ctx := context.Background()

	if _, err := svc.Add(ctx, "admin-1", "127.0.0.1", model.CreateBlacklistInput{
		Kind:   model.BlacklistIdentity,
		Name:   "Jane Roe",
		DOB:    "1979-06-15",
		Reason: "multiple identities",
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	res, err := svc.Check(ctx, model.BlacklistProbe{Name: "JANE ROE", DOB: "1979-06-15"})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !res.Hit {
		t.Fatalf("expected identity hit, got %+v", res)
	}

	miss, err := svc.Check(ctx, model.BlacklistProbe{Name: "Someone Else"})
	if err != nil {
		t.Fatalf("check miss: %v", err)
	}
	if miss.Hit {
		t.Fatalf("expected miss, got %+v", miss)
	}
}
