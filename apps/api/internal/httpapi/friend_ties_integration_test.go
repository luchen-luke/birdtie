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

func TestFriendTieHTTPIntegration(t *testing.T) {
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
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.person_ties') IS NOT NULL`).Scan(&installed); err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Skip("034_person_ties not applied")
	}
	ids := make([]string, 2)
	tokens := make([]string, 2)
	for i := range ids {
		if err := pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'Synthetic HTTP friend','public')`, ids[i]); err != nil {
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
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE id=ANY($1::uuid[])`, ids)
	}()
	store := postgres.New(pool, false)
	s := &server{access: store, connections: store}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/me/connection-requests", s.createConnectionRequest)
	mux.HandleFunc("POST /v1/me/connection-requests/{requestID}/decision", s.decideConnectionRequest)
	mux.HandleFunc("GET /v1/me/ties", s.listPersonTies)
	mux.HandleFunc("DELETE /v1/me/ties/{tieID}", s.removePersonTie)
	mux.HandleFunc("POST /v1/me/ties/{tieID}/conversation", s.startFriendConversation)
	mux.HandleFunc("POST /v1/me/blocks", s.blockAccount)
	mux.HandleFunc("DELETE /v1/me/blocks/{accountID}", s.unblockAccount)
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
	body := `{"recipientAccountId":"` + ids[1] + `","scope":"friend","note":"hello"}`
	if code, _ := call("POST", "/v1/me/connection-requests", -1, body); code != 401 {
		t.Fatalf("anonymous request: %d", code)
	}
	if code, _ := call("POST", "/v1/me/connection-requests", 0, `{"recipientAccountId":"`+ids[1]+`","scope":"friend","cityId":"aberdeen-gb","note":"hello"}`); code != 400 {
		t.Fatalf("city-bound friend: %d", code)
	}
	code, data := call("POST", "/v1/me/connection-requests", 0, body)
	if code != 201 {
		t.Fatalf("friend create: %d %+v", code, data)
	}
	requestID := data["data"].(map[string]any)["id"].(string)
	if code, _ := call("GET", "/v1/me/ties", 0, ""); code != 200 {
		t.Fatalf("list before accept: %d", code)
	}
	if code, _ := call("POST", "/v1/me/connection-requests/"+requestID+"/decision", 0, `{"action":"accept"}`); code != 404 {
		t.Fatalf("sender accepted own request: %d", code)
	}
	if code, _ := call("POST", "/v1/me/connection-requests/"+requestID+"/decision", 1, `{"action":"decline"}`); code != 200 {
		t.Fatalf("decline: %d", code)
	}
	code, data = call("POST", "/v1/me/connection-requests", 0, body)
	if code != 201 {
		t.Fatalf("retry after decline: %d %+v", code, data)
	}
	requestID = data["data"].(map[string]any)["id"].(string)
	if code, _ := call("POST", "/v1/me/connection-requests/"+requestID+"/decision", 1, `{"action":"accept"}`); code != 200 {
		t.Fatalf("accept: %d", code)
	}
	var tieID string
	for who := range ids {
		code, data = call("GET", "/v1/me/ties", who, "")
		if code != 200 || len(data["data"].([]any)) != 1 {
			t.Fatalf("bilateral HTTP Tie %d: %d %+v", who, code, data)
		}
		tieID = data["data"].([]any)[0].(map[string]any)["id"].(string)
	}
	if code, _ := call("POST", "/v1/me/connection-requests", 0, body); code != 404 && code != 409 {
		t.Fatalf("duplicate active Tie: %d", code)
	}
	if code, _ := call("GET", "/v1/me/ties", -1, ""); code != 401 {
		t.Fatalf("anonymous Tie list: %d", code)
	}
	if code, _ := call("DELETE", "/v1/me/ties/"+tieID, -1, ""); code != 401 {
		t.Fatalf("anonymous remove: %d", code)
	}
	if code, _ := call("DELETE", "/v1/me/ties/invalid", 0, ""); code != 400 {
		t.Fatalf("invalid Tie ID: %d", code)
	}
	if code, _ := call("DELETE", "/v1/me/ties/"+tieID, 0, ""); code != 204 {
		t.Fatalf("remove Tie: %d", code)
	}
	if code, data := call("GET", "/v1/me/ties", 1, ""); code != 200 || len(data["data"].([]any)) != 0 {
		t.Fatalf("remove did not hide Tie: %d %+v", code, data)
	}
	if code, _ := call("DELETE", "/v1/me/ties/"+tieID, 1, ""); code != 404 {
		t.Fatalf("duplicate remove: %d", code)
	}
	body = `{"recipientAccountId":"` + ids[0] + `","scope":"friend","note":"again"}`
	code, data = call("POST", "/v1/me/connection-requests", 1, body)
	if code != 201 {
		t.Fatalf("reapply: %d %+v", code, data)
	}
	requestID = data["data"].(map[string]any)["id"].(string)
	if code, _ := call("POST", "/v1/me/connection-requests/"+requestID+"/decision", 0, `{"action":"accept"}`); code != 200 {
		t.Fatalf("reaccept: %d", code)
	}
	var chatSchema bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.conversation_member_states') IS NOT NULL`).Scan(&chatSchema); err != nil {
		t.Fatal(err)
	}
	if chatSchema {
		code, data = call("POST", "/v1/me/ties/"+tieID+"/conversation", 1, "")
		if code != 200 {
			t.Fatalf("friend chat start: %d %+v", code, data)
		}
		chatID := data["data"].(map[string]any)["id"]
		code, data = call("POST", "/v1/me/ties/"+tieID+"/conversation", 0, "")
		if code != 200 || data["data"].(map[string]any)["id"] != chatID {
			t.Fatalf("friend chat idempotence: %d %+v", code, data)
		}
	}
	if code, _ := call("POST", "/v1/me/blocks", 0, `{"accountId":"`+ids[1]+`"}`); code != 204 {
		t.Fatalf("block: %d", code)
	}
	if code, data := call("GET", "/v1/me/ties", 1, ""); code != 200 || len(data["data"].([]any)) != 0 {
		t.Fatalf("blocked Tie visible: %d %+v", code, data)
	}
	if code, _ := call("POST", "/v1/me/connection-requests", 1, body); code != 404 && code != 409 {
		t.Fatalf("blocked request: %d", code)
	}
	if code, _ := call("DELETE", "/v1/me/blocks/"+ids[1], 0, ""); code != 204 {
		t.Fatalf("explicit unblock: %d", code)
	}
	if code, data := call("GET", "/v1/me/ties", 0, ""); code != 200 || len(data["data"].([]any)) != 0 {
		t.Fatalf("unblock restored Tie: %d %+v", code, data)
	}
}
