package placematch

import (
	"context"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	pp "github.com/birdtie/birdtie/apps/api/internal/placeprofile"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/birdtie/birdtie/apps/api/internal/venue"
)

const RuleVersion = "intent-place-v2-semantic"

type Supply struct {
	Place   foundation.Place
	Venue   *venue.Public
	Profile *pp.Public
}

type Inputs struct {
	PersonID   string
	Intent     socialintent.Record
	CityID     string
	Supply     []Supply
	ObservedAt time.Time
}

type EntityRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type Action struct {
	Type   string    `json:"type"`
	Target EntityRef `json:"target"`
}

type Match struct {
	ID                 string     `json:"id"`
	IntentID           string     `json:"intentId"`
	Place              EntityRef  `json:"place"`
	HasReviewedVenue   bool       `json:"hasReviewedVenue"`
	Capacity           *int       `json:"capacity,omitempty"`
	ReservationSupport string     `json:"reservationSupport,omitempty"`
	ReasonCodes        []string   `json:"reasonCodes"`
	Reason             string     `json:"reason"`
	Action             Action     `json:"action"`
	RuleVersion        string     `json:"ruleVersion"`
	SemanticScore      int        `json:"semanticScore"`
	SemanticProfile    *pp.Public `json:"semanticProfile,omitempty"`
	Rank               int        `json:"rank"`
	SourceExpiresAt    *time.Time `json:"sourceExpiresAt,omitempty"`
}

type Store interface {
	LoadPlaceMatchInputs(context.Context, string, string) (Inputs, error)
}

type CurrentStore interface {
	LoadCurrentPlaceMatchInputs(context.Context, pp.Access, string) (Inputs, error)
}
