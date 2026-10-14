package content

import (
	"context"
	"strings"
	"time"
	"unicode"
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

const PublicMomentAuthorSchemaVersion = "public-moment-v2"

// This is an explicit public Profile label, not the Moment's private owner
// metadata or an inferred identity. Both nullable fields remain on the v2 wire.
type PublicMomentAuthor struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
}

type PublicMomentWithAuthor struct {
	PublicMoment
	AuthorID            *string             `json:"authorId"`
	AuthorPublicProfile *PublicMomentAuthor `json:"authorPublicProfile"`
}

type PublicMomentWithAuthorStore interface {
	GetPublicMomentWithAuthor(context.Context, PublicMomentAccess, string) (PublicMomentWithAuthor, error)
}

func ValidatePublicMomentAuthor(id *string, profile *PublicMomentAuthor) error {
	if id == nil && profile == nil {
		return nil
	}
	if id == nil || profile == nil || !momentUUID.MatchString(*id) || profile.AccountID != *id || !utf8.ValidString(profile.DisplayName) || profile.DisplayName == "" || len(profile.DisplayName) > 80 || strings.TrimSpace(profile.DisplayName) != profile.DisplayName {
		return ErrInvalid
	}
	for _, r := range profile.DisplayName {
		if unicode.IsControl(r) {
			return ErrInvalid
		}
	}
	return nil
}

func ValidatePublicMomentWithAuthor(p PublicMomentWithAuthor, expected string) error {
	if p.SchemaVersion != PublicMomentAuthorSchemaVersion {
		return ErrInvalid
	}
	legacy := p.PublicMoment
	legacy.SchemaVersion = "public-moment-v1"
	if ValidatePublicMoment(legacy, expected) != nil || ValidatePublicMomentAuthor(p.AuthorID, p.AuthorPublicProfile) != nil {
		return ErrInvalid
	}
	return nil
}
