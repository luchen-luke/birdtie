package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/jackc/pgx/v5"
)

// Synthetic owner declaration, registered routes and real PostgreSQL. This is
// not evidence that the account holder visited Aberdeen in the declared year.
func historicalHTTPFixture(t *testing.T) *privateProfileHTTPDBFixture {
	t.Helper()
	f := privateProfileHTTPDBNew(t)
	f.handler = New(f.store, f.store, nil, f.store, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{
			`DELETE FROM agent_domain_outbox WHERE source_type='MOMENT' AND source_id IN (SELECT id FROM moments WHERE author_account_id=ANY($1::uuid[]))`,
			`DELETE FROM audit_events WHERE resource_type='moment' AND actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM moments WHERE author_account_id=ANY($1::uuid[])`,
		} {
			if _, err := f.pool.Exec(ctx, q, f.accountIDs); err != nil {
				t.Errorf("owned historical Moment cleanup: %v", err)
			}
		}
	})
	return f
}
func historicalMomentBody(precision, occurred string, revision int64) string {
	raw := map[string]any{"cityId": "aberdeen-gb", "title": "2025年的阿伯丁旅行", "body": "仅本人声明；本地合成测试", "timePrecision": precision, "locationPrecision": "city"}
	if occurred != "" {
		raw["occurredAt"] = occurred
	}
	if revision != 0 {
		raw["revision"] = revision
	}
	body, _ := json.Marshal(raw)
	return string(body)
}
func historicalHTTPCall(f *privateProfileHTTPDBFixture, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rw := httptest.NewRecorder()
	f.handler.ServeHTTP(rw, req)
	return rw
}
func historicalHTTPRecord(t *testing.T, rw *httptest.ResponseRecorder, want int) content.Moment {
	t.Helper()
	if rw.Code != want {
		t.Fatalf("registered historical HTTP status=%d want=%d body=%s", rw.Code, want, rw.Body.String())
	}
	var envelope struct {
		Data content.Moment `json:"data"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}
func TestHistoricalMomentHTTPTimePrecision(t *testing.T) {
	f := historicalHTTPFixture(t)
	for _, tc := range []struct{ precision, occurred string }{
		{"unknown", ""}, {"year", "2025-01-01T00:00:00Z"}, {"month", "2025-09-01T00:00:00Z"},
		{"day", "2025-09-12T00:00:00Z"}, {"instant", "2025-09-12T09:30:12.123456Z"},
	} {
		t.Run(tc.precision, func(t *testing.T) {
			m := historicalHTTPRecord(t, historicalHTTPCall(f, "POST", "/v1/me/moments", historicalMomentBody(tc.precision, tc.occurred, 0), f.tokens[0]), 201)
			if m.TimePrecision != tc.precision || m.CreatedAt.IsZero() || m.Visibility != "private" || m.Status != "draft" {
				t.Fatal("historical create lost time/privacy contract")
			}
			if tc.occurred == "" {
				if m.OccurredAt != nil {
					t.Fatal("unknown filled from creation")
				}
			} else {
				want, _ := time.Parse(time.RFC3339Nano, tc.occurred)
				if m.OccurredAt == nil || !m.OccurredAt.Equal(want) || m.CreatedAt.Equal(want) {
					t.Fatal("occurrence and creation conflated")
				}
			}
			loaded := historicalHTTPRecord(t, historicalHTTPCall(f, "GET", "/v1/me/moments/"+m.ID, "", f.tokens[0]), 200)
			if loaded.TimePrecision != m.TimePrecision || !loaded.CreatedAt.Equal(m.CreatedAt) {
				t.Fatal("native reread changed historical record")
			}
			edited := historicalHTTPRecord(t, historicalHTTPCall(f, "PUT", "/v1/me/moments/"+m.ID, historicalMomentBody(tc.precision, tc.occurred, m.Revision), f.tokens[0]), 200)
			if edited.Revision != m.Revision+1 || !edited.CreatedAt.Equal(m.CreatedAt) {
				t.Fatal("edit altered created time or revision")
			}
			if rw := historicalHTTPCall(f, "PUT", "/v1/me/moments/"+m.ID, historicalMomentBody(tc.precision, tc.occurred, m.Revision), f.tokens[0]); rw.Code != 409 {
				t.Fatalf("stale version=%d", rw.Code)
			}
			if rw := historicalHTTPCall(f, "GET", "/v1/me/moments/"+m.ID, "", f.tokens[1]); rw.Code != 404 {
				t.Fatalf("other account read=%d", rw.Code)
			}
		})
	}
	for _, tc := range []struct{ precision, occurred string }{
		{"unknown", "2025-01-01T00:00:00Z"}, {"year", ""}, {"day", "2025-02-30T00:00:00Z"},
		{"other", "2025-01-01T00:00:00Z"}, {"instant", time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339Nano)},
		{"instant", "0001-01-01T00:30:00+01:00"},
	} {
		rw := historicalHTTPCall(f, "POST", "/v1/me/moments", historicalMomentBody(tc.precision, tc.occurred, 0), f.tokens[0])
		if rw.Code != 400 {
			t.Fatalf("invalid time accepted precision=%s status=%d", tc.precision, rw.Code)
		}
	}
	for _, actor := range []int{2, 3} {
		rw := historicalHTTPCall(f, "POST", "/v1/me/moments", historicalMomentBody("year", "2025-01-01T00:00:00Z", 0), f.tokens[actor])
		if rw.Code != 403 {
			t.Fatalf("non-person create=%d", rw.Code)
		}
	}
	if rw := historicalHTTPCall(f, "POST", "/v1/me/moments", historicalMomentBody("unknown", "", 0), ""); rw.Code != 401 {
		t.Fatalf("anonymous create=%d", rw.Code)
	}
}

func TestHistoricalMomentHTTPRevokeWhileWaiting(t *testing.T) {
	for _, mode := range []string{"revoke", "expire"} {
		t.Run(mode, func(t *testing.T) {
			f := historicalHTTPFixture(t)
			m := historicalHTTPRecord(t, historicalHTTPCall(f, "POST", "/v1/me/moments", historicalMomentBody("year", "2025-01-01T00:00:00Z", 0), f.tokens[0]), 201)
			effects := func() string {
				var value string
				if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object('row',(SELECT to_jsonb(m) FROM moments m WHERE id=$1),'audit',(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY a.id),'[]') FROM audit_events a WHERE resource_type='moment' AND resource_id=$1::text),'outbox',(SELECT coalesce(jsonb_agg(to_jsonb(o) ORDER BY o.event_id),'[]') FROM agent_domain_outbox o WHERE source_type='MOMENT' AND source_id=$1))::text`, m.ID).Scan(&value); err != nil {
					t.Fatal(err)
				}
				return value
			}
			before := effects()
			blocker, err := f.pool.Acquire(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Release()
			tx, err := blocker.BeginTx(f.ctx, pgx.TxOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			if _, err = tx.Exec(f.ctx, `SELECT id FROM moments WHERE id=$1 FOR UPDATE`, m.ID); err != nil {
				t.Fatal(err)
			}
			result := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				result <- historicalHTTPCall(f, "PUT", "/v1/me/moments/"+m.ID, historicalMomentBody("month", "2025-09-01T00:00:00Z", m.Revision), f.tokens[0])
			}()
			deadline := time.Now().Add(8 * time.Second)
			reached := false
			for time.Now().Before(deadline) {
				if err = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_pids(pid)) AND query LIKE '%moments%')`, int(blocker.Conn().PgConn().PID())).Scan(&reached); err != nil {
					t.Fatal(err)
				}
				if reached {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if !reached {
				t.Fatal("actual HTTP write did not reach native row lock")
			}
			if mode == "revoke" {
				f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[0])
			} else {
				f.exec(`UPDATE sessions SET created_at=clock_timestamp()-interval '4 hours', idle_expires_at=clock_timestamp()-interval '2 seconds', expires_at=clock_timestamp()-interval '1 second' WHERE account_id=$1`, f.accountIDs[0])
			}
			if err = tx.Commit(f.ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case rw := <-result:
				after := effects()
				t.Logf("LOCAL_SYNTHETIC_RED_OR_GREEN mode=%s realHTTP=%d momentAuditOutboxUnchanged=%t", mode, rw.Code, before == after)
				if rw.Code == 200 || before != after {
					t.Fatalf("late session %s committed historical edit: HTTP=%d effectsUnchanged=%t", mode, rw.Code, before == after)
				}
			case <-time.After(8 * time.Second):
				t.Fatal(fmt.Sprintf("historical %s request did not finish", mode))
			}
		})
	}
}

func historicalWaitBlocked(t *testing.T, f *privateProfileHTTPDBFixture, pid uint32, resource string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var reached bool
		if err := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_pids(pid)) AND query LIKE $2)`, int(pid), "%"+resource+"%").Scan(&reached); err != nil {
			t.Fatal(err)
		}
		if reached {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("registered HTTP did not reach actual native resource lock")
}
func historicalAllEffects(t *testing.T, f *privateProfileHTTPDBFixture) string {
	t.Helper()
	var value string
	if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
 'moments',(SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY m.id),'[]') FROM moments m WHERE author_account_id=ANY($1::uuid[])),
 'audit',(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY a.id),'[]') FROM audit_events a WHERE resource_type='moment' AND actor_account_id=ANY($1::uuid[])),
 'outbox',(SELECT coalesce(jsonb_agg(to_jsonb(o) ORDER BY o.event_id),'[]') FROM agent_domain_outbox o WHERE source_type='MOMENT' AND subject_id=ANY($1::uuid[])),
 'activities',(SELECT coalesce(jsonb_agg(to_jsonb(l) ORDER BY l.moment_id,l.activity_id),'[]') FROM moment_activity_links l JOIN moments m ON m.id=l.moment_id WHERE m.author_account_id=ANY($1::uuid[])),
 'communities',(SELECT coalesce(jsonb_agg(to_jsonb(l) ORDER BY l.moment_id),'[]') FROM moment_community_links l JOIN moments m ON m.id=l.moment_id WHERE m.author_account_id=ANY($1::uuid[])),
 'organizations',(SELECT coalesce(jsonb_agg(to_jsonb(l) ORDER BY l.moment_id),'[]') FROM moment_organization_links l JOIN moments m ON m.id=l.moment_id WHERE m.author_account_id=ANY($1::uuid[])))::text`, f.accountIDs).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
func historicalExpireByDatabaseClock(t *testing.T, f *privateProfileHTTPDBFixture) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		var expired bool
		if err := f.pool.QueryRow(f.ctx, `SELECT bool_and(expires_at<=clock_timestamp()) FROM sessions WHERE account_id=$1`, f.accountIDs[0]).Scan(&expired); err != nil {
			t.Fatal(err)
		}
		if expired {
			t.Log("LOCAL_SYNTHETIC_ONLY database clock reached real expires_at while resource lock held")
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("actual database clock did not reach session expiry")
}
func TestHistoricalMomentHTTPCurrentReadWithdrawAndCreate(t *testing.T) {
	for _, operation := range []string{"get", "list", "withdraw", "create"} {
		for _, mode := range []string{"revoke", "naturalExpiry", "cancel"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				f := historicalHTTPFixture(t)
				m := historicalHTTPRecord(t, historicalHTTPCall(f, "POST", "/v1/me/moments", historicalMomentBody("year", "2025-01-01T00:00:00Z", 0), f.tokens[0]), 201)
				before := historicalAllEffects(t, f)
				if mode == "naturalExpiry" {
					f.exec(`WITH n AS MATERIALIZED(SELECT clock_timestamp() t) UPDATE sessions SET expires_at=n.t+interval '1 second',idle_expires_at=n.t+interval '1 second' FROM n WHERE account_id=$1`, f.accountIDs[0])
				}
				blocker, err := f.pool.Acquire(f.ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer blocker.Release()
				tx, err := blocker.BeginTx(f.ctx, pgx.TxOptions{})
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(context.Background())
				resource := "moments"
				if operation == "create" {
					resource = "accounts"
					_, err = tx.Exec(f.ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, f.accountIDs[0])
				} else {
					_, err = tx.Exec(f.ctx, `SELECT id FROM moments WHERE id=$1 FOR UPDATE`, m.ID)
				}
				if err != nil {
					t.Fatal(err)
				}
				method, path, body := "GET", "/v1/me/moments/"+m.ID, ""
				if operation == "list" {
					path = "/v1/me/moments"
				}
				if operation == "withdraw" {
					method = "DELETE"
					path += fmt.Sprintf("?revision=%d", m.Revision)
				}
				if operation == "create" {
					method = "POST"
					path = "/v1/me/moments"
					body = historicalMomentBody("year", "2024-01-01T00:00:00Z", 0)
				}
				requestCtx, cancel := context.WithCancel(f.ctx)
				defer cancel()
				req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(requestCtx)
				req.Header.Set("Authorization", "Bearer "+f.tokens[0])
				req.Header.Set("Content-Type", "application/json")
				result := make(chan *httptest.ResponseRecorder, 1)
				go func() { rw := httptest.NewRecorder(); f.handler.ServeHTTP(rw, req); result <- rw }()
				historicalWaitBlocked(t, f, blocker.Conn().PgConn().PID(), resource)
				switch mode {
				case "revoke":
					f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[0])
				case "naturalExpiry":
					historicalExpireByDatabaseClock(t, f)
				case "cancel":
					cancel()
				}
				if err = tx.Commit(f.ctx); err != nil {
					t.Fatal(err)
				}
				select {
				case rw := <-result:
					want := http.StatusUnauthorized
					if mode == "cancel" {
						want = http.StatusServiceUnavailable
					}
					if rw.Code != want || historicalAllEffects(t, f) != before || strings.Contains(rw.Body.String(), m.Title) {
						t.Fatalf("late human %s/%s status=%d want=%d or payload/effect leak", operation, mode, rw.Code, want)
					}
					t.Logf("LOCAL_SYNTHETIC_ONLY actualHTTP=%d operation=%s mode=%s allMomentContextAuditOutboxUnchanged=true", rw.Code, operation, mode)
				case <-time.After(8 * time.Second):
					t.Fatal("registered current human request did not finish")
				}
			})
		}
	}
	t.Run("accountSuspendedWhileCreateWaits", func(t *testing.T) {
		f := historicalHTTPFixture(t)
		before := historicalAllEffects(t, f)
		blocker, err := f.pool.Acquire(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer blocker.Release()
		tx, err := blocker.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		if _, err = tx.Exec(f.ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, f.accountIDs[0]); err != nil {
			t.Fatal(err)
		}
		result := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			result <- historicalHTTPCall(f, "POST", "/v1/me/moments", historicalMomentBody("year", "2025-01-01T00:00:00Z", 0), f.tokens[0])
		}()
		historicalWaitBlocked(t, f, blocker.Conn().PgConn().PID(), "accounts")
		if _, err = tx.Exec(f.ctx, `UPDATE accounts SET status='suspended' WHERE id=$1`, f.accountIDs[0]); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}
		select {
		case rw := <-result:
			if rw.Code != 401 || historicalAllEffects(t, f) != before {
				t.Fatal("cached actor bypassed suspended account")
			}
		case <-time.After(8 * time.Second):
			t.Fatal("account status test stalled")
		}
	})
}
