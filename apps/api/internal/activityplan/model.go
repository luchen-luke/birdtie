package activityplan

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("activity plan not found")
var ErrChanged = errors.New("activity plans current source changed")

type Plan struct {
	ID                  string     `json:"id"`
	ActivityID          string     `json:"activityId"`
	Title               string     `json:"title"`
	CityID              string     `json:"cityId"`
	StartsAt            *time.Time `json:"startsAt,omitempty"`
	EndsAt              *time.Time `json:"endsAt,omitempty"`
	Status              string     `json:"status"`
	Available           bool       `json:"available"`
	CreatedAt           time.Time  `json:"createdAt"`
	Modality            string     `json:"modality"`
	PhysicalPlaceStatus string     `json:"physicalPlaceStatus"`
	PlaceID             string     `json:"placeId"`
	PlaceName           string     `json:"placeName"`
	VenuePlaceID        string     `json:"venuePlaceId"`
	TimeZone            string     `json:"timeZone"`
}

// Ordinary own Plans do not require an Agent, Profile or cognitive grant.
type CurrentAccess struct {
	OwnerID       string
	SessionDigest [32]byte
}
type CurrentValidation func(context.Context) error
type HumanStore interface {
	ListActivityPlansCurrent(context.Context, CurrentAccess) ([]Plan, CurrentValidation, error)
}

type Store interface {
	PlanActivity(context.Context, string, string) (string, error)
	ListActivityPlans(context.Context, string) ([]Plan, error)
	RemoveActivityPlan(context.Context, string, string) error
}
