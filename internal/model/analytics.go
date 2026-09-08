package model

import "time"

// This file defines the API projections for the dashboard summary and the
// reports page. Both are computed by service/analytics_service.go from Mongo
// aggregation pipelines — there is no stored "analytics" document.

// VerdictSplit counts screenings by verdict band (genuine / suspicious / fake).
// INSUFFICIENT_IMAGE_QUALITY and PENDING fold into suspicious (see Verdict.Band).
type VerdictSplit struct {
	Genuine    int `json:"genuine"`
	Suspicious int `json:"suspicious"`
	Fake       int `json:"fake"`
}

// DayVolume is one bar in the "screenings this week" chart. Date is the UTC day
// (YYYY-MM-DD); Day is its short weekday label ("Mon") for convenience.
type DayVolume struct {
	Date       string `json:"date"`
	Day        string `json:"day"`
	Genuine    int    `json:"genuine"`
	Suspicious int    `json:"suspicious"`
	Fake       int    `json:"fake"`
}

// ActorActivity is a screening count grouped by one actor id — an officer_id
// (verifier roster) or a checkpoint_id (checkpoint table). ID is that raw id;
// the UI joins it to a display name from its own users/checkpoints lists.
type ActorActivity struct {
	ID    string `json:"id"`
	Today int    `json:"today"`
	Total int    `json:"total"`
}

// OrgTotals is the super-admin headline count row.
type OrgTotals struct {
	Checkpoints int `json:"checkpoints"`
	Admins      int `json:"admins"`
	Verifiers   int `json:"verifiers"`
}

// FlaggedCase is a compact undecided-and-risky screening for the admin/superadmin
// "flagged for review" card. It is a strict subset of ScreeningView.
type FlaggedCase struct {
	ID           string    `json:"id"`
	ReferenceNo  string    `json:"reference_no"`
	DocType      DocType   `json:"doc_type"`
	CheckpointID string    `json:"checkpoint_id"`
	OfficerID    string    `json:"officer_id"`
	Verdict      Verdict   `json:"verdict"`
	VerdictBand  Verdict   `json:"verdict_band"`
	Flags        []string  `json:"flags"`
	CreatedAt    time.Time `json:"created_at"`
}

// DashboardSummary is the payload of GET /api/dashboard/summary. It is
// role-aware: a verifier gets their own numbers, an admin their region's, a
// super admin the whole org. Fields that do not apply to the caller's role are
// omitted (nil slices / nil pointers).
type DashboardSummary struct {
	Role   string `json:"role"`
	Region string `json:"region,omitempty"`

	ScreeningsToday   int          `json:"screenings_today"`
	ScreeningsTotal   int          `json:"screenings_total"`
	DecidedToday      int          `json:"decided_today"`
	DecidedTotal      int          `json:"decided_total"`
	PendingDecisions  int          `json:"pending_decisions"`
	Escalated         int          `json:"escalated"`
	AvgDecisionSecond int          `json:"avg_decision_seconds"`
	VerdictSplit      VerdictSplit `json:"verdict_split"`
	WeeklyVolume      []DayVolume  `json:"weekly_volume"`

	Totals             *OrgTotals      `json:"totals,omitempty"`              // super admin
	VerifierActivity   []ActorActivity `json:"verifier_activity,omitempty"`   // admin
	CheckpointActivity []ActorActivity `json:"checkpoint_activity,omitempty"` // admin + super admin
	FlaggedCases       []FlaggedCase   `json:"flagged_cases,omitempty"`       // admin + super admin
}

// DocTypeCount is one row of the reports "by document type" breakdown.
type DocTypeCount struct {
	DocType DocType `json:"doc_type"`
	Count   int     `json:"count"`
}

// ReportsSummary is the payload of GET /api/reports (admin = own region,
// super admin = org-wide or ?region=).
type ReportsSummary struct {
	Region string `json:"region,omitempty"`

	TotalScreenings   int     `json:"total_screenings"`
	FakeRate          float64 `json:"fake_rate"` // percent of screenings with verdict band FAKE, one decimal
	Escalated         int     `json:"escalated"`
	AvgDecisionSecond int     `json:"avg_decision_seconds"`

	WeeklyVolume        []DayVolume     `json:"weekly_volume"`
	DocTypeBreakdown    []DocTypeCount  `json:"doc_type_breakdown"`
	CheckpointBreakdown []ActorActivity `json:"checkpoint_breakdown"`
}
