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
	// This is also ErrDuplicate at the original Submit contract. Acquisition
	// can separately distinguish current published reuse from a pending conflict.
	ErrSourceAlreadyCurrent = errors.New("current published source facts unchanged")
)

type Candidate struct {
	ID                string            `json:"id"`
	CityID            string            `json:"cityId"`
	SubmittedBy       string            `json:"submittedBy"`
	Name              string            `json:"name"`
	CategoryCode      string            `json:"categoryCode"`
	Summary           string            `json:"summary"`
	AddressLabel      string            `json:"addressLabel,omitempty"`
	Latitude          *float64          `json:"latitude,omitempty"`
	Longitude         *float64          `json:"longitude,omitempty"`
	LocationPrecision string            `json:"locationPrecision"`
	SourceLabel       string            `json:"sourceLabel"`
	SourceURL         string            `json:"sourceUrl"`
	RightsNote        string            `json:"rightsNote"`
	ProviderCode      *string           `json:"providerCode,omitempty"`
	ProviderPlaceID   *string           `json:"providerPlaceId,omitempty"`
	Attribution       *string           `json:"attribution,omitempty"`
	ExpiresAt         time.Time         `json:"expiresAt"`
	Status            string            `json:"status"`
	ReviewedBy        *string           `json:"reviewedBy,omitempty"`
	ReviewedAt        *time.Time        `json:"reviewedAt,omitempty"`
	ReviewNote        *string           `json:"reviewNote,omitempty"`
	ResolvedPlaceID   *string           `json:"resolvedPlaceId,omitempty"`
	CreatedAt         time.Time         `json:"createdAt"`
	PermissionID      *string           `json:"permissionId,omitempty"`
	Permission        *SourcePermission `json:"permission,omitempty"`
	FieldClaims       []PlaceFieldInput `json:"fieldClaims,omitempty"`
}

type SubmitInput struct {
	Name              string                 `json:"name"`
	CategoryCode      string                 `json:"categoryCode"`
	Summary           string                 `json:"summary"`
	AddressLabel      string                 `json:"addressLabel"`
	Latitude          *float64               `json:"latitude"`
	Longitude         *float64               `json:"longitude"`
	LocationPrecision string                 `json:"locationPrecision"`
	SourceLabel       string                 `json:"sourceLabel"`
	SourceURL         string                 `json:"sourceUrl"`
	RightsNote        string                 `json:"rightsNote"`
	ProviderCode      string                 `json:"providerCode"`
	ProviderPlaceID   string                 `json:"providerPlaceId"`
	Attribution       string                 `json:"attribution"`
	ExpiresAt         time.Time              `json:"expiresAt"`
	Rights            *SourcePermissionInput `json:"rights"`
	Fields            []PlaceFieldInput      `json:"fields"`
}

type ReviewInput struct {
	Decision                  string `json:"decision"`
	TargetPlaceID             string `json:"targetPlaceId"`
	Note                      string `json:"note"`
	RightsConfirmed           bool   `json:"rightsConfirmed"`
	FieldsConfirmed           bool   `json:"fieldsConfirmed"`
	ExpectedPermissionVersion int64  `json:"expectedPermissionVersion"`
	PermissionState           string `json:"permissionState,omitempty"`
}

type Store interface {
	ActivityStore
	Submit(context.Context, string, string, SubmitInput) (Candidate, error)
	List(context.Context, string, string) ([]Candidate, error)
	Review(context.Context, string, string, ReviewInput) (Candidate, error)
}
