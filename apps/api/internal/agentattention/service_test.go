package agentattention

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
)

func flagsFixture(t *testing.T, parent, attention bool) *agentfeature.Controller {
	t.Helper()
	config, err := agentfeature.ParseConfig([]byte(fmt.Sprintf(`{"schemaVersion":"agent-feature-flags-v1","flags":{"agent_enrichment":%t,"agent_memory":false,"agent_attention_policy":%t,"agent_social_policy":false,"life_map":false},"pilot":{"memory":"basic","inference":"conservative","autonomousAction":false,"sensitiveInference":false}}`, parent, attention)))
	if err != nil {
		t.Fatal(err)
	}
	controller, err := agentfeature.NewController(config)
	if err != nil {
		t.Fatal(err)
	}
	return controller
}

func TestBackendServiceRemainsUnavailableWithoutActualResolver(t *testing.T) {
	for _, parent := range []bool{false, true} {
		for _, flag := range []bool{false, true} {
			t.Run(fmt.Sprintf("parent_%t_attention_%t", parent, flag), func(t *testing.T) {
				now := time.Now()
				s, _ := policyFixture(t, specFixture(now))
				svc, err := NewService(s, flagsFixture(t, parent, flag))
				if err != nil {
					t.Fatal(err)
				}
				for _, kind := range []agentevent.Type{agentevent.MomentCreated, agentevent.UserQuery} {
					d, err := svc.Decide(context.Background(), eventFixture(now, kind))
					if err != ErrUnavailable || d != (Decision{}) {
						t.Fatal("invented authorization", d, err)
					}
				}
			})
		}
	}
	t.Run("serialized_valid_event_no_grant", func(t *testing.T) {
		now := time.Now()
		s, _ := policyFixture(t, specFixture(now))
		svc, _ := NewService(s, flagsFixture(t, true, true))
		e := eventFixture(now, agentevent.UserQuery)
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		restored, err := agentevent.Decode(data, now)
		if err != nil {
			t.Fatal(err)
		}
		d, err := svc.Decide(context.Background(), restored)
		if err != ErrUnavailable || d != (Decision{}) {
			t.Fatal(d, err)
		}
	})
	t.Run("wall_clock_not_historical_received", func(t *testing.T) {
		now := time.Now()
		s, _ := policyFixture(t, specFixture(now))
		svc, _ := NewService(s, flagsFixture(t, true, true))
		d, err := svc.Decide(context.Background(), eventFixture(now.Add(-agentevent.MaxEventTTL), agentevent.MomentCreated))
		if err != ErrExpired || d != (Decision{}) {
			t.Fatal(d, err)
		}
	})
	t.Run("revocation_does_not_need_flags_off", func(t *testing.T) {
		now := time.Now()
		s, _ := policyFixture(t, specFixture(now))
		svc, _ := NewService(s, flagsFixture(t, true, true))
		if err := s.Revoke(1); err != nil {
			t.Fatal(err)
		}
		d, err := svc.Decide(context.Background(), eventFixture(now, agentevent.MomentCreated))
		if err != ErrDenied || d != (Decision{}) {
			t.Fatal(d, err)
		}
	})
	t.Run("source_and_policy_revision_not_interchangeable", func(t *testing.T) {
		now := time.Now()
		s, p := policyFixture(t, specFixture(now))
		svc, _ := NewService(s, flagsFixture(t, true, true))
		e := eventFixture(now, agentevent.MomentCreated)
		e.Source.Version.Revision = int64(p.Revision())
		e.EventID = agentevent.StableEventID(e)
		if d, err := svc.Decide(context.Background(), e); err != ErrUnavailable || d != (Decision{}) {
			t.Fatal(d, err)
		}
	})
}

func TestServiceCancellationNilAndKillSwitch(t *testing.T) {
	now := time.Now()
	s, _ := policyFixture(t, specFixture(now))
	flags := flagsFixture(t, true, true)
	svc, _ := NewService(s, flags)
	e := eventFixture(now, agentevent.MomentCreated)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if d, err := svc.Decide(ctx, e); !errors.Is(err, context.Canceled) || d != (Decision{}) {
		t.Fatal(d, err)
	}
	if _, err := svc.Decide(nil, e); err != ErrInvalid {
		t.Fatal(err)
	}
	if _, err := (*Service)(nil).Decide(context.Background(), e); err != ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := NewService(nil, flags); err != ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := NewService(s, nil); err != ErrUnavailable {
		t.Fatal(err)
	}
	for _, feature := range []agentfeature.Feature{agentfeature.AttentionPolicy, agentfeature.Enrichment} {
		flags := flagsFixture(t, true, true)
		svc, _ := NewService(s, flags)
		if err := flags.Disable(feature); err != nil {
			t.Fatal(err)
		}
		if d, err := svc.Decide(context.Background(), e); err != ErrUnavailable || d != (Decision{}) {
			t.Fatal(d, err)
		}
	}
}

func TestConcurrentConfigurationCanNeverProduceDelivery(t *testing.T) {
	now := time.Now()
	s, _ := policyFixture(t, specFixture(now))
	flags := flagsFixture(t, true, true)
	svc, _ := NewService(s, flags)
	e := eventFixture(now, agentevent.MomentCreated)
	var wg sync.WaitGroup
	errs := make(chan error, 128)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := svc.Decide(context.Background(), e)
			if d != (Decision{}) || (err != ErrUnavailable && err != ErrDenied) {
				errs <- fmt.Errorf("unexpected route/error: %v %v", d, err)
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, _ := s.Snapshot()
			err := s.Replace(p.Revision(), specFixture(now))
			if err != nil && err != ErrConflict {
				errs <- err
			}
			if err := flags.Disable(agentfeature.AttentionPolicy); err != nil {
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
