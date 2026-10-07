// Package mapprojection projects current domain entities onto approved public
// anchors. It is neither a new domain ledger nor a location/processing grant.
package mapprojection

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

const SchemaVersion = "typed-map-layers-v1"
const Public = "PUBLIC"
const SelfPrivate = "SELF_PRIVATE"
const MaxItems = 150

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
}

func ValidateQuery(q Query) error {
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
	Kind          string `json:"kind"`
	ID            string `json:"id"`
	Title         string `json:"title"`
	Entity        Ref    `json:"entityRef"`
	Detail        Ref    `json:"detailRef"`
	Anchor        Anchor `json:"anchor"`
	SourceVersion string `json:"sourceVersion"`
}
type View struct {
	SchemaVersion string    `json:"schemaVersion"`
	CityID        string    `json:"cityId"`
	Scope         string    `json:"scope"`
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
}

func (Receipt) MarshalJSON() ([]byte, error) { return nil, ErrInvalid }

type Store interface {
	ReadMapLayers(context.Context, Access, Query) (Receipt, error)
	RevalidateMapLayers(context.Context, Access, Receipt) error
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
	if v.SchemaVersion != SchemaVersion || (v.Scope != Public && v.Scope != SelfPrivate) || v.Items == nil || len(v.Items) > MaxItems || v.ObservedAt.Year() < 1 || v.ObservedAt.Year() > 9999 || v.ValidUntil.Year() > 9999 || !v.ValidUntil.After(v.ObservedAt) || v.ValidUntil.Sub(v.ObservedAt) > 2*time.Minute || ValidateQuery(Query{CityID: v.CityID, West: -180, South: -90, East: 180, North: 90}) != nil {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, i := range v.Items {
		key := i.Kind + ":" + i.ID
		if !ValidItem(i, v.Scope) || seen[key] {
			return ErrInvalid
		}
		seen[key] = true
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
