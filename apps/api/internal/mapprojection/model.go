// Package mapprojection projects current domain entities onto approved public
// anchors. It is neither a new domain ledger nor a location/processing grant.
package mapprojection

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mp "github.com/birdtie/birdtie/apps/api/internal/momentphoto"
	pp "github.com/birdtie/birdtie/apps/api/internal/placephoto"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

const SchemaVersion = "typed-map-layers-v1"
const PublicAuthorSchemaVersion = "typed-map-layers-v2"
const MediaSchemaVersion = "typed-map-layers-v3"
const MomentMediaSchemaVersion = "typed-map-layers-v4"
const Public = "PUBLIC"
const PublicAuthor = "PUBLIC_AUTHOR"
const SelfPrivate = "SELF_PRIVATE"
const MaxItems = 150
const MaxPublicAuthorItems = 30

var ErrInvalid = errors.New("invalid map projection")
var ErrUnavailable = errors.New("map projection unavailable")
var ErrChanged = errors.New("map projection changed")
var ErrNotFound = errors.New("map context unavailable")

type Access struct {
	Actor  identity.Actor
	Digest [32]byte
}

func (a Access) Anonymous() bool { return a.Actor == (identity.Actor{}) && a.Digest == ([32]byte{}) }
func (a Access) Valid() bool {
	return a.Anonymous() || (a.Actor.AccountType == "person" && businessconsole.ValidID(a.Actor.ID) && a.Digest != ([32]byte{}))
}
func (Access) MarshalJSON() ([]byte, error) { return nil, ErrInvalid }

type Query struct {
	CityID                   string
	West, South, East, North float64
	Private                  bool
	AuthorID                 string
	Media                    bool
	MomentMedia              bool
}

func ValidateQuery(q Query) error {
	if q.MomentMedia && (q.Private || q.Media) {
		return ErrInvalid
	}
	if q.Media && (q.Private || q.AuthorID != "") {
		return ErrInvalid
	}
	if q.AuthorID != "" && (q.Private || !businessconsole.ValidID(q.AuthorID)) {
		return ErrInvalid
	}
	if len(q.CityID) < 1 || len(q.CityID) > 80 || strings.TrimSpace(q.CityID) != q.CityID || strings.ContainsAny(q.CityID, "/\\?#\x00") {
		return ErrInvalid
	}
	for _, v := range []float64{q.West, q.South, q.East, q.North} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return ErrInvalid
		}
	}
	if q.West < -180 || q.East > 180 || q.South < -90 || q.North > 90 || q.West >= q.East || q.South >= q.North {
		return ErrInvalid
	}
	return nil
}

type Ref struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}
type Anchor struct {
	PlaceID          string  `json:"placeId"`
	Label            string  `json:"label"`
	CoordinateSystem string  `json:"coordinateSystem"`
	Precision        string  `json:"precision"`
	Latitude         float64 `json:"latitude"`
	Longitude        float64 `json:"longitude"`
}
type Item struct {
	Kind          string         `json:"kind"`
	ID            string         `json:"id"`
	Title         string         `json:"title"`
	Entity        Ref            `json:"entityRef"`
	Detail        Ref            `json:"detailRef"`
	Anchor        Anchor         `json:"anchor"`
	SourceVersion string         `json:"sourceVersion"`
	PlacePhoto    *pp.Metadata   `json:"placePhoto,omitempty"`
	RecentMoments *RecentMoments `json:"recentMoments,omitempty"`
	MomentPhoto   *mp.Metadata   `json:"momentPhoto,omitempty"`
}

// A bounded reading summary of the complete current public member set. Its
// lease does not grant the base point or renew the original map snapshot.
type RecentMoments struct {
	Count       int64     `json:"count"`
	CheckedAt   time.Time `json:"checkedAt"`
	WindowStart time.Time `json:"windowStart"`
	ValidUntil  time.Time `json:"validUntil"`
}

func (r RecentMoments) Valid(observedAt, baseUntil time.Time) bool {
	return r.Count >= 0 && r.Count <= 9007199254740991 &&
		!r.CheckedAt.Before(observedAt) && r.CheckedAt.Year() >= 1 && r.CheckedAt.Year() <= 9999 &&
		r.WindowStart.Equal(r.CheckedAt.Add(-30*24*time.Hour)) &&
		r.ValidUntil.After(r.CheckedAt) && !r.ValidUntil.After(baseUntil) &&
		r.ValidUntil.Sub(r.CheckedAt) <= time.Minute
}

// Independent media retirement preserves every original object and anchor.
func RetireExpiredMedia(v View, at time.Time) View {
	v.Items = append(v.Items[:0:0], v.Items...)
	for i := range v.Items {
		if p := v.Items[i].PlacePhoto; p != nil && !p.ExpiresAt.After(at) {
			v.Items[i].PlacePhoto = nil
		}
		if r := v.Items[i].RecentMoments; r != nil && !r.ValidUntil.After(at) {
			v.Items[i].RecentMoments = nil
		}
		if p := v.Items[i].MomentPhoto; p != nil && !p.ExpiresAt.After(at) {
			v.Items[i].MomentPhoto = nil
		}
	}
	return v
}

type View struct {
	SchemaVersion string    `json:"schemaVersion"`
	CityID        string    `json:"cityId"`
	Scope         string    `json:"scope"`
	AuthorID      string    `json:"authorId,omitempty"`
	ObservedAt    time.Time `json:"observedAt"`
	ValidUntil    time.Time `json:"validUntil"`
	Truncated     bool      `json:"truncated"`
	Items         []Item    `json:"items"`
}

// Receipt is process sealed by the actual native store; never a client proof.
type Receipt struct {
	View        View
	Query       Query
	Proof, Seal string
	MediaProof  string
}

func (Receipt) MarshalJSON() ([]byte, error) { return nil, ErrInvalid }

type Store interface {
	ReadMapLayers(context.Context, Access, Query) (Receipt, error)
	RevalidateMapLayers(context.Context, Access, Receipt) error
}

// Finalization may retire an optional decoration; it cannot replace the base
// authority, inject a fresh photo or extend the original media/count lease.
type StoreWithMedia interface {
	Store
	FinalizeMapLayers(context.Context, Access, Receipt) (View, error)
}

func ValidKind(k string) bool {
	switch k {
	case "PLACE", "ACTIVITY", "MOMENT", "ORGANIZATION", "BUSINESS", "OPPORTUNITY":
		return true
	}
	return false
}
func text(s string, min, max int) bool {
	return utf8.ValidString(s) && strings.TrimSpace(s) == s && !strings.ContainsRune(s, 0) && len([]rune(s)) >= min && len([]rune(s)) <= max
}
func ValidItem(i Item, scope string) bool {
	if scope == PublicAuthor {
		if i.Kind != "MOMENT" {
			return false
		}
		// Author selection grants only explicitly public Moments. Its original
		// canonical Place anchor and detail contract stay identical to PUBLIC.
		scope = Public
	}
	if !ValidKind(i.Kind) || !text(i.Title, 1, 300) || !text(i.SourceVersion, 1, 180) || i.Entity.Type != i.Kind || i.Entity.ID != i.ID || i.Anchor.CoordinateSystem != "wgs84" || i.Anchor.Precision != "point" || !text(i.Anchor.Label, 1, 300) {
		return false
	}
	if math.IsNaN(i.Anchor.Latitude) || math.IsNaN(i.Anchor.Longitude) || math.IsInf(i.Anchor.Latitude, 0) || math.IsInf(i.Anchor.Longitude, 0) || i.Anchor.Latitude < -90 || i.Anchor.Latitude > 90 || i.Anchor.Longitude < -180 || i.Anchor.Longitude > 180 {
		return false
	}
	if i.Kind == "OPPORTUNITY" {
		ids := strings.Split(i.ID, ":")
		return scope == SelfPrivate && len(ids) == 2 && businessconsole.ValidID(ids[0]) && businessconsole.ValidID(ids[1]) && i.Detail.Type == "ACTIVITY" && i.Detail.ID == ids[1] && businessconsole.ValidID(i.Anchor.PlaceID)
	}
	return scope == Public && businessconsole.ValidID(i.ID) && i.Detail == i.Entity && ((i.Kind == "ORGANIZATION" && i.Anchor.PlaceID == "") || (i.Kind != "ORGANIZATION" && businessconsole.ValidID(i.Anchor.PlaceID)))
}
func ValidateView(v View) error {
	if v.Scope == PublicAuthor {
		if (v.SchemaVersion != PublicAuthorSchemaVersion && v.SchemaVersion != MomentMediaSchemaVersion) || !businessconsole.ValidID(v.AuthorID) || len(v.Items) > MaxPublicAuthorItems {
			return ErrInvalid
		}
	} else if (v.SchemaVersion != SchemaVersion && !((v.SchemaVersion == MediaSchemaVersion || v.SchemaVersion == MomentMediaSchemaVersion) && v.Scope == Public)) || (v.Scope != Public && v.Scope != SelfPrivate) || v.AuthorID != "" {
		return ErrInvalid
	}
	if v.Items == nil || len(v.Items) > MaxItems || v.ObservedAt.Year() < 1 || v.ObservedAt.Year() > 9999 || v.ValidUntil.Year() > 9999 || !v.ValidUntil.After(v.ObservedAt) || v.ValidUntil.Sub(v.ObservedAt) > 2*time.Minute || ValidateQuery(Query{CityID: v.CityID, West: -180, South: -90, East: 180, North: 90}) != nil {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, i := range v.Items {
		key := i.Kind + ":" + i.ID
		if !ValidItem(i, v.Scope) || seen[key] {
			return ErrInvalid
		}
		if i.PlacePhoto != nil || i.RecentMoments != nil {
			if (v.SchemaVersion != MediaSchemaVersion && v.SchemaVersion != MomentMediaSchemaVersion) || v.Scope != Public || i.Kind != "PLACE" || i.ID != i.Anchor.PlaceID {
				return ErrInvalid
			}
			if i.PlacePhoto != nil && (i.PlacePhoto.EntityID != i.ID || !i.PlacePhoto.Valid(v.ObservedAt)) {
				return ErrInvalid
			}
			if i.RecentMoments != nil && !i.RecentMoments.Valid(v.ObservedAt, v.ValidUntil) {
				return ErrInvalid
			}
		}
		if i.MomentPhoto != nil && (v.SchemaVersion != MomentMediaSchemaVersion || (v.Scope != Public && v.Scope != PublicAuthor) || i.Kind != "MOMENT" || i.MomentPhoto.MomentID != i.ID || !i.MomentPhoto.Valid(v.ObservedAt)) {
			return ErrInvalid
		}
		seen[key] = true
	}
	return nil
}

// A valid view cannot be substituted for a different author or private scope.
// The native Receipt also seals the full Query, including this selector.
func ValidateViewForQuery(v View, q Query) error {
	if ValidateQuery(q) != nil || ValidateView(v) != nil || v.CityID != q.CityID || v.AuthorID != q.AuthorID {
		return ErrInvalid
	}
	expected := Public
	if q.Private {
		expected = SelfPrivate
	} else if q.AuthorID != "" {
		expected = PublicAuthor
	}
	if v.Scope != expected {
		return ErrInvalid
	}
	if q.Media != (v.SchemaVersion == MediaSchemaVersion) {
		return ErrInvalid
	}
	if q.MomentMedia != (v.SchemaVersion == MomentMediaSchemaVersion) {
		return ErrInvalid
	}
	return nil
}
func Clone(r Receipt) (Receipt, error) {
	raw, e := json.Marshal(r.View)
	if e != nil {
		return Receipt{}, ErrUnavailable
	}
	if e = json.Unmarshal(raw, &r.View); e != nil {
		return Receipt{}, ErrUnavailable
	}
	return r, nil
}
