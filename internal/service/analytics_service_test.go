package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/platform/jwt"
	"github.com/sih26/ps188-backend/internal/repository"
	"github.com/sih26/ps188-backend/internal/service"
	"github.com/sih26/ps188-backend/internal/testsupport"
)

func newAnalyticsSvc(db *mongo.Database) *service.AnalyticsService {
	return service.NewAnalyticsService(
		repository.NewScreeningRepository(db),
		repository.NewUserRepository(db),
		repository.NewCheckpointRepository(db),
	)
}

func seedScreening(t *testing.T, db *mongo.Database, n int, mut func(*model.Screening)) *model.Screening {
	t.Helper()
	s := &model.Screening{
		ReferenceNo:  fmt.Sprintf("SCR-%04d", n),
		CheckpointID: "CP-1",
		Region:       "north",
		OfficerID:    "officer-1",
		DocType:      model.DocPassport,
		Status:       model.StatusCompleted,
		Verdict:      model.VerdictGenuine,
	}
	if mut != nil {
		mut(s)
	}
	created, err := repository.NewScreeningRepository(db).Create(context.Background(), s)
	if err != nil {
		t.Fatalf("seed screening: %v", err)
	}
	return created
}

func TestAnalyticsService_DashboardSummary_VerifierScoped(t *testing.T) {
	db := testsupport.RequireMongo(t)
	svc := newAnalyticsSvc(db)
	ctx := context.Background()

	// Two of mine, one decided; one belongs to another officer.
	seedScreening(t, db, 1, func(s *model.Screening) { s.OfficerID = "me" })
	seedScreening(t, db, 2, func(s *model.Screening) {
		s.OfficerID = "me"
		s.Verdict = model.VerdictFake
		s.OfficerDecision = &model.OfficerDecision{
			Decision: model.DecisionReject, Reason: "x", DecidedBy: "me",
			DecidedAt: time.Now().UTC().Add(2 * time.Minute),
		}
	})
	seedScreening(t, db, 3, func(s *model.Screening) { s.OfficerID = "other" })

	sum, err := svc.DashboardSummary(ctx, jwt.TokenData{UserID: "me", Role: string(model.RoleVerifier)})
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if sum.ScreeningsToday != 2 {
		t.Fatalf("screenings_today = %d, want 2", sum.ScreeningsToday)
	}
	if sum.PendingDecisions != 1 {
		t.Fatalf("pending = %d, want 1", sum.PendingDecisions)
	}
	if sum.DecidedToday != 1 {
		t.Fatalf("decided_today = %d, want 1", sum.DecidedToday)
	}
	if sum.VerdictSplit.Genuine != 1 || sum.VerdictSplit.Fake != 1 {
		t.Fatalf("verdict_split = %+v", sum.VerdictSplit)
	}
	if len(sum.WeeklyVolume) != 7 {
		t.Fatalf("weekly_volume len = %d, want 7", len(sum.WeeklyVolume))
	}
	if sum.AvgDecisionSecond <= 0 {
		t.Fatalf("avg_decision_seconds = %d, want > 0", sum.AvgDecisionSecond)
	}
	// A verifier gets no roster / totals.
	if sum.Totals != nil || sum.VerifierActivity != nil {
		t.Fatalf("verifier summary leaked admin fields: %+v", sum)
	}
}

func TestAnalyticsService_DashboardSummary_AdminRegionIsolation(t *testing.T) {
	db := testsupport.RequireMongo(t)
	svc := newAnalyticsSvc(db)
	ctx := context.Background()

	seedScreening(t, db, 1, func(s *model.Screening) { s.Region = "north"; s.Verdict = model.VerdictFake })
	seedScreening(t, db, 2, func(s *model.Screening) { s.Region = "north"; s.Verdict = model.VerdictSuspicious })
	seedScreening(t, db, 3, func(s *model.Screening) { s.Region = "south"; s.Verdict = model.VerdictFake })

	sum, err := svc.DashboardSummary(ctx, jwt.TokenData{UserID: "admin", Role: string(model.RoleAdmin), Region: "north"})
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if sum.ScreeningsToday != 2 {
		t.Fatalf("screenings_today = %d, want 2 (region north only)", sum.ScreeningsToday)
	}
	if sum.VerdictSplit.Fake != 1 || sum.VerdictSplit.Suspicious != 1 {
		t.Fatalf("verdict_split = %+v", sum.VerdictSplit)
	}
	if len(sum.FlaggedCases) != 2 {
		t.Fatalf("flagged_cases = %d, want 2", len(sum.FlaggedCases))
	}
	if sum.CheckpointActivity == nil {
		t.Fatal("admin summary missing checkpoint_activity")
	}
}

func TestAnalyticsService_Reports(t *testing.T) {
	db := testsupport.RequireMongo(t)
	svc := newAnalyticsSvc(db)
	ctx := context.Background()

	seedScreening(t, db, 1, func(s *model.Screening) { s.DocType = model.DocPassport; s.Verdict = model.VerdictFake })
	seedScreening(t, db, 2, func(s *model.Screening) { s.DocType = model.DocPassport; s.Verdict = model.VerdictGenuine })
	seedScreening(t, db, 3, func(s *model.Screening) { s.DocType = model.DocNationalID; s.Verdict = model.VerdictGenuine })
	seedScreening(t, db, 4, func(s *model.Screening) {
		s.DocType = model.DocNationalID
		s.OfficerDecision = &model.OfficerDecision{Decision: model.DecisionEscalate, Reason: "x", DecidedBy: "a", DecidedAt: time.Now().UTC()}
	})

	rep, err := svc.Reports(ctx, "north")
	if err != nil {
		t.Fatalf("reports: %v", err)
	}
	if rep.TotalScreenings != 4 {
		t.Fatalf("total = %d, want 4", rep.TotalScreenings)
	}
	if rep.FakeRate != 25.0 {
		t.Fatalf("fake_rate = %v, want 25.0", rep.FakeRate)
	}
	if rep.Escalated != 1 {
		t.Fatalf("escalated = %d, want 1", rep.Escalated)
	}
	if len(rep.DocTypeBreakdown) != 2 {
		t.Fatalf("doc_type_breakdown = %+v", rep.DocTypeBreakdown)
	}
	// Sorted by count desc — passport and national_id both have 2, order stable enough to check presence.
	seen := map[model.DocType]int{}
	for _, d := range rep.DocTypeBreakdown {
		seen[d.DocType] = d.Count
	}
	if seen[model.DocPassport] != 2 || seen[model.DocNationalID] != 2 {
		t.Fatalf("doc counts = %+v", seen)
	}
}
