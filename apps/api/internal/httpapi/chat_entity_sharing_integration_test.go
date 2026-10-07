package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEntityShareHTTPNativeRegisteredLifecycle(t *testing.T) {
	f := publicationHTTPNative(t)
	b := f.base
	b.handler = New(b.store, b.store, nil, b.store, nil, nil, nil, nil, nil, nil, b.store, nil, false, nil, nil, nil)
	ids := []string{f.account, b.accountIDs[0]}
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM native_notification_decisions WHERE recipient_id=ANY($1::uuid[]) OR actor_id=ANY($1::uuid[])`,
			`DELETE FROM conversation_messages WHERE sender_account_id=ANY($1::uuid[])`,
			`DELETE FROM conversation_member_states WHERE member_account_id=ANY($1::uuid[])`,
			`DELETE FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[])`,
			`DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`,
			`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`,
		} {
			if _, e := b.pool.Exec(context.Background(), q, ids); e != nil {
				t.Error("owned HTTP sharing cleanup", e)
			}
		}
	})
	request, e := b.store.CreateFriendRequest(b.ctx, ids[0], ids[1], "本地合成明确好友许可")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.DecideRequest(b.ctx, ids[1], request.ID, "accept"); e != nil {
		t.Fatal(e)
	}
	ties, e := b.store.ListTies(b.ctx, ids[0])
	if e != nil || len(ties) != 1 {
		t.Fatal(ties, e)
	}
	convo, e := b.store.StartFriendConversation(b.ctx, ids[0], ties[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	moment := publicationHTTPNativeDraft(t, f)
	preview := publicationHTTPNativePreview(t, f, moment.ID)
	if w := historicalHTTPCall(b, "POST", "/v1/me/moments/"+moment.ID+"/publication", publicationHTTPNativeBody(preview), f.token); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var operation string
	if e = b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid()::text`).Scan(&operation); e != nil {
		t.Fatal(e)
	}
	path := "/v1/me/conversations/" + convo.ID + "/entity-shares"
	body := fmt.Sprintf(`{"operationId":%q,"entity":{"type":"moment","id":%q}}`, operation, moment.ID)
	message := ""
	call := func(method, route, raw, token string, want int) connection.EntityShareReceipt {
		t.Helper()
		w := historicalHTTPCall(b, method, route, raw, token)
		if w.Code != want || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("registered sharing", method, w.Code, want, w.Body.String())
		}
		var result struct{ Data connection.EntityShareReceipt }
		if want == 200 || want == 201 {
			if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil {
				t.Fatal(e)
			}
		}
		return result.Data
	}
	first := call("POST", path, body, f.token, 201)
	message = first.Message.ID
	if first.OperationID != operation || message == "" || first.Message.SenderID != f.account || first.Message.Entity == nil || first.Message.Entity.ID != moment.ID {
		t.Fatal(first)
	}
	replay := call("POST", path, body, f.token, 201)
	if replay.Message.ID != message {
		t.Fatal("duplicate effect", replay)
	}
	recovery := call("GET", path+"/"+operation, "", f.token, 200)
	if recovery.Message.ID != message {
		t.Fatal("wrong recovery", recovery)
	}
	call("POST", path, body, "", 401)
	call("POST", path, body, b.tokens[2], 401)
	call("GET", path+"/"+operation, "", b.tokens[0], 404)
	for _, bad := range []string{strings.Replace(body, `"moment"`, `"memory"`, 1), strings.Replace(body, `"operationId":`, `"operationId":null,"operationId":`, 1), body + ` {}`, strings.Replace(body, `"entity":`, `"privateContext":{},"entity":`, 1)} {
		call("POST", path, bad, f.token, 400)
	}
	conflict := strings.Replace(body, `"moment"`, `"place"`, 1)
	conflict = strings.Replace(conflict, moment.ID, f.place, 1)
	call("POST", path, conflict, f.token, 409)
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+f.token)
	req.Header.Set("X-Birdtie-Organization-Workspace", b.accountIDs[2])
	w := httptest.NewRecorder()
	b.handler.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatal("workspace inherited old personal share", w.Code)
	}
	published := historicalHTTPRecord(t, historicalHTTPCall(b, "GET", "/v1/me/moments/"+moment.ID, "", f.token), 200)
	if w = historicalHTTPCall(b, "DELETE", fmt.Sprintf("/v1/me/moments/%s?revision=%d", moment.ID, published.Revision), "", f.token); w.Code != 204 {
		t.Fatal("native withdraw", w.Code, w.Body.String())
	}
	removed := call("GET", path+"/"+operation, "", f.token, 200)
	if removed.Message.ID != message || removed.Message.Entity == nil || removed.Message.Entity.Available || removed.Message.Entity.ID != "" || removed.Message.Entity.Title != "" {
		t.Fatal("withdrawn committed card leaked", removed)
	}
	replay = call("POST", path, body, f.token, 201)
	if replay.Message.ID != message || replay.Message.Entity.Available {
		t.Fatal(replay)
	}
	var count, decisions int
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM conversation_messages WHERE conversation_id=$1`, convo.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM native_notification_decisions WHERE source_id=$1`, message).Scan(&decisions); e != nil || decisions != 1 {
		t.Fatal(decisions, e)
	}
}
