package httpapi

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPlaceHistoryHTTPNativeStrictPublicSource(t *testing.T) {
	f := publicationHTTPNative(t)
	m := publicationHTTPNativeDraft(t, f)
	path := "/v1/places/" + f.place + "/social-history"
	for _, token := range []string{"", f.token, f.base.tokens[2], f.base.tokens[3]} {
		w := historicalHTTPCall(f.base, "GET", path, "", token)
		if w.Code != 200 || strings.Contains(w.Body.String(), m.Title) || strings.Contains(w.Body.String(), "不能复制") {
			t.Fatal("native private source/typed viewer leak", w.Code, w.Body.String())
		}
	}
	expired := f.base.newSession(f.account, true, false)
	w := historicalHTTPCall(f.base, "GET", path, "", expired)
	if w.Code != 401 {
		t.Fatal("supplied invalid session downgraded anonymous", w.Code)
	}
}
func TestPlaceHistoryHTTPNativeLateAccess(t *testing.T) {
	for _, mode := range []string{"revoke", "expiry", "target_hidden"} {
		t.Run(mode, func(t *testing.T) {
			f := publicationHTTPNative(t)
			digest, _ := identity.ParseBearer("Bearer " + f.token)
			if mode == "expiry" {
				f.base.exec(`UPDATE sessions SET expires_at=clock_timestamp()+interval '1500 milliseconds',idle_expires_at=clock_timestamp()+interval '1400 milliseconds' WHERE token_sha256=$1`, digest[:])
			}
			blocker, e := f.base.pool.Begin(f.base.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer blocker.Rollback(context.Background())
			var pid int
			if e = blocker.QueryRow(f.base.ctx, `SELECT pg_backend_pid() FROM places WHERE id=$1 FOR UPDATE`, f.place).Scan(&pid); e != nil {
				t.Fatal(e)
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				done <- historicalHTTPCall(f.base, "GET", "/v1/places/"+f.place+"/social-history", "", f.token)
			}()
			publicationHTTPWaitSource(t, f, pid, "FROM places")
			want := 401
			if mode == "revoke" {
				if e = f.base.store.RevokeSession(f.base.ctx, digest); e != nil {
					t.Fatal(e)
				}
			} else if mode == "expiry" {
				publicationHTTPWaitExpiry(t, f, `SELECT expires_at<=clock_timestamp() FROM sessions WHERE token_sha256=$1`, digest[:])
			} else {
				want = 404
				if _, e = blocker.Exec(f.base.ctx, `UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place); e != nil {
					t.Fatal(e)
				}
			}
			if e = blocker.Commit(f.base.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case w := <-done:
				if w.Code != want || strings.Contains(w.Body.String(), "recentMoments") {
					t.Fatal("late read leaked", w.Code, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("native public read stuck")
			}
		})
	}
}
