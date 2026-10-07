// Package socialnow defines ordinary current human reads, not Agent/model
// content authorization, a new identity, a feed ledger, or inferred interests.
package socialnow

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/opportunity"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
)

var ErrUnavailable = errors.New("current social sources unavailable")

type HumanTiesStore interface {
	ListHumanTies(context.Context, [32]byte, identity.Actor) ([]connection.Tie, error)
}
type HumanIntentsStore interface {
	ListHumanVisibleSocialIntents(context.Context, [32]byte, identity.Actor) ([]socialintent.Record, error)
	GetHumanVisibleSocialIntent(context.Context, [32]byte, identity.Actor, string) (socialintent.Record, error)
}
type HumanOpportunitiesStore interface {
	ListHumanOpportunities(context.Context, [32]byte, identity.Actor) ([]opportunity.Candidate, error)
}

// Same real Session source, fresh clock after actual locks. Authentication may
// refresh a currently eligible idle deadline; final validation never refreshes.
type HumanSessionStore interface {
	AuthenticateHumanSocial(context.Context, [32]byte) (identity.Actor, error)
	ValidateHumanSocialResponse(context.Context, [32]byte, identity.Actor) error
}
