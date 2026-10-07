package agentsocialpolicy

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
)

func flagsFixture(t *testing.T, parent, flag bool) *agentfeature.Controller {
	t.Helper()
	config, err := agentfeature.ParseConfig([]byte(fmt.Sprintf(`{"schemaVersion":"agent-feature-flags-v1","flags":{"agent_enrichment":%t,"agent_memory":false,"agent_attention_policy":false,"agent_social_policy":%t,"life_map":false},"pilot":{"memory":"basic","inference":"conservative","autonomousAction":false,"sensitiveInference":false}}`, parent, flag)))
	if err != nil {
		t.Fatal(err)
	}
	controller, err := agentfeature.NewController(config)
	if err != nil {
		t.Fatal(err)
	}
	return controller
}
func TestActualServiceCannotUseSyntheticOrOldPurposeAuthority(t *testing.T) {
	for _, parent := range []bool{false, true} {
		for _, flag := range []bool{false, true} {
			t.Run(fmt.Sprintf("parent_%t_social_%t", parent, flag), func(t *testing.T) {
				now := time.Now()
				spec := specFixture(now)
				for _, c := range Categories() {
					spec.Rules = append(spec.Rules, Rule{c, ReviewRequired})
				}
				s, _ := storeFixture(t, spec)
				service, err := NewService(s, flagsFixture(t, parent, flag))
				if err != nil {
					t.Fatal(err)
				}
				for _, category := range Categories() {
					r := requestFixture(now, category)
					d, err := service.Decide(context.Background(), r)
					if err != ErrUnavailable || d != (Decision{}) {
						t.Fatal("authorization invented", category, d, err)
					}
				}
			})
		}
	}
	t.Run("educational_declaration_not_verified_identity", func(t *testing.T) {
		now := time.Now()
		spec := specFixture(now)
		spec.Rules = []Rule{{SameUniversity, ReviewRequired}}
		s, p := storeFixture(t, spec)
		r := requestFixture(now, SameUniversity)
		d, err := EvaluateOffline(p, r, boundaryFixture(p, r, now), now)
		if err != nil || d.Preference != ReviewRequired || d.Mode != "OFFLINE_CONTRACT" {
			t.Fatal(d, err)
		}
		service, _ := NewService(s, flagsFixture(t, true, true))
		d, err = service.Decide(context.Background(), r)
		if err != ErrUnavailable || d != (Decision{}) {
			t.Fatal("declaration became authority", d, err)
		}
	})
	t.Run("wallclock_rejects_old_request", func(t *testing.T) {
		now := time.Now()
		s, _ := storeFixture(t, specFixture(now))
		service, _ := NewService(s, flagsFixture(t, true, true))
		r := requestFixture(now.Add(-MaxRequestTTL), UnknownPerson)
		if d, err := service.Decide(context.Background(), r); err != ErrExpired || d != (Decision{}) {
			t.Fatal(d, err)
		}
	})
	t.Run("revoked_policy_does_not_need_flags_off", func(t *testing.T) {
		now := time.Now()
		s, _ := storeFixture(t, specFixture(now))
		service, _ := NewService(s, flagsFixture(t, true, true))
		if s.Revoke(1) != nil {
			t.Fatal("revoke failed")
		}
		if d, err := service.Decide(context.Background(), requestFixture(now, UnknownPerson)); err != ErrDenied || d != (Decision{}) {
			t.Fatal(d, err)
		}
	})
}
func TestServiceNilCancelAndKillRemainFailClosed(t *testing.T) {
	now := time.Now()
	s, _ := storeFixture(t, specFixture(now))
	flags := flagsFixture(t, true, true)
	service, _ := NewService(s, flags)
	r := requestFixture(now, UnknownPerson)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if d, err := service.Decide(ctx, r); err != context.Canceled || d != (Decision{}) {
		t.Fatal(d, err)
	}
	if _, err := service.Decide(nil, r); err != ErrInvalid {
		t.Fatal(err)
	}
	if _, err := (*Service)(nil).Decide(context.Background(), r); err != ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := NewService(nil, flags); err != ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := NewService(s, nil); err != ErrUnavailable {
		t.Fatal(err)
	}
	for _, feature := range []agentfeature.Feature{agentfeature.Enrichment, agentfeature.SocialPolicy} {
		flags := flagsFixture(t, true, true)
		service, _ := NewService(s, flags)
		if flags.Disable(feature) != nil {
			t.Fatal("kill failed")
		}
		if d, err := service.Decide(context.Background(), r); err != ErrUnavailable || d != (Decision{}) {
			t.Fatal(d, err)
		}
	}
}
func TestConcurrentPolicyAndFeatureChangesCannotProduceSuggestionOrMessage(t *testing.T) {
	now := time.Now()
	s, _ := storeFixture(t, specFixture(now))
	flags := flagsFixture(t, true, true)
	service, _ := NewService(s, flags)
	r := requestFixture(now, UnknownPerson)
	var wg sync.WaitGroup
	errs := make(chan error, 128)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := service.Decide(context.Background(), r)
			if d != (Decision{}) || (err != ErrDenied && err != ErrUnavailable) {
				errs <- fmt.Errorf("unexpected decision %v %v", d, err)
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, _ := s.Snapshot()
			if err := s.Replace(p.revision, specFixture(now)); err != nil && err != ErrConflict {
				errs <- err
			}
			if err := flags.Disable(agentfeature.SocialPolicy); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
