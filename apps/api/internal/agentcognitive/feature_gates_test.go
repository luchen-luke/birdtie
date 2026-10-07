package agentcognitive

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func gateConfig(t *testing.T, enabled ...agentfeature.Feature) agentfeature.Config {
	t.Helper()
	flags := map[string]bool{"agent_enrichment": false, "agent_memory": false, "agent_attention_policy": false, "agent_social_policy": false, "life_map": false}
	for _, feature := range enabled {
		flags[string(feature)] = true
	}
	body, _ := json.Marshal(map[string]any{"schemaVersion": agentfeature.SchemaVersion, "flags": flags,
		"pilot": map[string]any{"memory": "basic", "inference": "conservative", "autonomousAction": false, "sensitiveInference": false}})
	config, err := agentfeature.ParseConfig(body)
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func gateFixture(t *testing.T, enabled ...agentfeature.Feature) (*FeatureGatedDomains, *agentfeature.Controller, *domainReadSpy, SessionAccess) {
	t.Helper()
	_, spy, request := currentDomainFixture(t)
	controller, err := agentfeature.NewController(gateConfig(t, enabled...))
	if err != nil {
		t.Fatal(err)
	}
	domains, err := NewFeatureGatedDomains(controller, spy)
	if err != nil {
		t.Fatal(err)
	}
	return domains, controller, spy, request
}

func TestFeatureGatedNativeOffNeverTouchesStoreAndOnRetainsNativeReads(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[enabled], func(t *testing.T) {
			features := []agentfeature.Feature{}
			if enabled {
				features = append(features, agentfeature.Enrichment)
			}
			domains, _, spy, request := gateFixture(t, features...)
			profile, profileErr := domains.ReadOwnProfile(context.Background(), request)
			declarations, contextErr := domains.ReadOwnContextDeclarations(context.Background(), request)
			if !enabled {
				if !errors.Is(profileErr, ErrUnavailable) || !errors.Is(contextErr, ErrUnavailable) || !reflect.ValueOf(profile).IsZero() || declarations != nil ||
					spy.authCalls+spy.agentCalls+spy.profileCalls+spy.contextCalls != 0 {
					t.Fatal("OFF touched a private source or returned a payload")
				}
				return
			}
			if profileErr != nil || contextErr != nil || profile != spy.profile || !reflect.DeepEqual(declarations, spy.declarations) ||
				spy.authCalls != 4 || spy.agentCalls != 4 || spy.profileCalls != 1 || spy.contextCalls != 1 {
				t.Fatal("ON did not reuse current native self-read guards")
			}
		})
	}
}

func TestFeatureGatedOnCannotOverrideSessionOwnerOrDomainSource(t *testing.T) {
	cases := map[string]func(*domainReadSpy, *SessionAccess){
		"anonymous":        func(_ *domainReadSpy, r *SessionAccess) { r.Digest = [32]byte{} },
		"other owner":      func(_ *domainReadSpy, r *SessionAccess) { r.Workspace.ID = "22222222-2222-4222-8222-222222222222" },
		"organization":     func(_ *domainReadSpy, r *SessionAccess) { r.Workspace.Type = actorref.Organization },
		"business":         func(_ *domainReadSpy, r *SessionAccess) { r.Workspace.Type = actorref.Business },
		"community":        func(_ *domainReadSpy, r *SessionAccess) { r.Workspace.Type = actorref.Community },
		"revoked session":  func(s *domainReadSpy, _ *SessionAccess) { s.authErr = identity.ErrUnauthorized },
		"inactive agent":   func(s *domainReadSpy, _ *SessionAccess) { s.active = false },
		"resolver failure": func(s *domainReadSpy, _ *SessionAccess) { s.authErr = errors.New("private-token-canary") },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			domains, _, spy, request := gateFixture(t, agentfeature.Enrichment)
			edit(spy, &request)
			profile, err := domains.ReadOwnProfile(context.Background(), request)
			if err == nil || !reflect.ValueOf(profile).IsZero() {
				t.Fatal("feature switch became authentication/owner permission")
			}
			items, err := domains.ReadOwnContextDeclarations(context.Background(), request)
			if err == nil || items != nil {
				t.Fatal("feature switch authorized another source")
			}
		})
	}
	t.Run("wrong profile owner", func(t *testing.T) {
		domains, _, spy, request := gateFixture(t, agentfeature.Enrichment)
		spy.profile.AccountID = "22222222-2222-4222-8222-222222222222"
		profile, err := domains.ReadOwnProfile(context.Background(), request)
		if !errors.Is(err, ErrDenied) || !reflect.ValueOf(profile).IsZero() {
			t.Fatal("feature ON accepted wrong native profile")
		}
	})
	t.Run("context not private", func(t *testing.T) {
		domains, _, spy, request := gateFixture(t, agentfeature.Enrichment)
		spy.declarations[0].Visibility = "public"
		items, err := domains.ReadOwnContextDeclarations(context.Background(), request)
		if !errors.Is(err, ErrDenied) || items != nil {
			t.Fatal("feature ON bypassed native context isolation")
		}
	})
}

func TestFeatureGatedLateDisableRevisionReplayAndSessionRevokeClearPayload(t *testing.T) {
	for _, source := range []string{"profile", "context"} {
		for _, change := range []string{"disable", "replace", "off then on", "session revoke", "agent revoke", "cancel"} {
			t.Run(source+"/"+change, func(t *testing.T) {
				domains, controller, spy, request := gateFixture(t, agentfeature.Enrichment)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				after := func() {
					switch change {
					case "disable":
						if controller.Disable(agentfeature.Enrichment) != nil {
							t.Fatal("brake failed")
						}
					case "replace":
						if controller.Replace(controller.Revision(), gateConfig(t, agentfeature.Enrichment)) != nil {
							t.Fatal("replace failed")
						}
					case "off then on":
						if controller.Disable(agentfeature.Enrichment) != nil || controller.Replace(controller.Revision(), gateConfig(t, agentfeature.Enrichment)) != nil {
							t.Fatal("replay setup failed")
						}
					case "session revoke":
						spy.authErr = identity.ErrUnauthorized
					case "agent revoke":
						spy.active = false
					case "cancel":
						cancel()
					}
				}
				if source == "profile" {
					spy.afterProfile = after
					profile, err := domains.ReadOwnProfile(ctx, request)
					if err == nil || !reflect.ValueOf(profile).IsZero() {
						t.Fatal("late native profile survived invalidation")
					}
				} else {
					spy.afterContext = after
					items, err := domains.ReadOwnContextDeclarations(ctx, request)
					if err == nil || items != nil {
						t.Fatal("late native declarations survived invalidation")
					}
				}
			})
		}
	}
}

func TestFeatureGatedMemoryPortsRemainUnavailableInEveryAcceptedFlagCombination(t *testing.T) {
	all := []agentfeature.Feature{agentfeature.Enrichment, agentfeature.Memory, agentfeature.AttentionPolicy, agentfeature.SocialPolicy, agentfeature.LifeMap}
	for bits := 0; bits < 31; bits++ {
		t.Run(string(rune('A'+bits)), func(t *testing.T) {
			enabled := []agentfeature.Feature{}
			for index, feature := range all {
				if bits&(1<<index) != 0 {
					enabled = append(enabled, feature)
				}
			}
			domains, _, spy, _ := gateFixture(t, enabled...)
			view, err := domains.ReadMemory(context.Background(), ReadRequest{})
			if !errors.Is(err, ErrUnavailable) || !reflect.ValueOf(view).IsZero() {
				t.Fatal("flag manufactured Memory reader")
			}
			receipt, err := domains.SubmitMemoryCandidate(context.Background(), CandidateSubmission{})
			if !errors.Is(err, ErrUnavailable) || receipt.Status != Unavailable || spy.authCalls+spy.profileCalls+spy.contextCalls+spy.agentCalls != 0 {
				t.Fatal("flag manufactured candidate write or touched ordinary sources")
			}
		})
	}
}

func TestFeatureGatedServerContextCannotBeChosenByClientOrJSON(t *testing.T) {
	domains, _, spy, request := gateFixture(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		installed, ok := FeatureDomainsFromContext(r.Context())
		if !ok || installed != domains {
			t.Fatal("actual server gate not bound into request")
		}
		profile, err := installed.ReadOwnProfile(r.Context(), request)
		if !errors.Is(err, ErrUnavailable) || !reflect.ValueOf(profile).IsZero() {
			t.Fatal("client enabled a switch")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	httpRequest := httptest.NewRequest("GET", "/existing-human-route", nil)
	httpRequest.Header.Set("X-Birdtie-Agent-Feature", "agent_enrichment=true")
	httpRequest.Header.Set("X-Birdtie-Feature-Revision", "1")
	FeatureBoundary(next, domains).ServeHTTP(recorder, httpRequest)
	if recorder.Code != http.StatusNoContent || spy.authCalls != 0 {
		t.Fatal("server wrapper changed direct path or trusted client flags")
	}
	if _, ok := FeatureDomainsFromContext(context.Background()); ok {
		t.Fatal("missing server context became authority")
	}
	if _, ok := FeatureDomainsFromContext(nil); ok {
		t.Fatal("nil context became authority")
	}
	if _, err := json.Marshal(domains); !errors.Is(err, ErrAuthorityJSON) {
		t.Fatal("server adapter could cross JSON boundary")
	}
	if err := json.Unmarshal([]byte(`{"controller":{"enabled":true},"store":"client"}`), domains); !errors.Is(err, ErrAuthorityJSON) || domains.controller != nil || domains.current != nil {
		t.Fatal("client decoded a server adapter")
	}
}

func TestFeatureGatedNilAndCanceledBoundaryFailsClosed(t *testing.T) {
	controller, _ := agentfeature.NewController(agentfeature.DefaultConfig())
	var typedNil *domainReadSpy
	for _, store := range []CurrentDomainStore{nil, typedNil} {
		if _, err := NewFeatureGatedDomains(controller, store); !errors.Is(err, ErrUnavailable) {
			t.Fatal("missing native store accepted")
		}
	}
	if _, err := NewFeatureGatedDomains(nil, &domainReadSpy{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing server controller accepted")
	}
	var absent *FeatureGatedDomains
	if value, err := absent.ReadOwnProfile(context.Background(), SessionAccess{}); !errors.Is(err, ErrUnavailable) || !reflect.ValueOf(value).IsZero() {
		t.Fatal("nil gate yielded profile")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	domains, _, spy, request := gateFixture(t, agentfeature.Enrichment)
	if _, err := domains.ReadOwnProfile(ctx, request); !errors.Is(err, context.Canceled) || spy.authCalls != 0 {
		t.Fatal("canceled request touched store")
	}
	for _, value := range []*FeatureGatedDomains{nil, {}} {
		recorder := httptest.NewRecorder()
		FeatureBoundary(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("invalid gate reached handler") }), value).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatal("invalid middleware did not fail closed")
		}
	}
}
