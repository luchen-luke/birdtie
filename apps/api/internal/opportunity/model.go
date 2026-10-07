package opportunity

import (
	"context"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
)

type EntityRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type Action struct {
	Type   string    `json:"type"`
	Target EntityRef `json:"target"`
}

// Candidate is a computed, rule-based suggestion. It does not reserve a place,
// send an invitation, change the Intent lifecycle or imply a verified match.
type Candidate struct {
	ID          string    `json:"id"`
	IntentID    string    `json:"intentId"`
	Entity      EntityRef `json:"entity"`
	Place       EntityRef `json:"place"`
	Title       string    `json:"title"`
	PlaceName   string    `json:"placeName"`
	ReasonCodes []string  `json:"reasonCodes"`
	Reason      string    `json:"reason"`
	Action      Action    `json:"action"`
	RuleVersion string    `json:"ruleVersion"`
	RouteTier   string    `json:"routeTier"`
	StartsAt    time.Time `json:"startsAt"`
}

type Supply struct {
	Activity foundation.Activity
	Place    foundation.Place
}

type Inputs struct {
	PersonID           string
	Intents            []socialintent.Record
	Supply             []Supply
	ContextCities      map[string]string // Context UUID -> City ID
	DeclaredCities     map[string]string // City ID -> explicit relation
	TiedPeople         map[string]bool   // active, unblocked Person ties
	JoinedCommunities  map[string]bool   // active Community membership
	FollowedOrganizers map[string]bool   // public, currently visible one-way Follow
}

type Store interface {
	LoadOpportunityInputs(context.Context, string) (Inputs, error)
}
