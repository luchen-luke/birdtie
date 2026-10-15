// Package legacyplacereference describes a current person's retained native
// reference. It grants no public Place, publication, copying or media access.
package legacyplacereference

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

const SchemaVersion = "owned-legacy-place-reference-v1"

var ErrNotFound = errors.New("owned legacy Place reference unavailable")
var ErrInvalid = identity.ErrControlInvalid
var ErrChanged = identity.ErrControlChanged
var ErrUnavailable = identity.ErrControlUnavailable

type Reference struct {
	Kind     string `json:"kind"`
	RecordID string `json:"recordId"`
	PlaceID  string `json:"placeId"`
}

func (r Reference) Valid() bool {
	return (r.Kind == "saved_place" || r.Kind == "theme_map") && identity.HumanControlUUID(r.RecordID) && identity.HumanControlUUID(r.PlaceID)
}

type Summary struct {
	Reference
	ObservedAt    time.Time `json:"observedAt"`
	ValidUntil    time.Time `json:"validUntil"`
	SourceVersion string    `json:"sourceVersion"`
}

func (s Summary) Valid(now time.Time) bool {
	if !s.Reference.Valid() || s.ObservedAt.IsZero() || s.ObservedAt.After(now.Add(time.Second)) || !s.ValidUntil.After(s.ObservedAt) || s.ValidUntil.Sub(s.ObservedAt) > 30*time.Second || !s.ValidUntil.After(now) || len(s.SourceVersion) != 64 {
		return false
	}
	for _, c := range s.SourceVersion {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

type Detail struct {
	SchemaVersion     string                     `json:"schemaVersion"`
	Reference         Summary                    `json:"reference"`
	Name              string                     `json:"name"`
	CategoryCode      string                     `json:"categoryCode"`
	Summary           string                     `json:"summary"`
	AddressLabel      string                     `json:"addressLabel"`
	CityID            string                     `json:"cityId"`
	CityName          string                     `json:"cityName"`
	CoordinateSystem  string                     `json:"coordinateSystem"`
	LocationPrecision string                     `json:"locationPrecision"`
	Latitude          *float64                   `json:"latitude,omitempty"`
	Longitude         *float64                   `json:"longitude,omitempty"`
	ResponseProof     identity.HumanControlProof `json:"-"`
}

func (v Detail) Valid(ref Reference, now time.Time) bool {
	if v.SchemaVersion != SchemaVersion || v.Reference.Reference != ref || !v.Reference.Valid(now) || v.Name == "" || (v.Latitude == nil) != (v.Longitude == nil) {
		return false
	}
	for _, text := range []string{v.Name, v.CategoryCode, v.Summary, v.AddressLabel, v.CityID, v.CityName} {
		if !utf8.ValidString(text) || strings.ContainsRune(text, 0) || len(text) > 16384 {
			return false
		}
	}
	if v.Latitude == nil {
		return v.LocationPrecision == "none" && v.CoordinateSystem == ""
	}
	return v.LocationPrecision == "point" && v.CoordinateSystem == "wgs84" && !math.IsNaN(*v.Latitude) && !math.IsNaN(*v.Longitude) && !math.IsInf(*v.Latitude, 0) && !math.IsInf(*v.Longitude, 0) && *v.Latitude >= -90 && *v.Latitude <= 90 && *v.Longitude >= -180 && *v.Longitude <= 180
}

type HumanStore interface {
	ReadHumanLegacyPlaceReference(context.Context, [32]byte, identity.Actor, Reference) (Detail, error)
}
