package content

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"regexp"
	"strings"
	"time"
)

var (
	ErrNotFound    = errors.New("content not found")
	ErrConflict    = errors.New("content conflict")
	ErrUnavailable = errors.New("content unavailable")
	ErrInvalid     = errors.New("invalid content")
)

type MomentInput struct {
	CityID            string     `json:"cityId"`
	PlaceID           string     `json:"placeId"`
	Title             string     `json:"title"`
	Body              string     `json:"body"`
	OccurredAt        *time.Time `json:"occurredAt"`
	TimePrecision     string     `json:"timePrecision"`
	LocationPrecision string     `json:"locationPrecision"`
	ActivityID        *string    `json:"activityId,omitempty"`
	CommunityID       *string    `json:"communityId,omitempty"`
	OrganizationID    *string    `json:"organizationId,omitempty"`
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
	ActivityIDs       []string   `json:"activityIds"`
	CommunityID       string     `json:"communityId,omitempty"`
	OrganizationID    string     `json:"organizationId,omitempty"`
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

// HumanMomentStore is the current ordinary person's private Moment gateway.
// Digest and initialActor originate in server Authenticate, never request JSON.
// It grants no Agent/model, organization workspace or Memory permission. The
// legacy owner-ID Store is not an authorized fallback for registered HTTP.
type HumanMomentStore interface {
	CreateHumanMomentDraft(context.Context, [32]byte, identity.Actor, MomentInput) (Moment, error)
	ListHumanMoments(context.Context, [32]byte, identity.Actor) ([]Moment, error)
	GetHumanMoment(context.Context, [32]byte, identity.Actor, string) (Moment, error)
	UpdateHumanMomentDraft(context.Context, [32]byte, identity.Actor, string, int64, MomentInput) (Moment, error)
	WithdrawHumanMoment(context.Context, [32]byte, identity.Actor, string, int64) error
}

var momentUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// NormalizeMomentInput preserves the original shape and byte limits. The
// current database wall clock, not a caller clock, checks the future bound.
func NormalizeMomentInput(input MomentInput) (MomentInput, error) {
	input.CityID = strings.TrimSpace(input.CityID)
	input.Title = strings.TrimSpace(input.Title)
	input.Body = strings.TrimSpace(input.Body)
	for _, slot := range []**string{&input.ActivityID, &input.CommunityID, &input.OrganizationID} {
		if *slot != nil {
			value := strings.TrimSpace(**slot)
			if value != "" && !momentUUID.MatchString(value) {
				return MomentInput{}, ErrInvalid
			}
			*slot = &value
		}
	}
	if len(input.CityID) == 0 || len(input.CityID) > 80 || len(input.Title) == 0 || len(input.Title) > 160 || len(input.Body) > 5000 || (input.PlaceID != "" && !momentUUID.MatchString(input.PlaceID)) {
		return MomentInput{}, ErrInvalid
	}
	switch input.TimePrecision {
	case "unknown":
		if input.OccurredAt != nil {
			return MomentInput{}, ErrInvalid
		}
	case "year", "month", "day", "instant":
		if input.OccurredAt == nil {
			return MomentInput{}, ErrInvalid
		}
		utc := input.OccurredAt.UTC()
		if utc.Year() < 1 || utc.Year() > 9999 {
			return MomentInput{}, ErrInvalid
		}
		input.OccurredAt = &utc
	default:
		return MomentInput{}, ErrInvalid
	}
	switch input.LocationPrecision {
	case "none", "city":
	case "place":
		if input.PlaceID == "" {
			return MomentInput{}, ErrInvalid
		}
	default:
		return MomentInput{}, ErrInvalid
	}
	return input, nil
}
