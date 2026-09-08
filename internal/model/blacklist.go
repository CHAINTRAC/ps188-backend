package model

import (
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// CollBlacklist is the MongoDB collection name for blacklist entries.
const CollBlacklist = "blacklist"

// BlacklistKind is what an entry matches against.
type BlacklistKind string

const (
	// BlacklistDocument matches a specific document number (passport no., visa no., …).
	BlacklistDocument BlacklistKind = "document"
	// BlacklistIdentity matches a person by name + date of birth + nationality.
	BlacklistIdentity BlacklistKind = "identity"
)

func (k BlacklistKind) Valid() bool {
	switch k {
	case BlacklistDocument, BlacklistIdentity:
		return true
	}
	return false
}

// BlacklistEntry is one blacklisted document number or identity. Entries are
// never hard-deleted — they are deactivated so the trail survives.
type BlacklistEntry struct {
	ID   bson.ObjectID `bson:"_id,omitempty"`
	Kind BlacklistKind `bson:"kind"`

	// Document match (Kind == BlacklistDocument). Stored upper-cased, trimmed.
	DocNumber string  `bson:"doc_number,omitempty"`
	DocType   DocType `bson:"doc_type,omitempty"`

	// Identity match (Kind == BlacklistIdentity). Name is stored upper-cased,
	// trimmed; DOB is an ISO date string (YYYY-MM-DD); Nationality an ISO-3 code.
	Name        string `bson:"name,omitempty"`
	DOB         string `bson:"dob,omitempty"`
	Nationality string `bson:"nationality,omitempty"`

	PhotoFileID bson.ObjectID `bson:"photo_file_id,omitempty"`
	PhotoName   string        `bson:"photo_name,omitempty"`

	Reason  string `bson:"reason"`
	Source  string `bson:"source,omitempty"`
	AddedBy string `bson:"added_by"` // user id hex
	Active  bool   `bson:"active"`

	CreatedAt time.Time `bson:"created_at"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// BlacklistView is the API projection.
type BlacklistView struct {
	ID          string        `json:"id"`
	Kind        BlacklistKind `json:"kind"`
	DocNumber   string        `json:"doc_number,omitempty"`
	DocType     DocType       `json:"doc_type,omitempty"`
	Name        string        `json:"name,omitempty"`
	DOB         string        `json:"dob,omitempty"`
	Nationality string        `json:"nationality,omitempty"`
	PhotoURL    string        `json:"photo_url,omitempty"`
	Reason      string        `json:"reason"`
	Source      string        `json:"source,omitempty"`
	AddedBy     string        `json:"added_by"`
	Active      bool          `json:"active"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

func (b *BlacklistEntry) View() BlacklistView {
	var photoURL string
	if !b.PhotoFileID.IsZero() {
		photoURL = "/api/blacklist/" + b.ID.Hex() + "/photo"
	}
	return BlacklistView{
		ID:          b.ID.Hex(),
		Kind:        b.Kind,
		DocNumber:   b.DocNumber,
		DocType:     b.DocType,
		Name:        b.Name,
		DOB:         b.DOB,
		Nationality: b.Nationality,
		PhotoURL:    photoURL,
		Reason:      b.Reason,
		Source:      b.Source,
		AddedBy:     b.AddedBy,
		Active:      b.Active,
		CreatedAt:   b.CreatedAt,
		UpdatedAt:   b.UpdatedAt,
	}
}

// CreateBlacklistInput is the validated payload the blacklist service accepts.
// Exactly one of the document / identity field sets is required, matching Kind.
type CreateBlacklistInput struct {
	Kind        BlacklistKind `json:"kind" binding:"required"`
	DocNumber   string        `json:"doc_number" binding:"omitempty,max=64"`
	DocType     DocType       `json:"doc_type" binding:"omitempty"`
	Name        string        `json:"name" binding:"omitempty,max=120"`
	DOB         string        `json:"dob" binding:"omitempty,datetime=2006-01-02"`
	Nationality string        `json:"nationality" binding:"omitempty,max=3"`
	Reason      string        `json:"reason" binding:"required,min=1,max=1000"`
	Source      string        `json:"source" binding:"omitempty,max=200"`
	// Photo is optional and only set by the multipart create path — never bound
	// from JSON.
	Photo     []byte `json:"-"`
	PhotoName string `json:"-"`
}

// BlacklistFilter narrows a list query. Zero values mean "no filter".
// Query, when set, matches case-insensitively against doc_number, name, and
// reason.
type BlacklistFilter struct {
	Kind    BlacklistKind
	DocType DocType
	Active  *bool
	Query   string
}

// BlacklistProbe is the set of values a screening submission is checked against.
type BlacklistProbe struct {
	DocNumber   string
	Name        string
	DOB         string
	Nationality string
}

// NormalizeBlacklistKey upper-cases and trims a document number or name so
// lookups are case- and whitespace-insensitive.
func NormalizeBlacklistKey(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}
