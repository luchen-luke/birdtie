package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentplacememory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type placeMemoryHTTPNativeFixture struct{ *contextBuilderHTTPFixture }

func placeMemoryHTTPNative(t *testing.T) *placeMemoryHTTPNativeFixture {
	t.Helper()
	f := &placeMemoryHTTPNativeFixture{contextBuilderHTTPNative(t)}
	for i := 0; i < 2; i++ {
		if _, e := f.store.EnsureAgentProfile(f.ctx, f.agentIDs[i], actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[i]}); e != nil {
			t.Fatal(e)
		}
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{`DELETE FROM saved_items WHERE owner_account_id=ANY($1::uuid[])`, `DELETE FROM moments WHERE author_account_id=ANY($1::uuid[])`} {
			if _, e := f.pool.Exec(ctx, q, f.accountIDs); e != nil {
				t.Error("owned Place HTTP source cleanup", e)
			}
		}
	})
	return f
}

// Instrument only the call boundary, never substitute a permission/source.
type placeMemoryFinalNativeCatalog struct {
	*postgres.Store
	entered chan struct{}
}

func (c *placeMemoryFinalNativeCatalog) RevalidateOwnPlaceMemory(ctx context.Context, a agentprofile.PrivateAccess, p agentplacememory.Projection) error {
	close(c.entered)
	return c.Store.RevalidateOwnPlaceMemory(ctx, a, p)
}
func TestPlaceMemoryHTTPNativeFinalPoolWait(t *testing.T) {
	for _, event := range []string{"sourceRemoved", "sessionRevoke", "agentRetired", "declarationExpiry"} {
		t.Run(event, func(t *testing.T) {
			f := placeMemoryHTTPNative(t)
			if event == "declarationExpiry" {
				body := f.body(t, 0, "LIKED")
				var obj map[string]any
				_ = json.Unmarshal([]byte(body), &obj)
				obj["validUntil"] = time.Now().UTC().Add(1100 * time.Millisecond).Format(time.RFC3339Nano)
				raw, _ := json.Marshal(obj)
				f.request(t, f.handler, "PUT", "/v1/me/place-declarations/"+f.newID(t), string(raw), f.tokens[0], 200, nil)
			} else if _, e := f.store.Save(f.ctx, f.accountIDs[0], "place", f.place); e != nil {
				t.Fatal(e)
			}
			cfg := f.pool.Config().Copy()
			cfg.MaxConns = 1
			pool, e := pgxpool.NewWithConfig(f.ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			catalog := &placeMemoryFinalNativeCatalog{Store: postgres.New(pool, false), entered: make(chan struct{})}
			gate := &socialNowFinalAccessBarrier{AccessStore: f.store, entered: make(chan struct{}), release: make(chan struct{})}
			handler := contextBuilderHTTPNew(catalog, gate, f.store)
			w := httptest.NewRecorder()
			r := privateProfileHTTPRequest("GET", f.readPath(), "", f.tokens[0], "application/json")
			done := make(chan struct{})
			go func() { handler.ServeHTTP(w, r); close(done) }()
			select {
			case <-gate.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("source snapshot never reached response gate")
			}
			holder, e := pool.Acquire(f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			close(gate.release)
			select {
			case <-catalog.entered:
			case <-time.After(5 * time.Second):
				holder.Release()
				t.Fatal("final native revalidation not reached")
			}
			select {
			case <-done:
				holder.Release()
				t.Fatal("final source check did not wait for owned pool")
			case <-time.After(80 * time.Millisecond):
			}
			want := 403
			switch event {
			case "sourceRemoved":
				f.exec(`DELETE FROM saved_items WHERE owner_account_id=$1`, f.accountIDs[0])
			case "sessionRevoke":
				f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[0])
			case "agentRetired":
				f.exec(`UPDATE agents SET status='retired' WHERE id=$1`, f.agentIDs[0])
			case "declarationExpiry":
				time.Sleep(1200 * time.Millisecond)
				want = 409
			}
			holder.Release()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("actual pool wait never completed")
			}
			if w.Code != want || strings.Contains(w.Body.String(), `"signals"`) {
				t.Fatalf("pool waited source %s status=%d want=%d", event, w.Code, want)
			}
			if pool.Stat().EmptyAcquireCount() == 0 {
				t.Fatal("fixture never observed real empty-pool acquisition")
			}
		})
	}
}
func (f *placeMemoryHTTPNativeFixture) readPath() string {
	return "/v1/me/places/" + f.place + "/memory"
}
func (f *placeMemoryHTTPNativeFixture) newID(t *testing.T) string {
	t.Helper()
	var id string
	if e := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	return id
}
func (f *placeMemoryHTTPNativeFixture) body(t *testing.T, version int64, kind string) string {
	t.Helper()
	raw, e := json.Marshal(map[string]any{"agentId": f.agentIDs[0], "expectedVersion": version, "placeId": f.place, "kind": kind, "visibility": "PRIVATE", "validUntil": time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	return string(raw)
}
func placeMemoryHTTPReadDTO(t *testing.T, w *httptest.ResponseRecorder) humanPlaceMemory {
	t.Helper()
	var out struct{ Data humanPlaceMemory }
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.SchemaVersion != "human-place-memory-v1" || out.Data.Signals == nil {
		t.Fatal("invalid actual human projection")
	}
	for _, secret := range []string{"authorityDigest", "targetDigest", "snapshotId", "structuredValue", "confidence", "occurredAt", "latitude", "longitude", "PRIVATE_PLACE_HTTP_BODY_CANARY", "PRIVATE_PLACE_HTTP_TITLE_CANARY"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("private body/authority leaked", secret)
		}
	}
	return out.Data
}
func placeMemoryHTTPReceipt(t *testing.T, w *httptest.ResponseRecorder) humanPlaceReceipt {
	t.Helper()
	var out struct{ Data humanPlaceReceipt }
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.SchemaVersion != "human-place-declaration-receipt-v1" {
		t.Fatal("invalid actual native receipt")
	}
	for _, key := range []string{"summary", "structuredValue", "confidence", "authorityDigest", "targetDigest"} {
		if strings.Contains(w.Body.String(), `"`+key+`"`) {
			t.Fatal("receipt copied contents", key)
		}
	}
	return out.Data
}
func TestPlaceMemoryHTTPNativeSourcesCASRetryAndHiddenDelete(t *testing.T) {
	f := placeMemoryHTTPNative(t)
	if got := placeMemoryHTTPReadDTO(t, f.request(t, f.handler, "GET", f.readPath(), "", f.tokens[0], 200, nil)); len(got.Signals) != 0 {
		t.Fatal("empty source invented")
	}
	if _, e := f.store.Save(f.ctx, f.accountIDs[0], "place", f.place); e != nil {
		t.Fatal(e)
	}
	if _, e := f.store.CreateMomentDraft(f.ctx, f.accountIDs[0], content.MomentInput{CityID: f.city, PlaceID: f.place, Title: "PRIVATE_PLACE_HTTP_TITLE_CANARY", Body: "PRIVATE_PLACE_HTTP_BODY_CANARY", TimePrecision: "unknown", LocationPrecision: "place"}); e != nil {
		t.Fatal(e)
	}
	got := placeMemoryHTTPReadDTO(t, f.request(t, f.handler, "GET", f.readPath(), "", f.tokens[0], 200, nil))
	if len(got.Signals) != 2 || got.VerifiedVisit != "UNAVAILABLE" || got.Attendance != "UNAVAILABLE" || got.ModelAccess != "UNAVAILABLE" {
		t.Fatal("native source or unavailable boundary lost")
	}
	for _, kind := range []string{"LIKED", "VISITED"} {
		id := f.newID(t)
		path := "/v1/me/place-declarations/" + id
		body := f.body(t, 0, kind)
		r := placeMemoryHTTPReceipt(t, f.request(t, f.handler, "PUT", path, body, f.tokens[0], 200, nil))
		if r.MemoryID != id || r.Version != 1 || r.Kind != agentplacememory.Kind(kind) || r.Basis != agentplacememory.SelfDeclaration {
			t.Fatal("native declaration/source invalid")
		}
		again := placeMemoryHTTPReceipt(t, f.request(t, f.handler, "PUT", path, body, f.tokens[0], 200, nil))
		if again.Version != 1 {
			t.Fatal("same CAS retry advanced version")
		}
		noOp := strings.Replace(body, `"expectedVersion":0`, `"expectedVersion":1`, 1)
		if placeMemoryHTTPReceipt(t, f.request(t, f.handler, "PUT", path, noOp, f.tokens[0], 200, nil)).Version != 1 {
			t.Fatal("exact no-op update should retain revision")
		}
		f.request(t, f.handler, "PUT", path, strings.Replace(body, `"visibility":"PRIVATE"`, `"visibility":"AGENT_ONLY"`, 1), f.tokens[0], 409, nil)
		f.request(t, f.handler, "DELETE", path, `{"expectedVersion":1}`, f.tokens[1], 404, nil)
		f.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place)
		deleted := placeMemoryHTTPReceipt(t, f.request(t, f.handler, "DELETE", path, `{"expectedVersion":1}`, f.tokens[0], 200, nil))
		if deleted.Status != "DELETED" || deleted.Version != 2 || deleted.PlaceID != "" || deleted.Kind != "" {
			t.Fatal("tombstone leaked erased facts")
		}
		if placeMemoryHTTPReceipt(t, f.request(t, f.handler, "DELETE", path, `{"expectedVersion":1}`, f.tokens[0], 200, nil)).Version != 2 {
			t.Fatal("withdrawal retry not idempotent")
		}
		f.exec(`UPDATE places SET publication_status='published' WHERE id=$1`, f.place)
		f.request(t, f.handler, "PUT", path, body, f.tokens[0], 409, nil)
	}
	peer := placeMemoryHTTPReadDTO(t, f.request(t, f.handler, "GET", f.readPath(), "", f.tokens[1], 200, nil))
	if len(peer.Signals) != 0 || peer.OwnerID != f.accountIDs[1] {
		t.Fatal("foreign owner contents leaked")
	}
	reopened := postgres.New(f.pool, false)
	handler := contextBuilderHTTPNew(reopened, reopened, reopened)
	if len(placeMemoryHTTPReadDTO(t, f.request(t, handler, "GET", f.readPath(), "", f.tokens[0], 200, nil)).Signals) != 2 {
		t.Fatal("reopened Store lost original sources")
	}
}
func TestPlaceMemoryHTTPNativeStrictWireAndIdentity(t *testing.T) {
	for _, kind := range []string{"anon", "organization", "business", "orgWorkspace", "emptyWorkspace", "forceQuery", "getBody", "privatePlace", "noMetadata", "inactiveAgent", "expiredSession"} {
		t.Run(kind, func(t *testing.T) {
			f := placeMemoryHTTPNative(t)
			token := f.tokens[0]
			path := f.readPath()
			body := ""
			var workspace *string
			want := 403
			switch kind {
			case "anon":
				token = ""
				want = 401
			case "organization":
				token = f.tokens[2]
			case "business":
				token = f.tokens[3]
			case "orgWorkspace":
				w := f.accountIDs[2]
				workspace = &w
			case "emptyWorkspace":
				w := ""
				workspace = &w
			case "forceQuery":
				path += "?"
				want = 400
			case "getBody":
				body = "{}"
				want = 400
			case "privatePlace":
				f.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place)
				want = 404
			case "noMetadata":
				f.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, f.agentIDs[0])
				want = 404
			case "inactiveAgent":
				f.exec(`UPDATE agents SET status='retired' WHERE id=$1`, f.agentIDs[0])
			case "expiredSession":
				token = f.newSession(f.accountIDs[0], true, false)
				want = 401
			}
			w := f.request(t, f.handler, "GET", path, body, token, want, workspace)
			if strings.Contains(w.Body.String(), `"data"`) {
				t.Fatal("denied source leaked")
			}
		})
	}
	f := placeMemoryHTTPNative(t)
	path := "/v1/me/place-declarations/" + f.newID(t)
	for _, bad := range []string{"confirmed", "ownerId", "purpose", "occurredAt", "latitude"} {
		body := f.body(t, 0, "LIKED")
		body = strings.TrimSuffix(body, "}") + `,"` + bad + `":true}`
		f.request(t, f.handler, "PUT", path, body, f.tokens[0], 400, nil)
	}
}
func TestPlaceMemoryHTTPNativeFinalSessionAndSourceABA(t *testing.T) {
	for _, event := range []string{"sessionRevoke", "sourceRemoved", "placeHideRestore", "cityHideRestore", "agentPauseRestore", "metadataRebuild", "sessionReplacement", "normalIdleRefresh"} {
		t.Run(event, func(t *testing.T) {
			f := placeMemoryHTTPNative(t)
			if _, e := f.store.Save(f.ctx, f.accountIDs[0], "place", f.place); e != nil {
				t.Fatal(e)
			}
			gate := &socialNowFinalAccessBarrier{AccessStore: f.store, entered: make(chan struct{}), release: make(chan struct{})}
			handler := contextBuilderHTTPNew(f.catalog, gate, f.store)
			r := privateProfileHTTPRequest("GET", f.readPath(), "", f.tokens[0], "application/json")
			w := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { handler.ServeHTTP(w, r); close(done) }()
			select {
			case <-gate.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("final Session gate not reached")
			}
			want := 403 // Native snapshot mismatch revokes the old read lease.
			switch event {
			case "sessionRevoke":
				f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[0])
				want = 401
			case "sourceRemoved":
				f.exec(`DELETE FROM saved_items WHERE owner_account_id=$1`, f.accountIDs[0])
			case "placeHideRestore":
				f.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place)
				f.exec(`UPDATE places SET publication_status='published' WHERE id=$1`, f.place)
			case "cityHideRestore":
				f.exec(`UPDATE cities SET publication_status='hidden' WHERE id=$1`, f.city)
				f.exec(`UPDATE cities SET publication_status='published' WHERE id=$1`, f.city)
			case "agentPauseRestore":
				f.exec(`UPDATE agents SET status='retired' WHERE id=$1`, f.agentIDs[0])
				f.exec(`UPDATE agents SET status='active' WHERE id=$1`, f.agentIDs[0])
			case "metadataRebuild":
				f.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, f.agentIDs[0])
				if _, e := f.store.EnsureAgentProfile(f.ctx, f.agentIDs[0], actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[0]}); e != nil {
					t.Fatal(e)
				}
			case "sessionReplacement":
				digest := contextPurposeHTTPDigest(t, f.tokens[0])
				f.exec(`DELETE FROM sessions WHERE token_sha256=$1`, digest[:])
				f.exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '2 hours',clock_timestamp()+interval '1 hour')`, f.accountIDs[0], digest[:])
			case "normalIdleRefresh":
				f.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '1 hour' WHERE account_id=$1`, f.accountIDs[0])
				want = 200
			}
			close(gate.release)
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("final gate did not finish")
			}
			if w.Code != want {
				t.Fatalf("final event %s status %d want %d", event, w.Code, want)
			}
			if want != 200 && strings.Contains(w.Body.String(), `"signals"`) {
				t.Fatal("changed source bytes escaped final gate")
			}
		})
	}
}
func (f *placeMemoryHTTPNativeFixture) controlPath() string {
	return "/v1/me/places/" + f.place + "/declarations"
}
func TestPlaceMemoryHTTPNativeControlExpiredRenewHiddenWithdrawal(t *testing.T) {
	f := placeMemoryHTTPNative(t)
	id := f.newID(t)
	body := f.body(t, 0, "VISITED")
	var input map[string]any
	json.Unmarshal([]byte(body), &input)
	input["validUntil"] = time.Now().UTC().Add(600 * time.Millisecond).Format(time.RFC3339Nano)
	raw, _ := json.Marshal(input)
	f.request(t, f.handler, "PUT", "/v1/me/place-declarations/"+id, string(raw), f.tokens[0], 200, nil)
	time.Sleep(700 * time.Millisecond)
	text := f.request(t, f.handler, "GET", f.controlPath(), "", f.tokens[0], 200, nil).Body.String()
	var envelope struct {
		Data struct {
			Declarations []agentplacememory.HumanDeclaration `json:"declarations"`
		} `json:"data"`
	}
	if e := json.Unmarshal([]byte(text), &envelope); e != nil {
		t.Fatal(e)
	}
	if len(envelope.Data.Declarations) != 1 || envelope.Data.Declarations[0].MemoryID != id || envelope.Data.Declarations[0].Version != 1 || envelope.Data.Declarations[0].Status != "EXPIRED" {
		t.Fatal("original expired control missing", text)
	}
	for _, forbidden := range []string{"authorityStamp", "snapshotId", "sourceStamp", "latitude", "cityId", "summary", "structuredValue"} {
		if strings.Contains(text, `"`+forbidden+`"`) {
			t.Fatal("private native stamp/detail exposed", forbidden)
		}
	}
	f.request(t, f.handler, "PUT", "/v1/me/place-declarations/"+id, f.body(t, 1, "VISITED"), f.tokens[0], 200, nil)
	f.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place)
	f.request(t, f.handler, "GET", f.readPath(), "", f.tokens[0], 404, nil)
	f.request(t, f.handler, "GET", f.controlPath(), "", f.tokens[0], 200, nil)
	peer := f.request(t, f.handler, "GET", f.controlPath(), "", f.tokens[1], 200, nil).Body.String()
	if strings.Contains(peer, id) {
		t.Fatal("crossowner declaration leak")
	}
	f.request(t, f.handler, "DELETE", "/v1/me/place-declarations/"+id, `{"expectedVersion":2}`, f.tokens[0], 200, nil)
	f.request(t, f.handler, "DELETE", "/v1/me/place-declarations/"+id, `{"expectedVersion":2}`, f.tokens[0], 200, nil)
	f.request(t, f.handler, "GET", f.controlPath(), "", f.tokens[0], 200, nil)
	for _, suffix := range []string{"?", "?purpose=PUBLIC", "?ownerId=" + f.accountIDs[1]} {
		f.request(t, f.handler, "GET", f.controlPath()+suffix, "", f.tokens[0], 400, nil)
	}
	empty := ""
	f.request(t, f.handler, "GET", f.controlPath(), "", f.tokens[0], 403, &empty)
	f.request(t, f.handler, "GET", f.controlPath(), "", "", 401, nil)
}

type placeControlFinalNativeCatalog struct {
	*postgres.Store
	entered chan struct{}
}

func (c *placeControlFinalNativeCatalog) RevalidateOwnPlaceDeclarationControls(ctx context.Context, a agentprofile.PrivateAccess, p agentplacememory.HumanControl) error {
	close(c.entered)
	return c.Store.RevalidateOwnPlaceDeclarationControls(ctx, a, p)
}
func TestPlaceMemoryHTTPNativeControlFinalPoolWait(t *testing.T) {
	for _, event := range []string{"sourceUpdated", "sessionRevoke", "agentRestore", "naturalExpiry", "idleRefresh"} {
		t.Run(event, func(t *testing.T) {
			f := placeMemoryHTTPNative(t)
			id := f.newID(t)
			body := f.body(t, 0, "LIKED")
			if event == "naturalExpiry" {
				var v map[string]any
				json.Unmarshal([]byte(body), &v)
				v["validUntil"] = time.Now().UTC().Add(1100 * time.Millisecond).Format(time.RFC3339Nano)
				raw, _ := json.Marshal(v)
				body = string(raw)
			}
			f.request(t, f.handler, "PUT", "/v1/me/place-declarations/"+id, body, f.tokens[0], 200, nil)
			cfg := f.pool.Config().Copy()
			cfg.MaxConns = 1
			pool, e := pgxpool.NewWithConfig(f.ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			catalog := &placeControlFinalNativeCatalog{Store: postgres.New(pool, false), entered: make(chan struct{})}
			gate := &socialNowFinalAccessBarrier{AccessStore: f.store, entered: make(chan struct{}), release: make(chan struct{})}
			handler := contextBuilderHTTPNew(catalog, gate, f.store)
			w := httptest.NewRecorder()
			r := privateProfileHTTPRequest("GET", f.controlPath(), "", f.tokens[0], "application/json")
			done := make(chan struct{})
			go func() { handler.ServeHTTP(w, r); close(done) }()
			select {
			case <-gate.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("control response gate not reached")
			}
			holder, e := pool.Acquire(f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			close(gate.release)
			select {
			case <-catalog.entered:
			case <-time.After(5 * time.Second):
				holder.Release()
				t.Fatal("final native control not reached")
			}
			select {
			case <-done:
				holder.Release()
				t.Fatal("control did not wait for owned pool")
			case <-time.After(80 * time.Millisecond):
			}
			want := 403
			switch event {
			case "sourceUpdated":
				f.exec(`UPDATE agent_memories SET version=version+1,updated_at=clock_timestamp() WHERE id=$1`, id)
			case "sessionRevoke":
				f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[0])
			case "agentRestore":
				f.exec(`UPDATE agents SET status='retired' WHERE id=$1`, f.agentIDs[0])
				f.exec(`UPDATE agents SET status='active' WHERE id=$1`, f.agentIDs[0])
			case "naturalExpiry":
				time.Sleep(1200 * time.Millisecond)
				want = 409
			case "idleRefresh":
				f.exec(`UPDATE sessions SET idle_expires_at=LEAST(expires_at,clock_timestamp()+interval '1 hour') WHERE account_id=$1`, f.accountIDs[0])
				want = 200
			}
			holder.Release()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("control final actual wait unfinished")
			}
			if w.Code != want || want != 200 && strings.Contains(w.Body.String(), `"declarations"`) {
				t.Fatalf("control wait %s status %d want %d", event, w.Code, want)
			}
			if pool.Stat().EmptyAcquireCount() == 0 {
				t.Fatal("no actual pool acquisition wait")
			}
		})
	}
}
func TestPlaceMemoryHTTPNativeHumanAgentTargetIsRequiredAndAtomic(t *testing.T) {
	f := placeMemoryHTTPNative(t)
	id := f.newID(t)
	body := f.body(t, 0, "LIKED")
	path := "/v1/me/place-declarations/" + id
	var fields map[string]any
	json.Unmarshal([]byte(body), &fields)
	delete(fields, "agentId")
	missing, _ := json.Marshal(fields)
	f.request(t, f.handler, "PUT", path, string(missing), f.tokens[0], 400, nil)
	f.request(t, f.handler, "PUT", path, strings.Replace(body, f.agentIDs[0], f.agentIDs[1], 1), f.tokens[0], 403, nil)
	old := f.agentIDs[0]
	f.exec(`UPDATE agents SET status='retired' WHERE id=$1`, old)
	f.exec(`DELETE FROM agents WHERE id=$1`, old)
	var current string
	if e := f.pool.QueryRow(f.ctx, `INSERT INTO agents(agent_type,principal_account_id,status) VALUES('personal',$1,'active') RETURNING id`, f.accountIDs[0]).Scan(&current); e != nil {
		t.Fatal(e)
	}
	if _, e := f.store.EnsureAgentProfile(f.ctx, current, actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[0]}); e != nil {
		t.Fatal(e)
	}
	f.request(t, f.handler, "PUT", path, body, f.tokens[0], 403, nil)
	var n int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agent_memories WHERE id=$1`, id).Scan(&n); e != nil || n != 0 {
		t.Fatal("mismatch created stale confirmation data", n, e)
	}
	receipt := placeMemoryHTTPReceipt(t, f.request(t, f.handler, "PUT", path, strings.Replace(body, old, current, 1), f.tokens[0], 200, nil))
	if receipt.AgentID != current {
		t.Fatal("current exacttarget receipt mismatch")
	}
}

type placeBoundWaitNativeCatalog struct {
	*postgres.Store
	entered chan struct{}
}

func (c *placeBoundWaitNativeCatalog) PutOwnPlaceDeclarationBound(ctx context.Context, a agentprofile.PrivateAccess, expected, id string, in agentplacememory.PutDeclarationInput) (agentmemory.Record, error) {
	close(c.entered)
	return c.Store.PutOwnPlaceDeclarationBound(ctx, a, expected, id, in)
}
func TestPlaceMemoryHTTPNativeHumanAgentTargetPoolWait(t *testing.T) {
	f := placeMemoryHTTPNative(t)
	cfg := f.pool.Config().Copy()
	cfg.MaxConns = 1
	pool, e := pgxpool.NewWithConfig(f.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	catalog := &placeBoundWaitNativeCatalog{Store: postgres.New(pool, false), entered: make(chan struct{})}
	handler := contextBuilderHTTPNew(catalog, f.store, f.store)
	holder, e := pool.Acquire(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	id := f.newID(t)
	w := httptest.NewRecorder()
	r := privateProfileHTTPRequest("PUT", "/v1/me/place-declarations/"+id, f.body(t, 0, "LIKED"), f.tokens[0], "application/json")
	done := make(chan struct{})
	go func() { handler.ServeHTTP(w, r); close(done) }()
	select {
	case <-catalog.entered:
	case <-time.After(5 * time.Second):
		holder.Release()
		t.Fatal("bound writer never reached")
	}
	select {
	case <-done:
		holder.Release()
		t.Fatal("writer did not wait for pool")
	case <-time.After(80 * time.Millisecond):
	}
	old := f.agentIDs[0]
	f.exec(`DELETE FROM agents WHERE id=$1`, old)
	var current string
	if e := f.pool.QueryRow(f.ctx, `INSERT INTO agents(agent_type,principal_account_id,status) VALUES('personal',$1,'active') RETURNING id`, f.accountIDs[0]).Scan(&current); e != nil {
		holder.Release()
		t.Fatal(e)
	}
	if _, e := f.store.EnsureAgentProfile(f.ctx, current, actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[0]}); e != nil {
		holder.Release()
		t.Fatal(e)
	}
	holder.Release()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("bound writer stayed blocked")
	}
	if w.Code != 403 {
		t.Fatal("pool wait replacement accepted old preview", w.Code)
	}
	var n int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agent_memories WHERE id=$1`, id).Scan(&n); e != nil || n != 0 {
		t.Fatal("late replacement wrote Memory", n, e)
	}
	if pool.Stat().EmptyAcquireCount() == 0 {
		t.Fatal("pool never actually empty")
	}
}
