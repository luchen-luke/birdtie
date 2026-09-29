package community

import (
	"context"
	"errors"
	"time"
)

var (
	ErrForbidden = errors.New("community access denied")
	ErrNotFound  = errors.New("community not found")
	ErrConflict  = errors.New("community conflict")
)

type Input struct {
	Name        string    `json:"name"`
	Summary     string    `json:"summary"`
	PlaceID     string    `json:"placeId"`
	SourceLabel string    `json:"sourceLabel"`
	SourceURL   string    `json:"sourceUrl"`
	RightsNote  string    `json:"rightsNote"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type Record struct {
	ID          string     `json:"id"`
	CityID      string     `json:"cityId"`
	OwnerID     string     `json:"ownerAccountId"`
	PlaceID     string     `json:"placeId,omitempty"`
	Name        string     `json:"name"`
	Summary     string     `json:"summary"`
	SourceLabel string     `json:"sourceLabel"`
	SourceURL   string     `json:"sourceUrl"`
	RightsNote  string     `json:"rightsNote"`
	ExpiresAt   time.Time  `json:"expiresAt"`
	Status      string     `json:"status"`
	ReviewedBy  string     `json:"reviewedBy,omitempty"`
	ReviewedAt  *time.Time `json:"reviewedAt,omitempty"`
	ReviewNote  string     `json:"reviewNote,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type ReviewInput struct {
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

type Store interface {
	SubmitCommunity(context.Context, string, string, Input) (Record, error)
	ListOwnCommunities(context.Context, string) ([]Record, error)
	ListCommunityQueue(context.Context, string, string) ([]Record, error)
	ReviewCommunity(context.Context, string, string, ReviewInput) (Record, error)
	WithdrawCommunity(context.Context, string, string) error
}
