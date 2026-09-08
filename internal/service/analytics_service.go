package service

import (
	"context"
	"math"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/platform/jwt"
	"github.com/sih26/ps188-backend/internal/repository"
)

// AnalyticsService is the single place every dashboard / report aggregation
// lives. It reads through the repositories only (ScreeningRepository.Aggregate
// for the rollups, plain counts for the org totals) and never writes.
//
// All day boundaries are UTC. For the SIH demo that is close enough to IST
// working hours; a timezone-aware version is deferred.
type AnalyticsService struct {
	screenings  repository.ScreeningRepository
	users       repository.UserRepository
	checkpoints repository.CheckpointRepository
}

func NewAnalyticsService(s repository.ScreeningRepository, u repository.UserRepository, c repository.CheckpointRepository) *AnalyticsService {
	return &AnalyticsService{screenings: s, users: u, checkpoints: c}
}

// DashboardSummary returns the role-aware payload for GET /api/dashboard/summary.
func (s *AnalyticsService) DashboardSummary(ctx context.Context, actor jwt.TokenData) (model.DashboardSummary, error) {
	now := time.Now().UTC()
	todayStart := startOfDay(now)
	weekStart := todayStart.AddDate(0, 0, -6)

	scope := bson.M{}
	switch actor.Role {
	case string(model.RoleVerifier):
		scope["officer_id"] = actor.UserID
	case string(model.RoleAdmin):
		if actor.Region != "" {
			scope["region"] = actor.Region
		}
	}

	facet := bson.M{
		"today":        countFacet(bson.M{"created_at": bson.M{"$gte": todayStart}}),
		"total":        bson.A{bson.M{"$count": "n"}},
		"decidedToday": countFacet(bson.M{"officer_decision.decided_at": bson.M{"$gte": todayStart}}),
		"decidedTotal": countFacet(bson.M{"officer_decision": bson.M{"$exists": true}}),
		"pending":      countFacet(bson.M{"officer_decision": bson.M{"$exists": false}}),
		"escalated":    countFacet(bson.M{"officer_decision.decision": model.DecisionEscalate}),
		"verdictSplit": bson.A{
			bson.M{"$group": bson.M{"_id": "$verdict", "n": bson.M{"$sum": 1}}},
		},
		"avgDecision": avgDecisionFacet(),
		"weekly":      weeklyFacet(weekStart),
		"verifierActivity": bson.A{
			activityGroup("$officer_id", todayStart),
		},
		"checkpointActivity": bson.A{
			activityGroup("$checkpoint_id", todayStart),
		},
		"flagged": bson.A{
			bson.M{"$match": bson.M{
				"officer_decision": bson.M{"$exists": false},
				"$or": bson.A{
					bson.M{"verdict": bson.M{"$in": bson.A{model.VerdictSuspicious, model.VerdictFake, model.VerdictInsufficient}}},
					bson.M{"flags": model.FlagBlacklistHit},
				},
			}},
			bson.M{"$sort": bson.M{"_id": -1}},
			bson.M{"$limit": 8},
		},
	}

	rows, err := s.screenings.Aggregate(ctx, mongo.Pipeline{
		bson.D{{Key: "$match", Value: scope}},
		bson.D{{Key: "$facet", Value: facet}},
	})
	if err != nil {
		return model.DashboardSummary{}, err
	}
	var f bson.M
	if len(rows) > 0 {
		f = rows[0]
	}

	out := model.DashboardSummary{
		Role:              actor.Role,
		Region:            actor.Region,
		ScreeningsToday:   facetCount(f, "today"),
		ScreeningsTotal:   facetCount(f, "total"),
		DecidedToday:      facetCount(f, "decidedToday"),
		DecidedTotal:      facetCount(f, "decidedTotal"),
		PendingDecisions:  facetCount(f, "pending"),
		Escalated:         facetCount(f, "escalated"),
		AvgDecisionSecond: facetAvgSeconds(f, "avgDecision"),
		VerdictSplit:      facetVerdictSplit(f, "verdictSplit"),
		WeeklyVolume:      facetWeekly(f, "weekly", weekStart),
	}

	switch actor.Role {
	case string(model.RoleAdmin):
		out.VerifierActivity = facetActivity(f, "verifierActivity")
		out.CheckpointActivity = facetActivity(f, "checkpointActivity")
		out.FlaggedCases = facetFlagged(f, "flagged")
	case string(model.RoleSuperAdmin):
		out.CheckpointActivity = facetActivity(f, "checkpointActivity")
		out.FlaggedCases = facetFlagged(f, "flagged")
		totals, err := s.orgTotals(ctx)
		if err != nil {
			return model.DashboardSummary{}, err
		}
		out.Totals = &totals
	}
	return out, nil
}

// Reports returns the payload for GET /api/reports. An empty region means
// org-wide (super admin); an admin always passes their own region.
func (s *AnalyticsService) Reports(ctx context.Context, region string) (model.ReportsSummary, error) {
	now := time.Now().UTC()
	weekStart := startOfDay(now).AddDate(0, 0, -6)

	scope := bson.M{}
	if region != "" {
		scope["region"] = region
	}

	facet := bson.M{
		"total":       bson.A{bson.M{"$count": "n"}},
		"fake":        countFacet(bson.M{"verdict": model.VerdictFake}),
		"escalated":   countFacet(bson.M{"officer_decision.decision": model.DecisionEscalate}),
		"avgDecision": avgDecisionFacet(),
		"weekly":      weeklyFacet(weekStart),
		"docType": bson.A{
			bson.M{"$group": bson.M{"_id": "$doc_type", "n": bson.M{"$sum": 1}}},
			bson.M{"$sort": bson.M{"n": -1}},
		},
		"checkpoint": bson.A{
			activityGroup("$checkpoint_id", startOfDay(now)),
		},
	}

	rows, err := s.screenings.Aggregate(ctx, mongo.Pipeline{
		bson.D{{Key: "$match", Value: scope}},
		bson.D{{Key: "$facet", Value: facet}},
	})
	if err != nil {
		return model.ReportsSummary{}, err
	}
	var f bson.M
	if len(rows) > 0 {
		f = rows[0]
	}

	total := facetCount(f, "total")
	fake := facetCount(f, "fake")
	rate := 0.0
	if total > 0 {
		rate = math.Round(float64(fake)/float64(total)*1000) / 10
	}

	return model.ReportsSummary{
		Region:              region,
		TotalScreenings:     total,
		FakeRate:            rate,
		Escalated:           facetCount(f, "escalated"),
		AvgDecisionSecond:   facetAvgSeconds(f, "avgDecision"),
		WeeklyVolume:        facetWeekly(f, "weekly", weekStart),
		DocTypeBreakdown:    facetDocTypes(f, "docType"),
		CheckpointBreakdown: facetActivity(f, "checkpoint"),
	}, nil
}

func (s *AnalyticsService) orgTotals(ctx context.Context) (model.OrgTotals, error) {
	cp, err := s.checkpoints.Count(ctx, "")
	if err != nil {
		return model.OrgTotals{}, err
	}
	admins, err := s.users.CountBy(ctx, model.UserFilter{Role: model.RoleAdmin})
	if err != nil {
		return model.OrgTotals{}, err
	}
	verifiers, err := s.users.CountBy(ctx, model.UserFilter{Role: model.RoleVerifier})
	if err != nil {
		return model.OrgTotals{}, err
	}
	return model.OrgTotals{
		Checkpoints: int(cp),
		Admins:      int(admins),
		Verifiers:   int(verifiers),
	}, nil
}

// --- pipeline fragment builders -------------------------------------------------

func countFacet(match bson.M) bson.A {
	return bson.A{bson.M{"$match": match}, bson.M{"$count": "n"}}
}

func avgDecisionFacet() bson.A {
	return bson.A{
		bson.M{"$match": bson.M{"officer_decision.decided_at": bson.M{"$exists": true}}},
		bson.M{"$group": bson.M{"_id": nil, "ms": bson.M{"$avg": bson.M{
			"$subtract": bson.A{"$officer_decision.decided_at", "$created_at"},
		}}}},
	}
}

func weeklyFacet(weekStart time.Time) bson.A {
	return bson.A{
		bson.M{"$match": bson.M{"created_at": bson.M{"$gte": weekStart}}},
		bson.M{"$group": bson.M{
			"_id": bson.M{
				"d": bson.M{"$dateToString": bson.M{"format": "%Y-%m-%d", "date": "$created_at", "timezone": "UTC"}},
				"v": "$verdict",
			},
			"n": bson.M{"$sum": 1},
		}},
	}
}

func activityGroup(field string, todayStart time.Time) bson.M {
	return bson.M{"$group": bson.M{
		"_id":   field,
		"total": bson.M{"$sum": 1},
		"today": bson.M{"$sum": bson.M{"$cond": bson.A{
			bson.M{"$gte": bson.A{"$created_at", todayStart}}, 1, 0,
		}}},
	}}
}

// --- facet result decoders ----------------------------------------------------

func toInt(v any) int {
	switch n := v.(type) {
	case int32:
		return int(n)
	case int64:
		return int(n)
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}

func facetArray(f bson.M, key string) bson.A {
	if f == nil {
		return nil
	}
	if a, ok := f[key].(bson.A); ok {
		return a
	}
	return nil
}

// facetCount reads a `[{n: <count>}]` sub-result.
func facetCount(f bson.M, key string) int {
	a := facetArray(f, key)
	if len(a) == 0 {
		return 0
	}
	if m, ok := a[0].(bson.M); ok {
		return toInt(m["n"])
	}
	return 0
}

func facetAvgSeconds(f bson.M, key string) int {
	a := facetArray(f, key)
	if len(a) == 0 {
		return 0
	}
	m, ok := a[0].(bson.M)
	if !ok {
		return 0
	}
	ms, _ := m["ms"].(float64)
	if ms <= 0 {
		return 0
	}
	return int(math.Round(ms / 1000))
}

func verdictBandKey(v model.Verdict) string {
	switch v.Band() {
	case model.VerdictGenuine:
		return "genuine"
	case model.VerdictFake:
		return "fake"
	default:
		return "suspicious"
	}
}

func facetVerdictSplit(f bson.M, key string) model.VerdictSplit {
	var out model.VerdictSplit
	for _, row := range facetArray(f, key) {
		m, ok := row.(bson.M)
		if !ok {
			continue
		}
		id, _ := m["_id"].(string)
		n := toInt(m["n"])
		switch verdictBandKey(model.Verdict(id)) {
		case "genuine":
			out.Genuine += n
		case "fake":
			out.Fake += n
		default:
			out.Suspicious += n
		}
	}
	return out
}

func facetWeekly(f bson.M, key string, weekStart time.Time) []model.DayVolume {
	// date -> band -> count
	byDate := map[string]*model.DayVolume{}
	days := make([]model.DayVolume, 7)
	for i := 0; i < 7; i++ {
		d := weekStart.AddDate(0, 0, i)
		key := d.Format("2006-01-02")
		days[i] = model.DayVolume{Date: key, Day: d.Format("Mon")}
		byDate[key] = &days[i]
	}
	for _, row := range facetArray(f, key) {
		m, ok := row.(bson.M)
		if !ok {
			continue
		}
		id, _ := m["_id"].(bson.M)
		if id == nil {
			continue
		}
		date, _ := id["d"].(string)
		bucket := byDate[date]
		if bucket == nil {
			continue
		}
		n := toInt(m["n"])
		verdict, _ := id["v"].(string)
		switch verdictBandKey(model.Verdict(verdict)) {
		case "genuine":
			bucket.Genuine += n
		case "fake":
			bucket.Fake += n
		default:
			bucket.Suspicious += n
		}
	}
	return days
}

func facetActivity(f bson.M, key string) []model.ActorActivity {
	out := []model.ActorActivity{}
	for _, row := range facetArray(f, key) {
		m, ok := row.(bson.M)
		if !ok {
			continue
		}
		id, _ := m["_id"].(string)
		if id == "" {
			continue
		}
		out = append(out, model.ActorActivity{
			ID:    id,
			Today: toInt(m["today"]),
			Total: toInt(m["total"]),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Today != out[j].Today {
			return out[i].Today > out[j].Today
		}
		return out[i].Total > out[j].Total
	})
	return out
}

func facetDocTypes(f bson.M, key string) []model.DocTypeCount {
	out := []model.DocTypeCount{}
	for _, row := range facetArray(f, key) {
		m, ok := row.(bson.M)
		if !ok {
			continue
		}
		id, _ := m["_id"].(string)
		if id == "" {
			continue
		}
		out = append(out, model.DocTypeCount{DocType: model.DocType(id), Count: toInt(m["n"])})
	}
	return out
}

func facetFlagged(f bson.M, key string) []model.FlaggedCase {
	out := []model.FlaggedCase{}
	for _, row := range facetArray(f, key) {
		m, ok := row.(bson.M)
		if !ok {
			continue
		}
		var id string
		if oid, ok := m["_id"].(bson.ObjectID); ok {
			id = oid.Hex()
		}
		verdict := model.Verdict(asString(m["verdict"]))
		fc := model.FlaggedCase{
			ID:           id,
			ReferenceNo:  asString(m["reference_no"]),
			DocType:      model.DocType(asString(m["doc_type"])),
			CheckpointID: asString(m["checkpoint_id"]),
			OfficerID:    asString(m["officer_id"]),
			Verdict:      verdict,
			VerdictBand:  verdict.Band(),
			Flags:        asStringSlice(m["flags"]),
			CreatedAt:    asTime(m["created_at"]),
		}
		out = append(out, fc)
	}
	return out
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asStringSlice(v any) []string {
	a, ok := v.(bson.A)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(a))
	for _, e := range a {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func asTime(v any) time.Time {
	switch t := v.(type) {
	case time.Time:
		return t
	case bson.DateTime:
		return t.Time()
	default:
		return time.Time{}
	}
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
