package content

import (
	"context"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// PublicMoment is the author's explicitly published text, never their private
// Moment metadata, activity links, evidence, profile or inferred attendance.
type PublicMoment struct {
	SchemaVersion string    `json:"schemaVersion"`
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Body          string    `json:"body"`
	PlaceID       string    `json:"placeId"`
	PlaceName     string    `json:"placeName"`
	CityID        string    `json:"cityId"`
	Revision      int64     `json:"revision"`
	PublishedAt   time.Time `json:"publishedAt"`
	SourceBinding string    `json:"-"`
}

type PublicMomentAccess struct {
	Actor         identity.Actor
	SessionDigest [32]byte
}

type PublicMomentStore interface {
	GetPublicMoment(context.Context, PublicMomentAccess, string) (PublicMoment, error)
}

func ValidatePublicMoment(p PublicMoment, expected string) error {
	if p.SchemaVersion != "public-moment-v1" || p.ID != expected || !momentUUID.MatchString(p.ID) || !momentUUID.MatchString(p.PlaceID) || p.Title == "" || len(p.Title) > 160 || len(p.Body) > 5000 || !utf8.ValidString(p.Title) || !utf8.ValidString(p.Body) || p.PlaceName == "" || !utf8.ValidString(p.PlaceName) || len(p.PlaceName) > 640 || p.CityID == "" || len(p.CityID) > 80 || p.Revision < 2 || p.PublishedAt.IsZero() || p.SourceBinding == "" {
		return ErrInvalid
	}
	return nil
}
