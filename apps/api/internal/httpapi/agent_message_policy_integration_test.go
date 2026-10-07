package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const messagePolicyPath = "/v1/me/message-request-policy"

func messagePolicyNativeHTTP(t *testing.T) *privateProfileHTTPDBFixture {
	t.Helper()
	f := privateProfileHTTPDBNew(t)
	f.handler = New(f.store, f.store, nil, nil, nil, nil, nil, nil, nil, nil, f.store, nil, false, nil, nil, nil)
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM conversation_messages WHERE conversation_id IN(SELECT id FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[]))`,
			`DELETE FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[])`,
			`DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`,
			`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`,
		} {
			f.exec(q, f.accountIDs)
		}
	})
	return f
}

func messagePolicyWire(t *testing.T, name string, raw []byte, status int, requestID string) {
	t.Helper()
	if dir := os.Getenv("BIRDTIE_OUTPUT_VALIDATION_WIRE_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(map[string]any{"status": status, "requestID": requestID, "fixture": "OWNED_NATIVE_SYNTHETIC_NOT_IDP_NOT_PILOT"})
		if err := os.WriteFile(filepath.Join(dir, name+".meta.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func messagePolicyObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var v struct {
		Data map[string]any `json:"data"`
	}
	if json.Unmarshal(raw, &v) != nil || v.Data == nil {
		t.Fatal("native response has no data")
	}
	return v.Data
}

func TestMessagePolicyNativeRegisteredContractAndOriginalHumanControl(t *testing.T) {
	f := messagePolicyNativeHTTP(t)
	// Real old registered Request -> explicit recipient Accept -> Tie -> Chat.
	w := f.request(t, f.handler, "POST", "/v1/me/connection-requests", `{"recipientAccountId":"`+f.accountIDs[1]+`","scope":"friend","note":"合成明确好友申请"}`, f.tokens[0], 201, nil)
	id := messagePolicyObject(t, w.Body.Bytes())["id"].(string)
	f.request(t, f.handler, "POST", "/v1/me/connection-requests/"+id+"/decision", `{"action":"accept"}`, f.tokens[1], 200, nil)
	ties, err := f.store.ListTies(f.ctx, f.accountIDs[0])
	if err != nil || len(ties) != 1 {
		t.Fatal("old actual Tie missing")
	}
	w = f.request(t, f.handler, "POST", "/v1/me/ties/"+ties[0].ID+"/conversation", "", f.tokens[0], 200, nil)
	chat := messagePolicyObject(t, w.Body.Bytes())["id"].(string)
	w = f.request(t, f.handler, "POST", "/v1/me/conversations/"+chat+"/messages", `{"body":"隔离库原人类聊天控制"}`, f.tokens[0], 201, nil)
	messagePolicyWire(t, "message-policy-old-human-control", w.Body.Bytes(), w.Code, w.Header().Get("X-Request-ID"))
	// A business RED: the actual registered HTTP lacks the required policy read.
	r := privateProfileHTTPRequest(http.MethodGet, messagePolicyPath, "", f.tokens[1], "application/json")
	r.Header.Set("X-Request-ID", "message_policy_real_read")
	rw := httptest.NewRecorder()
	f.handler.ServeHTTP(rw, r)
	messagePolicyWire(t, "message-policy-required-read", rw.Body.Bytes(), rw.Code, rw.Header().Get("X-Request-ID"))
	if rw.Code != 200 {
		t.Fatalf("actual native policy read=%d want=200 after old Request/Accept/Chat succeeded", rw.Code)
	}
}
