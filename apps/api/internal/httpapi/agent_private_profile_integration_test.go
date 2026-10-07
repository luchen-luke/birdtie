package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Real PostgreSQL + real registered HTTP routes with exclusively owned random
// identities. All names/fields/sessions are synthetic disposable test data;
// this is not production authentication, a real person or Closed Pilot proof.
type privateProfileHTTPDBFixture struct {
	t            *testing.T
	ctx          context.Context
	pool         *pgxpool.Pool
	store        *postgres.Store
	handler      http.Handler
	accountIDs   []string
	agentIDs     []string
	tokens       []string
	requestCount int
}

func privateProfileHTTPDBNew(t *testing.T) *privateProfileHTTPDBFixture {
	t.Helper()
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" || os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("private Profile HTTP integration requires an explicitly disposable PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal("cannot connect disposable private HTTP database")
	}
	t.Cleanup(pool.Close)
	var installed bool
	if err = pool.QueryRow(ctx, `SELECT to_regclass('public.agent_private_profiles') IS NOT NULL`).Scan(&installed); err != nil || !installed {
		t.Fatal("private Profile HTTP integration requires migration 054")
	}
	f := &privateProfileHTTPDBFixture{t: t, ctx: ctx, pool: pool, store: postgres.New(pool, false), accountIDs: []string{}, agentIDs: []string{}, tokens: []string{}}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		// Parent metadata removal cascades private contents safely, whereas a
		// direct private-row deletion requires a versioned human clear.
		for index, statement := range []string{
			`DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`,
			`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM agent_profiles WHERE owner_id=ANY($1::uuid[])`,
			`DELETE FROM agents WHERE principal_account_id=ANY($1::uuid[])`,
			`DELETE FROM organization_memberships WHERE organization_id IN (SELECT id FROM organizations WHERE account_id=ANY($1::uuid[]))`,
			`DELETE FROM organizations WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM accounts WHERE id=ANY($1::uuid[])`,
		} {
			if _, cleanupErr := pool.Exec(cleanupCtx, statement, f.accountIDs); cleanupErr != nil {
				t.Errorf("owned private HTTP fixture cleanup step %d failed", index)
			}
		}
		var residue int
		if cleanupErr := pool.QueryRow(cleanupCtx, `SELECT
			(SELECT count(*) FROM accounts WHERE id=ANY($1::uuid[]))+
			(SELECT count(*) FROM agents WHERE principal_account_id=ANY($1::uuid[]))+
			(SELECT count(*) FROM agent_profiles WHERE owner_id=ANY($1::uuid[]))+
			(SELECT count(*) FROM agent_private_profiles WHERE owner_id=ANY($1::uuid[]))+
			(SELECT count(*) FROM sessions WHERE account_id=ANY($1::uuid[]))+
			(SELECT count(*) FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[]))`, f.accountIDs).Scan(&residue); cleanupErr != nil || residue != 0 {
			t.Errorf("owned private HTTP fixture residue count=%d", residue)
		} else {
			t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY owned HTTP fixture residue=0")
		}
	})
	for _, kind := range []string{"person", "person", "organization", "business"} {
		var id string
		if err = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),$1) RETURNING id`, kind).Scan(&id); err != nil {
			t.Fatal("cannot insert owned synthetic private HTTP principal")
		}
		f.accountIDs = append(f.accountIDs, id)
		f.agentIDs = append(f.agentIDs, "")
		f.tokens = append(f.tokens, f.newSession(id, false, false))
		if kind == "organization" {
			if _, err = pool.Exec(ctx, `INSERT INTO organizations(account_id,organization_type,name) VALUES($1,'club','Synthetic private HTTP boundary organization')`, id); err != nil {
				t.Fatal("cannot insert owned synthetic organization")
			}
		}
		if kind != "business" {
			agentKind := "personal"
			if kind == "organization" {
				agentKind = "organization"
			}
			if err = pool.QueryRow(ctx, `INSERT INTO agents(agent_type,principal_account_id) VALUES($1,$2) RETURNING id`, agentKind, id).Scan(&f.agentIDs[len(f.agentIDs)-1]); err != nil {
				t.Fatal("cannot insert owned synthetic stable Agent")
			}
		}
	}
	for index := 0; index < 2; index++ {
		if _, err = pool.Exec(ctx, `INSERT INTO user_profiles(account_id,display_name,bio,visibility)
			VALUES($1,'合成 HTTP 公开资料','只有旧公开字段，非真实用户','public')`, f.accountIDs[index]); err != nil {
			t.Fatal("cannot insert owned ordinary public Profile")
		}
	}
	f.handler = privateProfileHTTPNew(f.store, f.store)
	return f
}

func (f *privateProfileHTTPDBFixture) newSession(accountID string, expired, revoked bool) string {
	f.t.Helper()
	token, digest, err := identity.NewToken()
	if err != nil {
		f.t.Fatal("cannot create synthetic HTTP session")
	}
	var statement string
	if expired {
		statement = `INSERT INTO sessions(account_id,token_sha256,authentication_method,created_at,expires_at,idle_expires_at)
			VALUES($1,$2,'test',now()-interval '4 hours',now()-interval '1 hour',now()-interval '2 hours')`
	} else if revoked {
		statement = `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at,revoked_at)
			VALUES($1,$2,'test',now()+interval '2 hours',now()+interval '1 hour',now())`
	} else {
		statement = `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)
			VALUES($1,$2,'test',now()+interval '2 hours',now()+interval '1 hour')`
	}
	if _, err = f.pool.Exec(f.ctx, statement, accountID, digest[:]); err != nil {
		f.t.Fatal("cannot insert owned HTTP session digest")
	}
	return token
}

func (f *privateProfileHTTPDBFixture) exec(statement string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(f.ctx, statement, args...); err != nil {
		f.t.Fatal("owned private HTTP fixture SQL failed")
	}
}

func (f *privateProfileHTTPDBFixture) request(t *testing.T, handler http.Handler, method, path, body, token string, want int, workspace *string) *httptest.ResponseRecorder {
	t.Helper()
	f.requestCount++
	r := privateProfileHTTPRequest(method, path, body, token, "application/json")
	requestID := fmt.Sprintf("private_real_http_%04d", f.requestCount)
	r.Header.Set("X-Request-ID", requestID)
	if workspace != nil {
		r.Header.Set("X-Birdtie-Organization-Workspace", *workspace)
	}
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, r)
	if rw.Code != want {
		t.Fatalf("real private HTTP status=%d want=%d request_id=%s", rw.Code, want, requestID)
	}
	if rw.Header().Get("Cache-Control") != "no-store" || rw.Header().Get("X-Request-ID") != requestID {
		t.Fatal("real private/public response lost no-store/request correlation")
	}
	if want != 200 && strings.Contains(rw.Body.String(), privateProfileHTTPMarker) {
		t.Fatal("denied real HTTP response leaked synthetic private fields")
	}
	t.Logf("LOCAL_DISPOSABLE_SYNTHETIC_ONLY method=%s status=%d request_id=%s", method, rw.Code, requestID)
	return rw
}

func privateProfileHTTPDBRecord(t *testing.T, rw *httptest.ResponseRecorder) agentprofile.PrivateRecord {
	t.Helper()
	var response struct {
		Data agentprofile.PrivateRecord `json:"data"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &response); err != nil || agentprofile.ValidatePrivateRecord(response.Data) != nil {
		t.Fatal("real owner response violated private resource schema")
	}
	t.Logf("LOCAL_DISPOSABLE_SYNTHETIC_ONLY profile_version=%d configured=%t", response.Data.Profile.ProfileVersion, response.Data.Configured)
	return response.Data
}

func privateProfileHTTPDBAssertPublic(t *testing.T, rw *httptest.ResponseRecorder, expected identity.Profile) {
	t.Helper()
	if strings.Contains(rw.Body.String(), privateProfileHTTPMarker) || strings.Contains(rw.Body.String(), "agentNotes") || strings.Contains(rw.Body.String(), "privateCityHistory") {
		t.Fatal("ordinary public/granted Profile leaked private Agent contents")
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &envelope); err != nil {
		t.Fatal("invalid public Profile envelope")
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Data, &keys); err != nil || len(keys) != 4 {
		t.Fatal("public Profile no longer consists of the original four fields")
	}
	for _, key := range []string{"accountId", "displayName", "bio", "visibility"} {
		if keys[key] == nil {
			t.Fatal("public Profile field contract changed")
		}
	}
	var actual identity.Profile
	if err := json.Unmarshal(envelope.Data, &actual); err != nil || actual != expected {
		t.Fatal("public Profile authority changed during private edit")
	}
}

type privateProfileHTTPDBEffects struct {
	PublicProfiles      string
	Grants              string
	AgentTasks          int
	RSVP                int
	Plans               int
	Ties                int
	Requests            int
	Contexts            int
	Intents             int
	RelationshipConsent int
	NewPeopleConsent    int
	SocialDisclosure    int
}

func (f *privateProfileHTTPDBFixture) effects(t *testing.T) privateProfileHTTPDBEffects {
	t.Helper()
	var out privateProfileHTTPDBEffects
	// Only this fixture's random IDs are counted, so concurrent package tests
	// cannot create a false before/after difference or be cleaned by this test.
	err := f.pool.QueryRow(f.ctx, `SELECT
		COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY account_id)::text FROM user_profiles p WHERE account_id=ANY($1::uuid[])),'[]'),
		COALESCE((SELECT jsonb_agg(to_jsonb(g) ORDER BY id)::text FROM consent_grants g WHERE owner_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])),'[]'),
		(SELECT count(*) FROM agent_tasks WHERE owner_account_id=ANY($1::uuid[])),
		(SELECT count(*) FROM activity_participations WHERE participant_account_id=ANY($1::uuid[])),
		(SELECT count(*) FROM activity_plans WHERE owner_account_id=ANY($1::uuid[])),
		(SELECT count(*) FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])),
		(SELECT count(*) FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])),
		(SELECT count(*) FROM person_contexts WHERE person_account_id=ANY($1::uuid[])),
		(SELECT count(*) FROM social_intents WHERE creator_account_id=ANY($1::uuid[])),
		(SELECT count(*) FROM person_agent_relationship_consent WHERE account_id=ANY($1::uuid[])),
		(SELECT count(*) FROM person_new_people_consent WHERE account_id=ANY($1::uuid[])),
		(SELECT count(*) FROM person_social_disclosure WHERE account_id=ANY($1::uuid[]))`, f.accountIDs).Scan(
		&out.PublicProfiles, &out.Grants, &out.AgentTasks, &out.RSVP, &out.Plans, &out.Ties, &out.Requests,
		&out.Contexts, &out.Intents, &out.RelationshipConsent, &out.NewPeopleConsent, &out.SocialDisclosure)
	if err != nil {
		t.Fatal("cannot inspect owned private-edit side effects")
	}
	return out
}

func (f *privateProfileHTTPDBFixture) assertEffectsUnchanged(t *testing.T, before privateProfileHTTPDBEffects) {
	t.Helper()
	if after := f.effects(t); after != before {
		t.Fatal("private edit changed public Profile, grants, analysis/relationship consent, AgentTask, RSVP, Plans, Tie, request or context")
	}
	t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY public_profile/grant/task/RSVP/Plans/Tie/request/context/consent unchanged")
}

func (f *privateProfileHTTPDBFixture) privateRows(t *testing.T, ownerIndex int) int {
	t.Helper()
	var rows int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agent_private_profiles WHERE owner_id=$1`, f.accountIDs[ownerIndex]).Scan(&rows); err != nil {
		t.Fatal("cannot inspect owned private row count")
	}
	return rows
}

func TestPrivateAgentProfileHTTPPostgresLifecycleIntegration(t *testing.T) {
	f := privateProfileHTTPDBNew(t)
	publicPath := "/v1/accounts/" + f.accountIDs[0] + "/profile"
	public, err := f.store.ReadProfile(f.ctx, "", f.accountIDs[0])
	if err != nil {
		t.Fatal("owned public fixture not readable")
	}
	if _, err = f.store.GrantProfileRead(f.ctx, f.accountIDs[0], f.accountIDs[1], time.Now().Add(time.Hour)); err != nil {
		t.Fatal("cannot create explicit owned public-Profile grant")
	}
	baseline := f.effects(t)
	var initial, saved agentprofile.PrivateRecord
	fields := agentprofile.PrivateFields{
		PersonalPreferences: []string{privateProfileHTTPMarker + "-personal"}, SocialPreferences: []string{privateProfileHTTPMarker + "-social"},
		Availability: privateProfileHTTPMarker + "-availability", PreferredActivityTypes: []string{privateProfileHTTPMarker + "-activity"},
		TravelPreferences: []string{privateProfileHTTPMarker + "-travel"}, InteractionPreferences: []string{privateProfileHTTPMarker + "-interaction"},
		PrivateCityHistory: privateProfileHTTPMarker + "-city-history", LanguagePreferences: []string{privateProfileHTTPMarker + "-language"},
		AgentNotes: privateProfileHTTPMarker + "-notes",
	}
	if !t.Run("self missing read does not write", func(t *testing.T) {
		initial = privateProfileHTTPDBRecord(t, f.request(t, f.handler, http.MethodGet, privateProfileHTTPPath, "", f.tokens[0], 200, nil))
		if initial.Configured || !agentprofile.PrivateFieldsEmpty(initial.Fields) || initial.Profile.ProfileVersion != 1 || initial.Profile.AgentID != f.agentIDs[0] || initial.Profile.OwnerID != f.accountIDs[0] || f.privateRows(t, 0) != 0 {
			t.Fatal("owner GET created data, invented a preference or changed native identity")
		}
		f.assertEffectsUnchanged(t, baseline)
	}) {
		t.Fatal("initial private self-read prerequisite failed")
	}
	if !t.Run("PUT saves all nine with atomic current version", func(t *testing.T) {
		body, encodeErr := json.Marshal(agentprofile.ReplacePrivateInput{ExpectedVersion: 1, Fields: fields})
		if encodeErr != nil {
			t.Fatal("cannot encode synthetic nine-field input")
		}
		saved = privateProfileHTTPDBRecord(t, f.request(t, f.handler, http.MethodPut, privateProfileHTTPPath, string(body), f.tokens[0], 200, nil))
		if !saved.Configured || saved.Profile.ProfileVersion != 2 || saved.Profile.AgentID != initial.Profile.AgentID || saved.Profile.OwnerID != initial.Profile.OwnerID || !reflect.DeepEqual(saved.Fields, fields) || f.privateRows(t, 0) != 1 {
			t.Fatal("nine-field save lost binding, values or atomic metadata version")
		}
		var written int64
		if queryErr := f.pool.QueryRow(f.ctx, `SELECT written_profile_version FROM agent_private_profiles WHERE agent_id=$1`, f.agentIDs[0]).Scan(&written); queryErr != nil || written != 2 {
			t.Fatal("private fields persisted with a different source revision")
		}
		f.assertEffectsUnchanged(t, baseline)
	}) {
		t.Fatal("private save prerequisite failed")
	}
	t.Run("other owner cannot see target even with Profile grant", func(t *testing.T) {
		other := privateProfileHTTPDBRecord(t, f.request(t, f.handler, http.MethodGet, privateProfileHTTPPath, "", f.tokens[1], 200, nil))
		if other.Configured || !agentprofile.PrivateFieldsEmpty(other.Fields) || other.Profile.ProfileVersion != 1 || other.Profile.OwnerID != f.accountIDs[1] || strings.Contains(string(mustPrivateHTTPDBMarshal(t, other)), privateProfileHTTPMarker) || f.privateRows(t, 1) != 0 {
			t.Fatal("other self-read inherited a granted private Profile or wrote missing data")
		}
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			body := ""
			if method == http.MethodPut {
				body = `{"expectedVersion":2,"fields":{}}`
			}
			f.request(t, f.handler, method, privateProfileHTTPPath+"?ownerId="+f.accountIDs[0], body, f.tokens[1], 400, nil)
		}
		f.assertEffectsUnchanged(t, baseline)
	})
	t.Run("anonymous and granted ordinary public projection remains four fields", func(t *testing.T) {
		for _, token := range []string{"", f.tokens[1]} {
			privateProfileHTTPDBAssertPublic(t, f.request(t, f.handler, http.MethodGet, publicPath, "", token, 200, nil), public)
		}
		f.assertEffectsUnchanged(t, baseline)
	})
	t.Run("anonymous Org Business and workspace selectors denied", func(t *testing.T) {
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			body := ""
			if method == http.MethodPut {
				body = `{"expectedVersion":2,"fields":{}}`
			}
			for _, token := range []string{"", f.tokens[2], f.tokens[3]} {
				want := 403
				if token == "" {
					want = 401
				}
				f.request(t, f.handler, method, privateProfileHTTPPath, body, token, want, nil)
			}
			for _, header := range []string{f.accountIDs[2], ""} {
				f.request(t, f.handler, method, privateProfileHTTPPath, body, f.tokens[0], 403, &header)
			}
		}
		f.assertEffectsUnchanged(t, baseline)
	})
	t.Run("expired revoked inactive session and Agent cannot read or write", func(t *testing.T) {
		expired := f.newSession(f.accountIDs[0], true, false)
		revoked := f.newSession(f.accountIDs[0], false, true)
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			body := ""
			if method == http.MethodPut {
				body = `{"expectedVersion":2,"fields":{}}`
			}
			for _, token := range []string{expired, revoked} {
				f.request(t, f.handler, method, privateProfileHTTPPath, body, token, 401, nil)
			}
		}
		f.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, f.agentIDs[0])
		func() {
			defer f.exec(`UPDATE agents SET status='active' WHERE id=$1`, f.agentIDs[0])
			for _, method := range []string{http.MethodGet, http.MethodPut} {
				body := ""
				if method == http.MethodPut {
					body = `{"expectedVersion":2,"fields":{}}`
				}
				f.request(t, f.handler, method, privateProfileHTTPPath, body, f.tokens[0], 403, nil)
			}
		}()
		f.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.accountIDs[0])
		func() {
			defer f.exec(`UPDATE accounts SET status='active' WHERE id=$1`, f.accountIDs[0])
			for _, method := range []string{http.MethodGet, http.MethodPut} {
				body := ""
				if method == http.MethodPut {
					body = `{"expectedVersion":2,"fields":{}}`
				}
				f.request(t, f.handler, method, privateProfileHTTPPath, body, f.tokens[0], 401, nil)
			}
		}()
		current := privateProfileHTTPDBRecord(t, f.request(t, f.handler, http.MethodGet, privateProfileHTTPPath, "", f.tokens[0], 200, nil))
		if current.Profile.ProfileVersion != 2 || !reflect.DeepEqual(current.Fields, saved.Fields) {
			t.Fatal("denied expired/revoked/inactive calls modified private fields")
		}
		f.assertEffectsUnchanged(t, baseline)
	})
	t.Run("stale CAS returns conflict without write", func(t *testing.T) {
		f.request(t, f.handler, http.MethodPut, privateProfileHTTPPath, `{"expectedVersion":1,"fields":{"agentNotes":"synthetic stale edit"}}`, f.tokens[0], 409, nil)
		current := privateProfileHTTPDBRecord(t, f.request(t, f.handler, http.MethodGet, privateProfileHTTPPath, "", f.tokens[0], 200, nil))
		if current.Profile.ProfileVersion != 2 || !reflect.DeepEqual(current.Fields, saved.Fields) {
			t.Fatal("stale CAS overwrote current private revision")
		}
		f.assertEffectsUnchanged(t, baseline)
	})
	t.Run("new database connection and registered handler restore saved fields", func(t *testing.T) {
		connected, connectErr := pgxpool.New(f.ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
		if connectErr != nil {
			t.Fatal("cannot open independent private HTTP connection")
		}
		defer connected.Close()
		store := postgres.New(connected, false)
		handler := privateProfileHTTPNew(store, store)
		current := privateProfileHTTPDBRecord(t, f.request(t, handler, http.MethodGet, privateProfileHTTPPath, "", f.tokens[0], 200, nil))
		if current.Profile.ProfileVersion != 2 || !reflect.DeepEqual(current.Fields, saved.Fields) || current.Profile.AgentID != saved.Profile.AgentID {
			t.Fatal("saved private content/current version was not durable across a new connection")
		}
		f.assertEffectsUnchanged(t, baseline)
	})
	if !t.Run("explicit Profile grant only authorizes the ordinary private UserProfile", func(t *testing.T) {
		var updateErr error
		public, updateErr = f.store.UpdateOwnProfile(f.ctx, f.accountIDs[0], identity.ProfileInput{DisplayName: public.DisplayName, Bio: public.Bio, Visibility: "private"})
		if updateErr != nil {
			t.Fatal("cannot explicitly configure ordinary private UserProfile")
		}
		f.request(t, f.handler, http.MethodGet, publicPath, "", "", 404, nil)
		privateProfileHTTPDBAssertPublic(t, f.request(t, f.handler, http.MethodGet, publicPath, "", f.tokens[1], 200, nil), public)
		other := privateProfileHTTPDBRecord(t, f.request(t, f.handler, http.MethodGet, privateProfileHTTPPath, "", f.tokens[1], 200, nil))
		if other.Configured || !agentprofile.PrivateFieldsEmpty(other.Fields) {
			t.Fatal("ordinary private UserProfile grant widened Agent private permission")
		}
	}) {
		t.Fatal("ordinary private Profile grant prerequisite failed")
	}
	t.Run("Block revokes public grant but neither widens nor erases private self authority", func(t *testing.T) {
		if blockErr := f.store.BlockAccount(f.ctx, f.accountIDs[0], f.accountIDs[1]); blockErr != nil {
			t.Fatal("cannot create owned Block through current native Store")
		}
		f.request(t, f.handler, http.MethodGet, publicPath, "", f.tokens[1], 404, nil)
		other := privateProfileHTTPDBRecord(t, f.request(t, f.handler, http.MethodGet, privateProfileHTTPPath, "", f.tokens[1], 200, nil))
		if other.Configured || !agentprofile.PrivateFieldsEmpty(other.Fields) {
			t.Fatal("Block exposed another owner's private resource")
		}
		owner := privateProfileHTTPDBRecord(t, f.request(t, f.handler, http.MethodGet, privateProfileHTTPPath, "", f.tokens[0], 200, nil))
		if !reflect.DeepEqual(owner.Fields, saved.Fields) || owner.Profile.ProfileVersion != 2 {
			t.Fatal("Block erased or changed owner's private content")
		}
		f.request(t, f.handler, http.MethodGet, privateProfileHTTPPath+"?ownerId="+f.accountIDs[0], "", f.tokens[1], 400, nil)
		if unblockErr := f.store.UnblockAccount(f.ctx, f.accountIDs[0], f.accountIDs[1]); unblockErr != nil {
			t.Fatal("cannot remove owned Block through native Store")
		}
		f.request(t, f.handler, http.MethodGet, publicPath, "", f.tokens[1], 404, nil)
	})
	beforeClear := f.effects(t)
	if !t.Run("explicit clear deletes only private row and advances authoritative revision", func(t *testing.T) {
		cleared := privateProfileHTTPDBRecord(t, f.request(t, f.handler, http.MethodPut, privateProfileHTTPPath, `{"expectedVersion":2,"fields":{}}`, f.tokens[0], 200, nil))
		if cleared.Configured || cleared.Profile.ProfileVersion != 3 || !agentprofile.PrivateFieldsEmpty(cleared.Fields) || f.privateRows(t, 0) != 0 {
			t.Fatal("clear failed to delete private contents with atomic aggregate version advance")
		}
		f.assertEffectsUnchanged(t, beforeClear)
	}) {
		t.Fatal("private clear prerequisite failed")
	}
	t.Run("cleared state remains durable and stale old version cannot restore fields", func(t *testing.T) {
		connected, connectErr := pgxpool.New(f.ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
		if connectErr != nil {
			t.Fatal("cannot open independent clear verification connection")
		}
		defer connected.Close()
		store := postgres.New(connected, false)
		handler := privateProfileHTTPNew(store, store)
		current := privateProfileHTTPDBRecord(t, f.request(t, handler, http.MethodGet, privateProfileHTTPPath, "", f.tokens[0], 200, nil))
		if current.Configured || current.Profile.ProfileVersion != 3 || !agentprofile.PrivateFieldsEmpty(current.Fields) {
			t.Fatal("clear was not durable across a new registered handler/database connection")
		}
		f.request(t, handler, http.MethodPut, privateProfileHTTPPath, `{"expectedVersion":2,"fields":{"agentNotes":"synthetic stale restore"}}`, f.tokens[0], 409, nil)
		if f.privateRows(t, 0) != 0 {
			t.Fatal("stale revision resurrected cleared private fields")
		}
		f.assertEffectsUnchanged(t, beforeClear)
	})
}

func mustPrivateHTTPDBMarshal(t *testing.T, record agentprofile.PrivateRecord) []byte {
	t.Helper()
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatal("cannot inspect synthetic typed private response")
	}
	return body
}
