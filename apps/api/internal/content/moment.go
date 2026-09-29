package content

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("content not found")
	ErrConflict = errors.New("content conflict")
)

type MomentInput struct {
	CityID            string     `json:"cityId"`
	PlaceID           string     `json:"placeId"`
	Title             string     `json:"title"`
	Body              string     `json:"body"`
	OccurredAt        *time.Time `json:"occurredAt"`
	TimePrecision     string     `json:"timePrecision"`
	LocationPrecision string     `json:"locationPrecision"`
}

type Moment struct {
	ID                string     `json:"id"`
	AuthorAccountID   string     `json:"authorAccountId"`
	CityID            string     `json:"cityId"`
	PlaceID           string     `json:"placeId,omitempty"`
	Title             string     `json:"title"`
	Body              string     `json:"body"`
	OccurredAt        *time.Time `json:"occurredAt,omitempty"`
	TimePrecision     string     `json:"timePrecision"`
	LocationPrecision string     `json:"locationPrecision"`
	Visibility        string     `json:"visibility"`
	Status            string     `json:"status"`
	Revision          int64      `json:"revision"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type MomentStore interface {
	CreateMomentDraft(context.Context, string, MomentInput) (Moment, error)
	ListOwnMoments(context.Context, string) ([]Moment, error)
	GetOwnMoment(context.Context, string, string) (Moment, error)
	UpdateMomentDraft(context.Context, string, string, int64, MomentInput) (Moment, error)
	WithdrawMoment(context.Context, string, string, int64) error
}
