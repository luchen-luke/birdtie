package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNotificationDestinationHTTPCurrentTaskIntegration(t *testing.T) {
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" || os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("Task HTTP integration requires explicitly disposable PostgreSQL")
	}
	// ACCESS EXCLUSIVE must not block an unrelated package's Task queries.
	// Reuse the actual-migration disposable DB helper; no authority is mocked.
	v4PrivacyHTTPDatabase(t)
	for _, mode := range []string{"positive", "foreign", "anonymous", "retiredDuringTaskWait", "revokedDuringTaskWait"} {
		t.Run(mode, func(t *testing.T) {
			f := notificationPolicyHTTPDBNew(t)
			// Only the real request Store uses this tag. Fixture writes, the locker
			// and observation queries retain the original pool and cleanup order.
			config := f.pool.Config().Copy()
			applicationName := "birdtie_task_request_" + f.accountIDs[0]
			config.ConnConfig.RuntimeParams["application_name"] = applicationName
			requestPool, err := pgxpool.NewWithConfig(f.ctx, config)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(requestPool.Close)
			requestStore := postgres.New(requestPool, false)
			handler := New(requestStore, requestStore, requestStore, requestStore, requestStore, requestStore, requestStore, requestStore, requestStore, requestStore, requestStore, requestStore, false, nil, requestPool, nil)
			task, err := f.store.SaveTask(f.ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: f.accountIDs[0], ActingUserID: f.accountIDs[0], CityID: "aberdeen-gb", Query: "原本人的任务", Intent: "FIND_ACTIVITY", Status: agentworkspace.TaskActive, Filters: map[string]string{}, Conversation: []agentworkspace.Message{}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				for _, q := range []string{`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`, `DELETE FROM native_notification_decisions WHERE recipient_id=ANY($1::uuid[])`, `DELETE FROM agent_tasks WHERE owner_account_id=ANY($1::uuid[])`} {
					if _, e := f.pool.Exec(ctx, q, f.accountIDs); e != nil {
						t.Error(e)
					}
				}
			})
			task.Status = agentworkspace.TaskCompleted
			task, err = f.store.UpdateTask(f.ctx, task)
			if err != nil {
				t.Fatal(err)
			}
			token := f.tokens[0]
			if mode == "foreign" {
				token = f.tokens[1]
			}
			call := func(ctx context.Context) *httptest.ResponseRecorder {
				r := httptest.NewRequest("GET", "/v1/me/agent-tasks/"+task.ID, nil).WithContext(ctx)
				if mode != "anonymous" {
					r.Header.Set("Authorization", "Bearer "+token)
				}
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				return w
			}
			want := 200
			if mode == "foreign" {
				want = 404
			}
			if mode == "anonymous" {
				want = 401
			}
			var w *httptest.ResponseRecorder
			if mode == "retiredDuringTaskWait" || mode == "revokedDuringTaskWait" {
				lock, e := f.pool.Begin(f.ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer lock.Rollback(context.Background())
				if _, e = lock.Exec(f.ctx, `LOCK TABLE agent_tasks IN ACCESS EXCLUSIVE MODE`); e != nil {
					t.Fatal(e)
				}
				// Demonstrate the old any-waiter barrier can be reached before this
				// HTTP request even starts. It must not authorize the retirement step.
				unrelated, e := f.pool.Acquire(f.ctx)
				if e != nil {
					t.Fatal(e)
				}
				waitCtx, stopWaiting := context.WithCancel(f.ctx)
				unrelatedDone := make(chan error, 1)
				done := make(chan *httptest.ResponseRecorder, 1)
				unrelatedConsumed, requestStarted, requestConsumed := false, false, false
				// Even Fatal paths cancel and unlock before releasing a connection
				// still executing native SQL or starting the fixture's cleanup.
				defer func() {
					stopWaiting()
					cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					_ = lock.Rollback(cleanupCtx)
					if !unrelatedConsumed {
						select {
						case <-unrelatedDone:
						case <-cleanupCtx.Done():
							t.Error("unrelated native waiter cleanup did not terminate")
						}
					}
					if requestStarted && !requestConsumed {
						select {
						case <-done:
						case <-cleanupCtx.Done():
							t.Error("HTTP native waiter cleanup did not terminate")
						}
					}
					unrelated.Release()
				}()
				go func() {
					_, err := unrelated.Exec(waitCtx, `SELECT count(*) FROM agent_tasks /* unrelated synthetic waiter */`)
					unrelatedDone <- err
				}()
				socialNotificationsWaitOwnedLock(t, f, lock)
				var unrelatedWaiting bool
				if e = f.pool.QueryRow(f.ctx, `SELECT $1::integer=ANY(pg_blocking_pids($2::integer))`, int(lock.Conn().PgConn().PID()), int(unrelated.Conn().PgConn().PID())).Scan(&unrelatedWaiting); e != nil || !unrelatedWaiting {
					t.Fatal("unrelated native waiter was not independently observed", e)
				}
				t.Logf("LOCAL_SYNTHETIC old any-waiter barrier reached before HTTP start: unrelatedPID=%d lockerPID=%d", unrelated.Conn().PgConn().PID(), lock.Conn().PgConn().PID())
				requestStarted = true
				go func() { done <- call(waitCtx) }()
				notificationTaskWaitExactNativeQuery(t, f, lock, applicationName)
				if mode == "retiredDuringTaskWait" {
					f.exec(`UPDATE agents SET status='retired' WHERE id=$1`, f.agentIDs[0])
					want = 404
				} else {
					f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[0])
					want = 401
				}
				if e = lock.Commit(f.ctx); e != nil {
					t.Fatal(e)
				}
				select {
				case e = <-unrelatedDone:
					unrelatedConsumed = true
					if e != nil {
						t.Fatal("unrelated query did not complete after unlock", e)
					}
				case <-time.After(10 * time.Second):
					t.Fatal("unrelated native query did not terminate")
				}
				select {
				case w = <-done:
					requestConsumed = true
				case <-time.After(10 * time.Second):
					t.Fatal("waiting original task did not terminate")
				}
			} else {
				w = call(f.ctx)
			}
			if w.Code != want {
				t.Fatalf("task current boundary got %d want %d: %s", w.Code, want, w.Body)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("task response cacheable")
			}
			if want == 200 {
				var wire struct {
					Data struct {
						TaskID string              `json:"taskId"`
						Task   agentworkspace.Task `json:"task"`
					}
				}
				if json.Unmarshal(w.Body.Bytes(), &wire) != nil || wire.Data.TaskID != task.ID || wire.Data.Task.ID != task.ID || wire.Data.Task.PrincipalID != f.accountIDs[0] {
					t.Fatal("wrong native restored task")
				}
			} else {
				var wire map[string]any
				_ = json.Unmarshal(w.Body.Bytes(), &wire)
				if wire["data"] != nil {
					t.Fatal("denied response released task")
				}
			}
		})
	}
}

// Observe the original Store.GetTask query after real HasActiveAgent passed.
// Unique pool identity, exact native SELECT and the ungranted relation lock
// jointly exclude other requests and the later FOR SHARE validation query.
func notificationTaskWaitExactNativeQuery(t *testing.T, f *privateProfileHTTPDBFixture, lock pgx.Tx, applicationName string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var pid int
		var statement string
		err := f.pool.QueryRow(f.ctx, `SELECT a.pid,a.query FROM pg_stat_activity a
 WHERE a.datname=current_database() AND a.application_name=$1
 AND $2::integer=ANY(pg_blocking_pids(a.pid))
 AND a.query LIKE '%FROM agent_tasks%'
 AND a.query LIKE '%WHERE id = $1 AND owner_account_id = $2%'
 AND a.query NOT LIKE '%FOR SHARE%'
 AND EXISTS(SELECT 1 FROM pg_locks l WHERE l.pid=a.pid
   AND l.relation='agent_tasks'::regclass AND l.mode='AccessShareLock' AND NOT l.granted)`, applicationName, int(lock.Conn().PgConn().PID())).Scan(&pid, &statement)
		if err == nil {
			t.Logf("LOCAL_SYNTHETIC exact native GetTask barrier: requestPID=%d lockerPID=%d application=%s SQL=%q", pid, lock.Conn().PgConn().PID(), applicationName, statement)
			return
		}
		if err != pgx.ErrNoRows {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("exact registered HTTP request native GetTask lock barrier not reached")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
