package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	nq "github.com/birdtie/birdtie/apps/api/internal/nowcontextquery"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type onlineHTTPFixture struct {
	*privateProfileHTTPDBFixture
	contextID, intentID string
}

func onlineHTTPNative(t *testing.T) *onlineHTTPFixture {
	f := privateProfileHTTPDBNew(t)
	x := &onlineHTTPFixture{privateProfileHTTPDBFixture: f}
	e := f.pool.QueryRow(f.ctx, `INSERT INTO contexts(context_type,online_key) VALUES('ONLINE',$1) RETURNING id::text`, fmt.Sprintf("合成线上阅读-%d", time.Now().UnixNano())).Scan(&x.contextID)
	if e != nil {
		t.Fatal(e)
	}
	f.exec(`INSERT INTO person_contexts(person_account_id,context_id,relation) VALUES($1,$2,'interest')`, f.accountIDs[0], x.contextID)
	t.Cleanup(func() {
		f.exec(`DELETE FROM social_intents WHERE context_id=$1`, x.contextID)
		f.exec(`DELETE FROM agent_tasks WHERE context_id=$1`, x.contextID)
		f.exec(`DELETE FROM person_contexts WHERE context_id=$1`, x.contextID)
		f.exec(`DELETE FROM contexts WHERE id=$1`, x.contextID)
	})
	x.intentID = x.add(t, "PUBLIC", "ACTIVE", "合成线上阅读", f.accountIDs[1], time.Now().Add(time.Hour))
	return x
}
func (f *onlineHTTPFixture) add(t *testing.T, audience, status, title, owner string, until time.Time) string {
	t.Helper()
	r, e := f.store.CreateSocialIntentDraft(f.ctx, owner, socialintent.DraftInput{Type: "FIND_COMPANION", Title: title, Constraints: json.RawMessage(`{}`), Audience: audience, Modality: "ONLINE", ContextID: f.contextID, ExpiresAt: until})
	if e != nil {
		t.Fatal(e)
	}
	if status == "ACTIVE" {
		r, e = f.store.ActivateSocialIntent(f.ctx, owner, r.ID)
		if e != nil {
			t.Fatal(e)
		}
	}
	return r.ID
}
func (f *onlineHTTPFixture) call(t *testing.T, method, path, body string, token int, want int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token >= 0 {
		r.Header.Set("Authorization", "Bearer "+f.tokens[token])
	}
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("%s %s status%d want%d body%s", method, path, w.Code, want, w.Body.String())
	}
	return w
}
func onlineHTTPDecode(t *testing.T, w *httptest.ResponseRecorder) nq.Response {
	t.Helper()
	var x struct{ Data nq.Response }
	if e := json.Unmarshal(w.Body.Bytes(), &x); e != nil {
		t.Fatal(e)
	}
	return x.Data
}
func (f *onlineHTTPFixture) body(query string, task *nq.Response) string {
	in := nq.Input{ContextID: f.contextID, Query: query}
	if task != nil {
		in.TaskID = task.Task.ID
		in.ExpectedTaskUpdatedAt = task.Task.UpdatedAt.Format(time.RFC3339Nano)
	}
	raw, _ := json.Marshal(in)
	return string(raw)
}
func TestNowOnlineHTTPNativeQueryRestoreAndStableOriginalIntent(t *testing.T) {
	f := onlineHTTPNative(t)
	f.add(t, "PRIVATE", "ACTIVE", "PRIVATE_CANARY阅读", f.accountIDs[1], time.Now().Add(time.Hour))
	f.add(t, "FRIENDS", "ACTIVE", "FRIENDS_CANARY阅读", f.accountIDs[1], time.Now().Add(time.Hour))
	f.add(t, "PUBLIC", "DRAFT", "DRAFT_CANARY阅读", f.accountIDs[1], time.Now().Add(time.Hour))
	contexts := f.call(t, "GET", "/v1/me/now/online-contexts", "", 0, 200)
	if !strings.Contains(contexts.Body.String(), f.contextID) {
		t.Fatal("missing actual declared context")
	}
	w := f.call(t, "POST", "/v1/me/now/online/tasks", f.body("帮我找线上阅读", nil), 0, 200)
	r := onlineHTTPDecode(t, w)
	if r.Task == nil || r.Task.CityID != "" || r.Task.ContextID != f.contextID || r.Task.ContextType != "ONLINE" || len(r.Items) != 1 || r.Items[0].ID != f.intentID || r.ModelAccess != "UNAVAILABLE" || r.Promotion {
		t.Fatalf("invalid actual result %+v", r)
	}
	for _, private := range []string{"CANARY", "creatorAccountId", "constraints", "platform", "latitude", "Proof", "Seal"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatalf("leaked %s", private)
		}
	}
	following := onlineHTTPDecode(t, f.call(t, "POST", "/v1/me/now/online/tasks", f.body("阅读", &r), 0, 200))
	if following.Task.ID != r.Task.ID || len(following.Task.Conversation) != 4 {
		t.Fatal("new shadow task or lost conversation")
	}
	f.call(t, "POST", "/v1/me/now/online/tasks", f.body("阅读", &r), 0, 409)
	got := onlineHTTPDecode(t, f.call(t, "GET", "/v1/me/now/online/tasks/"+r.Task.ID, "", 0, 200))
	if got.Task.ID != r.Task.ID || got.Task.UpdatedAt != following.Task.UpdatedAt {
		t.Fatal("restore wrote task")
	}
	original := onlineHTTPDecode(t, f.call(t, "GET", "/v1/me/now/online/intents/"+f.intentID, "", 0, 200))
	if len(original.Items) != 1 || original.Items[0].ID != f.intentID || original.Task != nil {
		t.Fatal("detail shadow entity")
	}
	f.call(t, "GET", "/v1/me/now/online/tasks/"+r.Task.ID, "", 1, 404)
	f.exec(`UPDATE social_intents SET status='CANCELLED',updated_at=clock_timestamp() WHERE id=$1`, f.intentID)
	empty := onlineHTTPDecode(t, f.call(t, "GET", "/v1/me/now/online/tasks/"+r.Task.ID, "", 0, 200))
	if len(empty.Items) != 0 || !strings.Contains(empty.Answer, "没有找到") {
		t.Fatal("stale source")
	}
	f.call(t, "GET", "/v1/me/now/online/intents/"+f.intentID, "", 0, 404)
}
func TestNowOnlineHTTPNativeWireAndAccountBoundaries(t *testing.T) {
	f := onlineHTTPNative(t)
	body := f.body("阅读", nil)
	for _, x := range []struct {
		name, path, body string
		who, want        int
	}{
		{"anonymous", "/v1/me/now/online/tasks", body, -1, 401}, {"foreignContext", "/v1/me/now/online/tasks", body, 1, 404}, {"org", "/v1/me/now/online/tasks", body, 2, 403}, {"business", "/v1/me/now/online/tasks", body, 3, 403},
		{"query", "/v1/me/now/online/tasks?owner=x", body, 0, 400}, {"forceQuery", "/v1/me/now/online/tasks?", body, 0, 400}, {"duplicate", "/v1/me/now/online/tasks", strings.Replace(body, `"query":"阅读"`, `"query":"阅读","query":"reading"`, 1), 0, 400},
	} {
		t.Run(x.name, func(t *testing.T) { f.call(t, "POST", x.path, x.body, x.who, x.want) })
	}
	f.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, f.agentIDs[0])
	f.call(t, "POST", "/v1/me/now/online/tasks", body, 0, 403)
}
func TestNowOnlineHTTPNativeReceiptSourceAndAuthorityABA(t *testing.T) {
	for _, kind := range []string{"intent", "declaration", "context", "agent", "owner", "session"} {
		t.Run(kind, func(t *testing.T) {
			f := onlineHTTPNative(t)
			digest, e := identity.ParseBearer("Bearer " + f.tokens[0])
			if e != nil {
				t.Fatal(e)
			}
			a := nq.Access{Digest: digest, Actor: identity.Actor{ID: f.accountIDs[0], AccountType: "person"}}
			r, e := f.store.QueryOwnPublicOnline(f.ctx, a, nq.Input{ContextID: f.contextID, Query: "阅读"})
			if e != nil {
				t.Fatal(e)
			}
			if e = f.store.RevalidateOwnPublicOnline(f.ctx, a, r); e != nil {
				t.Fatal("unchanged receipt", e)
			}
			switch kind {
			case "intent":
				f.exec(`UPDATE social_intents SET status='CANCELLED' WHERE id=$1`, f.intentID)
				f.exec(`UPDATE social_intents SET status='ACTIVE' WHERE id=$1`, f.intentID)
			case "declaration":
				f.exec(`DELETE FROM person_contexts WHERE person_account_id=$1 AND context_id=$2`, a.Actor.ID, f.contextID)
				f.exec(`INSERT INTO person_contexts(person_account_id,context_id,relation) VALUES($1,$2,'interest')`, a.Actor.ID, f.contextID)
			case "context":
				f.exec(`UPDATE contexts SET online_key=online_key||'-b' WHERE id=$1`, f.contextID)
				f.exec(`UPDATE contexts SET online_key=replace(online_key,'-b','') WHERE id=$1`, f.contextID)
			case "agent":
				f.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, f.agentIDs[0])
				f.exec(`UPDATE agents SET status='active' WHERE id=$1`, f.agentIDs[0])
			case "owner":
				f.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, a.Actor.ID)
				f.exec(`UPDATE accounts SET status='active' WHERE id=$1`, a.Actor.ID)
			case "session":
				f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, digest[:])
				f.exec(`UPDATE sessions SET revoked_at=NULL,created_at=created_at+interval '1 microsecond' WHERE token_sha256=$1`, digest[:])
			}
			if e = f.store.RevalidateOwnPublicOnline(f.ctx, a, r); e == nil {
				t.Fatal("ABA revived sealed source")
			}
		})
	}
}
func TestNowOnlineHTTPNativePoolWaitExpiredSessionNoTask(t *testing.T) {
	f := onlineHTTPNative(t)
	digest, _ := identity.ParseBearer("Bearer " + f.tokens[0])
	a := nq.Access{Digest: digest, Actor: identity.Actor{ID: f.accountIDs[0], AccountType: "person"}}
	f.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '400 milliseconds' WHERE token_sha256=$1`, digest[:])
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(f.ctx, `LOCK TABLE social_intents IN ACCESS EXCLUSIVE MODE`); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		_, e := f.store.QueryOwnPublicOnline(f.ctx, a, nq.Input{ContextID: f.contextID, Query: "阅读"})
		done <- e
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var wait bool
		e = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)))`, int(tx.Conn().PgConn().PID())).Scan(&wait)
		if e != nil {
			t.Fatal(e)
		}
		if wait {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("not actually waiting")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(550 * time.Millisecond)
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	if e = <-done; !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatalf("expired wait %v", e)
	}
	var n int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agent_tasks WHERE context_id=$1`, f.contextID).Scan(&n); e != nil || n != 0 {
		t.Fatal("unauthorized task residue", n, e)
	}
}

func TestNowOnlineHTTPNativeCurrentSourceAfterSessionWait(t *testing.T) {
	for _, event := range []string{"block", "creatorSuspended", "sourceExpired"} {
		t.Run(event, func(t *testing.T) {
			f := onlineHTTPNative(t)
			digest, _ := identity.ParseBearer("Bearer " + f.tokens[0])
			a := nq.Access{Digest: digest, Actor: identity.Actor{ID: f.accountIDs[0], AccountType: "person"}}
			if event == "sourceExpired" {
				f.exec(`UPDATE social_intents SET expires_at=clock_timestamp()+interval '400 milliseconds' WHERE id=$1`, f.intentID)
			}
			tx, e := f.pool.Begin(f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(context.Background())
			if _, e = tx.Exec(f.ctx, `SELECT id FROM sessions WHERE token_sha256=$1 FOR UPDATE`, digest[:]); e != nil {
				t.Fatal(e)
			}
			type outcome struct {
				r nq.Receipt
				e error
			}
			done := make(chan outcome, 1)
			go func() {
				r, e := f.store.QueryOwnPublicOnline(f.ctx, a, nq.Input{ContextID: f.contextID, Query: "阅读"})
				done <- outcome{r, e}
			}()
			until := time.Now().Add(5 * time.Second)
			for {
				var blocked bool
				e = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)))`, int(tx.Conn().PgConn().PID())).Scan(&blocked)
				if e != nil {
					t.Fatal(e)
				}
				if blocked {
					break
				}
				if time.Now().After(until) {
					t.Fatal("actual Session row barrier not reached")
				}
				time.Sleep(10 * time.Millisecond)
			}
			switch event {
			case "block":
				f.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.accountIDs[1], f.accountIDs[0])
			case "creatorSuspended":
				f.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.accountIDs[1])
			case "sourceExpired":
				time.Sleep(550 * time.Millisecond)
			}
			if e = tx.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			result := <-done
			if result.e != nil {
				t.Fatal(result.e)
			}
			if len(result.r.Response.Items) != 0 {
				t.Fatal("late changed source leaked after actual Session wait")
			}
		})
	}
}

func TestNowOnlineHTTPNativeReceiptTamperingAndOrdinaryIdleRefresh(t *testing.T) {
	f := onlineHTTPNative(t)
	digest, _ := identity.ParseBearer("Bearer " + f.tokens[0])
	a := nq.Access{Digest: digest, Actor: identity.Actor{ID: f.accountIDs[0], AccountType: "person"}}
	r, e := f.store.QueryOwnPublicOnline(f.ctx, a, nq.Input{ContextID: f.contextID, Query: "阅读"})
	if e != nil {
		t.Fatal(e)
	}
	f.exec(`UPDATE sessions SET idle_expires_at=LEAST(expires_at,clock_timestamp()+interval '30 minutes') WHERE token_sha256=$1`, digest[:])
	if e = f.store.RevalidateOwnPublicOnline(f.ctx, a, r); e != nil {
		t.Fatal("normal idle update wrongly changed authority", e)
	}
	forged := r
	forged.Response.Answer = "私人来源已获批准"
	if e = f.store.RevalidateOwnPublicOnline(f.ctx, a, forged); !errors.Is(e, nq.ErrDenied) {
		t.Fatal("forged receipt accepted", e)
	}
	wrong := a
	wrong.Actor.ID = f.accountIDs[1]
	if e = f.store.RevalidateOwnPublicOnline(f.ctx, wrong, r); !errors.Is(e, nq.ErrDenied) {
		t.Fatal("receipt crossed owner", e)
	}
	f.exec(`UPDATE social_intents SET title='已改稿',updated_at=clock_timestamp() WHERE id=$1`, f.intentID)
	if e = f.store.RevalidateOwnPublicOnline(f.ctx, a, r); e == nil {
		t.Fatal("old exact source version accepted")
	}
}

// Actual native rows change between Store assembly and the registered HTTP
// response. This test barrier does not manufacture a permission or source DTO.
type onlineOptionsAfterRead struct {
	*postgres.Store
	after func()
}

func (p *onlineOptionsAfterRead) ListOwnOnlineContexts(ctx context.Context, a nq.Access) (nq.ContextListReceipt, error) {
	receipt, e := p.Store.ListOwnOnlineContexts(ctx, a)
	if e == nil {
		p.after()
	}
	return receipt, e
}
func TestNowOnlineHTTPNativeContextOptionsChangedBeforeWire(t *testing.T) {
	for _, change := range []string{"withdraw", "declarationABA", "contextABA", "agentABA"} {
		t.Run(change, func(t *testing.T) {
			f := onlineHTTPNative(t)
			p := &onlineOptionsAfterRead{Store: f.store, after: func() {
				switch change {
				case "withdraw":
					f.exec(`DELETE FROM person_contexts WHERE person_account_id=$1 AND context_id=$2`, f.accountIDs[0], f.contextID)
				case "declarationABA":
					f.exec(`DELETE FROM person_contexts WHERE person_account_id=$1 AND context_id=$2`, f.accountIDs[0], f.contextID)
					f.exec(`INSERT INTO person_contexts(person_account_id,context_id,relation) VALUES($1,$2,'interest')`, f.accountIDs[0], f.contextID)
				case "contextABA":
					f.exec(`UPDATE contexts SET online_key=online_key||'-b' WHERE id=$1`, f.contextID)
					f.exec(`UPDATE contexts SET online_key=replace(online_key,'-b','') WHERE id=$1`, f.contextID)
				case "agentABA":
					f.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, f.agentIDs[0])
					f.exec(`UPDATE agents SET status='active' WHERE id=$1`, f.agentIDs[0])
				}
			}}
			f.handler = New(p, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, false, nil, f.pool, nil)
			w := f.call(t, "GET", "/v1/me/now/online-contexts", "", 0, 409)
			if strings.Contains(w.Body.String(), f.contextID) || strings.Contains(w.Body.String(), "合成线上阅读") {
				t.Fatal("withdrawn private context leaked")
			}
		})
	}
}
