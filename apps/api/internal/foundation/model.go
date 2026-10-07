package foundation

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("resource not found")

// Source describes the public provenance and freshness of a city or place.
// Internal owner IDs and private media metadata never appear in this view.
type Source struct {
	Label      string     `json:"label"`
	Reference  string     `json:"reference"`
	Maintainer string     `json:"maintainer"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	VerifiedAt *time.Time `json:"verifiedAt,omitempty"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	Freshness  string     `json:"freshness"`
}

func (s *Source) SetFreshness(now time.Time) {
	s.Freshness = "current"
	if s.ExpiresAt != nil && !s.ExpiresAt.After(now) {
		s.Freshness = "expired"
	} else if s.VerifiedAt == nil {
		s.Freshness = "unverified"
	} else if s.VerifiedAt.Before(s.UpdatedAt) {
		s.Freshness = "review_needed"
	}
}

type City struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Region        string   `json:"region"`
	CountryCode   string   `json:"countryCode"`
	TimeZone      string   `json:"timeZone"`
	ContentStatus string   `json:"contentStatus"`
	Source        Source   `json:"source"`
	Map           *CityMap `json:"map,omitempty"`
}

type CityMap struct {
	Provider    string  `json:"provider"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	DefaultZoom float64 `json:"defaultZoom"`
	SourceRef   string  `json:"sourceRef"`
}

type Location struct {
	CoordinateSystem string   `json:"coordinateSystem"`
	Precision        string   `json:"precision"`
	Latitude         *float64 `json:"latitude,omitempty"`
	Longitude        *float64 `json:"longitude,omitempty"`
}

type Place struct {
	ID           string   `json:"id"`
	CityID       string   `json:"cityId"`
	Name         string   `json:"name"`
	CategoryCode string   `json:"categoryCode"`
	Summary      string   `json:"summary"`
	AddressLabel string   `json:"addressLabel,omitempty"`
	Location     Location `json:"location"`
	Source       Source   `json:"source"`
}

// PlaceActivityCatalog resolves activities from the same canonical Place ID.
// Public read policy remains the Activity policy, including private organizers.
type PlaceActivityCatalog interface {
	ListPlaceActivities(context.Context, string, string) ([]Activity, error)
}

type Activity struct {
	ID                  string            `json:"id"`
	Organizer           ActivityOrganizer `json:"organizer"`
	Visibility          string            `json:"visibility"`
	OrganizationID      *string           `json:"organizationId,omitempty"`
	CityID              string            `json:"cityId"`
	PlaceID             string            `json:"placeId,omitempty"`
	PlaceName           string            `json:"placeName,omitempty"`
	Modality            string            `json:"modality"`
	PhysicalPlaceStatus string            `json:"physicalPlaceStatus"`
	VenuePlaceID        *string           `json:"venuePlaceId,omitempty"`
	HostLabel           string            `json:"hostLabel"`
	Title               string            `json:"title"`
	Summary             string            `json:"summary"`
	Description         string            `json:"description"`
	CategoryCode        string            `json:"categoryCode,omitempty"`
	Capacity            *int              `json:"capacity,omitempty"`
	ParticipantCount    *int              `json:"participantCount,omitempty"`
	PriceMinor          int               `json:"priceMinor"`
	Currency            string            `json:"currency,omitempty"`
	Eligibility         string            `json:"eligibility,omitempty"`
	LanguageCode        string            `json:"languageCode,omitempty"`
	OfficialURL         string            `json:"officialUrl,omitempty"`
	StartsAt            time.Time         `json:"startsAt"`
	EndsAt              time.Time         `json:"endsAt"`
	TimeZone            string            `json:"timeZone"`
	Schedule            string            `json:"schedule"`
	EndSchedule         string            `json:"endSchedule"`
	Status              string            `json:"status"`
	Location            *Location         `json:"location,omitempty"`
	Source              Source            `json:"source"`
}

type ActivityOrganizer struct {
	Type      string  `json:"type"`
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatarUrl,omitempty"`
}

// ActivitySearchFilter selects public, current activities for a map viewport.
// From and To form a half-open interval that overlaps the activity schedule.
type ActivitySearchFilter struct {
	West, South, East, North *float64
	From, To                 *time.Time
	Category                 string
}

type PulseCategory struct {
	Code  string `json:"code"`
	Count int64  `json:"count"`
}

type AreaPulse struct {
	CityID     string          `json:"cityId"`
	Bounds     [4]float64      `json:"bounds"`
	From       *time.Time      `json:"from,omitempty"`
	To         *time.Time      `json:"to,omitempty"`
	Status     string          `json:"status"`
	Total      int64           `json:"total"`
	Categories []PulseCategory `json:"categories"`
	Activities []Activity      `json:"activities"`
	Truncated  bool            `json:"truncated"`
}

// PublicCatalog is limited to published cities and places. Authenticated
// writes and private User/Media reads are separate capabilities.
type PublicCatalog interface {
	ListCities(context.Context) ([]City, error)
	GetCity(context.Context, string) (City, error)
	ListPlaces(context.Context, string, string) ([]Place, error)
	GetPlace(context.Context, string) (Place, error)
	ListActivities(context.Context, string, string) ([]Activity, error)
	FindActivities(context.Context, string, string, ActivitySearchFilter) ([]Activity, error)
	AreaPulse(context.Context, string, string, ActivitySearchFilter) (AreaPulse, error)
	GetActivity(context.Context, string, string) (Activity, error)
}
