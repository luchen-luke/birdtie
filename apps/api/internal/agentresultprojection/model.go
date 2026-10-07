// Package agentresultprojection describes projections of existing authorized
// entities. A DTO is never a permission, an entity ledger, or a source resolver.
package agentresultprojection

import (
	"errors"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
)

const Schema = "typed-agent-results-v1"
const AuthorizedView = "AUTHORIZED_VIEW"
const SelfPrivate = "SELF_PRIVATE"

var ErrInvalid = errors.New("invalid typed agent result")

type Ref struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func (r Ref) Key() string { return r.Type + ":" + r.ID }
func ProductKind(kind string) bool {
	switch kind {
	case "person", "activity", "place", "community", "organization", "business", "opportunity":
		return true
	}
	return false
}

// A compatibility Group is not a Community and has no new domain action.
func ValidRef(r Ref) bool {
	return (ProductKind(r.Type) || r.Type == "group") && bounded(r.ID, 1, 180)
}

type Anchor struct {
	CoordinateSystem string  `json:"coordinateSystem"`
	Precision        string  `json:"precision"`
	Latitude         float64 `json:"latitude"`
	Longitude        float64 `json:"longitude"`
	PublicZone       string  `json:"publicZone,omitempty"`
	PlaceID          string  `json:"placeId,omitempty"`
}

type Item struct {
	Entity               Ref             `json:"entityRef"`
	Title                string          `json:"title"`
	Summary              string          `json:"summary"`
	Scope                string          `json:"scope"`
	Detail               *Ref            `json:"detailRef,omitempty"`
	Share                *Ref            `json:"shareRef,omitempty"`
	Anchor               *Anchor         `json:"anchor,omitempty"`
	SourceVersion        string          `json:"sourceVersion,omitempty"`
	Actions              []ea.Descriptor `json:"actions,omitempty"`
	ActionsSourceVersion string          `json:"actionsSourceVersion,omitempty"`
	ActionsValidUntil    *time.Time      `json:"actionsValidUntil,omitempty"`
}

func bounded(s string, min, max int) bool {
	if !utf8.ValidString(s) || strings.TrimSpace(s) != s {
		return false
	}
	n := utf8.RuneCountInString(s)
	if n < min || n > max {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validSummary(s string) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > 1000 || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}

func ValidAnchor(kind string, a *Anchor) bool {
	if a == nil {
		return true
	}
	if a.CoordinateSystem != "wgs84" || math.IsNaN(a.Latitude) || math.IsNaN(a.Longitude) || math.IsInf(a.Latitude, 0) || math.IsInf(a.Longitude, 0) || a.Latitude < -90 || a.Latitude > 90 || a.Longitude < -180 || a.Longitude > 180 {
		return false
	}
	if kind == "person" {
		if a.Precision != "area" {
			return false
		}
		switch a.PublicZone {
		case "city_centre", "north", "south", "east", "west":
			return true
		}
		return false
	}
	return ProductKind(kind) && a.Precision == "point" && a.PublicZone == ""
}

func ValidateItem(i Item) error {
	if !ValidRef(i.Entity) || !bounded(i.Title, 1, 300) || !validSummary(i.Summary) || (i.Scope != AuthorizedView && i.Scope != SelfPrivate) || !bounded(i.SourceVersion, 0, 180) || !ValidAnchor(i.Entity.Type, i.Anchor) {
		return ErrInvalid
	}
	if i.Actions != nil {
		if i.ActionsValidUntil == nil || i.ActionsValidUntil.IsZero() {
			return ErrInvalid
		}
		target := i.Entity
		if i.Entity.Type == "opportunity" && i.Detail != nil {
			target = *i.Detail
		}
		v := ea.Project(ea.Ref{Type: target.Type, ID: target.ID}, ea.Facts{}, time.Unix(1, 0), time.Unix(2, 0), i.Title, i.ActionsSourceVersion)
		v.Actions = i.Actions
		if !v.Valid() {
			return ErrInvalid
		}
	} else if i.ActionsValidUntil != nil || i.ActionsSourceVersion != "" {
		return ErrInvalid
	}
	if i.Entity.Type == "group" {
		if i.Detail != nil || i.Share != nil || i.Anchor != nil || i.Scope != AuthorizedView {
			return ErrInvalid
		}
		return nil
	}
	if i.Entity.Type == "opportunity" {
		// The computed candidate is private. Only its original Activity may be
		// opened/shared; the private Intent and matching rationale are not sent.
		ids := strings.Split(i.Entity.ID, ":")
		if i.Scope != SelfPrivate || len(ids) != 2 || !bounded(ids[0], 1, 80) || !bounded(ids[1], 1, 80) || i.Detail == nil || *i.Detail != (Ref{Type: "activity", ID: ids[1]}) || (i.Share != nil && *i.Share != *i.Detail) {
			return ErrInvalid
		}
		return nil
	}
	if i.Scope != AuthorizedView || (i.Detail != nil && *i.Detail != i.Entity) || (i.Share != nil && *i.Share != i.Entity) {
		return ErrInvalid
	}
	return nil
}

func ValidateItems(items []Item) error {
	if items == nil || len(items) > 200 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, item := range items {
		if ValidateItem(item) != nil || seen[item.Entity.Key()] {
			return ErrInvalid
		}
		seen[item.Entity.Key()] = true
	}
	return nil
}

func Refs(items []Item) []Ref {
	refs := make([]Ref, 0, len(items))
	for _, i := range items {
		refs = append(refs, i.Entity)
	}
	return refs
}
func PinIDs(items []Item) []string {
	ids := []string{}
	for _, i := range items {
		if i.Anchor != nil {
			ids = append(ids, i.Entity.Key())
		}
	}
	return ids
}
