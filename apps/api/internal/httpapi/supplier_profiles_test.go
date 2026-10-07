package httpapi

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/birdtie/birdtie/apps/api/internal/supplierprofile"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Native store decorator controls ordering only. Session authentication and
// current public permission are still resolved by actual PostgreSQL.
type supplierWaitStore struct {
	*postgres.Store
	calls              atomic.Int32
	projected, release chan struct{}
}

func (s *supplierWaitStore) ReadPublicBusiness(ctx context.Context, id, viewer string) (supplierprofile.Business, error) {
	out, e := s.Store.ReadPublicBusiness(ctx, id, viewer)
	if s.calls.Add(1) == 1 {
		close(s.projected)
		select {
		case <-s.release:
		case <-ctx.Done():
			return out, ctx.Err()
		}
	}
	return out, e
}
func TestSupplierRegisteredNativeSourceChangesDuringAuthenticationWait(t *testing.T) {
	f := businessConsoleHTTPNew(t)
	f.claim(t)
	store := postgres.New(f.pool, false)
	// No public permission has been granted. The initial snapshot exposes only
	// the existing verified identity; revoke that identity during the wait.
	wrapped := &supplierWaitStore{Store: store, projected: make(chan struct{}), release: make(chan struct{})}
	handler := New(wrapped, store, store, store, store, store, store, store, store, store, store, store, false, nil, f.pool, nil)
	r := httptest.NewRequest(http.MethodGet, "/v1/businesses/"+f.business, nil)
	r.Header.Set("Authorization", "Bearer "+f.tokens[2])
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { w := httptest.NewRecorder(); handler.ServeHTTP(w, r); done <- w }()
	select {
	case <-wrapped.projected:
	case <-time.After(4 * time.Second):
		t.Fatal("initial native projection missing")
	}
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(f.ctx, `SELECT id FROM sessions WHERE account_id=$1 FOR UPDATE`, f.ids[2]); e != nil {
		t.Fatal(e)
	}
	close(wrapped.release)
	deadline := time.Now().Add(4 * time.Second)
	observed := false
	for time.Now().Before(deadline) {
		var wait bool
		if e = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query ILIKE '%UPDATE sessions%')`).Scan(&wait); e != nil {
			t.Fatal(e)
		}
		if wait {
			observed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !observed {
		t.Fatal("actual Authenticate session UPDATE wait not observed")
	}
	f.exec(t, `UPDATE businesses SET claim_status='revoked' WHERE id=$1`, f.business)
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case w := <-done:
		if w.Code != 404 || strings.Contains(w.Body.String(), "HTTP本地合成商家") {
			t.Fatal("pre-wait identity was emitted", w.Code, w.Body.String())
		}
		if wrapped.calls.Load() != 2 {
			t.Fatal("no post-wait complete source projection")
		}
		t.Log("Observed real Session UPDATE lock, then real claim revocation; current source returned 404, old identity not emitted")
	case <-time.After(4 * time.Second):
		t.Fatal("response did not resume after owned lock released")
	}
}
