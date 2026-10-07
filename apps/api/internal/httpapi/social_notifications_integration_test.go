package httpapi

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"net/http/httptest"
	"testing"
	"time"
)

// Offline transport companion only. It preserves the existing private/policy
// spies' exact actor and failures; native current authority is proven below.
func (a *privateProfileHTTPAccess) AuthenticateHumanSocial(ctx context.Context, digest [32]byte) (identity.Actor, error) {
	return a.Authenticate(ctx, digest)
}
func (a *privateProfileHTTPAccess) ValidateHumanSocialResponse(ctx context.Context, digest [32]byte, actor identity.Actor) error {
	current, err := a.Authenticate(ctx, digest)
	if err != nil {
		return err
	}
	if current.ID != actor.ID || current.AccountType != actor.AccountType {
		return identity.ErrUnauthorized
	}
	return ctx.Err()
}

// Owned native rows and registered handlers. No fake authority is used here.
func TestSocialNotificationsHTTPCurrentSession(t *testing.T) {
	for _, mode := range []string{"listSourceWaitRevoke", "readRowWaitRevoke", "policyInitialIdleWait"} {
		t.Run(mode, func(t *testing.T) {
			f := notificationPolicyHTTPDBNew(t)
			handler := New(f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, false, nil, f.pool, nil)
			method, path := "GET", "/v1/me/inbox"
			token := f.tokens[0]
			digest, e := identity.ParseBearer("Bearer " + token)
			if e != nil {
				t.Fatal(e)
			}
			var itemID string
			if mode != "policyInitialIdleWait" {
				if _, e = f.store.CreateFriendRequest(f.ctx, f.accountIDs[1], f.accountIDs[0], "Owned synthetic invitation"); e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					for _, q := range []string{
						`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`,
						`DELETE FROM native_notification_decisions WHERE recipient_id=ANY($1::uuid[])`,
						`DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`,
					} {
						if _, err := f.pool.Exec(ctx, q, f.accountIDs); err != nil {
							t.Error(err)
						}
					}
				})
				if e = f.pool.QueryRow(f.ctx, `SELECT id FROM inbox_items WHERE recipient_account_id=$1 AND resource_type='connection_request'`, f.accountIDs[0]).Scan(&itemID); e != nil {
					t.Fatal(e)
				}
				if mode == "readRowWaitRevoke" {
					method, path = "POST", "/v1/me/inbox/"+itemID+"/read"
				}
			} else {
				path = "/v1/me/notification-policy"
				f.exec(`WITH n AS MATERIALIZED(SELECT clock_timestamp() t) UPDATE sessions SET idle_expires_at=n.t+interval '2 seconds',expires_at=n.t+interval '1 hour' FROM n WHERE token_sha256=$1`, digest[:])
			}
			call := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest(method, path, nil)
				r.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				return w
			}
			if mode != "policyInitialIdleWait" {
				if w := call(); w.Code != 200 {
					t.Fatalf("source positive %d %s", w.Code, w.Body)
				}
			}
			if mode == "readRowWaitRevoke" {
				f.exec(`UPDATE inbox_items SET read_at=NULL WHERE id=$1`, itemID)
			}
			lock, e := f.pool.Begin(f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer lock.Rollback(context.Background())
			switch mode {
			case "listSourceWaitRevoke":
				_, e = lock.Exec(f.ctx, `LOCK TABLE inbox_items IN ACCESS EXCLUSIVE MODE`)
			case "readRowWaitRevoke":
				e = lock.QueryRow(f.ctx, `SELECT id FROM inbox_items WHERE id=$1 FOR UPDATE`, itemID).Scan(&itemID)
			case "policyInitialIdleWait":
				var id string
				e = lock.QueryRow(f.ctx, `SELECT id FROM sessions WHERE token_sha256=$1 FOR UPDATE`, digest[:]).Scan(&id)
			}
			if e != nil {
				t.Fatal(e)
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- call() }()
			socialNotificationsWaitOwnedLock(t, f, lock)
			if mode == "policyInitialIdleWait" {
				deadline := time.Now().Add(5 * time.Second)
				for {
					var expired bool
					if e = f.pool.QueryRow(f.ctx, `SELECT idle_expires_at<=clock_timestamp() FROM sessions WHERE token_sha256=$1`, digest[:]).Scan(&expired); e != nil {
						t.Fatal(e)
					}
					if expired {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("native idle expiry not reached")
					}
					time.Sleep(10 * time.Millisecond)
				}
			} else {
				f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, digest[:])
			}
			if e = lock.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case w := <-done:
				if w.Code != 401 {
					t.Fatalf("expired/revoked during actual wait returned %d %s", w.Code, w.Body)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("native request did not complete")
			}
			if mode == "readRowWaitRevoke" {
				var unchanged bool
				if e = f.pool.QueryRow(f.ctx, `SELECT read_at IS NULL FROM inbox_items WHERE id=$1`, itemID).Scan(&unchanged); e != nil || !unchanged {
					t.Fatalf("denied read changed Inbox: %v %v", unchanged, e)
				}
			}
			if mode == "policyInitialIdleWait" {
				var expired bool
				if e = f.pool.QueryRow(f.ctx, `SELECT idle_expires_at<=clock_timestamp() FROM sessions WHERE token_sha256=$1`, digest[:]).Scan(&expired); e != nil || !expired {
					t.Fatalf("idle source was revived: %v %v", expired, e)
				}
			}
		})
	}
}

func socialNotificationsWaitOwnedLock(t *testing.T, f *privateProfileHTTPDBFixture, lock pgx.Tx) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		err := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)))`, int(lock.Conn().PgConn().PID())).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("actual owned locker PID barrier not reached")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The barrier delays only the call into the real final validator, after the
// handler has materialized JSON. It supplies no actor or permission result.
func TestSocialNotificationsHTTPMaterializedSession(t *testing.T) {
	for _, surface := range []string{"list", "read", "policy"} {
		for _, mode := range []string{"revoked", "idleExpired"} {
			t.Run(surface+"/"+mode, func(t *testing.T) {
				f := notificationPolicyHTTPDBNew(t)
				digest, err := identity.ParseBearer("Bearer " + f.tokens[0])
				if err != nil {
					t.Fatal(err)
				}
				method, path := "GET", "/v1/me/inbox"
				if surface == "policy" {
					path = "/v1/me/notification-policy"
				} else {
					if _, err = f.store.CreateFriendRequest(f.ctx, f.accountIDs[1], f.accountIDs[0], "Owned synthetic invitation"); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
						defer cancel()
						for _, q := range []string{
							`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`,
							`DELETE FROM native_notification_decisions WHERE recipient_id=ANY($1::uuid[])`,
							`DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`,
						} {
							if _, e := f.pool.Exec(ctx, q, f.accountIDs); e != nil {
								t.Error(e)
							}
						}
					})
					if surface == "read" {
						var id string
						if err = f.pool.QueryRow(f.ctx, `SELECT id FROM inbox_items WHERE recipient_account_id=$1 AND resource_type='connection_request'`, f.accountIDs[0]).Scan(&id); err != nil {
							t.Fatal(err)
						}
						method, path = "POST", "/v1/me/inbox/"+id+"/read"
					}
				}
				barrier := &socialNowFinalAccessBarrier{AccessStore: f.store, entered: make(chan struct{}), release: make(chan struct{})}
				handler := New(f.store, barrier, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, false, nil, f.pool, nil)
				done := make(chan *httptest.ResponseRecorder, 1)
				go func() {
					r := httptest.NewRequest(method, path, nil)
					r.Header.Set("Authorization", "Bearer "+f.tokens[0])
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, r)
					done <- w
				}()
				select {
				case <-barrier.entered:
				case <-time.After(5 * time.Second):
					t.Fatal("post-JSON native validator not reached")
				}
				if mode == "revoked" {
					f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, digest[:])
					close(barrier.release)
				} else {
					// Shorten the owned current lease after the initial authentication;
					// the final read must not refresh it after its real row wait.
					f.exec(`WITH n AS MATERIALIZED(SELECT clock_timestamp() t) UPDATE sessions SET idle_expires_at=n.t+interval '1 second' FROM n WHERE token_sha256=$1`, digest[:])
					lock, e := f.pool.Begin(f.ctx)
					if e != nil {
						t.Fatal(e)
					}
					defer lock.Rollback(context.Background())
					var id string
					if e = lock.QueryRow(f.ctx, `SELECT id FROM sessions WHERE token_sha256=$1 FOR UPDATE`, digest[:]).Scan(&id); e != nil {
						t.Fatal(e)
					}
					close(barrier.release)
					socialNotificationsWaitOwnedLock(t, f, lock)
					deadline := time.Now().Add(5 * time.Second)
					for {
						var expired bool
						if e = f.pool.QueryRow(f.ctx, `SELECT idle_expires_at<=clock_timestamp() FROM sessions WHERE token_sha256=$1`, digest[:]).Scan(&expired); e != nil {
							t.Fatal(e)
						}
						if expired {
							break
						}
						if time.Now().After(deadline) {
							t.Fatal("owned DB idle expiry not reached")
						}
						time.Sleep(10 * time.Millisecond)
					}
					if e = lock.Commit(f.ctx); e != nil {
						t.Fatal(e)
					}
				}
				select {
				case w := <-done:
					if w.Code != 401 {
						t.Fatalf("materialized payload escaped current session: %d %s", w.Code, w.Body)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("current native response did not complete")
				}
				// A read_at committed before this response check is a completed
				// domain action. This test proves payload suppression, not rollback
				// of a previously authorized commit or a delivery acknowledgement.
			})
		}
	}
}
