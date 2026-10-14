package saved

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/communityavatar"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/entitycover"
	"github.com/birdtie/birdtie/apps/api/internal/placephoto"
	"math"
	"time"
)

var ErrNotFound = errors.New("saved item not found")

type Item struct {
	PlacePhoto      *placephoto.Metadata      `json:"placePhoto,omitempty"`
	Cover           *entitycover.Metadata     `json:"cover,omitempty"`
	ID              string                    `json:"id"`
	Kind            string                    `json:"kind"`
	TargetID        string                    `json:"targetId"`
	Title           string                    `json:"title"`
	Summary         string                    `json:"summary"`
	CityID          string                    `json:"cityId"`
	CityName        string                    `json:"cityName,omitempty"`
	Available       bool                      `json:"available"`
	SavedAt         time.Time                 `json:"savedAt"`
	Note            string                    `json:"note"`
	NoteRevision    int64                     `json:"noteRevision"`
	CommunityAvatar *communityavatar.Metadata `json:"communityAvatar,omitempty"`
	Latitude        *float64                  `json:"latitude,omitempty"`
	Longitude       *float64                  `json:"longitude,omitempty"`
	SourceExpiresAt *time.Time                `json:"sourceExpiresAt,omitempty"`
}

// A Saved reference grants no point permission. Coordinates are an optional
// current public Place projection, never a private note or inferred location.
func (i Item) ValidPoint(now time.Time) bool {
	if i.Latitude == nil && i.Longitude == nil && i.SourceExpiresAt == nil {
		return true
	}
	return i.Kind == "place" && i.Available && i.Latitude != nil && i.Longitude != nil && i.SourceExpiresAt != nil &&
		!math.IsNaN(*i.Latitude) && !math.IsInf(*i.Latitude, 0) && !math.IsNaN(*i.Longitude) && !math.IsInf(*i.Longitude, 0) &&
		*i.Latitude >= -90 && *i.Latitude <= 90 && *i.Longitude >= -180 && *i.Longitude <= 180 &&
		i.SourceExpiresAt.Location() == time.UTC && i.SourceExpiresAt.After(now) && !i.SourceExpiresAt.After(now.Add(61*time.Second))
}

func (i Item) ValidCommunityMedia() bool {
	return i.CommunityAvatar == nil || (i.Available && i.Kind == "group" && i.CommunityAvatar.Valid() && i.CommunityAvatar.CommunityID == i.TargetID)
}

func (i Item) ValidCover(now time.Time) bool {
	return i.Cover == nil || (i.Kind == "activity" && i.Available && i.Cover.Ref == (entitycover.Ref{Type: "activity", ID: i.TargetID}) && i.Cover.Valid(now))
}

func (i Item) ValidPlacePhoto(now time.Time) bool {
	return i.PlacePhoto == nil || (i.Kind == "place" && i.Available && i.PlacePhoto.EntityID == i.TargetID && i.PlacePhoto.Valid(now))
}

type BoundStore interface {
	SaveBound(context.Context, ea.Access, string, string, ea.BoundCondition) (string, error)
	RemoveSavedBound(context.Context, ea.Access, string, ea.BoundCondition) error
}

type Store interface {
	Save(context.Context, string, string, string) (string, error)
	ListSaved(context.Context, string) ([]Item, error)
	RemoveSaved(context.Context, string, string) error
}
