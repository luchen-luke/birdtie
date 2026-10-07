package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConversationReadHTTPIntegration(t *testing.T) {
	dsn := os.Getenv("BIRDTIE_DATABASE_URL")
	if dsn == "" {
		t.Skip("set BIRDTIE_DATABASE_URL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var installed bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.conversation_member_states') IS NOT NULL`).Scan(&installed); err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Skip("035_conversation_read_state not applied")
	}
	ids := make([]string, 3)
	tokens := make([]string, 3)
	for i := range ids {
		if err := pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'Synthetic chat HTTP','public')`, ids[i]); err != nil {
			t.Fatal(err)
		}
		token, digest, err := identity.NewToken()
		if err != nil {
			t.Fatal(err)
		}
		tokens[i] = token
		if _, err := pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)
			VALUES($1,$2,'test',now()+interval '1 hour',now()+interval '30 minutes')`, ids[i], digest[:]); err != nil {
			t.Fatal(err)
		}
	}
	var requestID, conversationID string
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM conversation_member_states WHERE conversation_id=$1`, conversationID)
		_, _ = pool.Exec(ctx, `DELETE FROM conversation_messages WHERE conversation_id=$1`, conversationID)
		_, _ = pool.Exec(ctx, `DELETE FROM conversations WHERE id=$1`, conversationID)
		_, _ = pool.Exec(ctx, `DELETE FROM connection_requests WHERE id=$1`, requestID)
		_, _ = pool.Exec(ctx, `DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE id=ANY($1::uuid[])`, ids)
	}()
	if err := pool.QueryRow(ctx, `INSERT INTO connection_requests(sender_account_id,recipient_account_id,city_id,note,scope,state,expires_at)
		VALUES($1,$2,'aberdeen-gb','Synthetic HTTP read','conversation','accepted',now()+interval '1 day') RETURNING id`, ids[0], ids[1]).Scan(&requestID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO conversations(request_id,member_a_account_id,member_b_account_id) VALUES($1,$2,$3) RETURNING id`, requestID, ids[0], ids[1]).Scan(&conversationID); err != nil {
		t.Fatal(err)
	}
	store := postgres.New(pool, false)
	message, err := store.SendMessage(ctx, ids[0], conversationID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	s := &server{access: store, connections: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/me/conversations", s.listConversations)
	mux.HandleFunc("POST /v1/me/conversations/{conversationID}/read", s.markConversationRead)
	call := func(method, path string, who int, body string) (int, map[string]any) {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if who >= 0 {
			req.Header.Set("Authorization", "Bearer "+tokens[who])
		}
		rw := httptest.NewRecorder()
		mux.ServeHTTP(rw, req)
		var data map[string]any
		_ = json.Unmarshal(rw.Body.Bytes(), &data)
		return rw.Code, data
	}
	path := "/v1/me/conversations/" + conversationID + "/read"
	body := `{"throughMessageId":"` + message.ID + `"}`
	if code, _ := call("POST", path, -1, body); code != 401 {
		t.Fatalf("anonymous read: %d", code)
	}
	if code, _ := call("POST", path, 2, body); code != 404 {
		t.Fatalf("outsider read: %d", code)
	}
	if code, _ := call("POST", path, 1, `{"throughMessageId":"invalid"}`); code != 400 {
		t.Fatalf("invalid cursor: %d", code)
	}
	code, data := call("GET", "/v1/me/conversations", 1, "")
	if code != 200 || len(data["data"].([]any)) != 1 || data["data"].([]any)[0].(map[string]any)["unreadCount"] != float64(1) {
		t.Fatalf("unread list: %d %+v", code, data)
	}
	if code, _ := call("POST", path, 1, body); code != 200 {
		t.Fatalf("mark read: %d", code)
	}
	code, data = call("GET", "/v1/me/conversations", 1, "")
	if code != 200 || data["data"].([]any)[0].(map[string]any)["unreadCount"] != float64(0) {
		t.Fatalf("read list: %d %+v", code, data)
	}
	if err := store.BlockAccount(ctx, ids[1], ids[0]); err != nil {
		t.Fatal(err)
	}
	if code, _ := call("POST", path, 1, body); code != 404 {
		t.Fatalf("blocked mark read: %d", code)
	}
}
