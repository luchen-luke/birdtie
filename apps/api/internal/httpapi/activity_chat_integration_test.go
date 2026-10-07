package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitychat"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestActivityConversationLifecycleIntegration(t *testing.T) {
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
	if err = pool.QueryRow(ctx, `SELECT to_regclass('public.activity_conversations') IS NOT NULL`).Scan(&installed); err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Skip("049 not applied")
	}
	ids, tokens := make([]string, 5), make([]string, 5)
	activities, communities := []string{}, []string{}
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
		exec(`INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'合成活动对话测试','private')`, ids[i])
		token, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		tokens[i] = token
		exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',now()+interval '1 hour',now()+interval '30 minutes')`, ids[i], digest[:])
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM incident_reports WHERE reporter_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM activity_conversations WHERE activity_id=ANY($1::uuid[])`, activities)
		_, _ = pool.Exec(ctx, `DELETE FROM activity_participations WHERE activity_id=ANY($1::uuid[])`, activities)
		_, _ = pool.Exec(ctx, `DELETE FROM activity_invitations WHERE activity_id=ANY($1::uuid[])`, activities)
		_, _ = pool.Exec(ctx, `DELETE FROM activity_organizers WHERE activity_id=ANY($1::uuid[])`, activities)
		_, _ = pool.Exec(ctx, `DELETE FROM activities WHERE id=ANY($1::uuid[])`, activities)
		_, _ = pool.Exec(ctx, `UPDATE communities SET lifecycle_status='archived' WHERE id=ANY($1::uuid[])`, communities)
		_, _ = pool.Exec(ctx, `DELETE FROM community_memberships WHERE community_id=ANY($1::uuid[])`, communities)
		_, _ = pool.Exec(ctx, `DELETE FROM communities WHERE id=ANY($1::uuid[])`, communities)
		_, _ = pool.Exec(ctx, `DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE id=ANY($1::uuid[])`, ids)
	}()
	store := postgres.New(pool, false)
	create := func(visibility string, organizer activitypublish.Organizer) string {
		t.Helper()
		a, e := store.CreateSocialDraft(ctx, ids[0], activitypublish.Input{CityID: "aberdeen-gb", Title: "Synthetic activity conversation", Summary: "合成活动对话验证", Description: "开发测试，无真实试点活动", StartsAt: time.Now().Add(time.Hour), EndsAt: time.Now().Add(2 * time.Hour), TimeZone: "Europe/London", Visibility: visibility, Modality: "in_person", PhysicalPlaceStatus: "tbd", Organizer: organizer})
		if e != nil {
			t.Fatal(e)
		}
		activities = append(activities, a.ID)
		if _, e = store.PublishSocialActivity(ctx, ids[0], a.ID); e != nil {
			t.Fatal(e)
		}
		return a.ID
	}
	activity := create("public", activitypublish.Organizer{Type: "PERSON", ID: ids[0]})
	for _, actor := range []int{1, 2} {
		if _, _, e := store.JoinActivity(ctx, ids[actor], activity); e != nil {
			t.Fatal(e)
		}
	}
	s := &server{access: store, activityChat: store, safety: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/activities/{activityID}/conversation", s.getActivityChat)
	mux.HandleFunc("POST /v1/activities/{activityID}/conversation", s.joinActivityChat)
	mux.HandleFunc("DELETE /v1/activities/{activityID}/conversation", s.leaveActivityChat)
	mux.HandleFunc("GET /v1/activities/{activityID}/conversation/messages", s.listActivityChatMessages)
	mux.HandleFunc("POST /v1/activities/{activityID}/conversation/messages", s.sendActivityChatMessage)
	mux.HandleFunc("DELETE /v1/activities/{activityID}/conversation/messages/{messageID}", s.removeActivityChatMessage)
	mux.HandleFunc("POST /v1/reports", s.createIncidentReport)
	call := func(method, path string, who int, body any, want int) []byte {
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
		if w.Code != want {
			t.Fatalf("%s %s actor=%d got %d want %d: %s", method, path, who, w.Code, want, w.Body.String())
		}
		if strings.Contains(path, "/conversation") && w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("private response cache protection missing")
		}
		return w.Body.Bytes()
	}
	path := "/v1/activities/" + activity + "/conversation"
	call("GET", path, -1, nil, 401)
	call("GET", path, 3, nil, 404)
	call("POST", path, 1, map[string]bool{"confirmed": false}, 400)
	raw := call("GET", path, 1, nil, 200)
	var state struct {
		Data activitychat.State `json:"data"`
	}
	if e := json.Unmarshal(raw, &state); e != nil {
		t.Fatal(e)
	}
	if state.Data.Joined || state.Data.ID != "" {
		t.Fatal("RSVP auto-enrolled in chat")
	}
	call("GET", path+"/messages", 1, nil, 404)
	for _, actor := range []int{0, 1, 2} {
		call("POST", path, actor, map[string]bool{"confirmed": true}, 200)
	}
	call("POST", path, 1, map[string]bool{"confirmed": true}, 200)
	var joins int
	if e := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND action='join' AND resource_type='activity_conversation'`, ids[1]).Scan(&joins); e != nil || joins != 1 {
		t.Fatalf("idempotent join audit=%d err=%v", joins, e)
	}
	messageBody := map[string]string{"clientMessageId": "b1740000-0000-4000-8000-000000000001", "body": "周末几点集合？"}
	raw = call("POST", path+"/messages", 1, messageBody, 201)
	var message struct {
		Data activitychat.Message `json:"data"`
	}
	if e := json.Unmarshal(raw, &message); e != nil {
		t.Fatal(e)
	}
	messageID := message.Data.ID
	if string(call("POST", path+"/messages", 1, messageBody, 201)) != string(raw) {
		t.Fatal("retry created another message")
	}
	call("POST", path+"/messages", 1, map[string]string{"clientMessageId": messageBody["clientMessageId"], "body": "不同内容"}, 409)
	call("POST", path+"/messages", 1, map[string]string{"clientMessageId": "invalid", "body": "hello"}, 400)
	call("POST", path+"/messages", 1, map[string]string{"clientMessageId": "b1740000-0000-4000-8000-000000000002", "body": strings.Repeat("中", 2001)}, 400)
	report := map[string]string{"targetType": "activity_message", "targetId": messageID, "reason": "technical", "details": "合成测试举报，验证活动消息的访问授权。"}
	call("POST", "/v1/reports", 3, report, 404)
	call("POST", "/v1/reports", 1, report, 404)
	call("POST", "/v1/reports", 2, report, 201)
	call("DELETE", path+"/messages/"+messageID, 2, nil, 403)
	exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, ids[2], ids[1])
	hidden := call("GET", path+"/messages", 2, nil, 200)
	if bytes.Contains(hidden, []byte(messageID)) {
		t.Fatal("blocked message leaked")
	}
	call("POST", "/v1/reports", 2, report, 404)
	exec(`DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, ids[2], ids[1])
	fresh := postgres.New(pool, false)
	page, e := fresh.ActivityChatMessages(ctx, ids[2], activity, "")
	if e != nil || len(page.Messages) != 1 {
		t.Fatalf("persisted history=%+v err=%v", page, e)
	}
	call("DELETE", path+"/messages/"+messageID, 0, nil, 204)
	call("DELETE", path+"/messages/"+messageID, 0, nil, 204)
	raw = call("GET", path+"/messages", 2, nil, 200)
	if bytes.Contains(raw, []byte("周末几点集合")) || !bytes.Contains(raw, []byte(`"removed":true`)) {
		t.Fatalf("removed body leaked: %s", raw)
	}
	call("POST", "/v1/reports", 2, report, 404)
	call("POST", path+"/messages", 1, messageBody, 409)
	// Concurrent retry must also return one durable message, then own removal works.
	type sendResult struct {
		message activitychat.Message
		err     error
	}
	results := make(chan sendResult, 6)
	for i := 0; i < 6; i++ {
		go func() {
			m, err := store.SendActivityChatMessage(ctx, ids[1], activity, "b1740000-0000-4000-8000-000000000004", "并发重试只保存一次")
			results <- sendResult{m, err}
		}()
	}
	concurrentID := ""
	for i := 0; i < 6; i++ {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		if concurrentID == "" {
			concurrentID = r.message.ID
		}
		if r.message.ID != concurrentID {
			t.Fatal("concurrent retry duplicated messages")
		}
	}
	call("DELETE", path+"/messages/"+concurrentID, 1, nil, 204)
	exec(`INSERT INTO activity_conversation_messages(conversation_id,sender_account_id,client_message_id,body,created_at)
        SELECT c.id,$2,gen_random_uuid(),'Synthetic pagination history',now()-interval '1 day'
        FROM activity_conversations c CROSS JOIN generate_series(1,101) WHERE c.activity_id=$1`, activity, ids[2])
	var history struct {
		Data activitychat.Page `json:"data"`
	}
	if e = json.Unmarshal(call("GET", path+"/messages", 2, nil, 200), &history); e != nil {
		t.Fatal(e)
	}
	if len(history.Data.Messages) != 100 || !history.Data.HasMore {
		t.Fatal("latest page bound/pagination incorrect")
	}
	cursor := history.Data.Messages[0].ID
	if e = json.Unmarshal(call("GET", path+"/messages?before="+cursor, 2, nil, 200), &history); e != nil {
		t.Fatal(e)
	}
	if len(history.Data.Messages) != 3 || history.Data.HasMore {
		t.Fatalf("older page=%+v", history.Data)
	}
	call("GET", path+"/messages?before=invalid", 2, nil, 400)
	call("GET", path+"/messages?before=b1740000-0000-4000-8000-000000000099", 2, nil, 404)
	exec(`INSERT INTO activity_conversation_messages(conversation_id,sender_account_id,client_message_id,body)
        SELECT c.id,$2,gen_random_uuid(),'Synthetic rate limit' FROM activity_conversations c
        CROSS JOIN generate_series(1,60) WHERE c.activity_id=$1`, activity, ids[2])
	call("POST", path+"/messages", 2, map[string]string{"clientMessageId": "b1740000-0000-4000-8000-000000000005", "body": "触发限流"}, 429)
	if _, e = store.CancelParticipation(ctx, ids[1], activity); e != nil {
		t.Fatal(e)
	}
	call("GET", path, 1, nil, 404)
	call("GET", path+"/messages", 1, nil, 404)
	if _, _, e = store.JoinActivity(ctx, ids[1], activity); e != nil {
		t.Fatal(e)
	}
	raw = call("GET", path, 1, nil, 200)
	if e = json.Unmarshal(raw, &state); e != nil || state.Data.Joined {
		t.Fatal("re-RSVP auto rejoined chat")
	}
	call("POST", path, 1, map[string]bool{"confirmed": true}, 200)
	call("DELETE", path, 1, nil, 204)
	call("DELETE", path, 1, nil, 204)
	call("GET", path+"/messages", 1, nil, 404)
	call("POST", path, 1, map[string]bool{"confirmed": true}, 200)
	if _, e = store.CancelSocialActivity(ctx, ids[0], activity); e != nil {
		t.Fatal(e)
	}
	call("GET", path+"/messages", 2, nil, 200)
	call("POST", path+"/messages", 2, map[string]string{"clientMessageId": "b1740000-0000-4000-8000-000000000003", "body": "不能继续发言"}, 409)
	call("POST", path, 1, map[string]bool{"confirmed": true}, 409)
	// Invited visibility and pending RSVP are checked on every access.
	private := create("invite_only", activitypublish.Organizer{Type: "PERSON", ID: ids[0]})
	privatePath := "/v1/activities/" + private + "/conversation"
	call("POST", privatePath, 3, map[string]bool{"confirmed": true}, 404)
	if e = store.InviteActivityPerson(ctx, ids[0], private, ids[3]); e != nil {
		t.Fatal(e)
	}
	if _, _, e = store.JoinActivity(ctx, ids[3], private); e != nil {
		t.Fatal(e)
	}
	call("POST", privatePath, 3, map[string]bool{"confirmed": true}, 200)
	exec(`UPDATE activity_participations SET status='pending' WHERE activity_id=$1 AND participant_account_id=$2`, private, ids[3])
	call("GET", privatePath, 3, nil, 404)
	exec(`UPDATE activity_participations SET status='going' WHERE activity_id=$1 AND participant_account_id=$2`, private, ids[3])
	call("GET", privatePath+"/messages", 3, nil, 404)
	// Current Community owner/admin is moderator; ordinary membership is insufficient.
	c, e := store.CreateSocialCommunity(ctx, ids[0], community.SocialInput{Name: "Synthetic chat roles", Visibility: "public", JoinPolicy: "open", CityID: "aberdeen-gb"})
	if e != nil {
		t.Fatal(e)
	}
	communities = append(communities, c.ID)
	exec(`INSERT INTO community_memberships(community_id,user_account_id,role,status) VALUES($1,$2,'admin','active'),($1,$3,'member','active')`, c.ID, ids[3], ids[4])
	ca := create("public", activitypublish.Organizer{Type: "COMMUNITY", ID: c.ID})
	cp := "/v1/activities/" + ca + "/conversation"
	call("POST", cp, 3, map[string]bool{"confirmed": true}, 200)
	call("POST", cp, 4, map[string]bool{"confirmed": true}, 404)
	exec(`UPDATE community_memberships SET role='member' WHERE community_id=$1 AND user_account_id=$2`, c.ID, ids[3])
	call("GET", cp, 3, nil, 404)
	if _, _, e = store.JoinActivity(ctx, ids[3], ca); e != nil {
		t.Fatal(e)
	}
	raw = call("GET", cp, 3, nil, 200)
	if e = json.Unmarshal(raw, &state); e != nil || state.Data.Moderator {
		t.Fatal("demoted admin retained moderation")
	}
	// Ended activities retain authorized history, but refuse new messages and joins.
	exec(`UPDATE activities SET starts_at=now()-interval '2 hours',ends_at=now()-interval '1 hour' WHERE id=$1`, ca)
	call("GET", cp+"/messages", 3, nil, 200)
	call("POST", cp, 0, map[string]bool{"confirmed": true}, 409)
	exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, ids[3], ids[0])
	call("GET", cp+"/messages", 3, nil, 404)
	t.Log("PASS explicit joining, persisted/idempotent messages, private/pending/Block, RSVP/leave/end/cancel, moderator current roles, removal/report privacy")
}
