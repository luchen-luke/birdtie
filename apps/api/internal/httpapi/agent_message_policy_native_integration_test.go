package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Original registered HTTP and PostgreSQL, synthetic owned people only. No
// provider, Agent screening, real contact or per-row deletion of old history.
func policyNativeHTTP(t *testing.T) *privateProfileHTTPDBFixture {
	t.Helper()
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" || os.Getenv("BIRDTIE_MESSAGE_POLICY_DATABASE") == "" {
		t.Skip("requires the explicitly owned message-policy database runner")
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || cfg.ConnConfig.Database != os.Getenv("BIRDTIE_MESSAGE_POLICY_DATABASE") ||
		!regexp.MustCompile(`^birdtie_age042_policy_[0-9a-f]{12}$`).MatchString(cfg.ConnConfig.Database) || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Port != 55432 {
		t.Fatal("refuse non-owned/remote message-policy database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	f := &privateProfileHTTPDBFixture{t: t, ctx: ctx, pool: pool, store: postgres.New(pool, false)}
	var installed bool
	if e = pool.QueryRow(ctx, `SELECT to_regclass('public.agent_message_request_policies') IS NOT NULL AND to_regclass('public.connection_request_decision_receipts') IS NOT NULL`).Scan(&installed); e != nil || !installed {
		t.Fatal("real original 098/103 required", e)
	}
	for i := 0; i < 3; i++ {
		var id, agent string
		if e = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&id); e != nil {
			t.Fatal(e)
		}
		f.accountIDs = append(f.accountIDs, id)
		f.tokens = append(f.tokens, f.newSession(id, false, false))
		if e = pool.QueryRow(ctx, `INSERT INTO agents(agent_type,principal_account_id) VALUES('personal',$1) RETURNING id`, id).Scan(&agent); e != nil {
			t.Fatal(e)
		}
		f.agentIDs = append(f.agentIDs, agent)
		f.exec(`INSERT INTO user_profiles(account_id,display_name,bio,visibility) VALUES($1,'独占本地消息策略','合成，不联系真实用户','public')`, id)
	}
	f.handler = New(f.store, f.store, nil, nil, nil, nil, nil, f.store, nil, nil, f.store, nil, false, nil, nil, nil)
	return f
}

func policyNativeWire(t *testing.T, f *privateProfileHTTPDBFixture, h http.Handler, method, path, body string, actor int) *httptest.ResponseRecorder {
	t.Helper()
	r := privateProfileHTTPRequest(method, path, body, f.tokens[actor], "application/json")
	r.Header.Set("X-Request-ID", "message_policy_owned_native")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func policyNativeNow(t *testing.T, f *privateProfileHTTPDBFixture) time.Time {
	t.Helper()
	var n time.Time
	if e := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n.UTC()
}
func policyNativePutBody(version int64, mode string, until time.Time) string {
	b, _ := json.Marshal(map[string]any{"expectedVersion": version, "incomingRequests": mode, "expiresAt": until.UTC()})
	return string(b)
}
func policyNativePut(t *testing.T, f *privateProfileHTTPDBFixture, actor int, version int64, mode string, ttl time.Duration) *httptest.ResponseRecorder {
	t.Helper()
	w := policyNativeWire(t, f, f.handler, "PUT", messagePolicyPath, policyNativePutBody(version, mode, policyNativeNow(t, f).Add(ttl)), actor)
	if w.Code != 200 {
		t.Fatalf("original registered policy PUT=%d, want200", w.Code)
	}
	return w
}

type policyNativeFinalAccess struct {
	*postgres.Store
	once  sync.Once
	after func(context.Context)
}

func (s *policyNativeFinalAccess) ValidateHumanSocialResponse(ctx context.Context, digest [32]byte, actor identity.Actor) error {
	s.once.Do(func() { s.after(ctx) })
	return s.Store.ValidateHumanSocialResponse(ctx, digest, actor)
}

func TestMessagePolicyRegisteredNativeResponseSourceRetirement(t *testing.T) {
	for _, c := range []struct {
		name, method               string
		decision, expiry, agentABA bool
	}{
		{"get_policy_change", "GET", false, false, false},
		{"put_policy_change", "PUT", false, false, false},
		{"decision_receiver_change", "GET", true, false, false},
		{"get_expiry", "GET", false, true, false},
		{"get_agent_ABA", "GET", false, false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := policyNativeHTTP(t)
			ttl := time.Hour
			if c.expiry {
				ttl = time.Second
			}
			policyNativePut(t, f, 1, 0, "SCREEN", ttl)
			path, actor, body := messagePolicyPath, 1, ""
			if c.method == "PUT" {
				body = policyNativePutBody(1, "SCREEN", policyNativeNow(t, f).Add(time.Hour))
			}
			if c.decision {
				path += "/decisions/" + f.accountIDs[1]
				actor = 0
			}
			a := &policyNativeFinalAccess{Store: f.store}
			a.after = func(ctx context.Context) {
				if c.expiry {
					f.exec(`SELECT pg_sleep(GREATEST(0,extract(epoch FROM (expires_at-clock_timestamp()))+0.03)) FROM agent_message_request_policies WHERE owner_id=$1`, f.accountIDs[1])
				} else if c.agentABA {
					f.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, f.agentIDs[1])
					f.exec(`UPDATE agents SET status='active' WHERE id=$1`, f.agentIDs[1])
				} else {
					f.exec(`UPDATE agent_message_request_policies SET native_revision=native_revision+1,incoming_requests='BLOCK' WHERE owner_id=$1`, f.accountIDs[1])
				}
			}
			h := New(f.store, a, nil, nil, nil, nil, nil, nil, nil, nil, f.store, nil, false, nil, nil, nil)
			w := policyNativeWire(t, f, h, c.method, path, body, actor)
			if w.Code != 409 || strings.Contains(w.Body.String(), `"data"`) {
				t.Fatalf("actual after-encoding %s status=%d want409, stale_data=%t", c.name, w.Code, strings.Contains(w.Body.String(), `"data"`))
			}
		})
	}
}

func policyNativePacket(t *testing.T, name, method, path, body string, f *privateProfileHTTPDBFixture, actor int, w *httptest.ResponseRecorder, extra map[string]any) {
	t.Helper()
	dir := os.Getenv("BIRDTIE_MESSAGE_POLICY_WIRE_DIR")
	if dir == "" {
		t.Fatal("actual packet directory required")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	meta := map[string]any{"method": method, "path": path, "body": json.RawMessage(body), "ownerId": f.accountIDs[actor], "agentId": f.agentIDs[actor], "httpStatus": w.Code, "observedAt": policyNativeNow(t, f), "syntheticOwnedOnly": true}
	if body == "" {
		meta["body"] = nil
	}
	for k, v := range extra {
		meta[k] = v
	}
	raw, e := json.MarshalIndent(meta, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	for suffix, bytes := range map[string][]byte{"response.json": w.Body.Bytes(), "request.json": append(raw, '\n')} {
		file, e := os.OpenFile(filepath.Join(dir, name+"-"+suffix), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		_, e = file.Write(bytes)
		closeErr := file.Close()
		if e != nil || closeErr != nil {
			t.Fatal(e, closeErr)
		}
	}
}

func TestMessagePolicyRegisteredNativeFourModesPackets(t *testing.T) {
	f := policyNativeHTTP(t)
	w := policyNativeWire(t, f, f.handler, "GET", messagePolicyPath, "", 1)
	if w.Code != 200 {
		t.Fatal("original unconfigured GET", w.Code)
	}
	policyNativePacket(t, "own-unconfigured", "GET", messagePolicyPath, "", f, 1, w, nil)
	for i, mode := range []mp.Disposition{mp.Request, mp.Block, mp.Screen} {
		body := policyNativePutBody(int64(i), string(mode), policyNativeNow(t, f).Add(time.Hour))
		w = policyNativeWire(t, f, f.handler, "PUT", messagePolicyPath, body, 1)
		if w.Code != 200 {
			t.Fatal("original native PUT", mode, w.Code)
		}
		policyNativePacket(t, "own-"+strings.ToLower(string(mode))+"-put", "PUT", messagePolicyPath, body, f, 1, w, nil)
		w = policyNativeWire(t, f, f.handler, "GET", messagePolicyPath, "", 1)
		if w.Code != 200 {
			t.Fatal("original configured GET", w.Code)
		}
		policyNativePacket(t, "own-"+strings.ToLower(string(mode))+"-get", "GET", messagePolicyPath, "", f, 1, w, nil)
		path := messagePolicyPath + "/decisions/" + f.accountIDs[1]
		w = policyNativeWire(t, f, f.handler, "GET", path, "", 0)
		var d struct {
			Data mp.Decision `json:"data"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &d) != nil || d.Data.Disposition != mode || d.Data.ScreeningAvailable || d.Data.AutomaticAcceptance {
			t.Fatal("native four-state decision", mode, w.Code)
		}
		policyNativePacket(t, "decision-"+strings.ToLower(string(mode)), "GET", path, "", f, 0, w, map[string]any{"peerId": f.accountIDs[1], "disposition": mode})
	}
	createBody, _ := json.Marshal(map[string]string{"recipientAccountId": f.accountIDs[1], "scope": "friend", "note": "合成人类明确申请"})
	w = policyNativeWire(t, f, f.handler, "POST", "/v1/me/connection-requests", string(createBody), 0)
	var pending struct {
		Data connection.Request `json:"data"`
	}
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &pending) != nil || pending.Data.ID == "" || pending.Data.State != "pending" || pending.Data.PolicyDisposition != "SCREEN" || pending.Data.ScreeningStatus != "PENDING_REVIEW" {
		t.Fatal("same original SCREEN create", w.Code)
	}
	id := pending.Data.ID
	w = policyNativeWire(t, f, f.handler, "GET", "/v1/me/connection-requests", "", 1)
	var list struct {
		Data []connection.Request `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Data) != 1 || list.Data[0].ID != id || list.Data[0].ScreeningStatus != "PENDING_REVIEW" {
		t.Fatal("original same SCREEN list", w.Code)
	}
	policyNativePacket(t, "request-screen-list", "GET", "/v1/me/connection-requests", "", f, 1, w, map[string]any{"requestId": id, "scope": "friend"})
	df := &decisionNativeFixture{privateProfileHTTPDBFixture: f}
	before := df.effects(t, id)
	if before.Notifications != 0 || before.Inbox != 0 || before.Ties != 0 || before.Conversations != 0 {
		t.Fatal("SCREEN caused automatic contact/delivery", before)
	}
	op := df.uuid(t)
	body := decisionNativeBody("accept", op)
	w = policyNativeWire(t, f, f.handler, "POST", decisionNativePath(id), body, 1)
	if w.Code != 200 {
		t.Fatal("original keyed human review", w.Code)
	}
	receipt := decisionNativeReceipt(t, w, f.accountIDs[1], id, op, "accept")
	if receipt.Status != "COMMITTED" {
		t.Fatal("actual immutable acceptance receipt missing")
	}
	policyNativePacket(t, "keyed-accept", "POST", decisionNativePath(id), body, f, 1, w, map[string]any{"requestId": id, "operationId": op, "action": "accept", "scope": "friend"})
	path := messagePolicyPath + "/decisions/" + f.accountIDs[1]
	w = policyNativeWire(t, f, f.handler, "GET", path, "", 0)
	var d struct {
		Data mp.Decision `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &d) != nil || d.Data.Disposition != mp.Allow {
		t.Fatal("ALLOW not actual accepted original Tie", w.Code)
	}
	policyNativePacket(t, "decision-allow", "GET", path, "", f, 0, w, map[string]any{"peerId": f.accountIDs[1], "disposition": "ALLOW", "requestId": id})
	df.assertNoOtherEffects(t)
}

func TestMessagePolicyRegisteredNativeSessionAndBlockABA(t *testing.T) {
	for _, c := range []struct {
		name, method, mutation string
		decision               bool
	}{
		{"get_session_revoke_restore", "GET", "session", false},
		{"put_session_revoke_restore", "PUT", "session", false},
		{"decision_session_revoke_restore", "GET", "session", true},
		{"get_method_restore", "GET", "method", false},
		{"put_method_restore", "PUT", "method", false},
		{"decision_method_restore", "GET", "method", true},
		{"decision_original_block_unblock", "GET", "block", true},
		{"get_live_idle_renewal", "GET", "idle", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := policyNativeHTTP(t)
			policyNativePut(t, f, 1, 0, "SCREEN", time.Hour)
			path, actor, body := messagePolicyPath, 1, ""
			if c.decision {
				actor = 0
				path += "/decisions/" + f.accountIDs[1]
			}
			if c.method == "PUT" {
				body = policyNativePutBody(1, "SCREEN", policyNativeNow(t, f).Add(time.Hour))
			}
			run := false
			a := &policyNativeFinalAccess{Store: f.store, after: func(context.Context) {
				run = true
				switch c.mutation {
				case "session":
					f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[actor])
					f.exec(`UPDATE sessions SET revoked_at=NULL WHERE account_id=$1`, f.accountIDs[actor])
				case "method":
					f.exec(`UPDATE sessions SET authentication_method='dev_phone' WHERE account_id=$1`, f.accountIDs[actor])
					f.exec(`UPDATE sessions SET authentication_method='test' WHERE account_id=$1`, f.accountIDs[actor])
				case "block":
					if e := f.store.BlockAccount(f.ctx, f.accountIDs[1], f.accountIDs[0]); e != nil {
						t.Fatal(e)
					}
					if e := f.store.UnblockAccount(f.ctx, f.accountIDs[1], f.accountIDs[0]); e != nil {
						t.Fatal(e)
					}
				case "idle":
					f.exec(`UPDATE sessions SET idle_expires_at=idle_expires_at+interval '1 hour' WHERE account_id=$1`, f.accountIDs[actor])
				}
			}}
			h := New(f.store, a, nil, nil, nil, nil, nil, nil, nil, nil, f.store, nil, false, nil, nil, nil)
			w := policyNativeWire(t, f, h, c.method, path, body, actor)
			want := 409
			if c.mutation == "idle" {
				want = 200
			}
			if !run || w.Code != want {
				t.Fatalf("original mutation executed=%t status=%d want=%d", run, w.Code, want)
			}
			if c.method == "PUT" {
				var revision int64
				if e := f.pool.QueryRow(f.ctx, `SELECT native_revision FROM agent_message_request_policies WHERE owner_id=$1`, f.accountIDs[1]).Scan(&revision); e != nil || revision != 2 {
					t.Fatal("response retirement must retain original committed PUT", revision, e)
				}
			}
		})
	}
}

func policyNativeDomain(t *testing.T) *decisionNativeFixture {
	t.Helper()
	f := policyNativeHTTP(t)
	df := &decisionNativeFixture{privateProfileHTTPDBFixture: f, city: "message-policy-owned-" + f.accountIDs[0]}
	f.exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label) VALUES($1,'独占合成城市','本地','GB','Europe/London','published','合成','urn:owned:message-policy','合成')`, df.city)
	return df
}
func policyNativeIntent(df *decisionNativeFixture) {
	df.exec(`INSERT INTO intents(owner_account_id,city_id,topic,available_from,available_until,time_zone,coarse_area_label,audience,state,expires_at,owner_confirmed_at) VALUES($1,$2,'合成独占公开意图',now(),now()+interval '1 hour','Europe/London','合成区域','public','active',now()+interval '2 hours',now())`, df.accountIDs[1], df.city)
}
func policyNativeCreateBody(df *decisionNativeFixture, scope string) string {
	m := map[string]string{"recipientAccountId": df.accountIDs[1], "scope": scope, "note": "合成人类明确申请"}
	if scope == "conversation" {
		m["cityId"] = df.city
	}
	raw, _ := json.Marshal(m)
	return string(raw)
}
func policyNativeCondition(t *testing.T, df *decisionNativeFixture, operation string) map[string]string {
	t.Helper()
	w := df.expect(t, "GET", "/v1/me/entity-actions/person/"+df.accountIDs[1], "", df.tokens[0], 200, nil)
	var v struct {
		Data ea.View `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || !v.Data.Valid() {
		t.Fatal("real original EntityActions contract")
	}
	available := false
	for _, a := range v.Data.Actions {
		if a.State == ea.Available {
			for _, op := range a.AllowedOperations {
				available = available || op == operation
			}
		}
	}
	if !available {
		t.Fatal("native current action unavailable", operation)
	}
	w = df.expect(t, "GET", messagePolicyPath+"/decisions/"+df.accountIDs[1], "", df.tokens[0], 200, nil)
	var p struct {
		Data mp.Decision `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &p) != nil {
		t.Fatal("real policy source")
	}
	h := map[string]string{"X-Birdtie-Action-Version": v.Data.SourceVersion, "X-Birdtie-Action-Until": v.Data.ValidUntil.Format(time.RFC3339Nano), "X-Birdtie-Action-Operation": operation}
	if p.Data.SourceVersion != "" {
		h["X-Birdtie-Message-Policy-Version"] = p.Data.SourceVersion
	}
	return h
}
func policyNativeEffects(t *testing.T, df *decisionNativeFixture) string {
	t.Helper()
	var result string
	if e := df.pool.QueryRow(df.ctx, `SELECT jsonb_build_array(
 (SELECT count(*) FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])),
 (SELECT count(*) FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])),
 (SELECT count(*) FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[])),
 (SELECT count(*) FROM conversation_messages WHERE sender_account_id=ANY($1::uuid[])),
 (SELECT count(*) FROM connection_request_policy_bindings WHERE recipient_id=ANY($1::uuid[])),
 (SELECT count(*) FROM audit_events WHERE actor_account_id=ANY($1::uuid[])))::text`, df.accountIDs).Scan(&result); e != nil {
		t.Fatal(e)
	}
	return result
}

func TestMessagePolicyRegisteredNativeOriginalDomainMatrix(t *testing.T) {
	for _, scope := range []string{"friend", "conversation"} {
		for _, mode := range []string{"REQUEST", "SCREEN", "BLOCK"} {
			for _, bound := range []bool{false, true} {
				name := scope + "/" + mode
				if bound {
					name += "/bound"
				} else {
					name += "/unbound"
				}
				t.Run(name, func(t *testing.T) {
					df := policyNativeDomain(t)
					policyNativePut(t, df.privateProfileHTTPDBFixture, 1, 0, mode, time.Hour)
					if scope == "conversation" {
						policyNativeIntent(df)
					}
					var headers map[string]string
					if bound {
						op := "REQUEST_FRIEND"
						if scope == "conversation" {
							op = "REQUEST_CONVERSATION"
						}
						headers = policyNativeCondition(t, df, op)
					}
					before := policyNativeEffects(t, df)
					want := 201
					if mode == "BLOCK" {
						want = 404
					}
					w := df.expect(t, "POST", "/v1/me/connection-requests", policyNativeCreateBody(df, scope), df.tokens[0], want, headers)
					if mode == "BLOCK" {
						if policyNativeEffects(t, df) != before {
							t.Fatal("BLOCK original writer produced effects")
						}
						return
					}
					var p struct {
						Data connection.Request `json:"data"`
					}
					if json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Data.State != "pending" || p.Data.Scope != scope || p.Data.PolicyDisposition != mode {
						t.Fatal("original pending identity/routing")
					}
					effects := df.effects(t, p.Data.ID)
					if effects.Ties != 0 || effects.Conversations != 0 || mode == "SCREEN" && (effects.Notifications != 0 || effects.Inbox != 0 || p.Data.ScreeningStatus != "PENDING_REVIEW") {
						t.Fatal("routing changed pending authority", effects)
					}
					// Exercise the original legacy no-key Current decision, not only103.
					w = df.expect(t, "POST", decisionNativePath(p.Data.ID), `{"action":"accept"}`, df.tokens[1], 200, nil)
					var accepted struct {
						Data connection.Request `json:"data"`
					}
					if json.Unmarshal(w.Body.Bytes(), &accepted) != nil || accepted.Data.ID != p.Data.ID || accepted.Data.State != "accepted" {
						t.Fatal("original no-key decision did not accept same request")
					}
					effects = df.effects(t, p.Data.ID)
					var conversation string
					if scope == "friend" {
						if effects.Ties != 1 || effects.Conversations != 0 {
							t.Fatal("friend accept must create only original Tie", effects)
						}
						var tie string
						if e := df.pool.QueryRow(df.ctx, `SELECT id FROM person_ties WHERE request_id=$1`, p.Data.ID).Scan(&tie); e != nil {
							t.Fatal(e)
						}
						var startHeaders map[string]string
						if bound {
							startHeaders = policyNativeCondition(t, df, "OPEN_CHAT")
						}
						w = df.expect(t, "POST", "/v1/me/ties/"+tie+"/conversation", "", df.tokens[0], 200, startHeaders)
						var c struct {
							Data connection.Conversation `json:"data"`
						}
						if json.Unmarshal(w.Body.Bytes(), &c) != nil || c.Data.ID == "" {
							t.Fatal("original friend Start")
						}
						conversation = c.Data.ID
						w = df.expect(t, "POST", "/v1/me/ties/"+tie+"/conversation", "", df.tokens[0], 200, nil)
						if json.Unmarshal(w.Body.Bytes(), &c) != nil || c.Data.ID != conversation {
							t.Fatal("Start must reuse original conversation")
						}
					} else {
						if effects.Ties != 0 || effects.Conversations != 1 || accepted.Data.ConversationID == "" {
							t.Fatal("scoped acceptance must not grant person Tie", effects)
						}
						conversation = accepted.Data.ConversationID
						policyNativePut(t, df.privateProfileHTTPDBFixture, 1, 1, "BLOCK", time.Hour)
						w = df.expect(t, "GET", messagePolicyPath+"/decisions/"+df.accountIDs[1], "", df.tokens[0], 200, nil)
						var d struct {
							Data mp.Decision `json:"data"`
						}
						if json.Unmarshal(w.Body.Bytes(), &d) != nil || d.Data.Disposition != mp.Block {
							t.Fatal("specific consent must not become global ALLOW")
						}
					}
					df.expect(t, "POST", "/v1/me/conversations/"+conversation+"/messages", `{"body":"本人明确合成发送"}`, df.tokens[0], 201, nil)
					var messages int
					if e := df.pool.QueryRow(df.ctx, `SELECT count(*) FROM conversation_messages WHERE conversation_id=$1`, conversation).Scan(&messages); e != nil || messages != 1 {
						t.Fatal("same original Send effect", messages, e)
					}
					before = policyNativeEffects(t, df)
					w = df.wire(df.handler, "POST", "/v1/me/conversations/"+conversation+"/messages", `{"body":"越权合成发送"}`, df.tokens[2], nil, nil)
					if w.Code < 400 || policyNativeEffects(t, df) != before {
						t.Fatal("third person bypassed dedicated ACL", w.Code)
					}
				})
			}
		}
	}
}

func policyNativeNewPeople(t *testing.T, df *decisionNativeFixture) []string {
	t.Helper()
	ids := []string{}
	for i := 0; i < 2; i++ {
		df.expect(t, "PUT", "/v1/me/new-people/consent", `{"enabled":true}`, df.tokens[i], 200, nil)
		body, _ := json.Marshal(newpeople.DraftInput{Title: "合成明确寻伴", Category: "owned-policy-" + df.accountIDs[0], Modality: "ONLINE", OnlinePlatform: "合成线上平台", ExpiresAt: policyNativeNow(t, df.privateProfileHTTPDBFixture).Add(24 * time.Hour)})
		w := df.expect(t, "POST", "/v1/me/new-people/intents", string(body), df.tokens[i], 201, nil)
		var v struct {
			Data socialintent.Record `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Data.ID == "" {
			t.Fatal("real original FIND_COMPANION draft")
		}
		ids = append(ids, v.Data.ID)
		df.expect(t, "POST", "/v1/me/social-intents/"+v.Data.ID+"/activate", `{"confirmed":true}`, df.tokens[i], 200, nil)
	}
	w := df.expect(t, "GET", "/v1/me/new-people/candidates?sourceIntentId="+ids[0], "", df.tokens[0], 200, nil)
	var r struct {
		Data newpeople.Response `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &r) != nil || len(r.Data.Candidates) != 1 || r.Data.Candidates[0].CandidateIntentID != ids[1] || r.Data.Candidates[0].AccountID != df.accountIDs[1] {
		t.Fatal("real current source/candidate matching", w.Body.String())
	}
	return ids
}
func TestMessagePolicyRegisteredNativeNewPeopleMatrix(t *testing.T) {
	for _, mode := range []string{"REQUEST", "SCREEN", "BLOCK", "consent_revoked", "source_expired", "unconfirmed", "session_revoked"} {
		t.Run(mode, func(t *testing.T) {
			df := policyNativeDomain(t)
			policy := mode
			if policy != "REQUEST" && policy != "SCREEN" && policy != "BLOCK" {
				policy = "SCREEN"
			}
			policyNativePut(t, df.privateProfileHTTPDBFixture, 1, 0, policy, time.Hour)
			ids := policyNativeNewPeople(t, df)
			body, _ := json.Marshal(map[string]any{"sourceIntentId": ids[0], "candidateIntentId": ids[1], "note": "明确合成邀请", "confirmed": mode != "unconfirmed"})
			want := 201
			switch mode {
			case "BLOCK":
				want = 404
			case "consent_revoked":
				df.expect(t, "PUT", "/v1/me/new-people/consent", `{"enabled":false}`, df.tokens[1], 200, nil)
				want = 404
			case "source_expired":
				df.exec(`UPDATE social_intents SET expires_at=clock_timestamp()-interval '1 microsecond' WHERE id=$1`, ids[0])
				want = 404
			case "unconfirmed":
				want = 400
			case "session_revoked":
				df.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, df.accountIDs[0])
				want = 401
			}
			before := policyNativeEffects(t, df)
			w := df.expect(t, "POST", "/v1/me/new-people/invitations", string(body), df.tokens[0], want, nil)
			if want != 201 {
				if policyNativeEffects(t, df) != before {
					t.Fatal("original invitation refusal wrote domain effects")
				}
				return
			}
			var p struct {
				Data connection.Request `json:"data"`
			}
			if json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Data.Scope != "friend" || p.Data.State != "pending" || p.Data.PolicyDisposition != mode {
				t.Fatal("NewPeople must use same original friend Request")
			}
			e := df.effects(t, p.Data.ID)
			if e.Ties != 0 || e.Conversations != 0 || mode == "SCREEN" && (e.Notifications != 0 || e.Inbox != 0) {
				t.Fatal("original NP routing created unauthorized contact", e)
			}
		})
	}
}

func TestMessagePolicyRegisteredNativeBoundConditions(t *testing.T) {
	for _, change := range []string{"policy_ABA", "profile_ABA", "session_ABA", "live_idle_extension", "wrong_operation", "missing_until", "foreign_session", "natural_expiry"} {
		t.Run(change, func(t *testing.T) {
			df := policyNativeDomain(t)
			policyNativePut(t, df.privateProfileHTTPDBFixture, 1, 0, "SCREEN", time.Hour)
			policyNativeIntent(df)
			if change == "natural_expiry" {
				df.exec(`UPDATE intents SET available_until=clock_timestamp()+interval '1 second',expires_at=clock_timestamp()+interval '1 second' WHERE owner_account_id=$1`, df.accountIDs[1])
			}
			headers := policyNativeCondition(t, df, "REQUEST_FRIEND")
			want := 409
			token := df.tokens[0]
			switch change {
			case "policy_ABA":
				policyNativePut(t, df.privateProfileHTTPDBFixture, 1, 1, "REQUEST", time.Hour)
				policyNativePut(t, df.privateProfileHTTPDBFixture, 1, 2, "SCREEN", time.Hour)
			case "profile_ABA":
				df.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, df.accountIDs[1])
				df.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, df.accountIDs[1])
			case "session_ABA":
				df.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, df.accountIDs[0])
				df.exec(`UPDATE sessions SET revoked_at=NULL WHERE account_id=$1`, df.accountIDs[0])
			case "live_idle_extension":
				var oldGeneration, newGeneration int64
				var oldIdle, newIdle time.Time
				if e := df.pool.QueryRow(df.ctx, `SELECT authorization_generation,idle_expires_at FROM sessions WHERE account_id=$1`, df.accountIDs[0]).Scan(&oldGeneration, &oldIdle); e != nil {
					t.Fatal(e)
				}
				// Use the original registered authentication renewal, within its
				// native idle window. An artificial extension beyond that window
				// would be shortened on Authenticate and correctly retire proof.
				df.expect(t, "GET", messagePolicyPath, "", df.tokens[0], 200, nil)
				if e := df.pool.QueryRow(df.ctx, `SELECT authorization_generation,idle_expires_at FROM sessions WHERE account_id=$1`, df.accountIDs[0]).Scan(&newGeneration, &newIdle); e != nil || oldGeneration != newGeneration || !newIdle.After(oldIdle) {
					t.Fatal("original live authentication must extend idle without changing authorization", e)
				}
				want = 201
			case "wrong_operation":
				headers["X-Birdtie-Action-Operation"] = "OPEN_CHAT"
				want = 400
			case "missing_until":
				delete(headers, "X-Birdtie-Action-Until")
				want = 400
			case "foreign_session":
				token = df.newSession(df.accountIDs[0], false, false)
			case "natural_expiry":
				df.exec(`SELECT pg_sleep(GREATEST(0,extract(epoch FROM ($1::timestamptz-clock_timestamp()))+0.03))`, headers["X-Birdtie-Action-Until"])
			}
			before := policyNativeEffects(t, df)
			w := df.wire(df.handler, "POST", "/v1/me/connection-requests", policyNativeCreateBody(df, "friend"), token, headers, nil)
			if w.Code != want || (policyNativeEffects(t, df) != before) != (want == 201) {
				t.Fatalf("bound current source %s status=%d want%d domain_changed=%t", change, w.Code, want, policyNativeEffects(t, df) != before)
			}
		})
	}
}

type policyNativeBeforeWriter struct {
	*postgres.Store
	once   sync.Once
	before func()
}

func (p *policyNativeBeforeWriter) enter() { p.once.Do(p.before) }
func (p *policyNativeBeforeWriter) CreateFriendRequestCurrent(ctx context.Context, a ea.Access, peer, note, v string, b *ea.BoundCondition) (connection.Request, error) {
	p.enter()
	return p.Store.CreateFriendRequestCurrent(ctx, a, peer, note, v, b)
}
func (p *policyNativeBeforeWriter) CreateRequestCurrent(ctx context.Context, a ea.Access, peer, city, note, v string, b *ea.BoundCondition) (connection.Request, error) {
	p.enter()
	return p.Store.CreateRequestCurrent(ctx, a, peer, city, note, v, b)
}
func (p *policyNativeBeforeWriter) DecideRequestCurrent(ctx context.Context, a ea.Access, id, action string) (connection.Request, error) {
	p.enter()
	return p.Store.DecideRequestCurrent(ctx, a, id, action)
}
func (p *policyNativeBeforeWriter) StartFriendConversationCurrent(ctx context.Context, a ea.Access, id string, b *ea.BoundCondition) (connection.Conversation, error) {
	p.enter()
	return p.Store.StartFriendConversationCurrent(ctx, a, id, b)
}
func (p *policyNativeBeforeWriter) SendMessageCurrent(ctx context.Context, a ea.Access, id, body, kind, entity string) (connection.Message, error) {
	p.enter()
	return p.Store.SendMessageCurrent(ctx, a, id, body, kind, entity)
}
func (p *policyNativeBeforeWriter) InviteNewPeopleCurrent(ctx context.Context, a ea.Access, source, candidate, note, v string) (connection.Request, error) {
	p.enter()
	return p.Store.InviteNewPeopleCurrent(ctx, a, source, candidate, note, v)
}

func TestMessagePolicyRegisteredNativeCapturedWriterSessions(t *testing.T) {
	for _, writer := range []string{"CreateFriend", "CreateConversation", "BoundCreate", "Decide", "Start", "Send", "NewPeople"} {
		t.Run(writer, func(t *testing.T) {
			df := policyNativeDomain(t)
			policyNativePut(t, df.privateProfileHTTPDBFixture, 1, 0, "SCREEN", time.Hour)
			path, body, actor := "/v1/me/connection-requests", policyNativeCreateBody(df, "friend"), 0
			var headers map[string]string
			switch writer {
			case "CreateConversation":
				policyNativeIntent(df)
				body = policyNativeCreateBody(df, "conversation")
			case "BoundCreate":
				headers = policyNativeCondition(t, df, "REQUEST_FRIEND")
			case "Decide":
				id := df.request(t, "friend")
				path = decisionNativePath(id)
				body = `{"action":"accept"}`
				actor = 1
			case "Start", "Send":
				id := df.request(t, "friend")
				df.expect(t, "POST", decisionNativePath(id), `{"action":"accept"}`, df.tokens[1], 200, nil)
				var tie string
				if e := df.pool.QueryRow(df.ctx, `SELECT id FROM person_ties WHERE request_id=$1`, id).Scan(&tie); e != nil {
					t.Fatal(e)
				}
				path = "/v1/me/ties/" + tie + "/conversation"
				body = ""
				if writer == "Send" {
					w := df.expect(t, "POST", path, "", df.tokens[0], 200, nil)
					var c struct {
						Data connection.Conversation `json:"data"`
					}
					if json.Unmarshal(w.Body.Bytes(), &c) != nil {
						t.Fatal("real Start")
					}
					path = "/v1/me/conversations/" + c.Data.ID + "/messages"
					body = `{"body":"明确合成发送"}`
				}
			case "NewPeople":
				ids := policyNativeNewPeople(t, df)
				path = "/v1/me/new-people/invitations"
				raw, _ := json.Marshal(map[string]any{"sourceIntentId": ids[0], "candidateIntentId": ids[1], "note": "合成明确邀请", "confirmed": true})
				body = string(raw)
			}
			before := policyNativeEffects(t, df)
			entered := false
			p := &policyNativeBeforeWriter{Store: df.store, before: func() {
				entered = true
				df.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, df.accountIDs[actor])
			}}
			h := New(p, p, nil, nil, nil, nil, nil, p, nil, nil, p, nil, false, nil, nil, nil)
			w := df.wire(h, "POST", path, body, df.tokens[actor], headers, nil)
			if !entered || w.Code != 401 || policyNativeEffects(t, df) != before {
				t.Fatalf("captured %s entered=%t status=%d domain_changed=%t", writer, entered, w.Code, policyNativeEffects(t, df) != before)
			}
		})
	}
}

func TestMessagePolicyRegisteredNativeDedicatedACLAndBidirectionalBlock(t *testing.T) {
	for _, scope := range []string{"friend", "conversation"} {
		for _, direction := range []int{0, 1} {
			t.Run(scope+string(rune('0'+direction)), func(t *testing.T) {
				df := policyNativeDomain(t)
				policyNativePut(t, df.privateProfileHTTPDBFixture, 1, 0, "SCREEN", time.Hour)
				id := df.request(t, scope)
				w := df.expect(t, "POST", decisionNativePath(id), `{"action":"accept"}`, df.tokens[1], 200, nil)
				var r struct {
					Data connection.Request `json:"data"`
				}
				if json.Unmarshal(w.Body.Bytes(), &r) != nil {
					t.Fatal("native acceptance")
				}
				conversation := r.Data.ConversationID
				if scope == "friend" {
					var tie string
					if e := df.pool.QueryRow(df.ctx, `SELECT id FROM person_ties WHERE request_id=$1`, id).Scan(&tie); e != nil {
						t.Fatal(e)
					}
					w = df.expect(t, "POST", "/v1/me/ties/"+tie+"/conversation", "", df.tokens[0], 200, nil)
					var c struct {
						Data connection.Conversation `json:"data"`
					}
					if json.Unmarshal(w.Body.Bytes(), &c) != nil {
						t.Fatal("native Start")
					}
					conversation = c.Data.ID
				}
				blockBody, _ := json.Marshal(map[string]string{"accountId": df.accountIDs[1-direction]})
				df.expect(t, "POST", "/v1/me/blocks", string(blockBody), df.tokens[direction], 204, nil)
				before := policyNativeEffects(t, df)
				w = df.wire(df.handler, "POST", "/v1/me/conversations/"+conversation+"/messages", `{"body":"屏蔽后合成发送"}`, df.tokens[0], nil, nil)
				if w.Code != 404 || policyNativeEffects(t, df) != before {
					t.Fatal("directional Block bypassed original conversation", w.Code)
				}
				w = df.expect(t, "GET", messagePolicyPath+"/decisions/"+df.accountIDs[1], "", df.tokens[0], 200, nil)
				var d struct {
					Data mp.Decision `json:"data"`
				}
				if json.Unmarshal(w.Body.Bytes(), &d) != nil || d.Data.Disposition != mp.Block || d.Data.SourceVersion != "" {
					t.Fatal("Block must beat accepted consent without disclosure")
				}
			})
		}
	}
}

func TestMessagePolicyRegisteredNativeDecisionProofBoundary(t *testing.T) {
	for _, change := range []string{"tie_ABA", "profile_ABA", "agent_profile_ABA", "decision_expiry", "future_valid_from", "unknown_BLOCK"} {
		t.Run(change, func(t *testing.T) {
			df := policyNativeDomain(t)
			ttl := time.Hour
			if change == "decision_expiry" {
				ttl = time.Second
			}
			policyNativePut(t, df.privateProfileHTTPDBFixture, 1, 0, "SCREEN", ttl)
			if change == "tie_ABA" {
				id := df.request(t, "friend")
				df.expect(t, "POST", decisionNativePath(id), `{"action":"accept"}`, df.tokens[1], 200, nil)
			}
			if change == "future_valid_from" {
				df.exec(`UPDATE agent_message_request_policies SET native_revision=native_revision+1,valid_from=clock_timestamp()+interval '1 second',expires_at=clock_timestamp()+interval '1 hour' WHERE owner_id=$1`, df.accountIDs[1])
			}
			peer := df.accountIDs[1]
			if change == "unknown_BLOCK" {
				peer = df.uuid(t)
			}
			path := messagePolicyPath + "/decisions/" + peer
			if change == "unknown_BLOCK" {
				w := df.expect(t, "GET", path, "", df.tokens[0], 200, nil)
				var d struct {
					Data mp.Decision `json:"data"`
				}
				if json.Unmarshal(w.Body.Bytes(), &d) != nil || d.Data.Disposition != mp.Block || d.Data.SourceVersion != "" || !d.Data.ValidUntil.Equal(d.Data.ObservedAt) {
					t.Fatal("opaque BLOCK zero public lease must remain legal")
				}
				return
			}
			called := false
			a := &policyNativeFinalAccess{Store: df.store, after: func(context.Context) {
				called = true
				switch change {
				case "tie_ABA":
					df.exec(`UPDATE person_ties SET status='removed' WHERE person_a_account_id=LEAST($1::uuid,$2::uuid) AND person_b_account_id=GREATEST($1::uuid,$2::uuid)`, df.accountIDs[0], df.accountIDs[1])
					df.exec(`UPDATE person_ties SET status='active' WHERE person_a_account_id=LEAST($1::uuid,$2::uuid) AND person_b_account_id=GREATEST($1::uuid,$2::uuid)`, df.accountIDs[0], df.accountIDs[1])
				case "profile_ABA":
					df.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, df.accountIDs[1])
					df.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, df.accountIDs[1])
				case "agent_profile_ABA":
					df.exec(`UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=$1`, df.agentIDs[1])
				case "decision_expiry":
					df.exec(`SELECT pg_sleep(GREATEST(0,extract(epoch FROM (expires_at-clock_timestamp()))+0.03)) FROM agent_message_request_policies WHERE owner_id=$1`, df.accountIDs[1])
				case "future_valid_from":
					df.exec(`SELECT pg_sleep(GREATEST(0,extract(epoch FROM (valid_from-clock_timestamp()))+0.03)) FROM agent_message_request_policies WHERE owner_id=$1`, df.accountIDs[1])
				}
			}}
			h := New(df.store, a, nil, nil, nil, nil, nil, nil, nil, nil, df.store, nil, false, nil, nil, nil)
			w := df.wire(h, "GET", path, "", df.tokens[0], nil, nil)
			if !called || w.Code != 409 || strings.Contains(w.Body.String(), `"data"`) {
				t.Fatalf("old decision proof %s callback=%t status=%d", change, called, w.Code)
			}
		})
	}
}

func TestMessagePolicyRegisteredNativeFinalSessionWaitCannotExtendLease(t *testing.T) {
	df := policyNativeDomain(t)
	policyNativePut(t, df.privateProfileHTTPDBFixture, 1, 0, "SCREEN", 2*time.Second)
	locked := make(chan struct{})
	release := make(chan struct{})
	errC := make(chan error, 1)
	a := &policyNativeFinalAccess{Store: df.store, after: func(context.Context) {
		go func() {
			tx, e := df.pool.Begin(df.ctx)
			if e != nil {
				errC <- e
				close(locked)
				return
			}
			defer tx.Rollback(context.Background())
			_, e = tx.Exec(df.ctx, `SELECT id FROM sessions WHERE account_id=$1 FOR UPDATE`, df.accountIDs[1])
			close(locked)
			if e == nil {
				<-release
				e = tx.Commit(df.ctx)
			}
			errC <- e
		}()
		<-locked
	}}
	h := New(df.store, a, nil, nil, nil, nil, nil, nil, nil, nil, df.store, nil, false, nil, nil, nil)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- policyNativeWire(t, df.privateProfileHTTPDBFixture, h, "GET", messagePolicyPath, "", 1)
	}()
	<-locked
	decisionNativeWaitLock(t, df, "SELECT id FROM sessions")
	df.exec(`SELECT pg_sleep(GREATEST(0,extract(epoch FROM (expires_at-clock_timestamp()))+0.03)) FROM agent_message_request_policies WHERE owner_id=$1`, df.accountIDs[1])
	close(release)
	if e := <-errC; e != nil {
		t.Fatal("owned Session waiter", e)
	}
	w := <-done
	if w.Code != 409 || strings.Contains(w.Body.String(), `"data"`) {
		t.Fatal("original native session wait renewed expired policy response", w.Code)
	}
}
