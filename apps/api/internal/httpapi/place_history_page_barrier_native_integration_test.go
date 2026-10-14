package httpapi

// Unapplied test proposal: no SQL/HTTP/runtime execution has occurred.
// This is real PG + registered HTTP when root installs and runs it against its
// exact disposable native parent. The tracer only delays driver delivery.
import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pagePayloadBarrierKey struct{}
type pagePayloadBarrierArrival struct {
	PID                   uint32
	WindowEnd, ValidUntil time.Time
}
type pagePayloadBarrier struct {
	armed   atomic.Bool
	hits    atomic.Int32
	arrived chan pagePayloadBarrierArrival
	resume  chan struct{}
	once    sync.Once
}

var _ pgx.QueryTracer = (*pagePayloadBarrier)(nil)

func (b *pagePayloadBarrier) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	// A source-final wrapper begins WITH page_snapshot, so it does not count as
	// the original payload. We stop only the second original payload statement:
	// Read -> encoded bytes -> Revalidate read -> HERE -> final native fence.
	payload := strings.HasPrefix(strings.TrimSpace(d.SQL), "WITH clock AS MATERIALIZED") &&
		strings.Contains(d.SQL, "page_rows AS MATERIALIZED") && strings.Contains(d.SQL, "payload.items")
	var event *pagePayloadBarrierArrival
	if payload && len(d.Args) == 10 {
		end, okEnd := d.Args[6].(time.Time)
		until, okUntil := d.Args[7].(time.Time)
		if okEnd && okUntil {
			event = &pagePayloadBarrierArrival{WindowEnd: end.UTC(), ValidUntil: until.UTC()}
		}
	}
	// Never retain or log Args: it contains the private session digest.
	return context.WithValue(ctx, pagePayloadBarrierKey{}, event)
}
func (b *pagePayloadBarrier) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, d pgx.TraceQueryEndData) {
	event, _ := ctx.Value(pagePayloadBarrierKey{}).(*pagePayloadBarrierArrival)
	// First Read has nil window/deadline args; only the fixed second read has an
	// event. hits counts matched fixed payloads, so stopping the first event is
	// exactly the HTTP post-encoding Revalidate read, not a setup/warm request.
	if d.Err != nil || event == nil || !b.armed.Load() || b.hits.Add(1) != 1 || !b.armed.CompareAndSwap(true, false) {
		return
	}
	event.PID = conn.PgConn().PID()
	b.arrived <- *event
	select {
	case <-b.resume:
	case <-ctx.Done():
	}
}
func (b *pagePayloadBarrier) release() { b.once.Do(func() { close(b.resume) }) }

func pageBarrierOwnedParent(t *testing.T) {
	t.Helper()
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" || os.Getenv("BIRDTIE_DATABASE_URL") == "" {
		t.Skip("explicit owned page native runner required")
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Port != 55432 ||
		!regexp.MustCompile(`^birdtie_nowv4_history_[0-9a-f]{12}$`).MatchString(cfg.ConnConfig.Database) ||
		cfg.ConnConfig.Database != os.Getenv("BIRDTIE_PLACE_HISTORY_DATABASE") {
		t.Fatal("refuse non-owned page barrier parent")
	}
}

func pageBarrierPool(t *testing.T, f *publicationHTTPNativeFixture, b *pagePayloadBarrier) *pgxpool.Pool {
	t.Helper()
	cfg := f.base.pool.Config().Copy()
	cfg.MaxConns = 1
	cfg.MinConns = 0
	cfg.ConnConfig.Tracer = b
	cfg.ConnConfig.RuntimeParams["application_name"] = "owned-history-page-final-barrier"
	pool, e := pgxpool.NewWithConfig(f.base.ctx, cfg)
	if e != nil {
		t.Fatal("create owned native page tracer pool", e)
	}
	t.Cleanup(pool.Close)
	var db, observerDB string
	if e = pool.QueryRow(f.base.ctx, `SELECT current_database()`).Scan(&db); e != nil {
		t.Fatal(e)
	}
	if e = f.base.pool.QueryRow(f.base.ctx, `SELECT current_database()`).Scan(&observerDB); e != nil || db != observerDB || db != os.Getenv("BIRDTIE_PLACE_HISTORY_DATABASE") {
		t.Fatal("tracer escaped exact owned fixture")
	}
	return pool
}

type pageBarrierWire struct {
	Data struct {
		Count                 int `json:"count"`
		WindowEnd, ValidUntil time.Time
		Items                 []struct {
			ID, Title, Excerpt  string
			AuthorID            *string
			AuthorPublicProfile *json.RawMessage
		}
	}
}

func pageBarrierPublish(t *testing.T, f *publicationHTTPNativeFixture) (content.Moment, int64) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"cityId": f.city, "placeId": f.place,
		"title": "PAGE_BARRIER_TITLE_CANARY", "body": "PAGE_BARRIER_BODY_CANARY",
		"occurredAt": "1999-01-01T00:00:00Z", "timePrecision": "day", "locationPrecision": "place"})
	m := historicalHTTPRecord(t, historicalHTTPCall(f.base, "POST", "/v1/me/moments", string(body), f.token), 201)
	p := publicationHTTPNativePreview(t, f, m.ID)
	w := historicalHTTPCall(f.base, "POST", "/v1/me/moments/"+m.ID+"/publication", publicationHTTPNativeBody(p), f.token)
	var result struct {
		Data content.MomentPublicationReceipt
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Data.Revision != m.Revision+1 {
		t.Fatal("actual checked original publication", w.Code)
	}
	return m, result.Data.Revision
}

func pageBarrierExpect(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("original registered source mutation status=%d want=%d", w.Code, want)
	}
}
func pageBarrierProfile(t *testing.T, f *publicationHTTPNativeFixture, visibility string) {
	t.Helper()
	body, _ := json.Marshal(identity.ProfileInput{DisplayName: "PAGE_PUBLIC_AUTHOR_CANARY", Bio: "owned-public-profile", Visibility: visibility})
	pageBarrierExpect(t, historicalHTTPCall(f.base, "PUT", "/v1/me/profile", string(body), f.token), 200)
}

func TestPlaceHistoryPageHTTPNativeFinalPayloadBarrier(t *testing.T) {
	for _, mode := range []string{"withdraw", "viewer_block", "author_block", "block_unblock_aba", "profile_private", "profile_private_public_aba", "session_revoke", "normal_auth_idle_renewal", "natural_place_expiry"} {
		t.Run(mode, func(t *testing.T) {
			pageBarrierOwnedParent(t)
			f := publicationHTTPNative(t)
			// Use the original ordinary personal fixture with a bootstrapped native
			// AgentProfile (053). This does not activate a model. No new private grants.
			f.account = f.base.accountIDs[0]
			f.token = f.base.tokens[0]
			pageBarrierProfile(t, f, "public")
			m, revision := pageBarrierPublish(t, f)
			viewer, viewerToken := f.base.accountIDs[1], f.base.tokens[1]
			path := "/v1/places/" + f.place + "/social-history/moments?include=public-author"
			if mode == "natural_place_expiry" {
				f.base.exec(`UPDATE places SET expires_at=clock_timestamp()+interval '1200 milliseconds' WHERE id=$1`, f.place)
			}
			warm := historicalHTTPCall(f.base, "GET", path, "", viewerToken)
			var current pageBarrierWire
			if warm.Code != 200 || json.Unmarshal(warm.Body.Bytes(), &current) != nil || current.Data.Count != 1 || len(current.Data.Items) != 1 ||
				current.Data.Items[0].ID != m.ID || current.Data.Items[0].AuthorID == nil || *current.Data.Items[0].AuthorID != f.account ||
				!strings.Contains(warm.Body.String(), "PAGE_PUBLIC_AUTHOR_CANARY") {
				t.Fatal("original native source/author precondition", warm.Code)
			}
			b := &pagePayloadBarrier{arrived: make(chan pagePayloadBarrierArrival, 1), resume: make(chan struct{})}
			pool := pageBarrierPool(t, f, b)
			native := postgres.New(pool, false)
			handler := New(native, native, nil, native, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
			ctx, cancel := context.WithTimeout(f.base.ctx, 8*time.Second)
			defer cancel()
			defer b.release()
			b.armed.Store(true)
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				req := httptest.NewRequest("GET", path, nil).WithContext(ctx)
				req.Header.Set("Authorization", "Bearer "+viewerToken)
				rw := httptest.NewRecorder()
				handler.ServeHTTP(rw, req)
				done <- rw
			}()
			var reached pagePayloadBarrierArrival
			select {
			case reached = <-b.arrived:
				t.Logf("actual native post-encoding payload barrier PID=%d fixedWindowEnd=%s fixedValidUntil=%s", reached.PID, reached.WindowEnd.Format(time.RFC3339Nano), reached.ValidUntil.Format(time.RFC3339Nano))
			case <-ctx.Done():
				t.Fatal("fixture failure: exact second payload barrier was not reached")
			}
			if reached.WindowEnd.IsZero() || !reached.ValidUntil.After(reached.WindowEnd) {
				t.Fatal("not a fixed original pagination generation")
			}
			switch mode {
			case "withdraw":
				pageBarrierExpect(t, historicalHTTPCall(f.base, "DELETE", fmt.Sprintf("/v1/me/moments/%s?revision=%d", m.ID, revision), "", f.token), 204)
			case "viewer_block", "block_unblock_aba":
				body, _ := json.Marshal(map[string]string{"accountId": f.account})
				pageBarrierExpect(t, historicalHTTPCall(f.base, "POST", "/v1/me/blocks", string(body), viewerToken), 204)
				if mode == "block_unblock_aba" {
					pageBarrierExpect(t, historicalHTTPCall(f.base, "DELETE", "/v1/me/blocks/"+f.account, "", viewerToken), 204)
				}
			case "author_block":
				body, _ := json.Marshal(map[string]string{"accountId": viewer})
				pageBarrierExpect(t, historicalHTTPCall(f.base, "POST", "/v1/me/blocks", string(body), f.token), 204)
			case "profile_private", "profile_private_public_aba":
				pageBarrierProfile(t, f, "private")
				if mode == "profile_private_public_aba" {
					pageBarrierProfile(t, f, "public")
				}
			case "session_revoke":
				pageBarrierExpect(t, historicalHTTPCall(f.base, "POST", "/v1/session/logout", "", viewerToken), 204)
			case "normal_auth_idle_renewal":
				digest, e := identity.ParseBearer("Bearer " + viewerToken)
				if e != nil {
					t.Fatal(e)
				}
				var before, after int64
				if e = f.base.pool.QueryRow(ctx, `SELECT authorization_generation FROM sessions WHERE account_id=$1 AND token_sha256=$2`, viewer, digest[:]).Scan(&before); e != nil {
					t.Fatal(e)
				}
				pageBarrierExpect(t, historicalHTTPCall(f.base, "GET", "/v1/me", "", viewerToken), 200)
				if e = f.base.pool.QueryRow(ctx, `SELECT authorization_generation FROM sessions WHERE account_id=$1 AND token_sha256=$2`, viewer, digest[:]).Scan(&after); e != nil || before != after {
					t.Fatal("normal original auth changed authority generation", e)
				}
			case "natural_place_expiry":
				for {
					var expired bool
					if e := f.base.pool.QueryRow(ctx, `SELECT expires_at<=clock_timestamp() FROM places WHERE id=$1`, f.place).Scan(&expired); e != nil {
						t.Fatal("observe actual native expiry", e)
					}
					if expired {
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal("native place deadline not reached")
					case <-time.After(10 * time.Millisecond):
					}
				}
			}
			b.release()
			var response *httptest.ResponseRecorder
			select {
			case response = <-done:
			case <-ctx.Done():
				t.Fatal("native registered request did not complete after releasing tracer")
			}
			want := 409
			if mode == "session_revoke" {
				want = 401
			}
			if mode == "normal_auth_idle_renewal" {
				want = 200
			}
			if response.Code != want {
				t.Fatalf("native final source result status=%d want=%d; STATIC risk may now be actual RED, preserve raw command/frame", response.Code, want)
			}
			if want != 200 {
				for _, canary := range []string{m.ID, "PAGE_BARRIER_TITLE_CANARY", "PAGE_BARRIER_BODY_CANARY", "PAGE_PUBLIC_AUTHOR_CANARY"} {
					if strings.Contains(response.Body.String(), canary) {
						t.Fatal("denied response leaked old native payload")
					}
				}
			} else {
				var preserved pageBarrierWire
				if json.Unmarshal(response.Body.Bytes(), &preserved) != nil || preserved.Data.Count != 1 || len(preserved.Data.Items) != 1 ||
					!preserved.Data.WindowEnd.Equal(reached.WindowEnd) || !preserved.Data.ValidUntil.Equal(reached.ValidUntil) {
					t.Fatal("normal renewal extended original pagination window/deadline")
				}
			}
		})
	}
}
