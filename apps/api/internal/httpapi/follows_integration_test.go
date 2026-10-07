package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/birdtie/birdtie/apps/api/internal/follow"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestFollowLifecycleAndPrivacyIntegration(t *testing.T) {
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("requires disposable database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	people := make([]string, 3) // follower, Person target, Community owner/reviewer
	tokens := make([]string, 3)
	for i := range people {
		if err = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&people[i]); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO user_profiles(account_id,display_name,visibility)
			VALUES($1,'Synthetic Follow Person','public')`, people[i]); err != nil {
			t.Fatal(err)
		}
		token, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		tokens[i] = token
		if _, e = pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)
			VALUES($1,$2,'test',now()+interval '1 hour',now()+interval '30 minutes')`, people[i], digest[:]); e != nil {
			t.Fatal(e)
		}
	}
	var orgPrincipal, bizPrincipal, orgID, commID, bizID string
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM follows WHERE follower_account_id=ANY($1::uuid[])`, people)
		_, _ = pool.Exec(ctx, `DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`, people)
		if commID != "" {
			_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE resource_type='community' AND resource_id=$1`, commID)
			_, _ = pool.Exec(ctx, `DELETE FROM community_memberships WHERE community_id=$1`, commID)
			_, _ = pool.Exec(ctx, `DELETE FROM communities WHERE id=$1`, commID)
		}
		if bizID != "" {
			_, _ = pool.Exec(ctx, `DELETE FROM businesses WHERE id=$1`, bizID)
		}
		if orgID != "" {
			_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, orgID)
		}
		_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, people)
		_, _ = pool.Exec(ctx, `DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`, people)
		_, _ = pool.Exec(ctx, `DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`, people)
		ids := append(append([]string{}, people...), orgPrincipal, bizPrincipal)
		_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE id=ANY($1::uuid[])`, ids)
	}()
	if err = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'organization') RETURNING id`).Scan(&orgPrincipal); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO organizations(account_id,organization_type,name)
		VALUES($1,'club','Synthetic Follow Organization') RETURNING id`, orgPrincipal).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	store := postgres.New(pool, false)
	comm, err := store.CreateSocialCommunity(ctx, people[2], community.SocialInput{
		Name: "Synthetic Follow Community", Visibility: "public", JoinPolicy: "open", CityID: "aberdeen-gb",
	})
	if err != nil {
		t.Fatal(err)
	}
	commID = comm.ID
	if err = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'business') RETURNING id`).Scan(&bizPrincipal); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO businesses(account_id,name) VALUES($1,'Synthetic Follow Business') RETURNING id`, bizPrincipal).Scan(&bizID); err != nil {
		t.Fatal(err)
	}
	s := &server{access: store, follows: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/me/follows", s.listOwnFollows)
	mux.HandleFunc("POST /v1/me/follows", s.followTarget)
	mux.HandleFunc("DELETE /v1/me/follows/{targetType}/{targetID}", s.unfollowTarget)
	mux.HandleFunc("GET /v1/accounts/{accountID}/profile", s.getProfile)
	call := func(method, path string, who int, body any) (int, []byte) {
		t.Helper()
		var payload []byte
		if body != nil {
			payload, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(payload))
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if who >= 0 {
			req.Header.Set("Authorization", "Bearer "+tokens[who])
		}
		rw := httptest.NewRecorder()
		mux.ServeHTTP(rw, req)
		return rw.Code, rw.Body.Bytes()
	}
	person := follow.Target{Type: follow.Person, ID: people[1]}
	org := follow.Target{Type: follow.Organization, ID: orgID}
	communityTarget := follow.Target{Type: follow.Community, ID: commID}
	business := follow.Target{Type: follow.Business, ID: bizID}
	if code, _ := call("POST", "/v1/me/follows", -1, person); code != 401 {
		t.Fatalf("anonymous Follow: %d", code)
	}
	if code, _ := call("POST", "/v1/me/follows", 0, follow.Target{Type: follow.Person, ID: people[0]}); code != 404 {
		t.Fatalf("self Follow: %d", code)
	}
	if code, _ := call("POST", "/v1/me/follows", 0, business); code != 404 {
		t.Fatalf("unverified Business Follow: %d", code)
	}
	for _, target := range []follow.Target{person, org, communityTarget} {
		if code, body := call("POST", "/v1/me/follows", 0, target); code != 201 {
			t.Fatalf("Follow %+v: %d %s", target, code, body)
		}
	}
	if _, err = pool.Exec(ctx, `UPDATE businesses SET claim_status='verified',
		claim_source_url='https://example.org/synthetic',claim_reviewed_by=$2,
		claim_reviewed_at=now() WHERE id=$1`, bizID, people[2]); err != nil {
		t.Fatal(err)
	}
	if code, body := call("POST", "/v1/me/follows", 0, business); code != 201 {
		t.Fatalf("verified Business Follow: %d %s", code, body)
	}
	if code, _ := call("POST", "/v1/me/follows", 0, person); code != 201 {
		t.Fatalf("duplicate Follow: %d", code)
	}
	code, body := call("GET", "/v1/me/follows", 0, nil)
	var listed struct {
		Data []follow.Record `json:"data"`
	}
	if code != 200 || json.Unmarshal(body, &listed) != nil || len(listed.Data) != 4 {
		t.Fatalf("four Follows: %d %s", code, body)
	}
	code, other := call("GET", "/v1/me/follows", 2, nil)
	var otherList struct {
		Data []follow.Record `json:"data"`
	}
	if code != 200 || json.Unmarshal(other, &otherList) != nil || len(otherList.Data) != 0 {
		t.Fatalf("other's Follow list leaked: %d %s", code, other)
	}
	var ties int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM person_ties WHERE
		(person_a_account_id=$1 AND person_b_account_id=$2) OR
		(person_a_account_id=$2 AND person_b_account_id=$1)`, people[0], people[1]).Scan(&ties); err != nil || ties != 0 {
		t.Fatalf("Follow became friendship: %d %v", ties, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, people[1]); err != nil {
		t.Fatal(err)
	}
	if code, _ := call("GET", "/v1/accounts/"+people[1]+"/profile", 0, nil); code == 200 {
		t.Fatal("Follow granted private profile access")
	}
	if code, raw := call("GET", "/v1/me/follows", 0, nil); code != 200 || bytes.Contains(raw, []byte(people[1])) {
		t.Fatalf("private target leaked in Follow list: %d %s", code, raw)
	}
	if _, err = pool.Exec(ctx, `UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, people[1]); err != nil {
		t.Fatal(err)
	}
	if err = store.BlockAccount(ctx, people[1], people[0]); err != nil {
		t.Fatal(err)
	}
	if code, raw := call("GET", "/v1/me/follows", 0, nil); code != 200 || bytes.Contains(raw, []byte(people[1])) {
		t.Fatalf("blocked Follow persisted: %d %s", code, raw)
	}
	if code, _ := call("POST", "/v1/me/follows", 0, person); code != 404 {
		t.Fatalf("blocked re-Follow: %d", code)
	}
	if code, _ := call("DELETE", "/v1/me/follows/BUSINESS/"+bizID, 0, nil); code != 204 {
		t.Fatalf("unfollow Business: %d", code)
	}
	if code, _ := call("DELETE", "/v1/me/follows/BUSINESS/"+bizID, 0, nil); code != 404 {
		t.Fatalf("repeated unfollow: %d", code)
	}
}
