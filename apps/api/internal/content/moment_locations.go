package content

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacyplacereference"
	"strings"
	"time"
	"unicode/utf8"
)

const MomentLocationMaxLinks = 50
const MomentLocationsSchemaVersion = "moment-locations-v1"

var ErrMomentLocationsClientRequired = errors.New("moment_locations_client_required")

type MomentLocationInput struct {
	ReferenceID string `json:"referenceId,omitempty"`
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	CityID      string `json:"cityId"`
	PlaceID     string `json:"placeId"`
	Precision   string `json:"precision"`
}
type MomentLocationAnchor struct {
	ReferenceID      string  `json:"referenceId,omitempty"`
	TargetKind       string  `json:"targetKind,omitempty"`
	PlaceID          string  `json:"placeId"`
	Label            string  `json:"label"`
	CoordinateSystem string  `json:"coordinateSystem"`
	Precision        string  `json:"precision"`
	Latitude         float64 `json:"latitude"`
	Longitude        float64 `json:"longitude"`
}
type MomentLocationResolved struct {
	ReferenceID     string                        `json:"referenceId,omitempty"`
	ReferenceName   string                        `json:"referenceName,omitempty"`
	Attribution     string                        `json:"attribution,omitempty"`
	SourceURL       string                        `json:"sourceUrl,omitempty"`
	LegacyReference *legacyplacereference.Summary `json:"legacyReference,omitempty"`
	ID              string                        `json:"id"`
	Position        int                           `json:"position"`
	Kind            string                        `json:"kind"`
	Precision       string                        `json:"precision"`
	CityID          string                        `json:"cityId"`
	CityName        string                        `json:"cityName"`
	PlaceID         string                        `json:"placeId"`
	PlaceName       string                        `json:"placeName"`
	Region          string                        `json:"region"`
	CountryCode     string                        `json:"countryCode"`
	Available       bool                          `json:"available"`
	SourceVersion   string                        `json:"sourceVersion"`
	ObservedAt      time.Time                     `json:"observedAt"`
	ValidUntil      time.Time                     `json:"validUntil"`
	Anchor          *MomentLocationAnchor         `json:"anchor,omitempty"`
}
type PublicMomentLocation struct {
	ReferenceID   string                `json:"referenceId,omitempty"`
	ReferenceName string                `json:"referenceName,omitempty"`
	Attribution   string                `json:"attribution,omitempty"`
	SourceURL     string                `json:"sourceUrl,omitempty"`
	ID            string                `json:"id"`
	DisplayOrder  int                   `json:"displayOrder"`
	Kind          string                `json:"kind"`
	Precision     string                `json:"precision"`
	CityID        string                `json:"cityId"`
	CityName      string                `json:"cityName"`
	PlaceID       string                `json:"placeId"`
	PlaceName     string                `json:"placeName"`
	Region        string                `json:"region"`
	CountryCode   string                `json:"countryCode"`
	SourceVersion string                `json:"sourceVersion"`
	Anchor        *MomentLocationAnchor `json:"anchor,omitempty"`
}
type MomentLocationsView struct {
	SchemaVersion  string    `json:"schemaVersion"`
	MomentID       string    `json:"momentId"`
	MomentRevision int64     `json:"momentRevision"`
	Items          any       `json:"items"`
	SourceVersion  string    `json:"sourceVersion"`
	ObservedAt     time.Time `json:"observedAt"`
	ValidUntil     time.Time `json:"validUntil"`
}
type MomentLocationsReceipt struct {
	View        MomentLocationsView
	Own         bool
	Proof, Seal string
	Frame       identity.HumanControlFrame
}

func (MomentLocationsReceipt) MarshalJSON() ([]byte, error) { return nil, ErrInvalid }

type MomentLocationsStore interface {
	CaptureMomentLocations(context.Context, PublicMomentAccess, string, bool) (MomentLocationsReceipt, error)
	FinalizeMomentLocations(context.Context, PublicMomentAccess, MomentLocationsReceipt) (MomentLocationsView, error)
}

func NormalizeMomentLocations(items []MomentLocationInput) ([]MomentLocationInput, error) {
	if items == nil || len(items) > MomentLocationMaxLinks {
		return nil, ErrInvalid
	}
	out := make([]MomentLocationInput, len(items))
	ids, targets := map[string]bool{}, map[string]bool{}
	for i, v := range items {
		v.ID = strings.ToLower(v.ID)
		v.PlaceID = strings.ToLower(v.PlaceID)
		v.ReferenceID = strings.ToLower(v.ReferenceID)
		if !identity.HumanControlUUID(v.ID) || ids[v.ID] || !utf8.ValidString(v.CityID) || strings.TrimSpace(v.CityID) != v.CityID || len(v.CityID) > 80 || strings.ContainsAny(v.CityID, "/\\?#\x00") {
			return nil, ErrInvalid
		}
		target := ""
		switch v.Kind {
		case "CITY":
			if v.ReferenceID != "" || v.CityID == "" || v.PlaceID != "" || (v.Precision != "none" && v.Precision != "city") {
				return nil, ErrInvalid
			}
			target = v.CityID
		case "PLACE":
			if v.ReferenceID != "" || v.CityID != "" || !identity.HumanControlUUID(v.PlaceID) || (v.Precision != "none" && v.Precision != "city" && v.Precision != "place") {
				return nil, ErrInvalid
			}
			target = v.PlaceID
		case "REFERENCE":
			if v.CityID != "" || v.PlaceID != "" || !identity.HumanControlUUID(v.ReferenceID) || (v.Precision != "none" && v.Precision != "place") {
				return nil, ErrInvalid
			}
			target = v.ReferenceID
		default:
			return nil, ErrInvalid
		}
		key := v.Kind + ":" + target
		if targets[key] {
			return nil, ErrInvalid
		}
		targets[key] = true
		ids[v.ID] = true
		out[i] = v
	}
	return out, nil
}
func DecodeMomentLocations(raw json.RawMessage) error {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || items == nil || len(items) > MomentLocationMaxLinks {
		return identity.ErrControlInvalid
	}
	for _, item := range items {
		var hint struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(item, &hint) != nil {
			return identity.ErrControlInvalid
		}
		keys := []string{"id", "kind", "cityId", "placeId", "precision"}
		if hint.Kind == "REFERENCE" {
			keys = append(keys, "referenceId")
		}
		m, e := momentControlClosed(item, keys, nil, "")
		if e != nil {
			return e
		}
		for _, v := range m {
			var s string
			if json.Unmarshal(v, &s) != nil {
				return identity.ErrControlInvalid
			}
		}
	}
	return nil
}
