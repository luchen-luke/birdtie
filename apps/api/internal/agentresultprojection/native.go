package agentresultprojection

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

var (
	ErrDenied      = errors.New("typed result access denied")
	ErrChanged     = errors.New("typed result source changed")
	ErrUnavailable = errors.New("typed result source unavailable")
)

// Query is constructed from the original versioned task by the server. Neither
// these fields nor a Receipt is accepted as client-supplied authorization.
type Query struct {
	CityID         string
	Kind           string
	SearchTerm     string
	Category       string
	TimePreference string
	Closer         bool
	Bounds         *Bounds
	Comparison     bool
	CompareIDs     []string
}
type Bounds struct{ West, South, East, North float64 }
type Access struct {
	Actor         identity.Actor
	SessionDigest [32]byte
	TaskID        string
	ExpectedTask  json.RawMessage
}
type Receipt struct {
	Activities           []foundation.Activity `json:"-"`
	Places               []foundation.Place    `json:"-"`
	PublicCommercialRefs []Ref                 `json:"-"`
	Items                []Item                `json:"-"`
	ObservedAt           time.Time             `json:"-"`
	ValidUntil           time.Time             `json:"-"`
	Proof                string                `json:"-"`
	Seal                 string                `json:"-"`
}
type NativeStore interface {
	ReadAgentResultProjection(context.Context, Access, Query) (Receipt, error)
	RevalidateAgentResultProjection(context.Context, Access, Query, Receipt) error
}

func (q Query) Valid() bool {
	if !bounded(q.CityID, 1, 160) || !bounded(q.SearchTerm, 0, 240) || !bounded(q.Category, 0, 80) {
		return false
	}
	switch q.Kind {
	case "activity", "person", "place", "community", "organization", "business", "opportunity":
	default:
		return false
	}
	switch q.TimePreference {
	case "", "anytime", "today", "tomorrow", "tonight", "weekend":
	default:
		return false
	}
	if q.Bounds != nil {
		b := q.Bounds
		if !(b.West >= -180 && b.East <= 180 && b.South >= -90 && b.North <= 90 && b.West < b.East && b.South < b.North) {
			return false
		}
	}
	if len(q.CompareIDs) > 2 || (!q.Comparison && len(q.CompareIDs) > 0) {
		return false
	}
	if q.Kind == "opportunity" && (q.Bounds != nil || q.Closer || q.SearchTerm != "" || q.Category != "" || (q.TimePreference != "" && q.TimePreference != "anytime") || len(q.CompareIDs) > 0) {
		return false
	}
	for _, id := range q.CompareIDs {
		if !bounded(id, 1, 80) || strings.Contains(id, ":") {
			return false
		}
	}
	return true
}
func (a Access) Valid() bool {
	return a.Actor.AccountType == "person" && bounded(a.Actor.ID, 1, 80) && a.SessionDigest != ([32]byte{}) && bounded(a.TaskID, 1, 80) && json.Valid(a.ExpectedTask)
}
func (r Receipt) Valid() bool {
	seen := map[Ref]bool{}
	for _, ref := range r.PublicCommercialRefs {
		if seen[ref] || (ref.Type != "activity" && ref.Type != "place") {
			return false
		}
		seen[ref] = true
		found := false
		for _, item := range r.Items {
			if item.Entity == ref && item.Scope == AuthorizedView {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return ValidateItems(r.Items) == nil && !r.ObservedAt.IsZero() && r.ValidUntil.After(r.ObservedAt) && !r.ValidUntil.After(r.ObservedAt.Add(2*time.Minute)) && len(r.Proof) == 64 && len(r.Seal) == 64
}
