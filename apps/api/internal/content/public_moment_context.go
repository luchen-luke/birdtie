package content

import (
	"context"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const PublicMomentContextSchemaVersion = "public-moment-v4"

// This explicit context wire permits city-only text. Empty Place fields are
// intentional; they never imply the city centre or a guessed map coordinate.
// Public photographs continue to use the independent Place-only 145 contract.
type PublicMomentWithContext struct {
	PublicMomentWithPhoto
	CityName          string `json:"cityName"`
	LocationPrecision string `json:"locationPrecision"`
}

func ValidatePublicMomentWithContext(v PublicMomentWithContext, id string) error {
	if v.SchemaVersion != PublicMomentContextSchemaVersion || v.ID != id || !momentUUID.MatchString(id) || len(v.CityID) > 80 || !utf8.ValidString(v.CityID) || len(v.CityName) > 640 || !utf8.ValidString(v.CityName) || (v.CityID == "") != (v.CityName == "") || len(v.Title) > 160 || !utf8.ValidString(v.Title) || len(v.Body) > 5000 || !utf8.ValidString(v.Body) || v.Revision < 2 || v.Revision > MomentControlMaxRevision || v.PublishedAt.IsZero() || v.SourceBinding == "" || v.ObservedAt.IsZero() || !v.ValidUntil.After(v.ObservedAt) || v.ValidUntil.Sub(v.ObservedAt) > time.Minute || ValidatePublicMomentAuthor(v.AuthorID, v.AuthorPublicProfile) != nil {
		return ErrInvalid
	}
	switch v.LocationPrecision {
	case "none":
		if v.CityID != "" || v.CityName != "" || v.PlaceID != "" || v.PlaceName != "" || v.Photo != nil {
			return ErrInvalid
		}
	case "city":
		if v.CityID == "" || v.PlaceID != "" || v.PlaceName != "" || v.Photo != nil {
			return ErrInvalid
		}
	case "place":
		if v.PlaceID == "" || v.PlaceName == "" {
			return ErrInvalid
		}
		legacy := v.PublicMomentWithPhoto
		legacy.SchemaVersion = PublicMomentPhotoSchemaVersion
		if ValidatePublicMomentWithPhoto(legacy, id) != nil {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

// Native, process-sealed proof. It cannot be serialized as public content.
type PublicMomentContextReceipt struct {
	View                        PublicMomentWithContext
	IncludeAuthor, IncludePhoto bool
	BaseProof, PhotoProof, Seal string
	Frame                       identity.HumanControlFrame
}

func (PublicMomentContextReceipt) MarshalJSON() ([]byte, error) { return nil, ErrInvalid }

type PublicMomentWithContextStore interface {
	CapturePublicMomentWithContext(context.Context, PublicMomentAccess, string, bool, bool) (PublicMomentContextReceipt, error)
	FinalizePublicMomentWithContext(context.Context, PublicMomentAccess, PublicMomentContextReceipt) (PublicMomentWithContext, error)
}
