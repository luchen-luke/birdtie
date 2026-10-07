package agentautonomy

import (
	"context"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentsocialpolicy"
)

// Service is an actual callable process boundary, not an executor. It accepts
// no OfflineView, Verified assertions, provider callback or approval token.
// No current source/purpose/approval resolver exists: valid requests return an
// empty Assessment and Unavailable, never Granted/Prepared/success.
type Service struct {
	store         *Store
	social        *agentsocialpolicy.Store
	features      *agentfeature.Controller
	socialService *agentsocialpolicy.Service
}

func (*Service) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (*Service) UnmarshalJSON([]byte) error   { return ErrServerOnly }
func NewService(store *Store, social *agentsocialpolicy.Store, features *agentfeature.Controller) (*Service, error) {
	if store == nil || social == nil || features == nil {
		return nil, ErrUnavailable
	}
	snapshot, err := store.Snapshot()
	if err != nil {
		return nil, err
	}
	policy, err := social.Snapshot()
	if err != nil {
		return nil, ErrUnavailable
	}
	if policy.Agent() != snapshot.agent {
		return nil, ErrDenied
	}
	socialService, err := agentsocialpolicy.NewService(social, features)
	if err != nil {
		return nil, ErrUnavailable
	}
	return &Service{store, social, features, socialService}, nil
}
func (s *Service) Evaluate(ctx context.Context, request Request) (Assessment, error) {
	if s == nil || s.store == nil || s.social == nil || s.features == nil || s.socialService == nil {
		return Assessment{}, ErrUnavailable
	}
	if ctx == nil {
		return Assessment{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Assessment{}, err
	}
	snapshot, err := s.store.Snapshot()
	if err != nil {
		return Assessment{}, err
	}
	now := time.Now()
	if err := validateRequest(snapshot, request, now); err != nil {
		return Assessment{}, err
	}
	if request.Operation == TakeAutonomousAction {
		return Assessment{}, ErrUnavailable
	}
	descriptor, _ := Lookup(request.Operation)
	actual, _ := levelRank(snapshot.spec.Level)
	minimum, _ := levelRank(descriptor.MinimumLevel)
	if actual < minimum {
		return Assessment{}, ErrDenied
	}
	parentTicket, err := s.features.Capture(agentfeature.Enrichment)
	if err != nil {
		return Assessment{}, ErrUnavailable
	}
	policy, err := s.social.Snapshot()
	if err != nil {
		return Assessment{}, ErrUnavailable
	}
	if err := checkSocialPolicy(policy, request, now); err != nil {
		return Assessment{}, err
	}
	var socialTicket agentfeature.Ticket
	if descriptor.NeedsSocialPolicy {
		socialTicket, err = s.features.Capture(agentfeature.SocialPolicy)
		if err != nil {
			return Assessment{}, ErrUnavailable
		}
		_, socialError := s.socialService.Decide(ctx, *request.Social)
		if !errors.Is(socialError, agentsocialpolicy.ErrUnavailable) {
			if err := ctx.Err(); err != nil {
				return Assessment{}, err
			}
			if errors.Is(socialError, agentsocialpolicy.ErrInvalid) {
				return Assessment{}, ErrInvalid
			}
			if errors.Is(socialError, agentsocialpolicy.ErrExpired) {
				return Assessment{}, ErrExpired
			}
			return Assessment{}, ErrDenied
		}
	}
	if err := ctx.Err(); err != nil {
		return Assessment{}, err
	}
	finalNow := time.Now()
	currentPolicy, err := s.social.Snapshot()
	if err != nil {
		return Assessment{}, ErrUnavailable
	}
	if !s.store.Current(snapshot, finalNow) || !s.features.Current(parentTicket) ||
		currentPolicy.Agent() != policy.Agent() || currentPolicy.Revision() != policy.Revision() || currentPolicy.Revoked() ||
		(descriptor.NeedsSocialPolicy && !s.features.Current(socialTicket)) {
		return Assessment{}, ErrDenied
	}
	if err := validateRequest(snapshot, request, finalNow); err != nil {
		return Assessment{}, err
	}
	if err := checkSocialPolicy(currentPolicy, request, finalNow); err != nil {
		return Assessment{}, err
	}
	if err := ctx.Err(); err != nil {
		return Assessment{}, err
	}
	return Assessment{}, ErrUnavailable
}
