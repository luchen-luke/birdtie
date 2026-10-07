package activityparticipation

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/activityplan"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"time"
)

var (
	ErrNotFound    = errors.New("activity or participation not found")
	ErrUnavailable = errors.New("activity unavailable for participation")
	ErrFull        = errors.New("activity capacity reached")
)

type Participation struct {
	ID          string     `json:"id"`
	ActivityID  string     `json:"activityId"`
	Status      string     `json:"status"`
	CancelledAt *time.Time `json:"cancelledAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type Overview struct {
	ID                  string     `json:"id"`
	ActivityID          string     `json:"activityId"`
	Status              string     `json:"status"`
	Title               string     `json:"title"`
	CityID              string     `json:"cityId"`
	StartsAt            *time.Time `json:"startsAt,omitempty"`
	EndsAt              *time.Time `json:"endsAt,omitempty"`
	ActivityStatus      string     `json:"activityStatus"`
	Available           bool       `json:"available"`
	Modality            string     `json:"modality"`
	PhysicalPlaceStatus string     `json:"physicalPlaceStatus"`
	PlaceID             string     `json:"placeId"`
	PlaceName           string     `json:"placeName"`
	VenuePlaceID        string     `json:"venuePlaceId"`
	TimeZone            string     `json:"timeZone"`
}

type HumanStore interface {
	ListParticipationsCurrent(context.Context, activityplan.CurrentAccess) ([]Overview, activityplan.CurrentValidation, error)
}

type Store interface {
	ListParticipations(context.Context, string) ([]Overview, error)
	GetParticipation(context.Context, string, string) (Participation, error)
	JoinActivity(context.Context, string, string) (Participation, bool, error)
	CancelParticipation(context.Context, string, string) (Participation, error)
}

type BoundStore interface {
	JoinActivityBound(context.Context, ea.Access, string, ea.BoundCondition) (Participation, bool, error)
	CancelParticipationBound(context.Context, ea.Access, string, ea.BoundCondition) (Participation, error)
}
