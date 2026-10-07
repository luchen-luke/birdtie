package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Native registered HTTP and real Store, exclusively synthetic local people.
// Durable immutable receipts are retained until the owning runner drops its
// entire random database. This is neither production IdP nor Agent screening.
type decisionNativeFixture struct {
	*privateProfileHTTPDBFixture
	serial atomic.Int64
	city   string
}

func decisionNative(t *testing.T) *decisionNativeFixture {
	t.Helper()
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" || os.Getenv("BIRDTIE_AGE042_DATABASE") == "" {
		t.Skip("103 immutable history requires the explicitly owned AGE042 native database runner")
	}
	cfg, err := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil || cfg.ConnConfig.Database != os.Getenv("BIRDTIE_AGE042_DATABASE") ||
		!regexp.MustCompile(`^birdtie_age042_native_[0-9a-f]{12}$`).MatchString(cfg.ConnConfig.Database) ||
		cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Port != 55432 {
		t.Fatal("refuse non-owned/remote database: this fixture retains immutable native receipts")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f := &decisionNativeFixture{privateProfileHTTPDBFixture: &privateProfileHTTPDBFixture{t: t, ctx: ctx, pool: pool, store: postgres.New(pool, false)}}
	var installed bool
	if err = pool.QueryRow(ctx, `SELECT to_regclass('public.connection_request_decision_receipts') IS NOT NULL`).Scan(&installed); err != nil || !installed {
		t.Fatal("actual migration 103 required", err)
	}
	for _, kind := range []string{"person", "person", "person", "organization"} {
		var id, agent string
		if err = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),$1) RETURNING id`, kind).Scan(&id); err != nil {
			t.Fatal(err)
		}
		f.accountIDs = append(f.accountIDs, id)
		f.tokens = append(f.tokens, f.newSession(id, false, false))
		if kind == "person" {
			if err = pool.QueryRow(ctx, `INSERT INTO agents(agent_type,principal_account_id) VALUES('personal',$1) RETURNING id`, id).Scan(&agent); err != nil {
				t.Fatal(err)
			}
			f.exec(`INSERT INTO user_profiles(account_id,display_name,bio,visibility) VALUES($1,'合成本人申请测试','仅独占本地库','public')`, id)
		}
		f.agentIDs = append(f.agentIDs, agent)
	}
	if err = pool.QueryRow(ctx, `SELECT id FROM cities WHERE publication_status='published' ORDER BY id LIMIT 1`).Scan(&f.city); err != nil {
		t.Fatal("development fixture city missing", err)
	}
	f.handler = f.newHandler(f.store, f.store)
	return f
}

func (f *decisionNativeFixture) newHandler(access identity.AccessStore, store connection.Store) http.Handler {
	return New(f.store, access, nil, nil, nil, nil, nil, nil, nil, nil, store, nil, false, nil, nil, nil)
}

func (f *decisionNativeFixture) wire(handler http.Handler, method, path, body, token string, headers map[string]string, ctx context.Context) *httptest.ResponseRecorder {
	r := privateProfileHTTPRequest(method, path, body, token, "application/json")
	if ctx != nil {
		r = r.WithContext(ctx)
	}
	r.Header.Set("X-Request-ID", fmt.Sprintf("age042_native_%06d", f.serial.Add(1)))
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func (f *decisionNativeFixture) expect(t *testing.T, method, path, body, token string, want int, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	w := f.wire(f.handler, method, path, body, token, headers, nil)
	if w.Code != want {
		t.Fatalf("actual HTTP %s %s status=%d want=%d body=%s", method, path, w.Code, want, w.Body)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("native private result lost no-store")
	}
	t.Logf("OWNED_NATIVE_103 method=%s status=%d request_id=%s", method, w.Code, w.Header().Get("X-Request-ID"))
	return w
}

func (f *decisionNativeFixture) uuid(t *testing.T) string {
	t.Helper()
	var id string
	if e := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	return id
}

func (f *decisionNativeFixture) request(t *testing.T, scope string) string {
	t.Helper()
	body := map[string]string{"recipientAccountId": f.accountIDs[1], "scope": scope, "note": "合成明确申请，非真实邀请"}
	if scope == "conversation" {
		f.exec(`INSERT INTO intents(owner_account_id,city_id,topic,available_from,available_until,time_zone,coarse_area_label,audience,state,expires_at,owner_confirmed_at)
		VALUES($1,$2,'合成独占公开意图',now(),now()+interval '1 hour','Europe/London','合成区域','public','active',now()+interval '2 hours',now())`, f.accountIDs[1], f.city)
		body["cityId"] = f.city
	}
	raw, _ := json.Marshal(body)
	w := f.expect(t, "POST", "/v1/me/connection-requests", string(raw), f.tokens[0], 201, nil)
	var v struct {
		Data connection.Request `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || !connection.ValidDecisionID(v.Data.ID) || v.Data.Scope != scope || v.Data.State != "pending" {
		t.Fatal("original real create did not return original pending Request")
	}
	return v.Data.ID
}

func decisionNativePath(id string) string { return "/v1/me/connection-requests/" + id + "/decision" }
func decisionNativeRead(id, op string) string {
	return "/v1/me/connection-requests/" + id + "/decision-operations/" + op
}
func decisionNativeBody(action, op string) string {
	raw, _ := json.Marshal(map[string]string{"action": action, "operationId": op})
	return string(raw)
}
func decisionNativeReceipt(t *testing.T, w *httptest.ResponseRecorder, owner, request, operation, action string) connection.DecisionOperationReceipt {
	t.Helper()
	var v struct {
		Data connection.DecisionOperationReceipt `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || connection.ValidateDecisionReceipt(v.Data, owner, request, operation, action) != nil {
		t.Fatal("actual registered causal receipt violates original binding", w.Body)
	}
	return v.Data
}

type decisionNativeEffects struct {
	State                                                      string
	Receipts, Ties, Conversations, Notifications, Inbox, Audit int
}

func (f *decisionNativeFixture) effects(t *testing.T, id string) decisionNativeEffects {
	t.Helper()
	var v decisionNativeEffects
	err := f.pool.QueryRow(f.ctx, `SELECT state,
	(SELECT count(*) FROM connection_request_decision_receipts WHERE request_id=$1),
	(SELECT count(*) FROM person_ties WHERE request_id=$1),
	(SELECT count(*) FROM conversations WHERE request_id=$1),
	(SELECT count(*) FROM native_notification_decisions WHERE source_id=$1),
	(SELECT count(*) FROM inbox_items WHERE routing_decision_id IN(SELECT id FROM native_notification_decisions WHERE source_id=$1)),
	(SELECT count(*) FROM audit_events WHERE resource_type='connection_request' AND resource_id=$1::text AND action IN('accepted','declined','withdrawn'))
	FROM connection_requests WHERE id=$1`, id).Scan(&v.State, &v.Receipts, &v.Ties, &v.Conversations, &v.Notifications, &v.Inbox, &v.Audit)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func (f *decisionNativeFixture) assertNoOtherEffects(t *testing.T) {
	t.Helper()
	var n int
	if e := f.pool.QueryRow(f.ctx, `SELECT
	(SELECT count(*) FROM conversation_messages WHERE sender_account_id=ANY($1::uuid[]))+
	(SELECT count(*) FROM agent_memories WHERE owner_id=ANY($1::uuid[]))+
	(SELECT count(*) FROM agent_action_dispatches WHERE owner_id=ANY($1::uuid[]))+
	(SELECT count(*) FROM agent_sandbox_writes WHERE owner_id=ANY($1::uuid[]))`, f.accountIDs).Scan(&n); e != nil || n != 0 {
		t.Fatal("decision wrote a human message/Memory/Agent effect", n, e)
	}
}

func TestConnectionDecisionNativeOriginalWriterAndCausalHistory(t *testing.T) {
	for _, c := range []struct{ scope, action string }{{"friend", "accept"}, {"conversation", "accept"}, {"friend", "decline"}, {"friend", "withdraw"}} {
		t.Run(c.scope+"_"+c.action, func(t *testing.T) {
			f := decisionNative(t)
			id := f.request(t, c.scope)
			op := f.uuid(t)
			actor := 1
			if c.action == "withdraw" {
				actor = 0
			}
			w := f.expect(t, "POST", decisionNativePath(id), decisionNativeBody(c.action, op), f.tokens[actor], 200, nil)
			r := decisionNativeReceipt(t, w, f.accountIDs[actor], id, op, c.action)
			if r.Status != "COMMITTED" || r.State != connection.DecisionState(c.action) {
				t.Fatal("real original writer did not commit", r)
			}
			before := f.effects(t, id)
			if before.Receipts != 1 || before.Audit != 1 || before.State != r.State {
				t.Fatal("effect/receipt/audit not same original decision", before)
			}
			wantTie, wantChat := 0, 0
			if c.action == "accept" {
				if c.scope == "friend" {
					wantTie = 1
				} else {
					wantChat = 1
				}
			}
			if before.Ties != wantTie || before.Conversations != wantChat {
				t.Fatal("original scope effect changed", before)
			}
			w = f.expect(t, "POST", decisionNativePath(id), decisionNativeBody(c.action, op), f.tokens[actor], 200, nil)
			replayed := decisionNativeReceipt(t, w, f.accountIDs[actor], id, op, c.action)
			w = f.expect(t, "GET", decisionNativeRead(id, op), "", f.tokens[actor], 200, nil)
			read := decisionNativeReceipt(t, w, f.accountIDs[actor], id, op, c.action)
			if read != r || replayed != r || f.effects(t, id) != before {
				t.Fatal("replay/read wrote duplicate effects or changed causal ID")
			}
			f.assertNoOtherEffects(t)
			t.Logf("original_request=%s operation=%s scope=%s state=%s unique_effects=%+v", id, op, c.scope, r.State, before)
		})
	}
}

func TestConnectionDecisionNativeUnknownAndNoEffect(t *testing.T) {
	for _, reason := range []string{"EXPIRED", "ALREADY_DECIDED"} {
		t.Run(reason, func(t *testing.T) {
			f := decisionNative(t)
			id := f.request(t, "friend")
			op := f.uuid(t)
			f.expect(t, "GET", decisionNativeRead(id, op), "", f.tokens[1], 404, nil)
			if reason == "EXPIRED" {
				f.exec(`UPDATE connection_requests SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, id)
			} else {
				f.expect(t, "POST", decisionNativePath(id), `{"action":"decline"}`, f.tokens[1], 200, nil)
			}
			before := f.effects(t, id)
			w := f.expect(t, "POST", decisionNativePath(id), decisionNativeBody("accept", op), f.tokens[1], 200, nil)
			r := decisionNativeReceipt(t, w, f.accountIDs[1], id, op, "accept")
			if r.Status != "NO_EFFECT" || r.Reason != reason || r.State != "" {
				t.Fatal("false authoritative no-effect", r)
			}
			after := f.effects(t, id)
			before.Receipts++
			if after != before {
				t.Fatal("NO_EFFECT altered original decision effects", before, after)
			}
			f.expect(t, "GET", decisionNativeRead(id, op), "", f.tokens[1], 200, nil)
			f.assertNoOtherEffects(t)
		})
	}
}

func TestConnectionDecisionNativeAuthorizationAndBinding(t *testing.T) {
	f := decisionNative(t)
	id := f.request(t, "friend")
	op := f.uuid(t)
	expired := f.newSession(f.accountIDs[1], true, false)
	revoked := f.newSession(f.accountIDs[1], false, true)
	for _, c := range []struct {
		name, token, method, body, suffix string
		want                              int
		headers                           map[string]string
	}{
		{"anonymous", "", "POST", decisionNativeBody("accept", op), "", 401, nil},
		{"expired", expired, "POST", decisionNativeBody("accept", op), "", 401, nil},
		{"revoked", revoked, "POST", decisionNativeBody("accept", op), "", 401, nil},
		{"foreign", f.tokens[2], "POST", decisionNativeBody("accept", op), "", 404, nil},
		{"wrong_role", f.tokens[0], "POST", decisionNativeBody("accept", op), "", 404, nil},
		{"organization", f.tokens[3], "POST", decisionNativeBody("accept", op), "", 403, nil},
		{"workspace", f.tokens[1], "POST", decisionNativeBody("accept", op), "", 403, map[string]string{"X-Birdtie-Organization-Workspace": f.accountIDs[3]}},
		{"query", f.tokens[1], "POST", decisionNativeBody("accept", op), "?ownerId=" + f.accountIDs[1], 400, nil},
		{"confirmed", f.tokens[1], "POST", `{"action":"accept","operationId":"` + op + `","confirmed":true}`, "", 400, nil},
		{"null_key", f.tokens[1], "POST", `{"action":"accept","operationId":null}`, "", 400, nil},
		{"history_body", f.tokens[1], "GET", `{}`, "", 400, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := decisionNativePath(id) + c.suffix
			if c.method == "GET" {
				path = decisionNativeRead(id, op) + c.suffix
			}
			w := f.expect(t, c.method, path, c.body, c.token, c.want, c.headers)
			if strings.Contains(w.Body.String(), "requestDigest") {
				t.Fatal("denied native result leaked receipt")
			}
		})
	}
	if v := f.effects(t, id); v.State != "pending" || v.Receipts != 0 || v.Audit != 0 || v.Ties != 0 {
		t.Fatal("denied request had effects", v)
	}
	f.expect(t, "POST", decisionNativePath(id), decisionNativeBody("accept", op), f.tokens[1], 200, nil)
	before := f.effects(t, id)
	f.expect(t, "POST", decisionNativePath(id), decisionNativeBody("decline", op), f.tokens[1], 409, nil)
	otherBody := `{"recipientAccountId":"` + f.accountIDs[1] + `","scope":"friend","note":"合成另一原申请"}`
	otherWire := f.expect(t, "POST", "/v1/me/connection-requests", otherBody, f.tokens[2], 201, nil)
	var other struct {
		Data connection.Request `json:"data"`
	}
	if json.Unmarshal(otherWire.Body.Bytes(), &other) != nil || !connection.ValidDecisionID(other.Data.ID) {
		t.Fatal("another original request missing")
	}
	f.expect(t, "POST", decisionNativePath(other.Data.ID), decisionNativeBody("accept", op), f.tokens[1], 409, nil)
	if v := f.effects(t, other.Data.ID); v.State != "pending" || v.Receipts != 0 || v.Ties != 0 {
		t.Fatal("key rebound to another original request", v)
	}
	f.expect(t, "GET", decisionNativeRead(id, op), "", f.tokens[0], 404, nil)
	f.expect(t, "GET", decisionNativeRead(id, op), "", f.tokens[2], 404, nil)
	if f.effects(t, id) != before {
		t.Fatal("mismatch or foreign read changed effects")
	}
	// Same owner with a genuinely new current Session can read history; no old approval is reused.
	newToken := f.newSession(f.accountIDs[1], false, false)
	digest := sha256.Sum256([]byte(newToken))
	f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1 AND token_sha256<>$2`, f.accountIDs[1], digest[:])
	f.expect(t, "GET", decisionNativeRead(id, op), "", f.tokens[1], 401, nil)
	f.expect(t, "GET", decisionNativeRead(id, op), "", newToken, 200, nil)
	f.assertNoOtherEffects(t)
}

type decisionNativeDroppedResponse struct {
	header http.Header
	status int
}

func (w *decisionNativeDroppedResponse) Header() http.Header         { return w.header }
func (w *decisionNativeDroppedResponse) WriteHeader(status int)      { w.status = status }
func (w *decisionNativeDroppedResponse) Write(b []byte) (int, error) { return 0, io.ErrClosedPipe }

func TestConnectionDecisionNativeLostReceiptReplayAndRestart(t *testing.T) {
	f := decisionNative(t)
	id := f.request(t, "friend")
	op := f.uuid(t)
	r := privateProfileHTTPRequest("POST", decisionNativePath(id), decisionNativeBody("accept", op), f.tokens[1], "application/json")
	w := &decisionNativeDroppedResponse{header: make(http.Header)}
	f.handler.ServeHTTP(w, r)
	if w.status != 200 {
		t.Fatal("actual dropped transport POST was not committed", w.status)
	}
	before := f.effects(t, id)
	if before.Receipts != 1 || before.Ties != 1 || before.Audit != 1 {
		t.Fatal("unknown transport did not retain unique actual effects", before)
	}
	// Close and recreate the actual pool/Store/registered handler against the same persistent database.
	f.pool.Close()
	pool, e := pgxpool.New(f.ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	f.pool = pool
	f.store = postgres.New(pool, false)
	f.handler = f.newHandler(f.store, f.store)
	f.expect(t, "GET", decisionNativeRead(id, op), "", f.tokens[1], 200, nil)
	var wg sync.WaitGroup
	failures := make(chan string, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v := f.wire(f.handler, "POST", decisionNativePath(id), decisionNativeBody("accept", op), f.tokens[1], nil, nil)
			if v.Code != 200 {
				failures <- fmt.Sprintf("concurrent replay status=%d body=%s", v.Code, v.Body)
			}
		}()
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	if f.effects(t, id) != before {
		t.Fatal("pool restart/concurrent replay duplicated original effects")
	}
	f.assertNoOtherEffects(t)
}

// A missing final current-read capability must remain unavailable even when
// the original native writer committed. It cannot downgrade to an unkeyed write.
type decisionNativeNoFinalPort struct {
	connection.Store
	connection.CurrentStore
	connection.DecisionOperationStore
}

func TestConnectionDecisionNativeMissingResponsePortKeepsUnknown(t *testing.T) {
	f := decisionNative(t)
	id := f.request(t, "friend")
	op := f.uuid(t)
	port := &decisionNativeNoFinalPort{f.store, f.store, f.store}
	h := f.newHandler(f.store, port)
	w := f.wire(h, "POST", decisionNativePath(id), decisionNativeBody("accept", op), f.tokens[1], nil, nil)
	if w.Code != 503 || strings.Contains(w.Body.String(), "requestDigest") {
		t.Fatal("missing final capability falsely completed/disclosed receipt", w.Code, w.Body)
	}
	before := f.effects(t, id)
	if before.State != "accepted" || before.Receipts != 1 || before.Ties != 1 || before.Audit != 1 {
		t.Fatal("late unavailable lost original committed outcome", before)
	}
	w = f.wire(h, "GET", decisionNativeRead(id, op), "", f.tokens[1], nil, nil)
	if w.Code != 503 || strings.Contains(w.Body.String(), "requestDigest") {
		t.Fatal("missing final capability falsely recovered", w.Code, w.Body)
	}
	f.expect(t, "GET", decisionNativeRead(id, op), "", f.tokens[1], 200, nil)
	if f.effects(t, id) != before {
		t.Fatal("restored GET repeated original write")
	}
	f.assertNoOtherEffects(t)
}

func decisionNativeSQLReject(t *testing.T, f *decisionNativeFixture, code string, statements []string, args []any) {
	t.Helper()
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	for i, q := range statements {
		_, e = tx.Exec(f.ctx, q, args...)
		if i < len(statements)-1 && e != nil {
			t.Fatal("constraint fixture precursor failed", e)
		}
	}
	var pe *pgconn.PgError
	if !errors.As(e, &pe) || pe.Code != code {
		t.Fatalf("actual native constraint=%v want SQLSTATE=%s", e, code)
	}
	t.Logf("actual_103_constraint SQLSTATE=%s", pe.Code)
}
func TestConnectionDecisionNative103ImmutableAndCausalConstraints(t *testing.T) {
	f := decisionNative(t)
	id := f.request(t, "friend")
	op := f.uuid(t)
	f.expect(t, "POST", decisionNativePath(id), decisionNativeBody("accept", op), f.tokens[1], 200, nil)
	for _, q := range []string{`UPDATE connection_request_decision_receipts SET recorded_at=recorded_at WHERE owner_account_id=$1 AND operation_id=$2`, `DELETE FROM connection_request_decision_receipts WHERE owner_account_id=$1 AND operation_id=$2`} {
		decisionNativeSQLReject(t, f, "55000", []string{q}, []any{f.accountIDs[1], op})
	}
	decisionNativeSQLReject(t, f, "23503", []string{`DELETE FROM connection_requests WHERE id=$1`}, []any{id})
	// A later transaction cannot forge another COMMITTED receipt from old state/audit.
	insert := `INSERT INTO connection_request_decision_receipts(owner_account_id,operation_id,request_id,request_digest,action,scope,status,state,reason,recorded_at) VALUES($1,$2,$3,repeat('a',64),'accept','friend','COMMITTED','accepted',NULL,clock_timestamp())`
	decisionNativeSQLReject(t, f, "23514", []string{insert}, []any{f.accountIDs[1], f.uuid(t), id})
	decisionNativeSQLReject(t, f, "23514", []string{insert}, []any{f.accountIDs[0], f.uuid(t), id})
	// Actual pending row does not establish expiry or NO_EFFECT; NULL is not evidence.
	g := decisionNative(t)
	pending := g.request(t, "friend")
	for _, reason := range []string{"NULL", "'EXPIRED'", "'ALREADY_DECIDED'"} {
		q := `INSERT INTO connection_request_decision_receipts(owner_account_id,operation_id,request_id,request_digest,action,scope,status,state,reason,recorded_at) VALUES($1,$2,$3,repeat('a',64),'accept','friend','NO_EFFECT','',` + reason + `,clock_timestamp())`
		decisionNativeSQLReject(t, g, "23514", []string{q}, []any{g.accountIDs[1], g.uuid(t), pending})
	}
	// Same-XID Request change without the original same-XID audit must rollback.
	decisionNativeSQLReject(t, g, "23514", []string{`UPDATE connection_requests SET state='accepted' WHERE id=$3 AND $1::uuid IS NOT NULL AND $2::uuid IS NOT NULL`, insert}, []any{g.accountIDs[1], g.uuid(t), pending})
	if v := g.effects(t, pending); v.State != "pending" || v.Receipts != 0 || v.Audit != 0 {
		t.Fatal("rejected causal SQL left a partial effect", v)
	}
}

func (f *decisionNativeFixture) setPolicy(t *testing.T, mode string, duration time.Duration) {
	t.Helper()
	w := f.expect(t, "GET", messagePolicyPath, "", f.tokens[1], 200, nil)
	var v struct {
		Data mp.Record `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &v) != nil {
		t.Fatal("policy native read")
	}
	body, _ := json.Marshal(mp.PutInput{ExpectedVersion: v.Data.NativeRevision, IncomingRequests: mp.Disposition(mode), ExpiresAt: time.Now().Add(duration).UTC().Truncate(time.Microsecond)})
	f.expect(t, "PUT", messagePolicyPath, string(body), f.tokens[1], 200, nil)
}

func TestConnectionDecisionNativeFourRoutesAndSameScreenRequest(t *testing.T) {
	f := decisionNative(t)
	f.setPolicy(t, "SCREEN", time.Hour)
	w := f.expect(t, "GET", messagePolicyPath+"/decisions/"+f.accountIDs[1], "", f.tokens[0], 200, nil)
	var route struct {
		Data mp.Decision `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &route) != nil || route.Data.Disposition != mp.Screen || route.Data.ScreeningAvailable || route.Data.AutomaticAcceptance {
		t.Fatal("native SCREEN must remain human review without Agent", w.Body)
	}
	id := f.request(t, "friend")
	var disposition string
	if e := f.pool.QueryRow(f.ctx, `SELECT disposition FROM connection_request_policy_bindings WHERE request_id=$1`, id).Scan(&disposition); e != nil || disposition != "SCREEN" {
		t.Fatal("same original request lacks native SCREEN binding", e, disposition)
	}
	w = f.expect(t, "GET", "/v1/me/connection-requests", "", f.tokens[1], 200, nil)
	var requests struct {
		Data []connection.Request `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &requests) != nil || len(requests.Data) != 1 || requests.Data[0].ID != id || requests.Data[0].PolicyDisposition != "SCREEN" || requests.Data[0].ScreeningStatus != "PENDING_REVIEW" {
		t.Fatal("real original list did not project same SCREEN request", w.Body)
	}
	before := f.effects(t, id)
	if before.Notifications != 0 || before.Inbox != 0 || before.Ties != 0 || before.Conversations != 0 {
		t.Fatal("SCREEN auto-delivered/accepted original request", before)
	}
	op := f.uuid(t)
	f.expect(t, "POST", decisionNativePath(id), decisionNativeBody("accept", op), f.tokens[1], 200, nil)
	w = f.expect(t, "GET", messagePolicyPath+"/decisions/"+f.accountIDs[1], "", f.tokens[0], 200, nil)
	if json.Unmarshal(w.Body.Bytes(), &route) != nil || route.Data.Disposition != mp.Allow || route.Data.ScreeningAvailable {
		t.Fatal("ALLOW not derived from actual original accepted Tie", w.Body)
	}
	f.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.accountIDs[1], f.accountIDs[0])
	w = f.expect(t, "GET", messagePolicyPath+"/decisions/"+f.accountIDs[1], "", f.tokens[0], 200, nil)
	if json.Unmarshal(w.Body.Bytes(), &route) != nil || route.Data.Disposition != mp.Block || route.Data.SourceVersion != "" {
		t.Fatal("actual Block must beat accepted Tie without reusable source", w.Body)
	}
	f.expect(t, "GET", decisionNativeRead(id, op), "", f.tokens[1], 403, nil)
	f.assertNoOtherEffects(t)
}

func decisionNativeWaitLock(t *testing.T, f *decisionNativeFixture, needle string) {
	t.Helper()
	until := time.Now().Add(4 * time.Second)
	for time.Now().Before(until) {
		var n int
		if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE $1`, "%"+needle+"%").Scan(&n); e != nil {
			t.Fatal(e)
		}
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	rows, e := f.pool.Query(f.ctx, `SELECT query,COALESCE(wait_event_type,''),COALESCE(wait_event,'') FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND state='active'`)
	if e == nil {
		defer rows.Close()
		for rows.Next() {
			var q, kind, event string
			if rows.Scan(&q, &kind, &event) == nil {
				t.Logf("owned_wait_diagnostic query=%s kind=%s event=%s", q, kind, event)
			}
		}
	}
	t.Fatal("actual registered writer did not enter the expected PostgreSQL lock wait", needle)
}
func decisionNativeWaitClock(t *testing.T, f *decisionNativeFixture, query string, arg any) {
	t.Helper()
	until := time.Now().Add(4 * time.Second)
	for time.Now().Before(until) {
		var done bool
		if e := f.pool.QueryRow(f.ctx, query, arg).Scan(&done); e != nil {
			t.Fatal(e)
		}
		if done {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("actual database clock did not cross original expiry")
}

type decisionNativeBeforeWrite struct {
	*postgres.Store
	before func()
}

func (p *decisionNativeBeforeWrite) DecideRequestOperation(ctx context.Context, a ea.Access, id, action, op string) (connection.DecisionOperationReceipt, error) {
	p.before()
	return p.Store.DecideRequestOperation(ctx, a, id, action, op)
}
func TestConnectionDecisionNativeCurrentClockAndCancellation(t *testing.T) {
	for _, name := range []string{"policy_final_clock", "session_final_clock", "request_final_clock", "cancelled_lock"} {
		t.Run(name, func(t *testing.T) {
			f := decisionNative(t)
			id := f.request(t, "friend")
			op := f.uuid(t)
			if name == "policy_final_clock" {
				f.setPolicy(t, "REQUEST", 1200*time.Millisecond)
			}
			if name == "session_final_clock" {
				f.exec(`UPDATE sessions SET expires_at=statement_timestamp()+interval '1.2 seconds',idle_expires_at=statement_timestamp()+interval '1.2 seconds' WHERE account_id=$1`, f.accountIDs[1])
			}
			if name == "request_final_clock" {
				f.exec(`UPDATE connection_requests SET expires_at=clock_timestamp()+interval '1.2 seconds' WHERE id=$1`, id)
			}
			lock, e := f.pool.Begin(f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer lock.Rollback(context.Background())
			// Real HTTP authentication updates the session first. Acquire the native
			// lock only after that original step; otherwise we test initial auth wait,
			// not the writer's post-route/final-clock boundary.
			port := &decisionNativeBeforeWrite{Store: f.store, before: func() {
				if _, err := lock.Exec(f.ctx, `SELECT id FROM sessions WHERE account_id=$1 FOR UPDATE`, f.accountIDs[1]); err != nil {
					panic(err)
				}
			}}
			handler := f.newHandler(f.store, port)
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				done <- f.wire(handler, "POST", decisionNativePath(id), decisionNativeBody("accept", op), f.tokens[1], nil, ctx)
			}()
			decisionNativeWaitLock(t, f, "SELECT id FROM sessions WHERE token_sha256")
			switch name {
			case "policy_final_clock":
				decisionNativeWaitClock(t, f, `SELECT expires_at<=clock_timestamp() FROM agent_message_request_policies WHERE owner_id=$1`, f.accountIDs[1])
			case "session_final_clock":
				decisionNativeWaitClock(t, f, `SELECT bool_and(expires_at<=clock_timestamp()) FROM sessions WHERE account_id=$1`, f.accountIDs[1])
			case "request_final_clock":
				decisionNativeWaitClock(t, f, `SELECT expires_at<=clock_timestamp() FROM connection_requests WHERE id=$1`, id)
			case "cancelled_lock":
				cancel()
			}
			if e = lock.Rollback(context.Background()); e != nil {
				t.Fatal(e)
			}
			var w *httptest.ResponseRecorder
			select {
			case w = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("native blocked HTTP did not complete")
			}
			want := 409
			if name == "session_final_clock" {
				want = 401
			}
			if name == "cancelled_lock" {
				want = 503
			}
			if name == "request_final_clock" {
				want = 200
			}
			if w.Code != want {
				t.Fatalf("current actual %s status=%d want=%d body=%s", name, w.Code, want, w.Body)
			}
			v := f.effects(t, id)
			if name == "request_final_clock" {
				r := decisionNativeReceipt(t, w, f.accountIDs[1], id, op, "accept")
				if r.Status != "NO_EFFECT" || r.Reason != "EXPIRED" || v.Receipts != 1 {
					t.Fatal("request clock was not authoritative NO_EFFECT", r, v)
				}
			} else if v.Receipts != 0 {
				t.Fatal("expired/cancelled current fence retained receipt", v)
			}
			if v.State != "pending" || v.Ties != 0 || v.Conversations != 0 || v.Audit != 0 {
				t.Fatal("late original writer effects escaped rollback", v)
			}
			t.Logf("actual_post_route_session_lock mode=%s final_status=%d effects=%+v", name, w.Code, v)
		})
	}
}

// This wrapper never supplies authority or a receipt. Its final-response hook
// mutates actual owned PG rows after JSON encoding, then delegates the actual
// Store's Session validator. It distinguishes session proof from resource proof.
type decisionNativeFinalResponse struct {
	*postgres.Store
	once          sync.Once
	afterEncoding func(context.Context)
}

func (p *decisionNativeFinalResponse) ValidateHumanSocialResponse(ctx context.Context, digest [32]byte, a identity.Actor) error {
	p.once.Do(func() { p.afterEncoding(ctx) })
	return p.Store.ValidateHumanSocialResponse(ctx, digest, a)
}
func TestConnectionDecisionNativeResponseRetirement(t *testing.T) {
	for _, mode := range []string{"session", "account", "block", "policy"} {
		t.Run(mode, func(t *testing.T) {
			f := decisionNative(t)
			scope := "friend"
			if mode == "policy" {
				scope = "conversation"
				f.setPolicy(t, "REQUEST", time.Hour)
			}
			id := f.request(t, scope)
			op := f.uuid(t)
			f.expect(t, "POST", decisionNativePath(id), decisionNativeBody("accept", op), f.tokens[1], 200, nil)
			before := f.effects(t, id)
			access := &decisionNativeFinalResponse{Store: f.store}
			access.afterEncoding = func(ctx context.Context) {
				switch mode {
				case "session":
					f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[1])
				case "account":
					f.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.accountIDs[1])
				case "block":
					f.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.accountIDs[1], f.accountIDs[0])
				case "policy":
					f.setPolicy(t, "BLOCK", time.Hour)
				}
			}
			h := f.newHandler(access, f.store)
			w := f.wire(h, "GET", decisionNativeRead(id, op), "", f.tokens[1], nil, nil)
			want := 403
			if mode == "session" || mode == "account" {
				want = 401
			}
			if w.Code != want || strings.Contains(w.Body.String(), "requestDigest") {
				t.Fatalf("actual after-encoding %s retirement returned status=%d want=%d private_receipt=%t body=%s", mode, w.Code, want, strings.Contains(w.Body.String(), "requestDigest"), w.Body)
			}
			if f.effects(t, id) != before {
				t.Fatal("response rejection changed committed historical effect")
			}
		})
	}
}

func TestConnectionDecisionNativePostResponseRetirement(t *testing.T) {
	for _, mode := range []string{"session", "account", "block", "policy"} {
		t.Run(mode, func(t *testing.T) {
			f := decisionNative(t)
			scope := "friend"
			if mode == "policy" {
				scope = "conversation"
				f.setPolicy(t, "REQUEST", time.Hour)
			}
			id := f.request(t, scope)
			op := f.uuid(t)
			access := &decisionNativeFinalResponse{Store: f.store}
			access.afterEncoding = func(ctx context.Context) {
				switch mode {
				case "session":
					f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[1])
				case "account":
					f.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.accountIDs[1])
				case "block":
					f.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.accountIDs[1], f.accountIDs[0])
				case "policy":
					f.setPolicy(t, "BLOCK", time.Hour)
				}
			}
			h := f.newHandler(access, f.store)
			w := f.wire(h, "POST", decisionNativePath(id), decisionNativeBody("accept", op), f.tokens[1], nil, nil)
			want := 403
			if mode == "session" || mode == "account" {
				want = 401
			}
			// A late refusal must not claim the original transaction was rolled back.
			v := f.effects(t, id)
			wantTie, wantChat := 1, 0
			if scope == "conversation" {
				wantTie, wantChat = 0, 1
			}
			if v.State != "accepted" || v.Receipts != 1 || v.Ties != wantTie || v.Conversations != wantChat || v.Audit != 1 {
				t.Fatal("late response retirement lost the actual original causal outcome", v)
			}
			if w.Code != want || strings.Contains(w.Body.String(), "requestDigest") {
				t.Fatalf("actual POST after-encoding %s retirement returned status=%d want=%d private_receipt=%t body=%s", mode, w.Code, want, strings.Contains(w.Body.String(), "requestDigest"), w.Body)
			}
			f.assertNoOtherEffects(t)
		})
	}
}
