package intent

import (
	"context"
	"errors"
	"time"
)

var (
	ErrForbidden = errors.New("intent access denied")
	ErrNotFound  = errors.New("intent not found")
	ErrConflict  = errors.New("intent conflict")
)

type Input struct {
	Confirmed       bool      `json:"confirmed"`
	Topic           string    `json:"topic"`
	Details         string    `json:"details"`
	AvailableFrom   time.Time `json:"availableFrom"`
	AvailableUntil  time.Time `json:"availableUntil"`
	TimeZone        string    `json:"timeZone"`
	CoarseAreaLabel string    `json:"coarseAreaLabel"`
	PublicMapZone   string    `json:"publicMapZone,omitempty"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

type Record struct {
	ID              string     `json:"id"`
	CityID          string     `json:"cityId"`
	OwnerID         string     `json:"ownerAccountId"`
	Topic           string     `json:"topic"`
	Details         string     `json:"details"`
	AvailableFrom   time.Time  `json:"availableFrom"`
	AvailableUntil  time.Time  `json:"availableUntil"`
	TimeZone        string     `json:"timeZone"`
	CoarseAreaLabel string     `json:"coarseAreaLabel"`
	PublicMapZone   string     `json:"publicMapZone,omitempty"`
	Audience        string     `json:"audience"`
	State           string     `json:"state"`
	ExpiresAt       time.Time  `json:"expiresAt"`
	ReviewedBy      string     `json:"reviewedBy,omitempty"`
	ReviewedAt      *time.Time `json:"reviewedAt,omitempty"`
	ReviewNote      string     `json:"reviewNote,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

type Store interface {
	SubmitIntent(context.Context, string, string, Input) (Record, error)
	ListOwnIntents(context.Context, string) ([]Record, error)
	WithdrawIntent(context.Context, string, string) error
}
