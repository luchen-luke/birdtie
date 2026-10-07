package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type agentProfileFixture struct {
	t          *testing.T
	ctx        context.Context
	pool       *pgxpool.Pool
	store      *Store
	accounts   []string
	person     actorref.PrincipalRef
	other      actorref.PrincipalRef
	org        actorref.PrincipalRef
	business   actorref.PrincipalRef
	personID   string
	otherID    string
	orgAgentID string
	bizAgentID string
	orgID      string
}

func agentProfileTestFixture(t *testing.T) *agentProfileFixture {
	t.Helper()
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" || os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("Agent Profile integration requires an explicitly disposable PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal("cannot connect Agent Profile verification database")
	}
	t.Cleanup(pool.Close)
	var installed bool
	if err = pool.QueryRow(ctx, `SELECT to_regclass('public.agent_profiles') IS NOT NULL`).Scan(&installed); err != nil || !installed {
		t.Fatal("Agent Profile verification requires migration 053")
	}
	f := &agentProfileFixture{t: t, ctx: ctx, pool: pool, store: New(pool, false), accounts: []string{}}
	// Every account/Agent/Organization below is exclusively owned by this test.
	// Cleanup is ordered by FK and fails on errors; no shared seed is sampled.
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		for _, statement := range []string{
			`DELETE FROM agent_profiles WHERE owner_id=ANY($1::uuid[])`,
			`DELETE FROM agents WHERE principal_account_id=ANY($1::uuid[])`,
			`DELETE FROM organization_memberships WHERE organization_id IN
				(SELECT id FROM organizations WHERE account_id=ANY($1::uuid[]))`,
			`DELETE FROM organizations WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM accounts WHERE id=ANY($1::uuid[])`,
		} {
			if _, cleanupErr := pool.Exec(cleanupCtx, statement, f.accounts); cleanupErr != nil {
				t.Errorf("owned Agent Profile cleanup failed: %v", cleanupErr)
			}
		}
		var remaining int
		if cleanupErr := pool.QueryRow(cleanupCtx, `SELECT
			(SELECT count(*) FROM accounts WHERE id=ANY($1::uuid[])) +
			(SELECT count(*) FROM agent_profiles WHERE owner_id=ANY($1::uuid[])) +
			(SELECT count(*) FROM agents WHERE principal_account_id=ANY($1::uuid[]))`, f.accounts).Scan(&remaining); cleanupErr != nil || remaining != 0 {
			t.Errorf("owned Agent Profile fixture residue: count=%d error=%v", remaining, cleanupErr)
		}
	})
	for _, item := range []struct {
		kind string
		ref  *actorref.PrincipalRef
	}{
		{"person", &f.person}, {"person", &f.other},
		{"organization", &f.org}, {"business", &f.business},
	} {
		var id string
		if err = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type)
			VALUES(gen_random_uuid(),$1) RETURNING id`, item.kind).Scan(&id); err != nil {
			t.Fatal(err)
		}
		f.accounts = append(f.accounts, id)
		*item.ref, err = actorref.ParsePrincipal(item.kind, id)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = pool.QueryRow(ctx, `INSERT INTO organizations(account_id,organization_type,name)
		VALUES($1,'club','Synthetic Agent Profile foundation only') RETURNING id`, f.org.ID).Scan(&f.orgID); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		kind, owner, status string
		id                  *string
	}{
		{"personal", f.person.ID, "active", &f.personID},
		{"personal", f.other.ID, "active", &f.otherID},
		{"organization", f.org.ID, "active", &f.orgAgentID},
		{"business", f.business.ID, "suspended", &f.bizAgentID},
	} {
		if err = pool.QueryRow(ctx, `INSERT INTO agents(agent_type,principal_account_id,status)
			VALUES($1,$2,$3) RETURNING id`, item.kind, item.owner, item.status).Scan(item.id); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *agentProfileFixture) exec(statement string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(f.ctx, statement, args...); err != nil {
		f.t.Fatal(err)
	}
}

func sameAgentProfileMetadata(left, right agentprofile.Record) bool {
	return left.AgentID == right.AgentID && left.OwnerType == right.OwnerType && left.OwnerID == right.OwnerID &&
		left.ProfileVersion == right.ProfileVersion && left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func requireAgentProfileError(t *testing.T, actual agentprofile.Record, err, expected error) {
	t.Helper()
	if !errors.Is(err, expected) {
		t.Fatalf("profile error=%v, expected %v", err, expected)
	}
	if actual != (agentprofile.Record{}) {
		t.Fatal("rejected metadata operation released a record")
	}
}

func TestAgentProfileStoreNativeBindingsIntegration(t *testing.T) {
	f := agentProfileTestFixture(t)
	for _, item := range []struct {
		name, agentID string
		owner         actorref.PrincipalRef
	}{
		{"Person", f.personID, f.person}, {"Organization", f.orgAgentID, f.org},
	} {
		t.Run(item.name, func(t *testing.T) {
			created, err := f.store.EnsureAgentProfile(f.ctx, item.agentID, item.owner)
			if err != nil || agentprofile.Validate(created) != nil {
				t.Fatalf("native metadata ensure failed: %v", err)
			}
			if created.AgentID != item.agentID || created.OwnerType != item.owner.Type || created.OwnerID != item.owner.ID || created.ProfileVersion != 1 {
				t.Fatalf("metadata changed the native identity binding: %+v", created)
			}
			loaded, err := f.store.GetAgentProfile(f.ctx, strings.ToUpper(item.agentID), actorref.PrincipalRef{Type: item.owner.Type, ID: strings.ToUpper(item.owner.ID)})
			if err != nil || !sameAgentProfileMetadata(created, loaded) {
				t.Fatalf("read did not return the authoritative metadata: %v", err)
			}
			encoded, err := json.Marshal(loaded)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err = json.Unmarshal(encoded, &fields); err != nil || len(fields) != 6 {
				t.Fatal("foundation response is not exactly the six metadata fields")
			}
			for _, key := range []string{"agentId", "ownerType", "ownerId", "profileVersion", "createdAt", "updatedAt"} {
				if _, ok := fields[key]; !ok {
					t.Fatalf("missing metadata field %s", key)
				}
			}
		})
	}
	for _, item := range []struct {
		name, agentID string
		owner         actorref.PrincipalRef
		expected      error
	}{
		{"another_person", f.personID, f.other, agentprofile.ErrForbidden},
		{"other_agent_of_same_type", f.otherID, f.person, agentprofile.ErrForbidden},
		{"person_as_organization", f.personID, actorref.PrincipalRef{Type: actorref.Organization, ID: f.person.ID}, agentprofile.ErrForbidden},
		{"organization_as_person", f.orgAgentID, actorref.PrincipalRef{Type: actorref.Person, ID: f.org.ID}, agentprofile.ErrForbidden},
		{"organization_actor_id_is_not_principal", f.orgAgentID, actorref.PrincipalRef{Type: actorref.Organization, ID: f.orgID}, agentprofile.ErrForbidden},
		{"nonexistent_agent", "11111111-1111-4111-8111-111111111111", f.person, agentprofile.ErrForbidden},
		{"community_is_not_agent_owner", f.personID, actorref.PrincipalRef{Type: actorref.Community, ID: f.person.ID}, agentprofile.ErrInvalid},
		{"invalid_agent_id", "client-model-id", f.person, agentprofile.ErrInvalid},
		{"zero_agent_id", "00000000-0000-0000-0000-000000000000", f.person, agentprofile.ErrInvalid},
		{"invalid_owner_id", f.personID, actorref.PrincipalRef{Type: actorref.Person, ID: "client-owner"}, agentprofile.ErrInvalid},
	} {
		t.Run(item.name, func(t *testing.T) {
			actual, err := f.store.GetAgentProfile(f.ctx, item.agentID, item.owner)
			requireAgentProfileError(t, actual, err, item.expected)
			actual, err = f.store.EnsureAgentProfile(f.ctx, item.agentID, item.owner)
			requireAgentProfileError(t, actual, err, item.expected)
		})
	}
}

func TestAgentProfileCurrentIdentityGuardsIntegration(t *testing.T) {
	f := agentProfileTestFixture(t)
	for _, item := range []struct {
		name, agentID string
		owner         actorref.PrincipalRef
		statement     string
		id, disabled  string
	}{
		{"person_agent_suspended", f.personID, f.person, `UPDATE agents SET status=$2 WHERE id=$1`, f.personID, "suspended"},
		{"person_agent_retired", f.personID, f.person, `UPDATE agents SET status=$2 WHERE id=$1`, f.personID, "retired"},
		{"person_account_suspended", f.personID, f.person, `UPDATE accounts SET status=$2 WHERE id=$1`, f.person.ID, "suspended"},
		{"person_account_deleted", f.personID, f.person, `UPDATE accounts SET status=$2 WHERE id=$1`, f.person.ID, "deleted"},
		{"organization_agent_suspended", f.orgAgentID, f.org, `UPDATE agents SET status=$2 WHERE id=$1`, f.orgAgentID, "suspended"},
		{"organization_agent_retired", f.orgAgentID, f.org, `UPDATE agents SET status=$2 WHERE id=$1`, f.orgAgentID, "retired"},
		{"organization_account_suspended", f.orgAgentID, f.org, `UPDATE accounts SET status=$2 WHERE id=$1`, f.org.ID, "suspended"},
		{"organization_account_deleted", f.orgAgentID, f.org, `UPDATE accounts SET status=$2 WHERE id=$1`, f.org.ID, "deleted"},
		{"organization_suspended", f.orgAgentID, f.org, `UPDATE organizations SET status=$2 WHERE id=$1`, f.orgID, "suspended"},
		{"organization_closed", f.orgAgentID, f.org, `UPDATE organizations SET status=$2 WHERE id=$1`, f.orgID, "closed"},
	} {
		t.Run(item.name, func(t *testing.T) {
			before, err := f.store.EnsureAgentProfile(f.ctx, item.agentID, item.owner)
			if err != nil {
				t.Fatal(err)
			}
			f.exec(item.statement, item.id, item.disabled)
			defer f.exec(item.statement, item.id, "active")
			actual, err := f.store.GetAgentProfile(f.ctx, item.agentID, item.owner)
			requireAgentProfileError(t, actual, err, agentprofile.ErrForbidden)
			actual, err = f.store.EnsureAgentProfile(f.ctx, item.agentID, item.owner)
			requireAgentProfileError(t, actual, err, agentprofile.ErrForbidden)
			var retained int64
			if err = f.pool.QueryRow(f.ctx, `SELECT profile_version FROM agent_profiles WHERE agent_id=$1`, before.AgentID).Scan(&retained); err != nil || retained != before.ProfileVersion {
				t.Fatal("suspension changed or deleted retained foundation metadata")
			}
		})
	}
}

func TestAgentProfileIdempotenceRevisionAndReconnectIntegration(t *testing.T) {
	f := agentProfileTestFixture(t)
	first, err := f.store.EnsureAgentProfile(f.ctx, f.personID, f.person)
	if err != nil {
		t.Fatal(err)
	}
	// This SQL is a future writer's DB CAS contract only. AGE001 deliberately
	// exposes no profile content/update/CAS service or user/model write endpoint.
	tag, err := f.pool.Exec(f.ctx, `UPDATE agent_profiles SET profile_version=profile_version+1,
		updated_at=now() WHERE agent_id=$1 AND profile_version=$2`, f.personID, first.ProfileVersion)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("native DB revision CAS failed: %v", err)
	}
	second, err := f.store.GetAgentProfile(f.ctx, f.personID, f.person)
	if err != nil || second.ProfileVersion != first.ProfileVersion+1 || !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatal("DB revision CAS changed identity or creation time")
	}
	tag, err = f.pool.Exec(f.ctx, `UPDATE agent_profiles SET profile_version=profile_version+1,
		updated_at=now() WHERE agent_id=$1 AND profile_version=$2`, f.personID, first.ProfileVersion)
	if err != nil || tag.RowsAffected() != 0 {
		t.Fatal("stale DB CAS affected a current revision")
	}
	t.Run("two_concurrent_db_cas_writers", func(t *testing.T) {
		// Use two acquired native connections and one start barrier. Only the
		// current expected version can win; the loser must observe zero effects.
		writers := make([]*pgxpool.Conn, 0, 2)
		for index := 0; index < 2; index++ {
			connection, acquireErr := f.pool.Acquire(f.ctx)
			if acquireErr != nil {
				t.Fatal(acquireErr)
			}
			defer connection.Release()
			writers = append(writers, connection)
		}
		type outcome struct {
			rows int64
			err  error
		}
		start := make(chan struct{})
		outcomes := make(chan outcome, 2)
		var attempts sync.WaitGroup
		for _, connection := range writers {
			attempts.Add(1)
			go func(connection *pgxpool.Conn) {
				defer attempts.Done()
				<-start
				changed, casErr := connection.Exec(f.ctx, `UPDATE agent_profiles
					SET profile_version=profile_version+1,updated_at=now()
					WHERE agent_id=$1 AND profile_version=$2`, f.personID, second.ProfileVersion)
				outcomes <- outcome{rows: changed.RowsAffected(), err: casErr}
			}(connection)
		}
		close(start)
		attempts.Wait()
		close(outcomes)
		var success, stale int
		for result := range outcomes {
			if result.err != nil {
				t.Fatal(result.err)
			}
			switch result.rows {
			case 1:
				success++
			case 0:
				stale++
			default:
				t.Fatal("concurrent DB CAS affected an unexpected number of rows")
			}
		}
		if success != 1 || stale != 1 {
			t.Fatalf("concurrent DB CAS outcomes: success=%d stale=%d", success, stale)
		}
		current, readErr := f.store.GetAgentProfile(f.ctx, f.personID, f.person)
		if readErr != nil || current.ProfileVersion != second.ProfileVersion+1 || !current.CreatedAt.Equal(first.CreatedAt) {
			t.Fatal("concurrent DB CAS did not retain a single next revision and stable binding")
		}
		second = current
	})
	for repeat := 0; repeat < 3; repeat++ {
		current, ensureErr := f.store.EnsureAgentProfile(f.ctx, f.personID, f.person)
		if ensureErr != nil || !sameAgentProfileMetadata(current, second) {
			t.Fatal("idempotent Ensure reset an existing revision or timestamp")
		}
	}
	connected, err := pgxpool.New(f.ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer connected.Close()
	reloaded, err := New(connected, false).GetAgentProfile(f.ctx, f.personID, f.person)
	if err != nil || !sameAgentProfileMetadata(reloaded, second) {
		t.Fatal("a new database connection did not retain the same profile/Agent/version")
	}
}

func TestAgentProfileDatabaseBindingAndRevisionGuardsIntegration(t *testing.T) {
	f := agentProfileTestFixture(t)
	before, err := f.store.EnsureAgentProfile(f.ctx, f.personID, f.person)
	if err != nil {
		t.Fatal(err)
	}
	// This Person has no Agent, so a rejected parent rebinding cannot merely be
	// explained by the existing per-principal unique Agent constraint.
	var unusedPrincipal string
	if err = f.pool.QueryRow(f.ctx, `INSERT INTO accounts(id,account_type)
		VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&unusedPrincipal); err != nil {
		t.Fatal(err)
	}
	f.accounts = append(f.accounts, unusedPrincipal)
	for _, item := range []struct {
		name, statement string
		args            []any
	}{
		{"immutable_owner", `UPDATE agent_profiles SET owner_id=$2,profile_version=profile_version+1,updated_at=now() WHERE agent_id=$1`, []any{f.personID, f.other.ID}},
		{"immutable_owner_type", `UPDATE agent_profiles SET owner_type='ORGANIZATION',profile_version=profile_version+1,updated_at=now() WHERE agent_id=$1`, []any{f.personID}},
		{"immutable_agent", `UPDATE agent_profiles SET agent_id=$2,profile_version=profile_version+1,updated_at=now() WHERE agent_id=$1`, []any{f.personID, f.otherID}},
		{"immutable_created_at", `UPDATE agent_profiles SET created_at=created_at-interval '1 second',profile_version=profile_version+1,updated_at=now() WHERE agent_id=$1`, []any{f.personID}},
		{"no_same_revision", `UPDATE agent_profiles SET updated_at=now() WHERE agent_id=$1`, []any{f.personID}},
		{"no_skipped_revision", `UPDATE agent_profiles SET profile_version=profile_version+2,updated_at=now() WHERE agent_id=$1`, []any{f.personID}},
		{"no_negative_revision", `UPDATE agent_profiles SET profile_version=-1,updated_at=now() WHERE agent_id=$1`, []any{f.personID}},
		{"no_parent_agent_rebinding", `UPDATE agents SET principal_account_id=$2 WHERE id=$1`, []any{f.personID, unusedPrincipal}},
		{"no_parent_account_retyping", `UPDATE accounts SET account_type='organization' WHERE id=$1`, []any{f.person.ID}},
	} {
		t.Run(item.name, func(t *testing.T) {
			_, mutationErr := f.pool.Exec(f.ctx, item.statement, item.args...)
			var pgError *pgconn.PgError
			if !errors.As(mutationErr, &pgError) || (!strings.HasPrefix(pgError.Code, "23") && pgError.Code != "P0001") {
				t.Fatalf("unsafe binding/revision mutation was not rejected by a data guard: %v", mutationErr)
			}
			current, readErr := f.store.GetAgentProfile(f.ctx, f.personID, f.person)
			if readErr != nil || !sameAgentProfileMetadata(current, before) {
				t.Fatal("rejected metadata mutation changed retained native binding/revision")
			}
		})
	}
}

func TestAgentProfileDormantBusinessIsUnavailableIntegration(t *testing.T) {
	f := agentProfileTestFixture(t)
	f.exec(`INSERT INTO agent_profiles(agent_id,owner_type,owner_id)
		VALUES($1,'BUSINESS',$2) ON CONFLICT(agent_id) DO NOTHING`, f.bizAgentID, f.business.ID)
	var dormant agentprofile.Record
	if err := f.pool.QueryRow(f.ctx, `SELECT `+agentProfileColumns+` FROM agent_profiles WHERE agent_id=$1`, f.bizAgentID).Scan(
		&dormant.AgentID, &dormant.OwnerType, &dormant.OwnerID,
		&dormant.ProfileVersion, &dormant.CreatedAt, &dormant.UpdatedAt); err != nil || agentprofile.Validate(dormant) != nil {
		t.Fatal("shared metadata infrastructure cannot retain a dormant Business binding")
	}
	for _, status := range []string{"suspended", "retired"} {
		t.Run(status, func(t *testing.T) {
			f.exec(`UPDATE agents SET status=$2 WHERE id=$1`, f.bizAgentID, status)
			actual, err := f.store.GetAgentProfile(f.ctx, f.bizAgentID, f.business)
			requireAgentProfileError(t, actual, err, agentprofile.ErrUnavailable)
			actual, err = f.store.EnsureAgentProfile(f.ctx, f.bizAgentID, f.business)
			requireAgentProfileError(t, actual, err, agentprofile.ErrUnavailable)
		})
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE agents SET status='active' WHERE id=$1`, f.bizAgentID); err == nil {
		t.Fatal("metadata foundation enabled an active Business Agent")
	}
	active, err := f.store.HasActiveAgent(f.ctx, "business", f.business.ID)
	if err != nil || active || agentruntime.ForType(actorref.Business).Available {
		t.Fatal("dormant Business metadata granted an Agent/runtime capability")
	}
}

func TestAgentProfileGetDoesNotBootstrapIdentityOrCopyUserProfileIntegration(t *testing.T) {
	f := agentProfileTestFixture(t)
	f.exec(`INSERT INTO user_profiles(account_id,display_name,bio,visibility)
		VALUES($1,'Synthetic native UserProfile','Private UserProfile content must stay in its existing domain','private')`, f.person.ID)
	f.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, f.personID)
	actual, err := f.store.GetAgentProfile(f.ctx, f.personID, f.person)
	requireAgentProfileError(t, actual, err, agentprofile.ErrNotFound)
	var profileCount, agentCount int
	if err = f.pool.QueryRow(f.ctx, `SELECT
		(SELECT count(*) FROM agent_profiles WHERE agent_id=$1),
		(SELECT count(*) FROM agents WHERE principal_account_id=$2)`, f.personID, f.person.ID).Scan(&profileCount, &agentCount); err != nil || profileCount != 0 || agentCount != 1 {
		t.Fatal("Get created metadata or duplicated the stable Agent identity")
	}
	created, err := f.store.EnsureAgentProfile(f.ctx, f.personID, f.person)
	if err != nil || created.AgentID != f.personID {
		t.Fatal("explicit internal Ensure did not use the already-existing stable Agent")
	}
	encoded, err := json.Marshal(created)
	if err != nil || strings.Contains(string(encoded), "Private UserProfile") || strings.Contains(string(encoded), "displayName") || strings.Contains(string(encoded), "bio") {
		t.Fatal("Agent metadata copied existing UserProfile contents")
	}
	var bio, visibility string
	if err = f.pool.QueryRow(f.ctx, `SELECT bio,visibility FROM user_profiles WHERE account_id=$1`, f.person.ID).Scan(&bio, &visibility); err != nil || visibility != "private" || bio != "Private UserProfile content must stay in its existing domain" {
		t.Fatal("Agent metadata Ensure changed the existing UserProfile")
	}
}

func TestAgentProfileConcurrentEnsureIntegration(t *testing.T) {
	f := agentProfileTestFixture(t)
	f.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, f.personID)
	type result struct {
		profile agentprofile.Record
		err     error
	}
	results := make(chan result, 8)
	var workers sync.WaitGroup
	for worker := 0; worker < cap(results); worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			profile, err := f.store.EnsureAgentProfile(f.ctx, f.personID, f.person)
			results <- result{profile, err}
		}()
	}
	workers.Wait()
	close(results)
	var first agentprofile.Record
	for item := range results {
		if item.err != nil {
			t.Fatalf("concurrent metadata Ensure failed: %v", item.err)
		}
		if first.AgentID == "" {
			first = item.profile
		}
		if !sameAgentProfileMetadata(first, item.profile) || item.profile.ProfileVersion != 1 {
			t.Fatal("concurrent Ensure rewrote revision/timestamps or split metadata identity")
		}
	}
	var count int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agent_profiles WHERE agent_id=$1`, f.personID).Scan(&count); err != nil || count != 1 {
		t.Fatal("concurrent Ensure did not retain exactly one authoritative metadata row")
	}
	if _, err := f.pool.Exec(f.ctx, `SELECT 1`); errors.Is(err, pgx.ErrNoRows) || err != nil {
		t.Fatal("metadata operations left the pool unusable")
	}
}
