package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const humanHTTPConv = "33333333-3333-4333-8333-333333333333"
const humanHTTPOp = "55555555-5555-4555-8555-555555555555"

type humanMessageHTTPSpy struct {
	connection.Store
	sent, read, final int
	access            ea.Access
	in                connection.MessageOperationInput
	receipt           connection.MessageOperationReceipt
	err, finalErr     error
	after             func()
}

func (p *humanMessageHTTPSpy) SendHumanMessageOperation(_ context.Context, a ea.Access, id string, in connection.MessageOperationInput) (connection.MessageOperationReceipt, error) {
	p.sent++
	p.access = a
	p.in = in
	if p.after != nil {
		p.after()
	}
	return p.receipt, p.err
}
func (p *humanMessageHTTPSpy) ReadHumanMessageOperation(_ context.Context, a ea.Access, id, op string) (connection.MessageOperationReceipt, error) {
	p.read++
	p.access = a
	if p.after != nil {
		p.after()
	}
	return p.receipt, p.err
}
func (p *humanMessageHTTPSpy) ValidateHumanMessageOperationResponse(_ context.Context, a ea.Access, r connection.MessageOperationReceipt) error {
	p.final++
	return p.finalErr
}
func humanMessageHTTP(t *testing.T) (http.Handler, *humanMessageHTTPSpy, *privateProfileHTTPAccess, string) {
	_, _, a, token := messagePolicyUnitHTTP(t)
	p := &humanMessageHTTPSpy{}
	p.receipt = connection.NewMessageOperationReceipt(privateProfileHTTPOwner, humanHTTPOp, connection.Message{ID: privateProfileHTTPAgent, ConversationID: humanHTTPConv, SenderID: privateProfileHTTPOwner, Body: "明确发送", CreatedAt: time.Date(2026, 10, 7, 1, 2, 3, 0, time.UTC)})
	return New(nil, a, nil, nil, nil, nil, nil, nil, nil, nil, p, nil, false, nil, nil, nil), p, a, token
}
func TestHumanMessageOperationRegisteredRoute(t *testing.T) {
	h := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	for _, method := range []string{"POST", "GET"} {
		path := "/v1/me/conversations/" + humanHTTPConv + "/message-operations"
		if method == "GET" {
			path += "/" + humanHTTPOp
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		if w.Code != 503 {
			t.Fatalf("actual registered %s status=%d body=%s", method, w.Code, w.Body.String())
		}
	}
}
func TestHumanMessageOperationRegisteredCurrentReceipt(t *testing.T) {
	for _, method := range []string{"POST", "GET"} {
		h, p, a, token := humanMessageHTTP(t)
		path := "/v1/me/conversations/" + humanHTTPConv + "/message-operations"
		body := `{"operationId":"` + humanHTTPOp + `","body":"明确发送"}`
		want := 201
		if method == "GET" {
			path += "/" + humanHTTPOp
			body = ""
			want = 200
		}
		w := messagePolicyUnitRequest(h, method, path, body, token, nil)
		if w.Code != want || p.final != 1 || p.sent+p.read != 1 || p.access.Actor.ID != privateProfileHTTPOwner || p.access.SessionDigest != a.digest {
			t.Fatalf("registered actual receipt %s status=%d calls=%+v body=%s", method, w.Code, p, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "明确发送") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("receipt must not output private message body/cache")
		}
	}
}
func TestHumanMessageOperationCurrentAndLateFailure(t *testing.T) {
	for _, tc := range []string{"anonymous", "org", "override", "duplicate", "notfound", "conflict", "schema-missing", "late-session", "late-cancel", "mismatched-body"} {
		t.Run(tc, func(t *testing.T) {
			h, p, _, token := humanMessageHTTP(t)
			path := "/v1/me/conversations/" + humanHTTPConv + "/message-operations"
			body := `{"operationId":"` + humanHTTPOp + `","body":"明确发送"}`
			status, calls := 503, 1
			var change func(*http.Request)
			switch tc {
			case "anonymous":
				token = ""
				status, calls = 401, 0
			case "org":
				change = func(r *http.Request) { r.Header.Set("X-Birdtie-Organization-Workspace", privateProfileHTTPForeign) }
				status, calls = 403, 0
			case "override":
				body = strings.TrimSuffix(body, "}") + `,"ownerId":"` + privateProfileHTTPForeign + `"}`
				status, calls = 400, 0
			case "duplicate":
				body = strings.TrimSuffix(body, "}") + `,"body":"改写"}`
				status, calls = 400, 0
			case "notfound":
				p.err = connection.ErrNotFound
				status = 404
			case "conflict":
				p.err = connection.ErrConflict
				status = 409
			case "schema-missing":
				p.err = errors.New("synthetic missing105")
			case "late-session":
				p.finalErr = identity.ErrUnauthorized
				status = 401
			case "late-cancel":
				change = func(r *http.Request) {
					ctx, cancel := context.WithCancel(r.Context())
					*r = *r.WithContext(ctx)
					p.after = cancel
				}
			case "mismatched-body":
				p.receipt.PayloadDigest = strings.Repeat("a", 64)
			}
			w := messagePolicyUnitRequest(h, "POST", path, body, token, change)
			if w.Code != status || p.sent != calls {
				t.Fatalf("%s status=%d calls=%d body=%s", tc, w.Code, p.sent, w.Body.String())
			}
			if strings.Contains(w.Body.String(), `"messageId"`) {
				t.Fatal("late/denied receipt output")
			}
		})
	}
}

func TestHumanMessageOperationUppercasePathRejectsBeforeStore(t *testing.T) {
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/v1/me/conversations/BBBBBBBB-BBBB-4BBB-8BBB-BBBBBBBBBBBB/message-operations", `{"operationId":"` + humanHTTPOp + `","body":"明确发送"}`},
		{"POST", "/v1/me/conversations/" + humanHTTPConv + "/message-operations", `{"operationId":"AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA","body":"明确发送"}`},
		{"GET", "/v1/me/conversations/" + humanHTTPConv + "/message-operations/AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA", ""},
	} {
		h, p, _, token := humanMessageHTTP(t)
		w := messagePolicyUnitRequest(h, tc.method, tc.path, tc.body, token, nil)
		if w.Code != 400 || p.sent+p.read != 0 {
			t.Fatalf("canonical input must reject before wire %d %d", w.Code, p.sent+p.read)
		}
	}
}

type humanOperationDroppedResponse struct {
	header         http.Header
	status, writes int
}

func (w *humanOperationDroppedResponse) Header() http.Header    { return w.header }
func (w *humanOperationDroppedResponse) WriteHeader(status int) { w.status = status }
func (w *humanOperationDroppedResponse) Write(raw []byte) (int, error) {
	w.writes++
	return 0, errors.New("synthetic caller did not receive already committed response")
}

type humanOperationNativeLate struct {
	connection.Store
	native connection.HumanMessageOperationStore
	after  func()
}

func (p *humanOperationNativeLate) SendHumanMessageOperation(ctx context.Context, a ea.Access, id string, in connection.MessageOperationInput) (connection.MessageOperationReceipt, error) {
	r, e := p.native.SendHumanMessageOperation(ctx, a, id, in)
	if e == nil && p.after != nil {
		p.after()
	}
	return r, e
}
func (p *humanOperationNativeLate) ReadHumanMessageOperation(ctx context.Context, a ea.Access, id, op string) (connection.MessageOperationReceipt, error) {
	return p.native.ReadHumanMessageOperation(ctx, a, id, op)
}
func (p *humanOperationNativeLate) ValidateHumanMessageOperationResponse(ctx context.Context, a ea.Access, r connection.MessageOperationReceipt) error {
	return p.native.ValidateHumanMessageOperationResponse(ctx, a, r)
}
func TestHumanMessageOperationNativeRegisteredLostReceipt(t *testing.T) {
	f := messagePolicyNativeHTTP(t)
	f.exec(`INSERT INTO user_profiles(account_id,display_name,visibility) SELECT id,'本地合成恢复好友','public' FROM accounts WHERE id=ANY($1::uuid[]) ON CONFLICT DO NOTHING`, f.accountIDs[:2])
	t.Cleanup(func() {
		f.exec(`DELETE FROM native_notification_decisions WHERE actor_id=ANY($1::uuid[]) OR recipient_id=ANY($1::uuid[])`, f.accountIDs)
		f.exec(`DELETE FROM conversation_member_states WHERE member_account_id=ANY($1::uuid[])`, f.accountIDs)
	})
	w := f.request(t, f.handler, "POST", "/v1/me/connection-requests", `{"recipientAccountId":"`+f.accountIDs[1]+`","scope":"friend","note":"合成本人明确好友许可"}`, f.tokens[0], 201, nil)
	id := messagePolicyObject(t, w.Body.Bytes())["id"].(string)
	f.request(t, f.handler, "POST", "/v1/me/connection-requests/"+id+"/decision", `{"action":"accept"}`, f.tokens[1], 200, nil)
	ties, e := f.store.ListTies(f.ctx, f.accountIDs[0])
	if e != nil || len(ties) != 1 {
		t.Fatal("original native friend acceptance", e)
	}
	w = f.request(t, f.handler, "POST", "/v1/me/ties/"+ties[0].ID+"/conversation", "", f.tokens[0], 200, nil)
	chat := messagePolicyObject(t, w.Body.Bytes())["id"].(string)
	var op string
	if e = f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text`).Scan(&op); e != nil {
		t.Fatal(e)
	}
	path := "/v1/me/conversations/" + chat + "/message-operations"
	body := `{"operationId":"` + op + `","body":"本人合成消息，回执丢失真实事务核实"}`
	drop := &humanOperationDroppedResponse{header: make(http.Header)}
	f.handler.ServeHTTP(drop, privateProfileHTTPRequest("POST", path, body, f.tokens[0], "application/json"))
	if drop.status != 201 || drop.writes < 1 {
		t.Fatalf("actual native send did not reach loss boundary status=%d writes=%d", drop.status, drop.writes)
	}
	w = f.request(t, f.handler, "GET", path+"/"+op, "", f.tokens[0], 200, nil)
	var first struct {
		Data connection.MessageOperationReceipt `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &first) != nil || first.Data.MessageID == "" {
		t.Fatal("missing exact current receipt")
	}
	messagePolicyWire(t, "human-message-lost-response-original-get", w.Body.Bytes(), w.Code, w.Header().Get("X-Request-ID"))
	w = f.request(t, f.handler, "POST", path, body, f.tokens[0], 201, nil)
	var same struct {
		Data connection.MessageOperationReceipt `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &same) != nil || same.Data != first.Data {
		t.Fatal("repeat returned a different effect")
	}
	var n, d int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM conversation_messages WHERE sender_account_id=$1 AND client_operation_id=$2`, f.accountIDs[0], op).Scan(&n); e != nil || n != 1 {
		t.Fatal("actual keyed effect count", n, e)
	}
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM native_notification_decisions WHERE source_id=$1`, first.Data.MessageID).Scan(&d); e != nil || d != 1 {
		t.Fatal("actual notification count", d, e)
	}
	proxy := &humanOperationNativeLate{Store: f.store, native: f.store, after: func() {
		f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[0])
	}}
	handler := New(nil, f.store, nil, nil, nil, nil, nil, nil, nil, nil, proxy, nil, false, nil, nil, nil)
	if e = f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text`).Scan(&op); e != nil {
		t.Fatal(e)
	}
	body = fmt.Sprintf(`{"operationId":"%s","body":"当前事务已提交，回执编码前撤Session不能冒称取消"}`, op)
	w = f.request(t, handler, "POST", path, body, f.tokens[0], 401, nil)
	if strings.Contains(w.Body.String(), `"messageId"`) {
		t.Fatal("late session response leaked effect receipt")
	}
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM conversation_messages WHERE sender_account_id=$1 AND client_operation_id=$2`, f.accountIDs[0], op).Scan(&n); e != nil || n != 1 {
		t.Fatal("post-commit auth refusal must not imply undo", n, e)
	}
}
