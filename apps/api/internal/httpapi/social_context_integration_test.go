package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/birdtie/birdtie/apps/api/internal/socialcontext"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSharedSocialContextPrivacyIntegration(t *testing.T) {
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("requires disposable database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var installed bool
	if err = pool.QueryRow(ctx, `SELECT to_regclass('public.person_social_disclosure') IS NOT NULL`).Scan(&installed); err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Skip("048 not applied")
	}
	ids := make([]string, 4)
	tokens := make([]string, 4)
	for i := range ids {
		if err = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'Synthetic shared context Person','public')`, ids[i]); err != nil {
			t.Fatal(err)
		}
		token, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		tokens[i] = token
		if _, err = pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)
            VALUES($1,$2,'test',now()+interval '1 hour',now()+interval '30 minutes')`, ids[i], digest[:]); err != nil {
			t.Fatal(err)
		}
	}
	communities := []string{}
	activities := []string{}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM activity_participations WHERE activity_id=ANY($1::uuid[])`, activities)
		_, _ = pool.Exec(ctx, `DELETE FROM activity_invitations WHERE activity_id=ANY($1::uuid[])`, activities)
		_, _ = pool.Exec(ctx, `DELETE FROM activity_organizers WHERE activity_id=ANY($1::uuid[])`, activities)
		_, _ = pool.Exec(ctx, `DELETE FROM activities WHERE id=ANY($1::uuid[])`, activities)
		// Archive before removing owners to satisfy the deferred owner constraint.
		_, _ = pool.Exec(ctx, `UPDATE communities SET lifecycle_status='archived' WHERE id=ANY($1::uuid[])`, communities)
		_, _ = pool.Exec(ctx, `DELETE FROM community_memberships WHERE community_id=ANY($1::uuid[])`, communities)
		_, _ = pool.Exec(ctx, `DELETE FROM communities WHERE id=ANY($1::uuid[])`, communities)
		_, _ = pool.Exec(ctx, `DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE id=ANY($1::uuid[])`, ids)
	}()
	store := postgres.New(pool, false)
	for _, v := range []int{0, 1} {
		request, e := store.CreateFriendRequest(ctx, ids[v], ids[2], "共同好友合成测试")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = store.DecideRequest(ctx, ids[2], request.ID, "accept"); e != nil {
			t.Fatal(e)
		}
	}
	for _, visibility := range []string{"public", "private"} {
		c, e := store.CreateSocialCommunity(ctx, ids[3], community.SocialInput{Name: "Synthetic " + visibility, Visibility: visibility, JoinPolicy: "open", CityID: "aberdeen-gb"})
		if e != nil {
			t.Fatal(e)
		}
		communities = append(communities, c.ID)
		for _, member := range ids[:2] {
			if _, err = pool.Exec(ctx, `INSERT INTO community_memberships(community_id,user_account_id,role,status) VALUES($1,$2,'member','active')`, c.ID, member); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, visibility := range []string{"public", "invite_only"} {
		input := activitypublish.Input{CityID: "aberdeen-gb", Title: "Synthetic shared " + visibility, Summary: "合成活动", Description: "隐私矩阵测试",
			StartsAt: time.Now().Add(time.Hour), EndsAt: time.Now().Add(2 * time.Hour), TimeZone: "Europe/London", Visibility: visibility,
			Modality: "in_person", PhysicalPlaceStatus: "tbd", Organizer: activitypublish.Organizer{Type: "PERSON", ID: ids[3]}}
		a, e := store.CreateSocialDraft(ctx, ids[3], input)
		if e != nil {
			t.Fatal(e)
		}
		activities = append(activities, a.ID)
		if _, e = store.PublishSocialActivity(ctx, ids[3], a.ID); e != nil {
			t.Fatal(e)
		}
		for _, person := range ids[:2] {
			if visibility == "invite_only" {
				if e = store.InviteActivityPerson(ctx, ids[3], a.ID, person); e != nil {
					t.Fatal(e)
				}
			}
			if _, _, e = store.JoinActivity(ctx, person, a.ID); e != nil {
				t.Fatal(e)
			}
		}
	}
	s := &server{access: store, socialContext: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/me/social-disclosure", s.ownSocialDisclosure)
	mux.HandleFunc("PUT /v1/me/social-disclosure", s.setSocialDisclosure)
	mux.HandleFunc("GET /v1/accounts/{accountID}/shared-context", s.sharedSocialContext)
	call := func(method, path string, who int, body any) (int, []byte) {
		t.Helper()
		var payload []byte
		if body != nil {
			payload, _ = json.Marshal(body)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(payload))
		if body != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		if who >= 0 {
			r.Header.Set("Authorization", "Bearer "+tokens[who])
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code, w.Body.Bytes()
	}
	path := "/v1/accounts/" + ids[1] + "/shared-context"
	read := func(wantCount, wantCommunities, wantActivities int) {
		t.Helper()
		code, raw := call("GET", path, 0, nil)
		var response struct {
			Data socialcontext.Signals `json:"data"`
		}
		if code != 200 || json.Unmarshal(raw, &response) != nil {
			t.Fatalf("read: %d %s", code, raw)
		}
		got := response.Data
		if got.MutualCount != wantCount || len(got.Communities) != wantCommunities || len(got.Activities) != wantActivities {
			t.Fatalf("signals %+v, expected %d/%d/%d", got, wantCount, wantCommunities, wantActivities)
		}
		if bytes.Contains(raw, []byte(communities[1])) || bytes.Contains(raw, []byte(activities[1])) || bytes.Contains(raw, []byte(ids[2])) {
			t.Fatalf("private object or mutual identity leaked: %s", raw)
		}
	}
	if code, _ := call("GET", path, -1, nil); code != 401 {
		t.Fatalf("anonymous %d", code)
	}
	if code, raw := call("GET", "/v1/me/social-disclosure", 0, nil); code != 200 || bytes.Contains(raw, []byte("true")) {
		t.Fatalf("not default private: %d %s", code, raw)
	}
	if code, _ := call("PUT", "/v1/me/social-disclosure", 0, map[string]any{"accountId": ids[1], "mutualTies": true}); code != 400 {
		t.Fatalf("override owner accepted %d", code)
	}
	read(0, 0, 0)
	all := socialcontext.Disclosure{MutualTies: true, SharedCommunities: true, SharedActivities: true}
	for i := 0; i < 3; i++ {
		if code, raw := call("PUT", "/v1/me/social-disclosure", i, all); code != 200 {
			t.Fatalf("consent %d %s", code, raw)
		}
	}
	read(1, 1, 1)
	if code, _ := call("PUT", "/v1/me/social-disclosure", 2, socialcontext.Disclosure{}); code != 200 {
		t.Fatal("mutual opt out")
	}
	read(0, 1, 1)
	if code, _ := call("PUT", "/v1/me/social-disclosure", 1, socialcontext.Disclosure{}); code != 200 {
		t.Fatal("target opt out")
	}
	read(0, 0, 0)
	if code, _ := call("PUT", "/v1/me/social-disclosure", 1, all); code != 200 {
		t.Fatal("target reenable")
	}
	if _, err = pool.Exec(ctx, `UPDATE community_memberships SET status='pending' WHERE community_id=$1 AND user_account_id=$2`, communities[0], ids[1]); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE activity_participations SET status='cancelled',cancelled_at=now() WHERE activity_id=$1 AND participant_account_id=$2`, activities[0], ids[1]); err != nil {
		t.Fatal(err)
	}
	read(0, 0, 0)
	if _, err = pool.Exec(ctx, `UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, ids[1]); err != nil {
		t.Fatal(err)
	}
	if code, _ := call("GET", path, 0, nil); code != 404 {
		t.Fatalf("private target %d", code)
	}
	if _, err = pool.Exec(ctx, `UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, ids[1]); err != nil {
		t.Fatal(err)
	}
	if err = store.BlockAccount(ctx, ids[1], ids[0]); err != nil {
		t.Fatal(err)
	}
	if code, _ := call("GET", path, 0, nil); code != 404 {
		t.Fatalf("Block target %d", code)
	}
	var audits int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND resource_type='social_disclosure'`, ids[1]).Scan(&audits); err != nil || audits != 3 {
		t.Fatalf("audit %d %v", audits, err)
	}
}
