package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcandidatepipeline"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
)

type featureMainStore struct{ calls int }

const featureMainOwner = "11111111-1111-4111-8111-111111111111"

func TestMainAgentRunDefaultOffAndNilNativeDependencies(t *testing.T) {
	flags, e := agentfeature.NewController(agentfeature.DefaultConfig())
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range []*agentfeature.Controller{flags, nil} {
		runs := postgres.NewAgentRuns(nil, c)
		if _, e = runs.ClaimAgentRun(context.Background(), "22222222-2222-4222-8222-222222222222"); !errors.Is(e, agentrun.ErrUnavailable) {
			t.Fatal("default/nil reached worker", e)
		}
		a := agentprofile.PrivateAccess{SessionDigest: [32]byte{1}, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: featureMainOwner}}
		r, e := runs.ScheduleOwn(context.Background(), a, agentrun.Input{MomentID: "22222222-2222-4222-8222-222222222222"})
		if !errors.Is(e, agentrun.ErrUnavailable) || r.ID != "" || r.Committed {
			t.Fatal("nil source invented Run", r, e)
		}
	}
}

func TestMainCandidatePipelineActualConstructorDefaultOffHasNoNativeWrite(t *testing.T) {
	controller, err := agentfeature.NewController(agentfeature.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	// Actual constructor with no database: default OFF must reject before any
	// native executor access. This is startup gate coverage, not IdP/DB proof.
	for _, c := range []*agentfeature.Controller{controller, nil} {
		gateway := postgres.NewCandidatePipeline(nil, c)
		access := agentprofile.PrivateAccess{SessionDigest: [32]byte{1}, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: featureMainOwner}}
		receipt, e := gateway.StageOwnMomentCandidate(context.Background(), access, "22222222-2222-4222-8222-222222222222")
		if !errors.Is(e, agentcandidatepipeline.ErrUnavailable) || receipt.Committed || receipt.Candidate != nil {
			t.Fatal("startup default/nil gate reached a native write", receipt, e)
		}
	}
}

func (s *featureMainStore) Authenticate(context.Context, [32]byte) (identity.Actor, error) {
	s.calls++
	return identity.Actor{ID: featureMainOwner, AccountType: "person"}, nil
}
func (*featureMainStore) HasActiveAgent(context.Context, string, string) (bool, error) {
	return true, nil
}
func (*featureMainStore) ReadProfile(context.Context, string, string) (identity.Profile, error) {
	return identity.Profile{AccountID: featureMainOwner, DisplayName: "合成本人资料", Visibility: "private"}, nil
}
func (*featureMainStore) ListOwnContextDeclarations(context.Context, string) ([]contextgraph.Declaration, error) {
	return nil, nil
}

func TestMainAgentFeatureBoundaryUsesActualServerConfigAndNativeAdapter(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "default off", true: "staged on"}[enabled], func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"schemaVersion": agentfeature.SchemaVersion,
				"flags": map[string]bool{"agent_enrichment": enabled, "agent_memory": false, "agent_attention_policy": false, "agent_social_policy": false, "life_map": false},
				"pilot": map[string]any{"memory": "basic", "inference": "conservative", "autonomousAction": false, "sensitiveInference": false}})
			config, err := agentfeature.LoadConfig(func(key string) (string, bool) {
				if key != agentfeature.EnvironmentKey {
					t.Fatal("wrong server configuration source")
				}
				return string(body), enabled
			})
			if err != nil {
				t.Fatal(err)
			}
			store := &featureMainStore{}
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				domains, ok := agentcognitive.FeatureDomainsFromContext(r.Context())
				if !ok {
					t.Fatal("main configured flags but discarded the actual boundary")
				}
				profile, readErr := domains.ReadOwnProfile(r.Context(), agentcognitive.SessionAccess{Digest: [32]byte{1},
					Workspace: actorref.PrincipalRef{Type: actorref.Person, ID: featureMainOwner}})
				if enabled {
					if readErr != nil || profile.AccountID != featureMainOwner || profile.Visibility != "private" {
						t.Fatal("configured wrapper did not use original native self review")
					}
				} else if !errors.Is(readErr, agentcognitive.ErrUnavailable) || profile.AccountID != "" {
					t.Fatal("client activated missing/default server flags")
				}
				if _, memoryErr := domains.ReadMemory(r.Context(), agentcognitive.ReadRequest{}); !errors.Is(memoryErr, agentcognitive.ErrUnavailable) {
					t.Fatal("main created a fake Memory service")
				}
				w.WriteHeader(http.StatusNoContent)
			})
			handler, err := newAgentFeatureBoundary(config, store, next)
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest("GET", "/test-only-internal-adapter", nil)
			request.Header.Set("X-Birdtie-Agent-Feature", "ALL=true")
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusNoContent || (!enabled && store.calls != 0) || (enabled && store.calls != 2) {
				t.Fatal("main integration ignored current gates or altered the native auth path")
			}
		})
	}
}

func TestMainAgentFeatureBoundaryDoesNotBlockDirectHumanPrivacyManagement(t *testing.T) {
	store := &featureMainStore{}
	for _, path := range []string{"/v1/me", "/v1/me/contexts", "/v1/me/agent-private-profile", "/v1/me/agent-profile-visibility", "/v1/me/agent-memory"} {
		t.Run(path, func(t *testing.T) {
			handler, err := newAgentFeatureBoundary(agentfeature.DefaultConfig(), store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != path {
					t.Fatal("wrapper changed direct route")
				}
				w.WriteHeader(http.StatusAccepted)
			}))
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest("PUT", path, nil))
			if recorder.Code != http.StatusAccepted || store.calls != 0 {
				t.Fatal("OFF blocked the direct handler or read ordinary/private state")
			}
		})
	}
}

func TestMainAgentFeatureBoundaryRefusesInvalidConfigOrMissingStore(t *testing.T) {
	if _, err := newAgentFeatureBoundary(agentfeature.Config{}, &featureMainStore{}, http.NotFoundHandler()); !errors.Is(err, agentfeature.ErrInvalidConfig) {
		t.Fatal("invalid config got actual server handler")
	}
	if _, err := newAgentFeatureBoundary(agentfeature.DefaultConfig(), nil, http.NotFoundHandler()); !errors.Is(err, agentcognitive.ErrUnavailable) {
		t.Fatal("missing store got actual server adapter")
	}
}

func TestMainAgentFeatureBoundaryUsesSharedStartupController(t *testing.T) {
	body := `{"schemaVersion":"agent-feature-flags-v1","flags":{"agent_enrichment":true,"agent_memory":false,"agent_attention_policy":false,"agent_social_policy":false,"life_map":false},"pilot":{"memory":"basic","inference":"conservative","autonomousAction":false,"sensitiveInference":false}}`
	config, err := agentfeature.LoadConfig(func(key string) (string, bool) { return body, key == agentfeature.EnvironmentKey })
	if err != nil {
		t.Fatal(err)
	}
	controller, err := agentfeature.NewController(config)
	if err != nil {
		t.Fatal(err)
	}
	store := &featureMainStore{}
	allowed := true
	handler, err := newAgentFeatureBoundaryWithController(controller, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		domains, ok := agentcognitive.FeatureDomainsFromContext(r.Context())
		if !ok {
			t.Fatal("missing original domains")
		}
		_, e := domains.ReadOwnProfile(r.Context(), agentcognitive.SessionAccess{Digest: [32]byte{1}, Workspace: actorref.PrincipalRef{Type: actorref.Person, ID: featureMainOwner}})
		if allowed && e != nil || !allowed && !errors.Is(e, agentcognitive.ErrUnavailable) {
			t.Fatal("startup brake was copied into another controller", e)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "/original-shared-boundary", nil))
		if w.Code != 204 {
			t.Fatal(w.Code)
		}
		if err = controller.Disable(agentfeature.Enrichment); err != nil {
			t.Fatal(err)
		}
		allowed = false
	}
}
