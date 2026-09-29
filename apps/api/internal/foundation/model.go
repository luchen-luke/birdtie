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
	Location     Location `json:"location"`
	Source       Source   `json:"source"`
}

type Activity struct {
	ID        string    `json:"id"`
	CityID    string    `json:"cityId"`
	PlaceID   string    `json:"placeId,omitempty"`
	HostLabel string    `json:"hostLabel"`
	Title     string    `json:"title"`
	Summary   string    `json:"summary"`
	StartsAt  time.Time `json:"startsAt"`
	EndsAt    time.Time `json:"endsAt"`
	TimeZone  string    `json:"timeZone"`
	Status    string    `json:"status"`
	Location  *Location `json:"location,omitempty"`
	Source    Source    `json:"source"`
}

// PublicCatalog is limited to published cities and places. Authenticated
// writes and private User/Media reads are separate capabilities.
type PublicCatalog interface {
	ListCities(context.Context) ([]City, error)
	GetCity(context.Context, string) (City, error)
	ListPlaces(context.Context, string, string) ([]Place, error)
	GetPlace(context.Context, string) (Place, error)
	ListActivities(context.Context, string, string) ([]Activity, error)
	GetActivity(context.Context, string, string) (Activity, error)
}
