package agentcognitive_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ agentcognitive.CurrentDomainStore = (*postgres.Store)(nil)

// A real local/disposable Postgres fixture exercises the existing native read
// adapter. Accounts, agents and ONLINE context nodes are exclusively owned by
// this test; no borrowed people/place/activity fixture or production assertion.
func TestCognitiveCurrentDomainPostgresIntegration(t *testing.T) {
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" || os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("native cognition-domain adapter requires disposable PostgreSQL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal("cannot connect disposable PostgreSQL")
	}
	t.Cleanup(pool.Close)
	var installed bool
	if err = pool.QueryRow(ctx, `SELECT to_regclass('public.agents') IS NOT NULL
		AND to_regclass('public.person_contexts') IS NOT NULL AND to_regclass('public.sessions') IS NOT NULL`).Scan(&installed); err != nil || !installed {
		t.Fatal("native adapter verification requires identity and context migrations")
	}
	accounts := []string{}
	contexts := []string{}
	t.Cleanup(func() {
		// Cleanup errors fail the test. Ownership is limited to inserted IDs;
		// shared seeds and concurrent package fixtures are never modified.
		for _, statement := range []string{
			`DELETE FROM person_contexts WHERE person_account_id=ANY($1::uuid[])`,
			`DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM agents WHERE principal_account_id=ANY($1::uuid[])`,
			`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM accounts WHERE id=ANY($1::uuid[])`,
		} {
			if _, cleanupErr := pool.Exec(ctx, statement, accounts); cleanupErr != nil {
				t.Errorf("owned cognitive account cleanup failed: %v", cleanupErr)
			}
		}
		if _, cleanupErr := pool.Exec(ctx, `DELETE FROM contexts WHERE id=ANY($1::uuid[])`, contexts); cleanupErr != nil {
			t.Errorf("owned cognitive context cleanup failed: %v", cleanupErr)
		}
		var remaining int
		if cleanupErr := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM accounts WHERE id=ANY($1::uuid[])) +
			(SELECT count(*) FROM contexts WHERE id=ANY($2::uuid[]))`, accounts, contexts).Scan(&remaining); cleanupErr != nil || remaining != 0 {
			t.Errorf("owned cognitive fixture residue: count=%d error=%v", remaining, cleanupErr)
		}
	})
	store := postgres.New(pool, false)
	adapter, err := agentcognitive.NewCurrentDomainAdapter(store)
	if err != nil {
		t.Fatal(err)
	}
	requests := []agentcognitive.SessionAccess{}
	for _, kind := range []string{"person", "person", "organization", "business"} {
		var id string
		if err = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),$1) RETURNING id`, kind).Scan(&id); err != nil {
			t.Fatal(err)
		}
		accounts = append(accounts, id)
		principal, parseErr := actorref.ParsePrincipal(kind, id)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		_, digest, tokenErr := identity.NewToken()
		if tokenErr != nil {
			t.Fatal("cannot create disposable session")
		}
		if _, err = pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)
			VALUES($1,$2,'test',now()+interval '1 hour',now()+interval '30 minutes')`, id, digest[:]); err != nil {
			t.Fatal("cannot insert disposable session")
		}
		requests = append(requests, agentcognitive.SessionAccess{Digest: digest, Workspace: principal})
		if kind == "person" {
			if _, err = pool.Exec(ctx, `INSERT INTO user_profiles(account_id,display_name,bio,visibility)
				VALUES($1,'合成认知边界本人','仅本人普通领域资料','private')`, id); err != nil {
				t.Fatal(err)
			}
			// The identity migration's trigger may already have created the Agent.
			if _, err = pool.Exec(ctx, `INSERT INTO agents(agent_type,principal_account_id)
				VALUES('personal',$1) ON CONFLICT DO NOTHING`, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	var stableAgentID string
	if err = pool.QueryRow(ctx, `SELECT id FROM agents WHERE principal_account_id=$1 AND agent_type='personal'`, accounts[0]).Scan(&stableAgentID); err != nil {
		t.Fatal(err)
	}
	declaration, err := store.DeclareContext(ctx, accounts[0], contextgraph.DeclarationInput{
		Type: contextgraph.Online, SourceKey: "cognitive-owned-online-" + accounts[0], Relation: "interest"})
	if err != nil {
		t.Fatal(err)
	}
	contexts = append(contexts, declaration.ContextID)

	t.Run("private native self profile and declaration", func(t *testing.T) {
		profile, readErr := adapter.ReadOwnProfile(ctx, requests[0])
		if readErr != nil || profile.AccountID != accounts[0] || profile.Visibility != "private" {
			t.Fatalf("native own profile read failed: %v", readErr)
		}
		items, readErr := adapter.ReadOwnContextDeclarations(ctx, requests[0])
		if readErr != nil || len(items) != 1 || items[0] != declaration {
			t.Fatalf("native own declarations not reused: %v", readErr)
		}
	})
	t.Run("other person and organization cannot select private owner", func(t *testing.T) {
		for _, index := range []int{1, 2, 3} {
			request := requests[index]
			request.Workspace = requests[0].Workspace
			profile, readErr := adapter.ReadOwnProfile(ctx, request)
			if !errors.Is(readErr, agentcognitive.ErrDenied) || profile.AccountID != "" {
				t.Fatal("different person/organization/business inherited own profile")
			}
			items, readErr := adapter.ReadOwnContextDeclarations(ctx, request)
			if !errors.Is(readErr, agentcognitive.ErrDenied) || items != nil {
				t.Fatal("different principal inherited private declarations")
			}
		}
	})
	t.Run("non-person workspaces have no self adapter", func(t *testing.T) {
		for _, index := range []int{2, 3} {
			if _, readErr := adapter.ReadOwnProfile(ctx, requests[index]); !errors.Is(readErr, agentcognitive.ErrDenied) {
				t.Fatal("non-person workspace became personal profile")
			}
		}
		community := requests[0]
		community.Workspace.Type = actorref.Community
		if _, readErr := adapter.ReadOwnProfile(ctx, community); !errors.Is(readErr, agentcognitive.ErrDenied) {
			t.Fatal("Community became Agent identity")
		}
	})
	t.Run("declaration removal never revives cached context", func(t *testing.T) {
		if removeErr := store.RemoveContextDeclaration(ctx, accounts[0], declaration.ContextID, declaration.Relation); removeErr != nil {
			t.Fatal(removeErr)
		}
		items, readErr := adapter.ReadOwnContextDeclarations(ctx, requests[0])
		if readErr != nil || len(items) != 0 {
			t.Fatalf("removed source was cached: %v", readErr)
		}
	})
	t.Run("agent suspension blocks adapter and stable identity survives", func(t *testing.T) {
		if _, execErr := pool.Exec(ctx, `UPDATE agents SET status='suspended' WHERE id=$1`, stableAgentID); execErr != nil {
			t.Fatal(execErr)
		}
		if profile, readErr := adapter.ReadOwnProfile(ctx, requests[0]); !errors.Is(readErr, agentcognitive.ErrDenied) || profile.AccountID != "" {
			t.Fatal("suspended Agent read private source")
		}
		if _, execErr := pool.Exec(ctx, `UPDATE agents SET status='active' WHERE id=$1`, stableAgentID); execErr != nil {
			t.Fatal(execErr)
		}
		var after string
		if queryErr := pool.QueryRow(ctx, `SELECT id FROM agents WHERE principal_account_id=$1 AND agent_type='personal'`, accounts[0]).Scan(&after); queryErr != nil || after != stableAgentID {
			t.Fatal("role state transition replaced native Agent")
		}
	})
	t.Run("session revocation blocks profile and context", func(t *testing.T) {
		if revokeErr := store.RevokeSession(ctx, requests[0].Digest); revokeErr != nil {
			t.Fatal(revokeErr)
		}
		profile, readErr := adapter.ReadOwnProfile(ctx, requests[0])
		if !errors.Is(readErr, agentcognitive.ErrDenied) || profile.AccountID != "" {
			t.Fatal("revoked session read private profile")
		}
		items, readErr := adapter.ReadOwnContextDeclarations(ctx, requests[0])
		if !errors.Is(readErr, agentcognitive.ErrDenied) || items != nil {
			t.Fatal("revoked session read private declarations")
		}
	})
}
