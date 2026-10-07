package agentsocialpolicy

import (
	"context"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
)

// Service is a callable backend domain boundary. Current repository has no
// purpose-specific resolver for SocialInteractionPolicy/education/current
// pair consent. Valid enabled requests remain Unavailable. Old matching,
// Profile projection, friend consent or role grants are not substituted.
type Service struct {
	store    *Store
	features *agentfeature.Controller
}

func NewService(store *Store, features *agentfeature.Controller) (*Service, error) {
	if store == nil || features == nil {
		return nil, ErrUnavailable
	}
	return &Service{store, features}, nil
}
func (s *Service) Decide(ctx context.Context, request Request) (Decision, error) {
	if s == nil || s.store == nil || s.features == nil {
		return Decision{}, ErrUnavailable
	}
	if ctx == nil {
		return Decision{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	ticket, err := s.features.Capture(agentfeature.SocialPolicy)
	if err != nil {
		return Decision{}, ErrUnavailable
	}
	policy, err := s.store.Snapshot()
	if err != nil {
		return Decision{}, err
	}
	if err := validateRequest(policy, request, time.Now()); err != nil {
		return Decision{}, err
	}
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	if !s.features.Current(ticket) || !s.store.current(policy) {
		return Decision{}, ErrDenied
	}
	// No production entry accepts OfflineBoundary or calls EvaluateOffline.
	return Decision{}, ErrUnavailable
}
