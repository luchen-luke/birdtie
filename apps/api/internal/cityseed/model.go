package cityseed

import (
	"context"
	"errors"
	"time"
)

var (
	ErrForbidden = errors.New("city editor access denied")
	ErrNotFound  = errors.New("candidate not found")
	ErrConflict  = errors.New("candidate conflict")
	ErrDuplicate = errors.New("provider reference already belongs to a place")
)

type Candidate struct {
	ID                string     `json:"id"`
	CityID            string     `json:"cityId"`
	SubmittedBy       string     `json:"submittedBy"`
	Name              string     `json:"name"`
	CategoryCode      string     `json:"categoryCode"`
	Summary           string     `json:"summary"`
	Latitude          *float64   `json:"latitude,omitempty"`
	Longitude         *float64   `json:"longitude,omitempty"`
	LocationPrecision string     `json:"locationPrecision"`
	SourceLabel       string     `json:"sourceLabel"`
	SourceURL         string     `json:"sourceUrl"`
	RightsNote        string     `json:"rightsNote"`
	ProviderCode      *string    `json:"providerCode,omitempty"`
	ProviderPlaceID   *string    `json:"providerPlaceId,omitempty"`
	Attribution       *string    `json:"attribution,omitempty"`
	ExpiresAt         time.Time  `json:"expiresAt"`
	Status            string     `json:"status"`
	ReviewedBy        *string    `json:"reviewedBy,omitempty"`
	ReviewedAt        *time.Time `json:"reviewedAt,omitempty"`
	ReviewNote        *string    `json:"reviewNote,omitempty"`
	ResolvedPlaceID   *string    `json:"resolvedPlaceId,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
}

type SubmitInput struct {
	Name              string    `json:"name"`
	CategoryCode      string    `json:"categoryCode"`
	Summary           string    `json:"summary"`
	Latitude          *float64  `json:"latitude"`
	Longitude         *float64  `json:"longitude"`
	LocationPrecision string    `json:"locationPrecision"`
	SourceLabel       string    `json:"sourceLabel"`
	SourceURL         string    `json:"sourceUrl"`
	RightsNote        string    `json:"rightsNote"`
	ProviderCode      string    `json:"providerCode"`
	ProviderPlaceID   string    `json:"providerPlaceId"`
	Attribution       string    `json:"attribution"`
	ExpiresAt         time.Time `json:"expiresAt"`
}

type ReviewInput struct {
	Decision      string `json:"decision"`
	TargetPlaceID string `json:"targetPlaceId"`
	Note          string `json:"note"`
}

type Store interface {
	ActivityStore
	Submit(context.Context, string, string, SubmitInput) (Candidate, error)
	List(context.Context, string, string) ([]Candidate, error)
	Review(context.Context, string, string, ReviewInput) (Candidate, error)
}
