package connection

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/follow"
)

const (
	ContactTies     = "ties"
	ContactFollows  = "follows"
	ContactRequests = "connection-requests"
)

var ErrInvalidContactPage = errors.New("invalid contact page")

// This bounded location hint is never a permission or a frozen permission set.
// Snapshot excludes newer insertions; every page reapplies current source ACLs.
type ContactCursor struct {
	Version  int       `json:"v"`
	OwnerID  string    `json:"owner"`
	Kind     string    `json:"kind"`
	Snapshot time.Time `json:"snapshot"`
	Time     time.Time `json:"time"`
	ID       string    `json:"id"`
}
type ContactPage[T any] struct {
	Items         []T     `json:"items"`
	NextCursor    *string `json:"nextCursor"`
	ResponseProof any     `json:"-"`
}
type ContactPagedStore interface {
	ListContactTiesPage(context.Context, ea.Access, PageRequest) (ContactPage[Tie], error)
	ListContactFollowsPage(context.Context, ea.Access, PageRequest) (ContactPage[follow.Record], error)
	ListContactRequestsPage(context.Context, ea.Access, PageRequest) (ContactPage[Request], error)
	ValidateContactPageResponse(context.Context, ea.Access, string, PageRequest, any) error
	ReadContactRequestCurrent(context.Context, ea.Access, string) (Request, error)
	ValidateContactRequestResponse(context.Context, ea.Access, Request) error
}

func ValidContactKind(kind string) bool {
	return kind == ContactTies || kind == ContactFollows || kind == ContactRequests
}
func validContactTime(t time.Time) bool {
	return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 && t.Location() == time.UTC
}
func EncodeContactCursor(c ContactCursor) (string, error) {
	if c.Version != 1 || !ValidChatPageID(c.OwnerID) || !ValidChatPageID(c.ID) || !ValidContactKind(c.Kind) || !validContactTime(c.Time) || !validContactTime(c.Snapshot) || c.Time.After(c.Snapshot) {
		return "", ErrInvalidContactPage
	}
	raw, e := json.Marshal(c)
	if e != nil {
		return "", ErrInvalidContactPage
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func DecodeContactCursor(raw, owner, kind string) (*ContactCursor, error) {
	if !ValidChatPageID(owner) || !ValidContactKind(kind) {
		return nil, ErrInvalidContactPage
	}
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 1024 {
		return nil, ErrInvalidContactPage
	}
	bytes, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil {
		return nil, ErrInvalidContactPage
	}
	var c ContactCursor
	if json.Unmarshal(bytes, &c) != nil || c.OwnerID != owner || c.Kind != kind {
		return nil, ErrInvalidContactPage
	}
	canonical, e := EncodeContactCursor(c)
	if e != nil || canonical != raw {
		return nil, ErrInvalidContactPage
	}
	return &c, nil
}
func NormalizeContactPage(in PageRequest, owner, kind string) (PageRequest, *ContactCursor, error) {
	if in.Limit == 0 {
		in.Limit = DefaultChatPageLimit
	}
	if in.Limit < 1 || in.Limit > MaxChatPageLimit {
		return in, nil, ErrInvalidContactPage
	}
	c, e := DecodeContactCursor(in.Cursor, owner, kind)
	return in, c, e
}

// Validate the actual emitted boundary, not just the presence of a cursor.
func ValidateContactPage[T any](p ContactPage[T], in PageRequest, owner, kind string, row func(T) (string, time.Time, bool)) error {
	in, c, e := NormalizeContactPage(in, owner, kind)
	if e != nil || len(p.Items) > in.Limit {
		return ErrInvalidContactPage
	}
	var lastID string
	var lastAt time.Time
	seen := map[string]bool{}
	for _, item := range p.Items {
		id, at, valid := row(item)
		if !valid || !ValidChatPageID(id) || !validContactTime(at) || seen[id] || (lastID != "" && (at.After(lastAt) || at.Equal(lastAt) && id >= lastID)) || (c != nil && (at.After(c.Snapshot) || at.After(c.Time) || at.Equal(c.Time) && id >= c.ID)) {
			return ErrInvalidContactPage
		}
		seen[id] = true
		lastID, lastAt = id, at
	}
	if p.NextCursor != nil {
		if len(p.Items) != in.Limit || *p.NextCursor == "" || *p.NextCursor == in.Cursor {
			return ErrInvalidContactPage
		}
		next, e := DecodeContactCursor(*p.NextCursor, owner, kind)
		if e != nil || next == nil || next.ID != lastID || !next.Time.Equal(lastAt) || c != nil && !next.Snapshot.Equal(c.Snapshot) {
			return ErrInvalidContactPage
		}
		for _, item := range p.Items {
			_, at, _ := row(item)
			if at.After(next.Snapshot) {
				return ErrInvalidContactPage
			}
		}
	}
	return nil
}
func ContactTieRow(t Tie, owner string) (string, time.Time, bool) {
	return t.ID, t.CreatedAt, ValidChatPageID(t.OtherAccountID) && t.OtherAccountID != owner
}
func ContactFollowRow(f follow.Record) (string, time.Time, bool) {
	n, e := follow.Normalize(f.Target)
	return f.ID, f.CreatedAt, e == nil && n == f.Target && ValidChatPageID(f.Target.ID) && f.ValidMedia()
}
func ContactRequestRow(r Request, owner string) (string, time.Time, bool) {
	valid := ValidChatPageID(r.OtherAccountID) && r.OtherAccountID != owner && (r.Direction == "incoming" || r.Direction == "outgoing") && (r.Scope == "friend" || r.Scope == "conversation") && validContactTime(r.ExpiresAt)
	switch r.State {
	case "pending", "accepted", "declined", "withdrawn", "expired":
	default:
		valid = false
	}
	if r.ConversationID != "" && !ValidChatPageID(r.ConversationID) {
		valid = false
	}
	return r.ID, r.CreatedAt, valid
}
