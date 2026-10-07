package agentattention

import (
	"context"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
)

// Service is the real backend boundary for Attention Policy evaluation. Current
// repository source/event validation does not authorize attention processing.
// Until an authoritative purpose-specific session/source/consent resolver is
// implemented, valid requests remain Unavailable even with both flags ON.
// No injection constructor accepts client Verified/facts or a fake resolver.
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

// Decide uses wall-clock time itself. Envelope.ReceivedAt cannot extend source
// expiry, and a serialized policy/fact object cannot grant processing.
func (s *Service) Decide(ctx context.Context, event agentevent.Envelope) (Decision, error) {
	if s == nil || s.store == nil || s.features == nil {
		return Decision{}, ErrUnavailable
	}
	if ctx == nil {
		return Decision{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	ticket, err := s.features.Capture(agentfeature.AttentionPolicy)
	if err != nil {
		return Decision{}, ErrUnavailable
	}
	policy, err := s.store.Snapshot()
	if err != nil {
		return Decision{}, err
	}
	if err := validateRequest(policy, event, time.Now()); err != nil {
		return Decision{}, err
	}
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	if !s.features.Current(ticket) || !s.store.current(policy) {
		return Decision{}, ErrDenied
	}
	return Decision{}, ErrUnavailable
}
