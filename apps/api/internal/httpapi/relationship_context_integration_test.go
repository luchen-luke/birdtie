package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/birdtie/birdtie/apps/api/internal/relationshipcontext"
	"github.com/birdtie/birdtie/apps/api/internal/socialcontext"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestPersonalAgentRelationshipRetrievalIntegration(t *testing.T) {
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("requires disposable DB")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var installed bool
	if err = pool.QueryRow(ctx, `SELECT to_regclass('public.person_agent_relationship_consent') IS NOT NULL`).Scan(&installed); err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Skip("051 not applied")
	}
	ids, tokens := make([]string, 4), make([]string, 4)
	activities := []string{}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	for i := range ids {
		if err = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'合成关系信号测试','public')`, ids[i])
		token, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		tokens[i] = token
		exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',now()+interval '1 hour',now()+interval '30 minutes')`, ids[i], digest[:])
		exec(`INSERT INTO agents(agent_type,principal_account_id) VALUES('personal',$1)`, ids[i])
	}
	var orgID string
	if err = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'organization') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	token, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	ids = append(ids, orgID)
	tokens = append(tokens, token)
	exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',now()+interval '1 hour',now()+interval '30 minutes')`, orgID, digest[:])
	defer func() {
		for _, sql := range []string{
			`DELETE FROM activity_participations WHERE activity_id=ANY($1::uuid[])`,
			`DELETE FROM activity_invitations WHERE activity_id=ANY($1::uuid[])`,
			`DELETE FROM activity_organizers WHERE activity_id=ANY($1::uuid[])`,
			`DELETE FROM activities WHERE id=ANY($1::uuid[])`,
		} {
			_, _ = pool.Exec(ctx, sql, activities)
		}
		for _, sql := range []string{
			`DELETE FROM agent_tasks WHERE owner_account_id=ANY($1::uuid[])`,
			`DELETE FROM conversation_member_states WHERE member_account_id=ANY($1::uuid[])`,
			`DELETE FROM conversation_messages WHERE sender_account_id=ANY($1::uuid[])`,
			`DELETE FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[])`,
			`DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`,
			`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`,
			`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM accounts WHERE id=ANY($1::uuid[])`,
		} {
			_, _ = pool.Exec(ctx, sql, ids)
		}
	}()
	store := postgres.New(pool, false)
	req, e := store.CreateFriendRequest(ctx, ids[0], ids[1], "合成好友测试")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = store.DecideRequest(ctx, ids[1], req.ID, "accept"); e != nil {
		t.Fatal(e)
	}
	exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, ids[1])
	ties, e := store.ListTies(ctx, ids[0])
	if e != nil || len(ties) != 1 {
		t.Fatal("friend fixture", ties, e)
	}
	friendly, e := store.StartFriendConversation(ctx, ids[0], ties[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = store.SendMessage(ctx, ids[0], friendly.ID, "PRIVATE_BODY_FRIEND_ENTRY"); e != nil {
		t.Fatal(e)
	}
	var cv string
	if err = pool.QueryRow(ctx, `WITH req AS (INSERT INTO connection_requests(sender_account_id,recipient_account_id,city_id,note,scope,state,expires_at)
 VALUES($1,$2,'aberdeen-gb','Synthetic metadata','conversation','accepted',now()+interval '1 day') RETURNING id)
 INSERT INTO conversations(request_id,member_a_account_id,member_b_account_id) SELECT id,$1,$2 FROM req RETURNING id`, ids[0], ids[1]).Scan(&cv); err != nil {
		t.Fatal(err)
	}
	for _, days := range []int{0, 1, 4, 40} {
		exec(`INSERT INTO conversation_messages(conversation_id,sender_account_id,body,created_at) VALUES($1,$2,'PRIVATE_BODY_MUST_NEVER_LEAK',now()-($3::int*interval '1 day'))`, cv, ids[0], days)
	}
	exec(`INSERT INTO conversation_messages(conversation_id,sender_account_id,body) VALUES($1,$2,'PEER_PRIVATE_BODY')`, cv, ids[1])
	s := &server{access: store, agent: store, catalog: store, relationshipContext: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/me/agent-relationship-consent", s.ownRelationshipConsent)
	mux.HandleFunc("PUT /v1/me/agent-relationship-consent", s.setRelationshipConsent)
	mux.HandleFunc("GET /v1/me/agent-relationship-context", s.ownRelationshipContext)
	mux.HandleFunc("POST /v1/cities/{cityID}/agent/tasks", s.createAgentTask)
	mux.HandleFunc("GET /v1/me/agent-tasks/{taskID}", s.getAgentTask)
	call := func(method, path string, who int, body any, org bool, want int) []byte {
		t.Helper()
		payload, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(payload))
		r.Header.Set("Content-Type", "application/json")
		if who >= 0 {
			r.Header.Set("Authorization", "Bearer "+tokens[who])
		}
		if org {
			r.Header.Set("X-Birdtie-Organization-Workspace", "b1700000-0000-4000-8000-000000000001")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s %d got %d want%d %s", method, path, who, w.Code, want, w.Body.String())
		}
		if path == "/v1/me/agent-relationship-context" && who >= 0 && !org && w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("private response cached")
		}
		if bytes.Contains(w.Body.Bytes(), []byte("PRIVATE_BODY")) || bytes.Contains(w.Body.Bytes(), []byte("mapLatitude")) {
			t.Fatal("raw body/location leaked", w.Body.String())
		}
		return w.Body.Bytes()
	}
	path := "/v1/me/agent-relationship-context"
	consent := "/v1/me/agent-relationship-consent"
	call("GET", path, -1, nil, false, 401)
	call("GET", path, 4, nil, false, 403)
	call("GET", path, 0, nil, true, 403)
	call("GET", path+"?accountId="+ids[1], 0, nil, false, 400)
	call("PUT", consent, 0, map[string]any{"enabled": true, "accountId": ids[1]}, false, 400)
	call("PUT", consent, 0, map[string]any{}, false, 400)
	read := func(want int, enabled bool) relationshipcontext.Context {
		t.Helper()
		var out struct {
			Data relationshipcontext.Context `json:"data"`
		}
		raw := call("GET", path, 0, nil, false, 200)
		if json.Unmarshal(raw, &out) != nil || out.Data.Enabled != enabled || len(out.Data.Peers) != want {
			t.Fatalf("context %s want %d %v", raw, want, enabled)
		}
		return out.Data
	}
	read(0, false)
	call("PUT", consent, 0, map[string]bool{"enabled": true}, false, 200)
	got := read(1, true)
	if got.Peers[0].SentMessages != 4 || got.Peers[0].ActiveDays != 3 || got.Peers[0].Frequency != "RECENT_REPEATED" || len(got.Peers[0].SharedActivities) != 0 {
		t.Fatalf("metadata %+v", got)
	}
	// A non-Tie cannot acquire relationship signals through a prior conversation.
	var other struct {
		Data relationshipcontext.Context `json:"data"`
	}
	call("PUT", consent, 2, map[string]bool{"enabled": true}, false, 200)
	json.Unmarshal(call("GET", path, 2, nil, false, 200), &other)
	if len(other.Data.Peers) != 0 {
		t.Fatal("cross owner leak")
	}
	for _, visibility := range []string{"public", "invite_only"} {
		input := activitypublish.Input{CityID: "aberdeen-gb", Title: "合成共同报名 " + visibility, Summary: "合成信号", Description: "隐私测试", StartsAt: time.Now().Add(time.Hour), EndsAt: time.Now().Add(2 * time.Hour), TimeZone: "Europe/London", Visibility: visibility, Modality: "in_person", PhysicalPlaceStatus: "tbd", Organizer: activitypublish.Organizer{Type: "PERSON", ID: ids[3]}}
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
	for _, person := range ids[:2] {
		if _, e = store.SetSocialDisclosure(ctx, person, socialcontext.Disclosure{SharedActivities: true}); e != nil {
			t.Fatal(e)
		}
	}
	if len(read(1, true).Peers[0].SharedActivities) != 0 {
		t.Fatal("private peer attendance leaked")
	}
	exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, ids[1])
	got = read(1, true)
	if len(got.Peers[0].SharedActivities) != 1 || got.Peers[0].SharedActivities[0].ID != activities[0] {
		t.Fatal("activity source authorization", got)
	}
	exec(`UPDATE activity_participations SET status='cancelled',cancelled_at=now() WHERE activity_id=$1 AND participant_account_id=$2`, activities[0], ids[1])
	if len(read(1, true).Peers[0].SharedActivities) != 0 {
		t.Fatal("cancelled RSVP leaked")
	}
	exec(`UPDATE activity_participations SET status='going',cancelled_at=NULL WHERE activity_id=$1 AND participant_account_id=$2`, activities[0], ids[1])
	exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, ids[3], ids[0])
	if len(read(1, true).Peers[0].SharedActivities) != 0 {
		t.Fatal("blocked host activity leaked")
	}
	exec(`DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, ids[3], ids[0])
	exec(`UPDATE activities SET cancelled_at=now() WHERE id=$1`, activities[0])
	if len(read(1, true).Peers[0].SharedActivities) != 0 {
		t.Fatal("cancelled activity leaked")
	}
	exec(`UPDATE activities SET cancelled_at=NULL WHERE id=$1`, activities[0])
	if _, e = store.SetSocialDisclosure(ctx, ids[1], socialcontext.Disclosure{}); e != nil {
		t.Fatal(e)
	}
	if len(read(1, true).Peers[0].SharedActivities) != 0 {
		t.Fatal("peer disclosure revocation stale")
	}
	raw := call("POST", "/v1/cities/aberdeen-gb/agent/tasks", 0, map[string]string{"query": "我的关系信号"}, false, 200)
	var result struct {
		Data struct {
			TaskID              string                      `json:"taskId"`
			RelationshipContext relationshipcontext.Context `json:"relationshipContext"`
			MapEffects          struct {
				PinIDs []string `json:"pinEntityIds"`
			} `json:"mapEffects"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil || len(result.Data.RelationshipContext.Peers) != 1 || len(result.Data.MapEffects.PinIDs) != 0 {
		t.Fatal("Agent tool not wired", string(raw))
	}
	var history string
	if err = pool.QueryRow(ctx, `SELECT conversation::text||filters::text FROM agent_tasks WHERE id=$1`, result.Data.TaskID).Scan(&history); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains([]byte(history), []byte(ids[1])) || bytes.Contains([]byte(history), []byte("合成关系信号测试")) || bytes.Contains([]byte(history), []byte("sentMessages")) {
		t.Fatal("signals persisted in history", history)
	}
	call("PUT", consent, 0, map[string]bool{"enabled": false}, false, 200)
	read(0, false)
	var reloaded struct {
		Data struct {
			RelationshipContext relationshipcontext.Context `json:"relationshipContext"`
			Message             string                      `json:"message"`
		} `json:"data"`
	}
	raw = call("GET", "/v1/me/agent-tasks/"+result.Data.TaskID, 0, nil, false, 200)
	json.Unmarshal(raw, &reloaded)
	if reloaded.Data.RelationshipContext.Enabled || len(reloaded.Data.RelationshipContext.Peers) != 0 || !bytes.Contains([]byte(reloaded.Data.Message), []byte("尚未开启")) {
		t.Fatal("old task bypasses revoked consent", string(raw))
	}
	call("PUT", consent, 0, map[string]bool{"enabled": true}, false, 200)
	exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, ids[1], ids[0])
	read(0, true)
	exec(`DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, ids[1], ids[0])
	read(1, true)
	exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, ids[1])
	read(0, true)
	exec(`UPDATE accounts SET status='active' WHERE id=$1`, ids[1])
	exec(`UPDATE person_ties SET status='removed' WHERE request_id=$1`, req.ID)
	read(0, true)
	exec(`UPDATE agents SET status='suspended' WHERE principal_account_id=$1`, ids[0])
	call("GET", path, 0, nil, false, 403)
	call("PUT", consent, 0, map[string]bool{"enabled": false}, false, 200)
	fresh, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer fresh.Close()
	choice, e := postgres.New(fresh, false).OwnRelationshipConsent(ctx, ids[0])
	if e != nil || choice.Enabled {
		t.Fatal("consent persistence", choice, e)
	}
	var audits int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND resource_type='agent_relationship_consent' AND purpose='explicit_revoke'`, ids[0]).Scan(&audits); e != nil || audits < 2 {
		t.Fatal("missing revoke audit", audits, e)
	}
	exec(`UPDATE agents SET status='active' WHERE principal_account_id=$1`, ids[0])
	call("PUT", consent, 0, map[string]bool{"enabled": true}, false, 200)
	for i := 0; i < 51; i++ {
		var peer, request string
		if e = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&peer); e != nil {
			t.Fatal(e)
		}
		ids = append(ids, peer)
		if e = pool.QueryRow(ctx, `INSERT INTO connection_requests(sender_account_id,recipient_account_id,scope,state,note,expires_at) VALUES($1,$2,'friend','accepted','Synthetic limit test',now()+interval '1 day') RETURNING id`, ids[0], peer).Scan(&request); e != nil {
			t.Fatal(e)
		}
		exec(`INSERT INTO person_ties(person_a_account_id,person_b_account_id,request_id) VALUES(LEAST($1::uuid,$2::uuid),GREATEST($1::uuid,$2::uuid),$3)`, ids[0], peer, request)
	}
	if limited := read(50, true); !limited.Truncated {
		t.Fatal("limit missing truncation")
	}
}
