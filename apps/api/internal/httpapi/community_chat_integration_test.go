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
	"github.com/birdtie/birdtie/apps/api/internal/communitychat"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCommunityConversationMembershipIntegration(t *testing.T) {
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
	if err = pool.QueryRow(ctx, `SELECT to_regclass('public.community_conversations') IS NOT NULL`).Scan(&installed); err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Skip("050 not applied")
	}
	ids, tokens := make([]string, 5), make([]string, 5)
	communities := []string{}
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
		exec(`INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'合成社群对话测试','private')`, ids[i])
		token, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		tokens[i] = token
		exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',now()+interval '1 hour',now()+interval '30 minutes')`, ids[i], digest[:])
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM incident_reports WHERE reporter_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM community_conversations WHERE community_id=ANY($1::uuid[])`, communities)
		_, _ = pool.Exec(ctx, `UPDATE communities SET lifecycle_status='archived' WHERE id=ANY($1::uuid[])`, communities)
		_, _ = pool.Exec(ctx, `DELETE FROM community_memberships WHERE community_id=ANY($1::uuid[])`, communities)
		_, _ = pool.Exec(ctx, `DELETE FROM communities WHERE id=ANY($1::uuid[])`, communities)
		_, _ = pool.Exec(ctx, `DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE id=ANY($1::uuid[])`, ids)
	}()
	store := postgres.New(pool, false)
	s := &server{access: store, communityChat: store, socialCommunities: store, safety: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/communities", s.discoverSocialCommunities)
	mux.HandleFunc("GET /v1/communities/{communityID}/conversation", s.getCommunityChat)
	mux.HandleFunc("POST /v1/communities/{communityID}/conversation", s.joinCommunityChat)
	mux.HandleFunc("DELETE /v1/communities/{communityID}/conversation", s.leaveCommunityChat)
	mux.HandleFunc("GET /v1/communities/{communityID}/conversation/messages", s.listCommunityChatMessages)
	mux.HandleFunc("POST /v1/communities/{communityID}/conversation/messages", s.sendCommunityChatMessage)
	mux.HandleFunc("DELETE /v1/communities/{communityID}/conversation/messages/{messageID}", s.removeCommunityChatMessage)
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
			t.Fatalf("%s %s actor=%d got=%d want=%d %s", method, path, who, w.Code, want, w.Body.String())
		}
		return w.Body.Bytes()
	}
	for _, visibility := range []string{"public", "private", "hidden"} {
		c, e := store.CreateSocialCommunity(ctx, ids[0], community.SocialInput{Name: "Synthetic conversation " + visibility, Visibility: visibility, JoinPolicy: "request"})
		if e != nil {
			t.Fatal(e)
		}
		communities = append(communities, c.ID)
		exec(`INSERT INTO community_memberships(community_id,user_account_id,role,status) VALUES($1,$2,'member','active'),($1,$3,'admin','active'),($1,$4,'member','pending'),($1,$5,'member','invited')`, c.ID, ids[1], ids[2], ids[3], ids[4])
		path := "/v1/communities/" + c.ID + "/conversation"
		call("GET", path, -1, nil, 401)
		call("GET", path, 3, nil, 404)
		call("POST", path, 4, map[string]bool{"confirmed": true}, 404)
		if visibility == "public" {
			discover := call("GET", "/v1/communities", 3, nil, 200)
			if !bytes.Contains(discover, []byte(c.ID)) {
				t.Fatal("non-member cannot discover public community independently")
			}
		}
		var state struct {
			Data communitychat.State `json:"data"`
		}
		if e = json.Unmarshal(call("GET", path, 1, nil, 200), &state); e != nil {
			t.Fatal(e)
		}
		if state.Data.Joined || state.Data.ID != "" {
			t.Fatal("Community membership auto-enrolled chat")
		}
		call("GET", path+"/messages", 1, nil, 404)
		call("POST", path, 1, map[string]bool{"confirmed": false}, 400)
		for _, actor := range []int{0, 1, 2} {
			call("POST", path, actor, map[string]bool{"confirmed": true}, 200)
		}
		body := map[string]string{"body": "合成社群成员协调", "clientMessageId": "b1750000-0000-4000-8000-000000000001"}
		raw := call("POST", path+"/messages", 1, body, 201)
		if !bytes.Equal(raw, call("POST", path+"/messages", 1, body, 201)) {
			t.Fatal("retry duplicated message")
		}
		var message struct {
			Data communitychat.Message `json:"data"`
		}
		if e = json.Unmarshal(raw, &message); e != nil {
			t.Fatal(e)
		}
		report := map[string]string{"targetType": "community_message", "targetId": message.Data.ID, "reason": "technical", "details": "合成社群消息举报，验证当前成员权限。"}
		call("POST", "/v1/reports", 3, report, 404)
		call("POST", "/v1/reports", 1, report, 404)
		call("POST", "/v1/reports", 2, report, 201)
		exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, ids[1])
		if bytes.Contains(call("GET", path+"/messages", 2, nil, 200), []byte(message.Data.ID)) {
			t.Fatal("inactive sender message leaked")
		}
		call("POST", "/v1/reports", 2, report, 404)
		exec(`UPDATE accounts SET status='active' WHERE id=$1`, ids[1])
		exec(`UPDATE community_memberships SET role='member' WHERE community_id=$1 AND user_account_id=$2`, c.ID, ids[2])
		call("DELETE", path+"/messages/"+message.Data.ID, 2, nil, 403)
		exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, ids[2], ids[1])
		if bytes.Contains(call("GET", path+"/messages", 2, nil, 200), []byte(message.Data.ID)) {
			t.Fatal("blocked message leaked")
		}
		call("POST", "/v1/reports", 2, report, 404)
		exec(`DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, ids[2], ids[1])
		fresh := postgres.New(pool, false)
		page, e := fresh.CommunityChatMessages(ctx, ids[2], c.ID, "")
		if e != nil || len(page.Messages) != 1 {
			t.Fatalf("persistence=%+v err=%v", page, e)
		}
		call("DELETE", path+"/messages/"+message.Data.ID, 0, nil, 204)
		if bytes.Contains(call("GET", path+"/messages", 2, nil, 200), []byte("合成社群成员协调")) {
			t.Fatal("removed text leaked")
		}
		call("POST", path+"/messages", 1, body, 409)
		call("DELETE", path, 1, nil, 204)
		call("GET", path+"/messages", 1, nil, 404)
		var status string
		if e = pool.QueryRow(ctx, `SELECT status FROM community_memberships WHERE community_id=$1 AND user_account_id=$2`, c.ID, ids[1]).Scan(&status); e != nil || status != "active" {
			t.Fatal("chat leave changed community membership")
		}
		call("POST", path, 1, map[string]bool{"confirmed": true}, 200)
		if e = store.LeaveSocialCommunity(ctx, ids[1], c.ID); e != nil {
			t.Fatal(e)
		}
		call("GET", path+"/messages", 1, nil, 404)
		exec(`UPDATE community_memberships SET status='active' WHERE community_id=$1 AND user_account_id=$2`, c.ID, ids[1])
		call("GET", path+"/messages", 1, nil, 404)
		call("POST", path, 1, map[string]bool{"confirmed": true}, 200)
		exec(`DELETE FROM community_memberships WHERE community_id=$1 AND user_account_id=$2`, c.ID, ids[1])
		call("GET", path, 1, nil, 404)
		exec(`INSERT INTO community_memberships(community_id,user_account_id,role,status) VALUES($1,$2,'member','active')`, c.ID, ids[1])
		call("GET", path+"/messages", 1, nil, 404)
		if e = store.ArchiveSocialCommunity(ctx, ids[0], c.ID); e != nil {
			t.Fatal(e)
		}
		call("GET", path+"/messages", 2, nil, 404)
		call("POST", path+"/messages", 2, body, 404)
	}
	t.Log("PASS public discovery independent; public/private/hidden membership consent, pending/invited denial, current moderation, reports/Block, leave/revoke/delete/rejoin/archive and persistence")
}
