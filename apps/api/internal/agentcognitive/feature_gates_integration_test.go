package agentcognitive_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

type countedFeatureStore struct {
	store                      *postgres.Store
	auth, profiles, contexts   int
	afterProfile, afterContext func()
}

func (s *countedFeatureStore) Authenticate(ctx context.Context, digest [32]byte) (identity.Actor, error) {
	s.auth++
	return s.store.Authenticate(ctx, digest)
}
func (s *countedFeatureStore) HasActiveAgent(ctx context.Context, kind, id string) (bool, error) {
	return s.store.HasActiveAgent(ctx, kind, id)
}
func (s *countedFeatureStore) ReadProfile(ctx context.Context, actor, target string) (identity.Profile, error) {
	s.profiles++
	profile, err := s.store.ReadProfile(ctx, actor, target)
	if s.afterProfile != nil {
		s.afterProfile()
	}
	return profile, err
}
func (s *countedFeatureStore) ListOwnContextDeclarations(ctx context.Context, actor string) ([]contextgraph.Declaration, error) {
	s.contexts++
	items, err := s.store.ListOwnContextDeclarations(ctx, actor)
	if s.afterContext != nil {
		s.afterContext()
	}
	return items, err
}

func featureIntegrationConfig(t *testing.T, enrichment bool) agentfeature.Config {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"schemaVersion": agentfeature.SchemaVersion,
		"flags": map[string]bool{"agent_enrichment": enrichment, "agent_memory": false, "agent_attention_policy": false, "agent_social_policy": false, "life_map": false},
		"pilot": map[string]any{"memory": "basic", "inference": "conservative", "autonomousAction": false, "sensitiveInference": false}})
	config, err := agentfeature.ParseConfig(body)
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func TestFeatureGatedCurrentDomainsPostgresIntegration(t *testing.T) {
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" || os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("feature adapter requires explicitly disposable PostgreSQL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal("cannot connect disposable PostgreSQL")
	}
	t.Cleanup(pool.Close)
	var installed bool
	if err = pool.QueryRow(ctx, `SELECT to_regclass('public.agents') IS NOT NULL AND to_regclass('public.person_contexts') IS NOT NULL AND to_regclass('public.sessions') IS NOT NULL`).Scan(&installed); err != nil || !installed {
		t.Fatal("feature adapter requires current identity/context schema")
	}
	accounts, contexts := []string{}, []string{}
	t.Cleanup(func() {
		for _, sql := range []string{
			`DELETE FROM person_contexts WHERE person_account_id=ANY($1::uuid[])`,
			`DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM agents WHERE principal_account_id=ANY($1::uuid[])`,
			`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM accounts WHERE id=ANY($1::uuid[])`,
		} {
			if _, cleanupErr := pool.Exec(ctx, sql, accounts); cleanupErr != nil {
				t.Errorf("owned feature account cleanup failed: %v", cleanupErr)
			}
		}
		if _, cleanupErr := pool.Exec(ctx, `DELETE FROM contexts WHERE id=ANY($1::uuid[])`, contexts); cleanupErr != nil {
			t.Errorf("owned feature context cleanup failed: %v", cleanupErr)
		}
		var remaining int
		if cleanupErr := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM accounts WHERE id=ANY($1::uuid[]))+(SELECT count(*) FROM contexts WHERE id=ANY($2::uuid[]))`, accounts, contexts).Scan(&remaining); cleanupErr != nil || remaining != 0 {
			t.Errorf("owned feature fixture residue count=%d error=%v", remaining, cleanupErr)
		}
	})
	store := postgres.New(pool, false)
	requests := []agentcognitive.SessionAccess{}
	for index := 0; index < 2; index++ {
		var id string
		if err = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		accounts = append(accounts, id)
		if _, err = pool.Exec(ctx, `INSERT INTO user_profiles(account_id,display_name,bio,visibility) VALUES($1,'开关合成本人','flag-private-canary','private')`, id); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO agents(agent_type,principal_account_id) VALUES('personal',$1) ON CONFLICT DO NOTHING`, id); err != nil {
			t.Fatal(err)
		}
		_, digest, tokenErr := identity.NewToken()
		if tokenErr != nil {
			t.Fatal(tokenErr)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',now()+interval '1 hour',now()+interval '30 minutes')`, id, digest[:]); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, agentcognitive.SessionAccess{Digest: digest, Workspace: actorref.PrincipalRef{Type: actorref.Person, ID: id}})
	}
	declaration, err := store.DeclareContext(ctx, accounts[0], contextgraph.DeclarationInput{Type: contextgraph.Online, SourceKey: "feature-owned-" + accounts[0], Relation: "interest"})
	if err != nil {
		t.Fatal(err)
	}
	contexts = append(contexts, declaration.ContextID)
	newDomains := func(enabled bool) (*agentcognitive.FeatureGatedDomains, *agentfeature.Controller, *countedFeatureStore) {
		controller, newErr := agentfeature.NewController(featureIntegrationConfig(t, enabled))
		if newErr != nil {
			t.Fatal(newErr)
		}
		counted := &countedFeatureStore{store: store}
		domains, newErr := agentcognitive.NewFeatureGatedDomains(controller, counted)
		if newErr != nil {
			t.Fatal(newErr)
		}
		return domains, controller, counted
	}
	t.Run("off never reaches actual store", func(t *testing.T) {
		domains, _, counted := newDomains(false)
		profile, readErr := domains.ReadOwnProfile(ctx, requests[0])
		items, contextErr := domains.ReadOwnContextDeclarations(ctx, requests[0])
		if !errors.Is(readErr, agentcognitive.ErrUnavailable) || !reflect.ValueOf(profile).IsZero() || !errors.Is(contextErr, agentcognitive.ErrUnavailable) || items != nil || counted.auth+counted.profiles+counted.contexts != 0 {
			t.Fatal("OFF reached private database or released data")
		}
	})
	t.Run("on native self profile and declaration", func(t *testing.T) {
		domains, _, counted := newDomains(true)
		profile, readErr := domains.ReadOwnProfile(ctx, requests[0])
		items, contextErr := domains.ReadOwnContextDeclarations(ctx, requests[0])
		if readErr != nil || profile.AccountID != accounts[0] || profile.Bio != "flag-private-canary" || contextErr != nil || len(items) != 1 || items[0] != declaration || counted.auth != 4 || counted.profiles != 1 || counted.contexts != 1 {
			t.Fatal("gate did not reuse native current self reads")
		}
	})
	t.Run("another person cannot select source", func(t *testing.T) {
		domains, _, _ := newDomains(true)
		request := requests[1]
		request.Workspace = requests[0].Workspace
		profile, readErr := domains.ReadOwnProfile(ctx, request)
		if !errors.Is(readErr, agentcognitive.ErrDenied) || !reflect.ValueOf(profile).IsZero() {
			t.Fatal("ON granted another source")
		}
	})
	t.Run("profile brake after real source read", func(t *testing.T) {
		domains, controller, counted := newDomains(true)
		counted.afterProfile = func() {
			if controller.Disable(agentfeature.Enrichment) != nil {
				t.Fatal("brake failed")
			}
		}
		profile, readErr := domains.ReadOwnProfile(ctx, requests[0])
		if !errors.Is(readErr, agentcognitive.ErrUnavailable) || !reflect.ValueOf(profile).IsZero() || counted.profiles != 1 {
			t.Fatal("real loaded profile survived brake")
		}
	})
	t.Run("context revision after real source read", func(t *testing.T) {
		domains, controller, counted := newDomains(true)
		counted.afterContext = func() {
			if controller.Replace(controller.Revision(), featureIntegrationConfig(t, true)) != nil {
				t.Fatal("replace failed")
			}
		}
		items, readErr := domains.ReadOwnContextDeclarations(ctx, requests[0])
		if !errors.Is(readErr, agentcognitive.ErrUnavailable) || items != nil || counted.contexts != 1 {
			t.Fatal("real loaded context survived config revision")
		}
	})
	t.Run("memory always unavailable", func(t *testing.T) {
		domains, _, counted := newDomains(true)
		if view, readErr := domains.ReadMemory(ctx, agentcognitive.ReadRequest{}); !errors.Is(readErr, agentcognitive.ErrUnavailable) || !reflect.ValueOf(view).IsZero() {
			t.Fatal("ordinary store became cognition Memory")
		}
		if receipt, writeErr := domains.SubmitMemoryCandidate(ctx, agentcognitive.CandidateSubmission{}); !errors.Is(writeErr, agentcognitive.ErrUnavailable) || receipt.Status != agentcognitive.Unavailable || counted.auth+counted.profiles+counted.contexts != 0 {
			t.Fatal("ordinary store became candidate writer")
		}
	})
	t.Run("session revoked after actual private read", func(t *testing.T) {
		domains, _, counted := newDomains(true)
		counted.afterProfile = func() {
			if _, revokeErr := pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE account_id=$1`, accounts[1]); revokeErr != nil {
				t.Fatal(revokeErr)
			}
		}
		profile, readErr := domains.ReadOwnProfile(ctx, requests[1])
		if !errors.Is(readErr, agentcognitive.ErrDenied) || !reflect.ValueOf(profile).IsZero() || counted.profiles != 1 {
			t.Fatal("real loaded profile survived committed session revoke")
		}
	})
	t.Run("actual revoked session remains denied", func(t *testing.T) {
		if _, err = pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE account_id=$1`, accounts[1]); err != nil {
			t.Fatal(err)
		}
		domains, _, _ := newDomains(true)
		profile, readErr := domains.ReadOwnProfile(ctx, requests[1])
		if !errors.Is(readErr, agentcognitive.ErrDenied) || !reflect.ValueOf(profile).IsZero() {
			t.Fatal("ON bypassed revoked actual session")
		}
	})
}
