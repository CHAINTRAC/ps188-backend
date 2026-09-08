package service

import (
	"bytes"
	"context"
	"io"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/repository"
	"github.com/sih26/ps188-backend/internal/response"
	"github.com/sih26/ps188-backend/internal/storage"
)

// BlacklistService manages blacklisted document numbers and identities
// (admin-only writes) and the read-side Check used during screening.
type BlacklistService struct {
	repo  repository.BlacklistRepository
	audit repository.AuditRepository
	files storage.FileStore
}

func NewBlacklistService(repo repository.BlacklistRepository, audit repository.AuditRepository, files storage.FileStore) *BlacklistService {
	return &BlacklistService{repo: repo, audit: audit, files: files}
}

// BlacklistCheckResult is what Check returns: whether anything matched and the
// matching entries (client-safe views).
type BlacklistCheckResult struct {
	Hit     bool                  `json:"hit"`
	Matches []model.BlacklistView `json:"matches"`
}

func (s *BlacklistService) Add(ctx context.Context, actorID, ip string, in model.CreateBlacklistInput) (model.BlacklistView, error) {
	var zero model.BlacklistView
	if !in.Kind.Valid() {
		return zero, apperr.ERRORS.InvalidBlacklistKind
	}
	if strings.TrimSpace(in.Reason) == "" {
		return zero, apperr.ERRORS.BlacklistFieldsMissing
	}
	if in.DocType != "" && !in.DocType.Valid() {
		return zero, apperr.ERRORS.InvalidDocType
	}

	entry := &model.BlacklistEntry{
		Kind:    in.Kind,
		DocType: in.DocType,
		Reason:  in.Reason,
		Source:  in.Source,
		AddedBy: actorID,
		Active:  true,
	}

	if len(in.Photo) > 0 {
		fileID, err := s.files.Put(ctx, in.PhotoName, bytes.NewReader(in.Photo))
		if err != nil {
			return zero, apperr.ERRORS.StorageFailed.Wrap(err)
		}
		oid, _ := bson.ObjectIDFromHex(fileID)
		entry.PhotoFileID = oid
		entry.PhotoName = in.PhotoName
	}

	switch in.Kind {
	case model.BlacklistDocument:
		entry.DocNumber = model.NormalizeBlacklistKey(in.DocNumber)
		if entry.DocNumber == "" {
			return zero, apperr.ERRORS.BlacklistFieldsMissing
		}
		hits, err := s.repo.LookupByDocNumber(ctx, entry.DocNumber)
		if err != nil {
			return zero, err
		}
		if len(hits) > 0 {
			return zero, apperr.ERRORS.BlacklistEntryExists
		}
	case model.BlacklistIdentity:
		entry.Name = model.NormalizeBlacklistKey(in.Name)
		entry.DOB = model.NormalizeBlacklistKey(in.DOB)
		entry.Nationality = model.NormalizeBlacklistKey(in.Nationality)
		if entry.Name == "" {
			return zero, apperr.ERRORS.BlacklistFieldsMissing
		}
		hits, err := s.repo.LookupByIdentity(ctx, entry.Name, entry.DOB, entry.Nationality)
		if err != nil {
			return zero, err
		}
		for _, h := range hits {
			if h.DOB == entry.DOB && h.Nationality == entry.Nationality {
				return zero, apperr.ERRORS.BlacklistEntryExists
			}
		}
	}

	created, err := s.repo.Create(ctx, entry)
	if err != nil {
		return zero, err
	}

	_ = s.audit.Insert(ctx, model.AuditLog{
		UserID:        actorID,
		Action:        model.ActionBlacklistAdded,
		ReferenceType: "blacklist",
		ReferenceID:   created.ID.Hex(),
		NewData: bson.M{
			"kind":       created.Kind,
			"doc_number": created.DocNumber,
			"name":       created.Name,
			"reason":     created.Reason,
		},
		IPAddress: ip,
		CreatedAt: time.Now().UTC(),
	})
	return created.View(), nil
}

func (s *BlacklistService) Get(ctx context.Context, id string) (model.BlacklistView, error) {
	e, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return model.BlacklistView{}, err
	}
	return e.View(), nil
}

func (s *BlacklistService) List(ctx context.Context, f model.BlacklistFilter, cursor string, limit int64) (response.Page[model.BlacklistView], error) {
	return s.repo.List(ctx, f, cursor, limit)
}

// StreamPhoto writes the stored photo for id into w.
func (s *BlacklistService) StreamPhoto(ctx context.Context, id string, w io.Writer) (string, error) {
	e, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return "", err
	}
	if e.PhotoFileID.IsZero() {
		return "", apperr.ERRORS.BlacklistEntryNotFound
	}
	if err := s.files.Get(ctx, e.PhotoFileID.Hex(), w); err != nil {
		return "", apperr.ERRORS.StorageFailed.Wrap(err)
	}
	return e.PhotoName, nil
}

func (s *BlacklistService) Deactivate(ctx context.Context, actorID, ip, id string) (model.BlacklistView, error) {
	before, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return model.BlacklistView{}, err
	}
	updated, err := s.repo.Deactivate(ctx, id)
	if err != nil {
		return model.BlacklistView{}, err
	}

	_ = s.audit.Insert(ctx, model.AuditLog{
		UserID:        actorID,
		Action:        model.ActionBlacklistDeactivated,
		ReferenceType: "blacklist",
		ReferenceID:   id,
		OldData:       bson.M{"active": before.Active},
		NewData:       bson.M{"active": updated.Active},
		IPAddress:     ip,
		CreatedAt:     time.Now().UTC(),
	})
	return updated.View(), nil
}

// Check screens a probe (document number and/or identity fields) against the
// active blacklist. It never blocks — callers decide what to do with a hit.
func (s *BlacklistService) Check(ctx context.Context, p model.BlacklistProbe) (BlacklistCheckResult, error) {
	seen := map[string]struct{}{}
	var matches []model.BlacklistView

	add := func(entries []model.BlacklistEntry) {
		for _, e := range entries {
			id := e.ID.Hex()
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			matches = append(matches, e.View())
		}
	}

	if p.DocNumber != "" {
		hits, err := s.repo.LookupByDocNumber(ctx, p.DocNumber)
		if err != nil {
			return BlacklistCheckResult{}, err
		}
		add(hits)
	}
	if p.Name != "" {
		hits, err := s.repo.LookupByIdentity(ctx, p.Name, p.DOB, p.Nationality)
		if err != nil {
			return BlacklistCheckResult{}, err
		}
		add(hits)
	}

	return BlacklistCheckResult{Hit: len(matches) > 0, Matches: matches}, nil
}
