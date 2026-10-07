package agentautonomy

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentsocialpolicy"
)

func featureFixture(t *testing.T, parent, social bool) *agentfeature.Controller {
	t.Helper()
	config, err := agentfeature.ParseConfig([]byte(fmt.Sprintf(`{"schemaVersion":"agent-feature-flags-v1","flags":{"agent_enrichment":%t,"agent_memory":false,"agent_attention_policy":false,"agent_social_policy":%t,"life_map":false},"pilot":{"memory":"basic","inference":"conservative","autonomousAction":false,"sensitiveInference":false}}`, parent, social)))
	if err != nil {
		t.Fatal(err)
	}
	controller, err := agentfeature.NewController(config)
	if err != nil {
		t.Fatal(err)
	}
	return controller
}
func serviceFixture(t *testing.T, level Level, operation Operation, parent, socialFlag bool, now time.Time) (*Service, Request) {
	t.Helper()
	store, request, _ := offlineFixture(t, level, operation, now)
	service, e := NewService(store, socialFixture(t, now), featureFixture(t, parent, socialFlag))
	if e != nil {
		t.Fatal(e)
	}
	return service, request
}

func TestAutonomyActualServiceEveryOperationNeverProducesGrantOrPrepared(t *testing.T) {
	now := time.Now()
	for _, descriptor := range Operations() {
		t.Run(string(descriptor.Operation), func(t *testing.T) {
			service, request := serviceFixture(t, LevelPrepare, descriptor.Operation, true, true, now)
			result, err := service.Evaluate(context.Background(), request)
			if !errors.Is(err, ErrUnavailable) || result != (Assessment{}) || result.Authorized() {
				t.Fatalf("actual service fabricated result: %#v %v", result, err)
			}
		})
	}
}

func TestAutonomyParentAndSocialBrakesCannotAuthorizeOrPromoteLevel(t *testing.T) {
	now := time.Now()
	for _, descriptor := range Operations() {
		for _, parent := range []bool{false, true} {
			for _, social := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/parent%t/social%t", descriptor.Operation, parent, social), func(t *testing.T) {
					service, request := serviceFixture(t, LevelPrepare, descriptor.Operation, parent, social, now)
					result, err := service.Evaluate(context.Background(), request)
					if !errors.Is(err, ErrUnavailable) || result != (Assessment{}) {
						t.Fatal("flags opened missing source/purpose resolver", err)
					}
				})
			}
		}
	}
	service, request := serviceFixture(t, LevelObserve, Summarize, true, true, now)
	if result, err := service.Evaluate(context.Background(), request); !errors.Is(err, ErrDenied) || result != (Assessment{}) {
		t.Fatal("parent promoted observe to assist")
	}
	for _, feature := range []agentfeature.Feature{agentfeature.Enrichment, agentfeature.SocialPolicy} {
		t.Run("disable/"+string(feature), func(t *testing.T) {
			service, request := serviceFixture(t, LevelPrepare, PrepareInvitation, true, true, now)
			if e := service.features.Disable(feature); e != nil {
				t.Fatal(e)
			}
			result, e := service.Evaluate(context.Background(), request)
			if !errors.Is(e, ErrUnavailable) || result != (Assessment{}) {
				t.Fatal(e)
			}
		})
	}
}

func TestAutonomyServiceUsesCurrentSocialSnapshotWithoutInventedAuthority(t *testing.T) {
	now := time.Now()
	for name, mutate := range map[string]func(*Service, *Request){
		"autonomyRevoked": func(s *Service, r *Request) {
			if e := s.store.Revoke(r.SettingsRevision); e != nil {
				t.Fatal(e)
			}
		},
		"autonomyChanged": func(s *Service, r *Request) {
			if e := s.store.Replace(r.SettingsRevision, Specification{LevelPrepare, now, now.Add(time.Hour)}, now); e != nil {
				t.Fatal(e)
			}
		},
		"socialRevoked": func(s *Service, r *Request) {
			if e := s.social.Revoke(r.SocialPolicyRevision); e != nil {
				t.Fatal(e)
			}
		},
		"socialChanged": func(s *Service, r *Request) {
			p, _ := s.social.Snapshot()
			if e := s.social.Replace(p.Revision(), p.Specification()); e != nil {
				t.Fatal(e)
			}
		},
		"crossAgent": func(s *Service, r *Request) { r.Agent.AgentID = otherAgentID },
		"crossOwner": func(s *Service, r *Request) { r.Actor.ID = peerID },
	} {
		t.Run(name, func(t *testing.T) {
			service, request := serviceFixture(t, LevelPrepare, PrepareInvitation, true, true, now)
			mutate(service, &request)
			result, e := service.Evaluate(context.Background(), request)
			if !errors.Is(e, ErrDenied) || result != (Assessment{}) {
				t.Fatalf("%v %#v", e, result)
			}
		})
	}
	service, request := serviceFixture(t, LevelPrepare, PrepareInvitation, true, true, now)
	request.Social.Counterparty = actorref.PrincipalRef{Type: actorref.Business, ID: peerID}
	request.Social.Relations = []agentsocialpolicy.Relation{{Category: agentsocialpolicy.Business}}
	if result, e := service.Evaluate(context.Background(), request); !errors.Is(e, ErrUnavailable) || result != (Assessment{}) {
		t.Fatal("reserved Business activated", e)
	}
	request.Social.Relations[0].Category = agentsocialpolicy.SameUniversity
	if result, e := service.Evaluate(context.Background(), request); !errors.Is(e, ErrInvalid) || result != (Assessment{}) {
		t.Fatal("existing 041 type/category validation bypassed", e)
	}
}

func TestAutonomyServiceInvalidInputsAndContextNeverReturnPayload(t *testing.T) {
	now := time.Now()
	service, request := serviceFixture(t, LevelPrepare, Observe, true, true, now)
	if _, e := NewService(nil, service.social, service.features); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	if _, e := NewService(service.store, nil, service.features); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	if _, e := NewService(service.store, service.social, nil); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	wrong := agentFixture()
	wrong.Principal.ID = peerID
	wrong.AgentID = otherAgentID
	otherSocial, e := agentsocialpolicy.NewStore(wrong, agentsocialpolicy.Specification{ValidFrom: now, ExpiresAt: now.Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := NewService(service.store, otherSocial, service.features); !errors.Is(e, ErrDenied) {
		t.Fatal("different social principal bound")
	}
	var missing *Service
	if result, e := missing.Evaluate(context.Background(), request); !errors.Is(e, ErrUnavailable) || result != (Assessment{}) {
		t.Fatal(e)
	}
	if result, e := service.Evaluate(nil, request); !errors.Is(e, ErrInvalid) || result != (Assessment{}) {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, e := service.Evaluate(ctx, request); !errors.Is(e, context.Canceled) || result != (Assessment{}) {
		t.Fatal(e)
	}
	request.RequestedAt, request.ExpiresAt = now.Add(-MaxRequestTTL), now
	if result, e := service.Evaluate(context.Background(), request); !errors.Is(e, ErrExpired) || result != (Assessment{}) {
		t.Fatal("late request returned content", e)
	}
}
