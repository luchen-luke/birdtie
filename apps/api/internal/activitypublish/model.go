package activitypublish

import (
	"context"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

var ErrForbidden = errors.New("organization admin required")
var ErrConflict = errors.New("activity state conflict")

type Input struct {
	Organizer           Organizer `json:"organizer"`
	CityID              string    `json:"cityId"`
	PlaceID             string    `json:"placeId"`
	Modality            string    `json:"modality"`
	PhysicalPlaceStatus string    `json:"physicalPlaceStatus"`
	VenuePlaceID        string    `json:"venuePlaceId"`
	Title               string    `json:"title"`
	Summary             string    `json:"summary"`
	Description         string    `json:"description"`
	StartsAt            time.Time `json:"startsAt"`
	EndsAt              time.Time `json:"endsAt"`
	TimeZone            string    `json:"timeZone"`
	CategoryCode        string    `json:"categoryCode"`
	Capacity            *int      `json:"capacity"`
	PriceMinor          int       `json:"priceMinor"`
	Currency            string    `json:"currency"`
	Eligibility         string    `json:"eligibility"`
	LanguageCode        string    `json:"languageCode"`
	Visibility          string    `json:"visibility"`
}

type Organizer struct {
	Type      string  `json:"type"`
	ID        string  `json:"id"`
	Name      string  `json:"name,omitempty"`
	AvatarURL *string `json:"avatarUrl,omitempty"`
}

func (o Organizer) ActorRef() (actorref.ActorRef, error) {
	return actorref.Parse(o.Type, o.ID)
}

type Activity struct {
	ID                  string     `json:"id"`
	Organizer           Organizer  `json:"organizer"`
	OrganizationID      string     `json:"organizationId"`
	CityID              string     `json:"cityId"`
	PlaceID             *string    `json:"placeId,omitempty"`
	Modality            string     `json:"modality"`
	PhysicalPlaceStatus string     `json:"physicalPlaceStatus"`
	VenuePlaceID        *string    `json:"venuePlaceId,omitempty"`
	Title               string     `json:"title"`
	Summary             string     `json:"summary"`
	Description         string     `json:"description"`
	StartsAt            time.Time  `json:"startsAt"`
	EndsAt              time.Time  `json:"endsAt"`
	TimeZone            string     `json:"timeZone"`
	CategoryCode        *string    `json:"categoryCode,omitempty"`
	Capacity            *int       `json:"capacity,omitempty"`
	PriceMinor          int        `json:"priceMinor"`
	Currency            *string    `json:"currency,omitempty"`
	Eligibility         string     `json:"eligibility"`
	LanguageCode        *string    `json:"languageCode,omitempty"`
	Visibility          string     `json:"visibility"`
	PublicationStatus   string     `json:"publicationStatus"`
	PublishedAt         *time.Time `json:"publishedAt,omitempty"`
	CancelledAt         *time.Time `json:"cancelledAt,omitempty"`
	Revision            int64      `json:"revision"`
}

type Store interface {
	CreateDraft(context.Context, string, string, Input) (Activity, error)
	UpdateActivity(context.Context, string, string, string, Input) (Activity, error)
	PublishActivity(context.Context, string, string, string) (Activity, error)
	CancelActivity(context.Context, string, string, string) (Activity, error)
	ListManagedActivities(context.Context, string, string) ([]Activity, error)
}

type SocialStore interface {
	ListManagedBusinessOrganizers(context.Context, string) ([]Organizer, error)
	CreateSocialDraft(context.Context, string, Input) (Activity, error)
	UpdateSocialActivity(context.Context, string, string, Input) (Activity, error)
	PublishSocialActivity(context.Context, string, string) (Activity, error)
	CancelSocialActivity(context.Context, string, string) (Activity, error)
	ListSocialActivities(context.Context, string) ([]Activity, error)
	InviteActivityPerson(context.Context, string, string, string) error
}
