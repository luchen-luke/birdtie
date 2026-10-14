package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// Real registered mux and original native domain writers, owned synthetic
// people only. No model, provider, real contact or shared database is used.
func closeNativeHTTP(t *testing.T) *privateProfileHTTPDBFixture {
	t.Helper()
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" || os.Getenv("BIRDTIE_VISIBILITY_DATABASE") == "" {
		t.Skip("requires explicitly owned SAF002 visibility runner")
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || cfg.ConnConfig.Database != os.Getenv("BIRDTIE_VISIBILITY_DATABASE") || !regexp.MustCompile(`^birdtie_saf002_visibility_[0-9a-f]{12}$`).MatchString(cfg.ConnConfig.Database) || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Port != 55432 {
		t.Fatal("refuse unowned/remote Close HTTP database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	var db string
	var ready bool
	if e = pool.QueryRow(ctx, `SELECT current_database(),to_regclass('public.person_close_connections') IS NOT NULL AND to_regprocedure('public.birdtie_profile_visible_to(uuid,uuid)') IS NOT NULL`).Scan(&db, &ready); e != nil || db != cfg.ConnConfig.Database || !ready {
		t.Fatal("owned real118+119 required", e)
	}
	f := &privateProfileHTTPDBFixture{t: t, ctx: ctx, pool: pool, store: postgres.New(pool, false)}
	// This is registered before any source creation, not only after a successful
	// fixture. Each account is tracked before its dependent Agent/Session.
	t.Cleanup(func() { closeNativeHTTPCleanup(f) })
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
		f.exec(`INSERT INTO user_profiles(account_id,display_name,bio,visibility) VALUES($1,'本地合成Close好友','CLOSE_PRIVATE_BIO_CANARY','public')`, id)
	}
	f.handler = New(f.store, f.store, nil, nil, nil, nil, nil, f.store, nil, nil, f.store, nil, false, nil, nil, nil)
	return f
}
func closeNativeHTTPCleanup(f *privateProfileHTTPDBFixture) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if len(f.accountIDs) == 0 {
		return
	}
	// Native parent cascade, exclusively for this fixture's accumulated IDs.
	// Close source fences cannot be deleted while their native parents exist.
	for i, sql := range []string{
		`DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`,
		`DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`,
		`DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`,
		`DELETE FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`,
		`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
		`DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`,
		`DELETE FROM agents WHERE principal_account_id=ANY($1::uuid[])`,
		`DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`,
		`DELETE FROM accounts WHERE id=ANY($1::uuid[])`,
	} {
		if _, e := f.pool.Exec(ctx, sql, f.accountIDs); e != nil {
			f.t.Errorf("owned Close HTTP fixture cleanup step %d failed: %v", i, e)
		}
	}
	var residue int
	e := f.pool.QueryRow(ctx, `SELECT
	 (SELECT count(*) FROM accounts WHERE id=ANY($1::uuid[]))+
	 (SELECT count(*) FROM agents WHERE principal_account_id=ANY($1::uuid[]))+
	 (SELECT count(*) FROM user_profiles WHERE account_id=ANY($1::uuid[]))+
	 (SELECT count(*) FROM sessions WHERE account_id=ANY($1::uuid[]))+
	 (SELECT count(*) FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[]))+
	 (SELECT count(*) FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[]))+
	 (SELECT count(*) FROM person_close_connections WHERE owner_account_id=ANY($1::uuid[]) OR person_account_id=ANY($1::uuid[]))+
	 (SELECT count(*) FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[]))+
	 (SELECT count(*) FROM follows WHERE follower_account_id=ANY($1::uuid[]) OR person_account_id=ANY($1::uuid[]))+
	 (SELECT count(*) FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[]))+
	 (SELECT count(*) FROM audit_events WHERE actor_account_id=ANY($1::uuid[]))+
	 (SELECT count(*) FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[]))`, f.accountIDs).Scan(&residue)
	if e != nil || residue != 0 {
		f.t.Errorf("owned Close HTTP fixture residue=%d: %v", residue, e)
	} else {
		f.t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY owned Close HTTP fixture residue=0")
	}
}
func closeNativeRequest(f *privateProfileHTTPDBFixture, h http.Handler, method, path, body string, actor int) *httptest.ResponseRecorder {
	r := privateProfileHTTPRequest(method, path, body, f.tokens[actor], "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func closeNativeFriend(t *testing.T, f *privateProfileHTTPDBFixture) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"recipientAccountId": f.accountIDs[1], "scope": "friend", "note": "本人明确接受合成好友"})
	w := closeNativeRequest(f, f.handler, "POST", "/v1/me/connection-requests", string(body), 0)
	var out struct{ Data connection.Request }
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("registered original Friend request %d %s", w.Code, w.Body.String())
	}
	w = closeNativeRequest(f, f.handler, "POST", "/v1/me/connection-requests/"+out.Data.ID+"/decision", `{"action":"accept"}`, 1)
	if w.Code != 200 {
		t.Fatalf("registered original Friend accept %d %s", w.Code, w.Body.String())
	}
}
func closeNativeView(t *testing.T, f *privateProfileHTTPDBFixture) (connection.CloseView, *httptest.ResponseRecorder) {
	t.Helper()
	w := closeNativeRequest(f, f.handler, "GET", "/v1/me/close-connections", "", 0)
	var out struct{ Data connection.CloseView }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || !connection.ValidCloseView(out.Data, f.accountIDs[0]) {
		t.Fatalf("actual registered current Close GET %d %s", w.Code, w.Body.String())
	}
	return out.Data, w
}
func closeNativePutBody(v connection.CloseView) string {
	entry := v.Connections[0]
	b, _ := json.Marshal(connection.ClosePut{TieID: entry.TieID, RequestID: entry.RequestID, SourceVersion: entry.SourceVersion, ValidUntil: v.ValidUntil, Confirmed: true})
	return string(b)
}
func closeNativeDeleteBody(v connection.CloseEntry) string {
	b, _ := json.Marshal(connection.CloseDelete{TieID: v.TieID, RequestID: v.RequestID, ExpectedRevision: v.NativeRevision, Confirmed: true})
	return string(b)
}
func closeNativePacket(t *testing.T, f *privateProfileHTTPDBFixture, name, method, path, body string, w *httptest.ResponseRecorder) {
	t.Helper()
	dir := os.Getenv("BIRDTIE_CLOSE_PACKET_DIR")
	if dir == "" {
		return
	}
	absolute, e := filepath.Abs(dir)
	allowed := filepath.Clean(`D:\Project\birdtie\docs\testing\evidence\unfinished-completion-2026-10-10\visibility\close`)
	if e != nil || !strings.HasPrefix(strings.ToLower(absolute), strings.ToLower(allowed)+string(filepath.Separator)) {
		t.Fatal("refuse non-owned Close packet path")
	}
	if e = os.MkdirAll(absolute, 0700); e != nil {
		t.Fatal(e)
	}
	var observed time.Time
	if e = f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&observed); e != nil {
		t.Fatal(e)
	}
	meta := map[string]any{"ownerId": f.accountIDs[0], "personId": f.accountIDs[1], "method": method, "path": path, "requestBody": body, "httpStatus": w.Code, "observedAt": observed.UTC(), "modelAccess": false, "transport": "original registered HTTP + native PG synthetic owned fixtures"}
	metadata, _ := json.MarshalIndent(meta, "", "  ")
	for _, file := range []struct {
		suffix string
		raw    []byte
	}{{".body.json", append([]byte{}, w.Body.Bytes()...)}, {".request.json", metadata}} {
		out, e := os.OpenFile(filepath.Join(absolute, name+file.suffix), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal("immutable native packet", e)
		}
		_, e = out.Write(file.raw)
		closeErr := out.Close()
		if e != nil || closeErr != nil {
			t.Fatal("native packet write", e, closeErr)
		}
	}
}
func TestCloseNativeRegisteredHTTPContractAndOriginalFriend(t *testing.T) {
	f := closeNativeHTTP(t)
	view, w := closeNativeView(t, f)
	if len(view.Connections) != 0 || view.ModelAccess || view.Truncated {
		t.Fatal("own unconfigured native view")
	}
	closeNativePacket(t, f, "own-empty", "GET", "/v1/me/close-connections", "", w)
	closeNativeFriend(t, f)
	view, w = closeNativeView(t, f)
	if len(view.Connections) != 1 || view.Connections[0].IsClose || view.Connections[0].DisplayName != "本地合成Close好友" {
		t.Fatal("actual eligible source and current visible name", view)
	}
	closeNativePacket(t, f, "own-eligible", "GET", "/v1/me/close-connections", "", w)
	path := "/v1/me/close-connections/" + f.accountIDs[1]
	body := closeNativePutBody(view)
	w = closeNativeRequest(f, f.handler, "PUT", path, body, 0)
	var selected struct{ Data connection.CloseResult }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &selected) != nil || !connection.ValidCloseResult(selected.Data, f.accountIDs[0], f.accountIDs[1]) || selected.Data.Connection.NativeRevision != 1 || strings.Contains(w.Body.String(), "ResponseProof") {
		t.Fatalf("actual specific Close selection %d %s", w.Code, w.Body.String())
	}
	closeNativePacket(t, f, "own-selected", "PUT", path, body, w)
	w = closeNativeRequest(f, f.handler, "PUT", path, body, 0)
	if w.Code != 409 {
		t.Fatal("old confirmed source cannot execute twice", w.Code)
	}
	body = closeNativeDeleteBody(selected.Data.Connection)
	w = closeNativeRequest(f, f.handler, "DELETE", path, body, 0)
	if w.Code != 204 || w.Body.Len() != 0 {
		t.Fatal("actual owned revoke is private204", w.Code, w.Body.String())
	}
	closeNativePacket(t, f, "own-revoked", "DELETE", path, body, w)
	view, w = closeNativeView(t, f)
	if view.Connections[0].IsClose || view.Connections[0].NativeRevision != 2 {
		t.Fatal("retired native fence persists", view)
	}
	closeNativePacket(t, f, "own-retired", "GET", "/v1/me/close-connections", "", w)
	f.exec(`UPDATE user_profiles SET visibility='private',display_name='CLOSE_HIDDEN_NAME_CANARY' WHERE account_id=$1`, f.accountIDs[1])
	view, w = closeNativeView(t, f)
	if view.Connections[0].DisplayName != "Birdtie 成员" || strings.Contains(w.Body.String(), "CLOSE_HIDDEN_NAME_CANARY") || strings.Contains(w.Body.String(), "CLOSE_PRIVATE_BIO_CANARY") {
		t.Fatal("Friend management cannot reveal private Profile")
	}
	closeNativePacket(t, f, "own-hidden-name", "GET", "/v1/me/close-connections", "", w)
}

type closeNativeLateAccess struct {
	*postgres.Store
	once  sync.Once
	after func(context.Context)
}

func (a *closeNativeLateAccess) ValidateHumanSocialResponse(ctx context.Context, d [32]byte, actor identity.Actor) error {
	a.once.Do(func() { a.after(ctx) })
	return a.Store.ValidateHumanSocialResponse(ctx, d, actor)
}
func TestCloseNativeRegisteredHTTPAfterEncodingSourceAndSession(t *testing.T) {
	for _, kind := range []string{"GET-name", "GET-profile-private", "GET-block", "GET-session-ABA", "PUT-name", "PUT-session-revoked", "DELETE-new-choice"} {
		t.Run(kind, func(t *testing.T) {
			f := closeNativeHTTP(t)
			closeNativeFriend(t, f)
			view, _ := closeNativeView(t, f)
			method, path, body := "GET", "/v1/me/close-connections", ""
			want := 409
			if strings.HasPrefix(kind, "PUT") {
				method = "PUT"
				path += "/" + f.accountIDs[1]
				body = closeNativePutBody(view)
			}
			if strings.HasPrefix(kind, "DELETE") {
				path += "/" + f.accountIDs[1]
				w := closeNativeRequest(f, f.handler, "PUT", path, closeNativePutBody(view), 0)
				var selected struct{ Data connection.CloseResult }
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &selected) != nil {
					t.Fatal("fresh original selection", w.Code)
				}
				method = "DELETE"
				body = closeNativeDeleteBody(selected.Data.Connection)
			}
			access := &closeNativeLateAccess{Store: f.store, after: func(ctx context.Context) {
				switch kind {
				case "GET-name", "PUT-name":
					f.exec(`UPDATE user_profiles SET display_name='LATE_CLOSE_NAME_CANARY' WHERE account_id=$1`, f.accountIDs[1])
				case "GET-profile-private":
					f.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, f.accountIDs[1])
				case "GET-block":
					if e := f.store.BlockAccount(ctx, f.accountIDs[1], f.accountIDs[0]); e != nil {
						t.Fatal(e)
					}
				case "GET-session-ABA":
					f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[0])
					f.exec(`UPDATE sessions SET revoked_at=NULL WHERE account_id=$1`, f.accountIDs[0])
				case "PUT-session-revoked":
					f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[0])
				case "DELETE-new-choice":
					fresh, _ := closeNativeView(t, f)
					w := closeNativeRequest(f, f.handler, "PUT", "/v1/me/close-connections/"+f.accountIDs[1], closeNativePutBody(fresh), 0)
					if w.Code != 200 {
						t.Fatal("separate fresh human choice", w.Code)
					}
				}
			}}
			if kind == "PUT-session-revoked" {
				want = 401
			}
			h := New(f.store, access, nil, nil, nil, nil, nil, f.store, nil, nil, f.store, nil, false, nil, nil, nil)
			w := closeNativeRequest(f, h, method, path, body, 0)
			if w.Code != want || strings.Contains(w.Body.String(), "LATE_CLOSE_NAME_CANARY") || strings.Contains(w.Body.String(), "CLOSE_PRIVATE_BIO_CANARY") {
				t.Fatalf("old encoded DTO must failclosed %d want%d %s", w.Code, want, w.Body.String())
			}
			// PUT may already be committed when response authority changes. Never label
			// this a NO_EFFECT rollback: its current owner view remains the authority.
			if kind == "PUT-name" || kind == "DELETE-new-choice" {
				var active bool
				if e := f.pool.QueryRow(f.ctx, `SELECT is_active FROM person_close_connections WHERE owner_account_id=$1 AND person_account_id=$2`, f.accountIDs[0], f.accountIDs[1]).Scan(&active); e != nil || !active {
					t.Fatal("separate committed native effect is historical fact", e)
				}
			}
		})
	}
}
