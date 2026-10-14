package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	ap "github.com/birdtie/birdtie/apps/api/internal/activityparticipation"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

type rsvpHTTPNativeFixture struct {
	*privateProfileHTTPDBFixture
	city, place string
}

func rsvpHTTPNative(t *testing.T) *rsvpHTTPNativeFixture {
	t.Helper()
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" || os.Getenv("BIRDTIE_RSVP_DATABASE") == "" {
		t.Skip("requires newly owned AIR038 RSVP parent; immutable rows retained until exact database cleanup")
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || cfg.ConnConfig.Database != os.Getenv("BIRDTIE_RSVP_DATABASE") || !regexp.MustCompile(`^birdtie_air038_rsvp_[0-9a-f]{12}$`).MatchString(cfg.ConnConfig.Database) || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Port != 55432 {
		t.Fatal("refuse existing/remote/shared database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	f := &rsvpHTTPNativeFixture{privateProfileHTTPDBFixture: &privateProfileHTTPDBFixture{t: t, ctx: ctx, pool: pool, store: postgres.New(pool, false)}}
	var installed bool
	if e = pool.QueryRow(ctx, `SELECT to_regclass('public.human_participation_operation_receipts') IS NOT NULL`).Scan(&installed); e != nil || !installed {
		t.Fatal("actual117 required", e)
	}
	for _, kind := range []string{"person", "person", "person", "organization"} {
		var id string
		if e = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),$1) RETURNING id`, kind).Scan(&id); e != nil {
			t.Fatal(e)
		}
		f.accountIDs = append(f.accountIDs, id)
		f.tokens = append(f.tokens, f.newSession(id, false, false))
		if kind == "person" {
			f.exec(`INSERT INTO user_profiles(account_id,display_name,bio,visibility) VALUES($1,'独占合成 HTTP 报名本人','非真实联系人','public')`, id)
			f.exec(`INSERT INTO agents(agent_type,principal_account_id) VALUES('personal',$1)`, id)
		}
	}
	f.city = "rsvp-http117-" + f.uuid()
	f.place = f.uuid()
	f.exec(`INSERT INTO cities(id,name,country_code,region,time_zone,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES($1,'独占合成恢复城市','GB','LOCAL_SYNTHETIC','Europe/London','published','LOCAL_SYNTHETIC_FIXTURE','local:AIR038','合成维护者',$2)`, f.city, f.accountIDs[1])
	f.exec(`INSERT INTO city_contexts(city_id) VALUES($1)`, f.city)
	f.exec(`INSERT INTO places(id,city_id,name,category_code,latitude,longitude,location_precision,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES($1,$2,'独占合成恢复地点','sports',57.15,-2.1,'point','published','LOCAL_SYNTHETIC_FIXTURE','local:AIR038','合成维护者',$3)`, f.place, f.city, f.accountIDs[1])
	f.handler = contextBuilderHTTPNew(f.store, f.store, f.store)
	return f
}
func (f *rsvpHTTPNativeFixture) uuid() string {
	f.t.Helper()
	var id string
	if e := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()`).Scan(&id); e != nil {
		f.t.Fatal(e)
	}
	return id
}
func (f *rsvpHTTPNativeFixture) activity() string {
	f.t.Helper()
	var now time.Time
	if e := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		f.t.Fatal(e)
	}
	v, e := f.store.CreateSocialDraft(f.ctx, f.accountIDs[1], activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: f.accountIDs[1]}, CityID: f.city, PlaceID: f.place, Title: "合成真实注册 HTTP 报名恢复", Summary: "本地测试", Description: "PRIVATE_ACTIVITY_BODY_MUST_NOT_APPEAR", CategoryCode: "badminton", Visibility: "public", StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour), TimeZone: "Europe/London"})
	if e != nil {
		f.t.Fatal(e)
	}
	if _, e = f.store.PublishSocialActivity(f.ctx, f.accountIDs[1], v.ID); e != nil {
		f.t.Fatal(e)
	}
	return v.ID
}
func rsvpHTTPPath(id string) string { return "/v1/activities/" + id + "/participation-operations" }
func (f *rsvpHTTPNativeFixture) call(method, path, body, token string, headers map[string]string, want int) *httptest.ResponseRecorder {
	f.t.Helper()
	w := participationHTTPCall(f.handler, method, path, body, token, headers, nil)
	if w.Code != want || w.Header().Get("Cache-Control") != "no-store" {
		f.t.Fatalf("registered native %s status%d want%d body%s", method, w.Code, want, w.Body)
	}
	return w
}
func (f *rsvpHTTPNativeFixture) review(id, op string) map[string]string {
	f.t.Helper()
	w := f.call("GET", "/v1/me/entity-actions/activity/"+id, "", f.tokens[0], nil, 200)
	var v struct {
		Data ea.View `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || !v.Data.Valid() {
		f.t.Fatal(w.Body)
	}
	return map[string]string{"X-Birdtie-Action-Version": v.Data.SourceVersion, "X-Birdtie-Action-Until": v.Data.ValidUntil.UTC().Format("2006-01-02T15:04:05.000000Z"), "X-Birdtie-Action-Operation": op}
}
func rsvpHTTPBody(op, action string) string {
	v, _ := json.Marshal(ap.OperationInput{OperationID: op, Operation: action})
	return string(v)
}
func rsvpHTTPCloseBody(action, digest string) string {
	v, _ := json.Marshal(ap.OperationCloseInput{Operation: action, RequestDigest: digest})
	return string(v)
}
func rsvpHTTPReceipt(t *testing.T, w *httptest.ResponseRecorder, owner, id, op string) ap.OperationReceipt {
	t.Helper()
	var v struct {
		Data ap.OperationReceipt `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || !ap.ValidOperationReceipt(v.Data, owner, id, op) {
		t.Fatal("actual native DTO binding", w.Body)
	}
	return v.Data
}
func (f *rsvpHTTPNativeFixture) counts(id string) (p, a, r int) {
	f.t.Helper()
	if e := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM activity_participations WHERE activity_id=$1 AND participant_account_id=$2),(SELECT count(*) FROM audit_events WHERE target_resource_id=$1 AND actor_account_id=$2 AND purpose='human_rsvp'),(SELECT count(*) FROM human_participation_operation_receipts WHERE activity_id=$1 AND owner_account_id=$2)`, id, f.accountIDs[0]).Scan(&p, &a, &r); e != nil {
		f.t.Fatal(e)
	}
	return
}
func rsvpHTTPFreeze(t *testing.T, name string, raw []byte) {
	t.Helper()
	dir := os.Getenv("BIRDTIE_RSVP_WIRE_DIR")
	if dir == "" {
		t.Fatal("actual native wire requires owned evidence destination")
	}
	expected, e := filepath.Abs(filepath.Join("..", "..", "..", "..", "docs", "testing", "evidence", "unfinished-completion-2026-10-10", "rsvp", "backend", "native-wire"))
	if e != nil || !strings.HasPrefix(strings.ToLower(filepath.Clean(dir)), strings.ToLower(expected)+string(os.PathSeparator)) {
		t.Fatal("wire evidence path outside owned backend scope")
	}
	if e = os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	file, e := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = file.Write(raw); e != nil {
		file.Close()
		t.Fatal(e)
	}
	if e = file.Close(); e != nil {
		t.Fatal(e)
	}
}

type rsvpNativeDropWriter struct {
	header http.Header
	code   int
	raw    []byte
}

func (w *rsvpNativeDropWriter) Header() http.Header  { return w.header }
func (w *rsvpNativeDropWriter) WriteHeader(code int) { w.code = code }
func (w *rsvpNativeDropWriter) Write(raw []byte) (int, error) {
	w.raw = append(w.raw, raw...)
	return len(raw), nil
}

type rsvpNativeLateStore struct {
	*postgres.Store
	before func()
}

func (s *rsvpNativeLateStore) ValidateHumanParticipationOperationResponse(ctx context.Context, a ea.Access, r ap.OperationReceipt) error {
	if s.before != nil {
		s.before()
	}
	return s.Store.ValidateHumanParticipationOperationResponse(ctx, a, r)
}
func (s *rsvpNativeLateStore) ValidateHumanParticipationClosureResponse(ctx context.Context, a ea.Access, r ap.OperationClosure) error {
	if s.before != nil {
		s.before()
	}
	return s.Store.ValidateHumanParticipationClosureResponse(ctx, a, r)
}

func TestHumanParticipationOperationRegisteredNativeActualDTOAndRecovery(t *testing.T) {
	f := rsvpHTTPNative(t)
	t.Run("real_committed_cancel_history_and_three_frozen_dtos", func(t *testing.T) {
		f.t = t
		id, op := f.activity(), f.uuid()
		headers := f.review(id, "JOIN")
		w := f.call("POST", rsvpHTTPPath(id), rsvpHTTPBody(op, "JOIN"), f.tokens[0], headers, 201)
		r := rsvpHTTPReceipt(t, w, f.accountIDs[0], id, op)
		rsvpHTTPFreeze(t, "committed-response.json", w.Body.Bytes())
		request, _ := json.MarshalIndent(map[string]any{"ownerId": f.accountIDs[0], "activityId": id, "operationId": op, "body": ap.OperationInput{OperationID: op, Operation: "JOIN"}, "reviewedHeaders": headers}, "", "  ")
		rsvpHTTPFreeze(t, "committed-request.json", append(request, '\n'))
		again := f.call("POST", rsvpHTTPPath(id), rsvpHTTPBody(op, "JOIN"), f.tokens[0], headers, 200)
		if again.Body.String() != w.Body.String() {
			t.Fatal("original replay wire changed")
		}
		cancelOp := f.uuid()
		c := f.call("POST", rsvpHTTPPath(id), rsvpHTTPBody(cancelOp, "CANCEL_RSVP"), f.tokens[0], f.review(id, "CANCEL_RSVP"), 201)
		cancel := rsvpHTTPReceipt(t, c, f.accountIDs[0], id, cancelOp)
		if cancel.EffectID != r.EffectID || cancel.State != "cancelled" {
			t.Fatal(cancel)
		}
		old := f.call("GET", rsvpHTTPPath(id)+"/"+op, "", f.tokens[0], nil, 200)
		if old.Body.String() != w.Body.String() {
			t.Fatal("later independent cancel changed historical result")
		}
		id2, op2 := f.activity(), f.uuid()
		h2 := f.review(id2, "JOIN")
		until, _ := time.Parse(time.RFC3339Nano, h2["X-Birdtie-Action-Until"])
		digest, e := ap.HumanParticipationRequestDigest(f.accountIDs[0], id2, ap.OperationInput{OperationID: op2, Operation: "JOIN"}, ea.BoundCondition{Kind: ea.Join, Operation: "JOIN", SourceVersion: h2["X-Birdtie-Action-Version"], ValidUntil: until})
		if e != nil {
			t.Fatal(e)
		}
		f.call("GET", rsvpHTTPPath(id2)+"/"+op2, "", f.tokens[0], nil, 404)
		closeRequest, _ := json.MarshalIndent(map[string]any{"ownerId": f.accountIDs[0], "activityId": id2, "operationId": op2, "body": ap.OperationCloseInput{Operation: "JOIN", RequestDigest: digest}, "reviewedHeaders": h2}, "", "  ")
		rsvpHTTPFreeze(t, "close-request.json", append(closeRequest, '\n'))
		close := f.call("POST", rsvpHTTPPath(id2)+"/"+op2+"/close", rsvpHTTPCloseBody("JOIN", digest), f.tokens[0], nil, 200)
		rsvpHTTPFreeze(t, "closure-response.json", close.Body.Bytes())
		none := f.call("GET", rsvpHTTPPath(id2)+"/"+op2, "", f.tokens[0], nil, 200)
		n := rsvpHTTPReceipt(t, none, f.accountIDs[0], id2, op2)
		if n.Status != "NO_EFFECT" || n.Reason != "USER_CLOSED" {
			t.Fatal(n)
		}
		rsvpHTTPFreeze(t, "no-effect-response.json", none.Body.Bytes())
		f.call("POST", rsvpHTTPPath(id2), rsvpHTTPBody(op2, "JOIN"), f.tokens[0], h2, 200)
		p, a, receipts := f.counts(id2)
		if p != 0 || a != 0 || receipts != 1 {
			t.Fatal(p, a, receipts)
		}
	})
	t.Run("actual_socket_response_loss_get_recovers_without_resend", func(t *testing.T) {
		f.t = t
		id, op := f.activity(), f.uuid()
		headers := f.review(id, "JOIN")
		done := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			drop := &rsvpNativeDropWriter{header: make(http.Header)}
			f.handler.ServeHTTP(drop, r)
			conn, _, e := w.(http.Hijacker).Hijack()
			if e == nil {
				_ = conn.Close()
			}
			close(done)
		}))
		defer srv.Close()
		req, e := http.NewRequest("POST", srv.URL+rsvpHTTPPath(id), strings.NewReader(rsvpHTTPBody(op, "JOIN")))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Authorization", "Bearer "+f.tokens[0])
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		client := &http.Client{Timeout: 10 * time.Second}
		response, e := client.Do(req)
		if response != nil {
			_, _ = io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}
		if e == nil {
			t.Fatal("native response was not lost on actual socket")
		}
		<-done
		p, a, n := f.counts(id)
		if p != 1 || a != 1 || n != 1 {
			t.Fatal("lost socket did not commit exactly original effect", p, a, n)
		}
		f.call("GET", rsvpHTTPPath(id)+"/"+op, "", f.tokens[0], nil, 200)
		pool, e := pgxpool.NewWithConfig(f.ctx, f.pool.Config().Copy())
		if e != nil {
			t.Fatal(e)
		}
		defer pool.Close()
		newStore := postgres.New(pool, false)
		freshHandler := contextBuilderHTTPNew(newStore, newStore, newStore)
		w := participationHTTPCall(freshHandler, "GET", rsvpHTTPPath(id)+"/"+op, "", f.tokens[0], nil, nil)
		if w.Code != 200 {
			t.Fatal("new pool/registered handler failed durable recovery", w.Code, w.Body)
		}
	})
	t.Run("opaque_close_after_source_revoke_never_cancels", func(t *testing.T) {
		f.t = t
		id, op := f.activity(), f.uuid()
		w := f.call("POST", rsvpHTTPPath(id), rsvpHTTPBody(op, "JOIN"), f.tokens[0], f.review(id, "JOIN"), 201)
		r := rsvpHTTPReceipt(t, w, f.accountIDs[0], id, op)
		f.exec(`UPDATE activities SET publication_status='draft',updated_at=clock_timestamp() WHERE id=$1`, id)
		f.call("GET", rsvpHTTPPath(id)+"/"+op, "", f.tokens[0], nil, 404)
		closed := f.call("POST", rsvpHTTPPath(id)+"/"+op+"/close", rsvpHTTPCloseBody("JOIN", r.RequestDigest), f.tokens[0], nil, 200)
		for _, key := range []string{"status", "reason", "state", "effectId", "effectAt", "PRIVATE_ACTIVITY"} {
			if strings.Contains(closed.Body.String(), `"`+key+`"`) {
				t.Fatal("opaque closure leaked original outcome", closed.Body)
			}
		}
		f.call("GET", rsvpHTTPPath(id)+"/"+op+"/closure", "", f.tokens[0], nil, 200)
		var state string
		if e := f.pool.QueryRow(f.ctx, `SELECT status FROM activity_participations WHERE activity_id=$1 AND participant_account_id=$2`, id, f.accountIDs[0]).Scan(&state); e != nil || state != "going" {
			t.Fatal("close changed RSVP", state, e)
		}
		wrong := f.call("GET", rsvpHTTPPath(f.activity())+"/"+op+"/closure", "", f.tokens[0], nil, 409)
		if strings.Contains(wrong.Body.String(), r.EffectID) {
			t.Fatal("cross-target closure leaked original effect")
		}
	})
	t.Run("registered_bad_shapes_org_and_unknown_get", func(t *testing.T) {
		f.t = t
		id, op := f.activity(), f.uuid()
		h := f.review(id, "JOIN")
		f.call("POST", rsvpHTTPPath(id), rsvpHTTPBody(op, "JOIN"), f.tokens[0], nil, 400)
		f.call("POST", rsvpHTTPPath(id), rsvpHTTPBody(op, "JOIN"), f.tokens[3], h, 403)
		f.call("GET", rsvpHTTPPath(id)+"/"+op, "", f.tokens[0], nil, 404)
		f.call("GET", rsvpHTTPPath(id)+"/"+op+"/closure", "", f.tokens[0], nil, 404)
		f.call("POST", rsvpHTTPPath(id), `{"operationId":"`+op+`","operation":"JOIN","confirmed":true}`, f.tokens[0], h, 400)
		f.call("POST", rsvpHTTPPath(id)+"?confirmed=true", rsvpHTTPBody(op, "JOIN"), f.tokens[0], h, 400)
		w := participationHTTPCall(f.handler, "POST", rsvpHTTPPath(id), rsvpHTTPBody(op, "JOIN"), f.tokens[0], h, func(r *http.Request) { r.Header.Add("X-Birdtie-Action-Version", h["X-Birdtie-Action-Version"]) })
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body)
		}
		p, a, n := f.counts(id)
		if p != 0 || a != 0 || n != 0 {
			t.Fatal(p, a, n)
		}
	})
	t.Run("committed_output_source_revoke_preserves_original_history", func(t *testing.T) {
		f.t = t
		id, op := f.activity(), f.uuid()
		h := f.review(id, "JOIN")
		late := &rsvpNativeLateStore{Store: f.store, before: func() {
			f.exec(`UPDATE activities SET publication_status='draft',updated_at=clock_timestamp() WHERE id=$1`, id)
		}}
		handler := contextBuilderHTTPNew(late, f.store, f.store)
		w := participationHTTPCall(handler, "POST", rsvpHTTPPath(id), rsvpHTTPBody(op, "JOIN"), f.tokens[0], h, nil)
		if w.Code != 404 || strings.Contains(w.Body.String(), `"data"`) {
			t.Fatal("late source revoke disclosed output", w.Code, w.Body)
		}
		p, a, n := f.counts(id)
		if p != 1 || a != 1 || n != 1 {
			t.Fatal("rejected output erased native commit", p, a, n)
		}
		f.call("GET", rsvpHTTPPath(id)+"/"+op+"/closure", "", f.tokens[0], nil, 200)
	})
	t.Run("closure_output_late_session_revoke_does_not_erase_fence", func(t *testing.T) {
		f.t = t
		id, op := f.activity(), f.uuid()
		h := f.review(id, "JOIN")
		until, _ := time.Parse(time.RFC3339Nano, h["X-Birdtie-Action-Until"])
		d, _ := ap.HumanParticipationRequestDigest(f.accountIDs[0], id, ap.OperationInput{OperationID: op, Operation: "JOIN"}, ea.BoundCondition{Kind: ea.Join, Operation: "JOIN", SourceVersion: h["X-Birdtie-Action-Version"], ValidUntil: until})
		late := &rsvpNativeLateStore{Store: f.store, before: func() {
			f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1 AND revoked_at IS NULL`, f.accountIDs[0])
		}}
		handler := contextBuilderHTTPNew(late, f.store, f.store)
		w := participationHTTPCall(handler, "POST", rsvpHTTPPath(id)+"/"+op+"/close", rsvpHTTPCloseBody("JOIN", d), f.tokens[0], nil, nil)
		if w.Code != 401 || strings.Contains(w.Body.String(), `"data"`) {
			t.Fatal(w.Code, w.Body)
		}
		f.tokens[0] = f.newSession(f.accountIDs[0], false, false)
		f.call("GET", rsvpHTTPPath(id)+"/"+op+"/closure", "", f.tokens[0], nil, 200)
		r := rsvpHTTPReceipt(t, f.call("GET", rsvpHTTPPath(id)+"/"+op, "", f.tokens[0], nil, 200), f.accountIDs[0], id, op)
		if r.Status != "NO_EFFECT" {
			t.Fatal(r)
		}
	})
	t.Run("legacy_unkeyed_join_delete_stays_original_contract", func(t *testing.T) {
		f.t = t
		id := f.activity()
		w := f.call("POST", "/v1/activities/"+id+"/participations", "{}", f.tokens[0], nil, 201)
		if strings.Contains(w.Body.String(), "schemaVersion") {
			t.Fatal("legacy wire replaced")
		}
		f.call("DELETE", "/v1/activities/"+id+"/participations/me", "", f.tokens[0], nil, 200)
		p, a, n := f.counts(id)
		if p != 1 || a != 2 || n != 0 {
			t.Fatal(p, a, n)
		}
	})
	t.Run("actual_socket_close_response_loss_get_only_proves_fence", func(t *testing.T) {
		f.t = t
		id, op := f.activity(), f.uuid()
		headers := f.review(id, "JOIN")
		until, _ := time.Parse(time.RFC3339Nano, headers["X-Birdtie-Action-Until"])
		d, _ := ap.HumanParticipationRequestDigest(f.accountIDs[0], id, ap.OperationInput{OperationID: op, Operation: "JOIN"}, ea.BoundCondition{Kind: ea.Join, Operation: "JOIN", SourceVersion: headers["X-Birdtie-Action-Version"], ValidUntil: until})
		done := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			drop := &rsvpNativeDropWriter{header: make(http.Header)}
			f.handler.ServeHTTP(drop, r)
			conn, _, e := w.(http.Hijacker).Hijack()
			if e == nil {
				_ = conn.Close()
			}
			close(done)
		}))
		defer srv.Close()
		req, e := http.NewRequest("POST", srv.URL+rsvpHTTPPath(id)+"/"+op+"/close", strings.NewReader(rsvpHTTPCloseBody("JOIN", d)))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Authorization", "Bearer "+f.tokens[0])
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 10 * time.Second}
		response, e := client.Do(req)
		if response != nil {
			response.Body.Close()
		}
		if e == nil {
			t.Fatal("close response not lost")
		}
		<-done
		f.call("GET", rsvpHTTPPath(id)+"/"+op+"/closure", "", f.tokens[0], nil, 200)
		r := rsvpHTTPReceipt(t, f.call("GET", rsvpHTTPPath(id)+"/"+op, "", f.tokens[0], nil, 200), f.accountIDs[0], id, op)
		if r.Status != "NO_EFFECT" {
			t.Fatal(r)
		}
		f.call("POST", rsvpHTTPPath(id)+"/"+op+"/close", rsvpHTTPCloseBody("JOIN", d), f.tokens[0], nil, 200)
		var audits int
		if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND resource_id=$2 AND purpose='human_rsvp_operation_close'`, f.accountIDs[0], op).Scan(&audits); e != nil || audits != 1 {
			t.Fatal("close replay duplicated audit", audits, e)
		}
		p, a, n := f.counts(id)
		if p != 0 || a != 0 || n != 1 {
			t.Fatal(p, a, n)
		}
	})
	t.Run("missing_actual_schema_registered_503_never_legacy_fallback", func(t *testing.T) {
		id, op := f.activity(), f.uuid()
		headers := f.review(id, "JOIN")
		f.exec(`ALTER TABLE human_participation_operation_receipts RENAME TO rsvp117_temporarily_unavailable`)
		defer f.exec(`ALTER TABLE rsvp117_temporarily_unavailable RENAME TO human_participation_operation_receipts`)
		f.call("POST", rsvpHTTPPath(id), rsvpHTTPBody(op, "JOIN"), f.tokens[0], headers, 503)
		f.call("GET", rsvpHTTPPath(id)+"/"+op, "", f.tokens[0], nil, 503)
		f.call("GET", rsvpHTTPPath(id)+"/"+op+"/closure", "", f.tokens[0], nil, 503)
		var p, a int
		if e := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM activity_participations WHERE activity_id=$1),(SELECT count(*) FROM audit_events WHERE target_resource_id=$1 AND purpose='human_rsvp')`, id).Scan(&p, &a); e != nil || p != 0 || a != 0 {
			t.Fatal("missing schema reached unkeyed write", p, a, e)
		}
	})
}
