package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	ic "github.com/birdtie/birdtie/apps/api/internal/intentconversion"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func conversionHTTPNative(t *testing.T) (*crossCityOnlineFixture, string, string, ic.Gateway) {
	f := crossCityOnlineNative(t)
	activity := f.activityOf(activitypublish.Organizer{Type: "PERSON", ID: f.accountIDs[0]}, "public")
	f.call("POST", "/v1/activities/"+activity+"/participations", nil, 1, 201)
	i, e := f.store.CreateSocialIntentDraft(f.ctx, f.accountIDs[1], socialintent.DraftInput{Type: "FIND_ACTIVITY", Title: "本人线上活动寻找", Audience: "PRIVATE", Modality: "ONLINE", Constraints: json.RawMessage(`{}`), ExpiresAt: time.Now().UTC().Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	i, e = f.store.ActivateSocialIntent(f.ctx, f.accountIDs[1], i.ID)
	if e != nil {
		t.Fatal(e)
	}
	// Remove this synthetic association before the original generic fixture
	// cleans its Participation FK; no mutation of retained pre-existing rows.
	t.Cleanup(func() { f.exec(`DELETE FROM social_intents WHERE id=$1`, i.ID) })
	g, e := postgres.NewHumanIntentConversions(f.store)
	if e != nil {
		t.Fatal(e)
	}
	active, e := postgres.NewHumanActiveIntents(f.store)
	if e != nil {
		t.Fatal(e)
	}
	f.handler = New(f.store, f.store, nil, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil, WithHumanIntentConversions(g), WithHumanActiveIntents(active))
	return f, i.ID, activity, g
}
func conversionHTTPPreview(t *testing.T, f *crossCityOnlineFixture, id, activity string) ic.Preview {
	t.Helper()
	path := "/v1/me/social-intents/" + id + "/activity-conversion"
	var list struct{ Data ic.List }
	listRaw := f.call("GET", path, nil, 1, 200)
	json.Unmarshal(listRaw.Body.Bytes(), &list)
	if len(list.Data.Choices) != 1 || list.Data.Choices[0].Activity.ActivityID != activity {
		t.Fatal("actual current selector", list.Data)
	}
	var originalCreated time.Time
	if e := f.pool.QueryRow(f.ctx, `SELECT created_at FROM activities WHERE id=$1`, activity).Scan(&originalCreated); e != nil {
		t.Fatal(e)
	}
	if list.Data.Choices[0].Activity.CreatedAt.IsZero() || !list.Data.Choices[0].Activity.CreatedAt.Equal(originalCreated) {
		t.Fatal("selector creation time must be actual original Activity fact")
	}
	var p struct{ Data ic.Preview }
	raw := f.call("POST", path+"/preview", ic.Input{ActivityID: activity, ExpectedVersion: list.Data.Version}, 1, 200)
	if json.Unmarshal(raw.Body.Bytes(), &p) != nil || p.Data.PreviewID == "" {
		t.Fatal(raw.Body.String())
	}
	t.Logf("PLN_ACTUAL_CONVERSION_LIST_WIRE=%s", listRaw.Body.String())
	t.Logf("PLN_ACTUAL_CONVERSION_PREVIEW_WIRE=%s", raw.Body.String())
	return p.Data
}
func mustConversionJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
func TestIntentConversionHTTPNativeRegisteredOriginalRSVPAndRestart(t *testing.T) {
	f, id, activity, _ := conversionHTTPNative(t)
	path := "/v1/me/social-intents/" + id + "/activity-conversion"
	before := f.publicDomainDigest()
	p := conversionHTTPPreview(t, f, id, activity)
	if before != f.publicDomainDigest() {
		t.Fatal("GET/preview domain effect")
	}
	var v struct{ Data ic.Receipt }
	raw := f.call("POST", path+"/approve", ic.Approval{PreviewID: p.PreviewID}, 1, 200)
	if json.Unmarshal(raw.Body.Bytes(), &v) != nil || !v.Data.Committed || v.Data.ActivityID != activity || v.Data.Intent.Status != "CONVERTED" {
		t.Fatal(raw.Body.String())
	}
	t.Logf("PLN_ACTUAL_CONVERSION_RECEIPT_WIRE=%s", raw.Body.String())

	own := f.call("GET", "/v1/me/social-intents/"+id, nil, 1, 200)
	if !strings.Contains(own.Body.String(), v.Data.ParticipationID) {
		t.Fatal("original own DTO association missing", own.Body.String())
	}
	active := f.call("GET", "/v1/me/active-social-intents/"+id, nil, 1, 200)
	if strings.Contains(active.Body.String(), "convertedParticipationId") {
		t.Fatal("old minimal human projection unexpectedly widened")
	}
	t.Logf("PLN_ACTUAL_OWN_CONVERTED_WIRE=%s", own.Body.String())
	t.Logf("PLN_ACTUAL_ACTIVE_CONVERTED_WIRE=%s", active.Body.String())
	for _, path := range []string{"/v1/social-intents", "/v1/social-intents/" + id} {
		want := 200
		if strings.HasSuffix(path, id) {
			want = 404
		}
		raw := f.call("GET", path, nil, 0, want)
		if strings.Contains(raw.Body.String(), v.Data.ParticipationID) || strings.Contains(raw.Body.String(), "convertedActivityId") {
			t.Fatal("public reader leaked private association")
		}
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, req)
		if w.Code != want || strings.Contains(w.Body.String(), v.Data.ParticipationID) || strings.Contains(w.Body.String(), "convertedActivityId") {
			t.Fatal("anonymous native terminal filter", w.Code, w.Body.String())
		}
	}
	after := f.publicDomainDigest()
	f.call("POST", path+"/approve", ic.Approval{PreviewID: p.PreviewID}, 1, 200)
	if after != f.publicDomainDigest() {
		t.Fatal("repeat conversion effect")
	}
	f.call("GET", path, nil, 0, 409)
	for _, suffix := range []string{"?foo=x", "?"} {
		f.call("GET", path+suffix, nil, 1, 400)
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "birdtie_saf001_http_") {
		t.Fatal("owned restart", e)
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	for round := 0; round < 2; round++ {
		before = f.publicDomainDigest()
		cmd := exec.CommandContext(f.ctx, exe, "-test.run=^TestIntentConversionHTTPRestartChild$", "-test.v")
		cmd.Env = append(os.Environ(), "BIRDTIE_CONVERSION_CHILD=owned-native-http", "BIRDTIE_CONVERSION_DB="+cfg.ConnConfig.Database, "BIRDTIE_CONVERSION_TOKEN="+f.tokens[1], "BIRDTIE_CONVERSION_INTENT="+id, "BIRDTIE_CONVERSION_ACTIVITY="+activity, "BIRDTIE_CONVERSION_RSVP="+v.Data.ParticipationID, "BIRDTIE_CONVERSION_PREVIEW="+p.PreviewID)
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatal("actual OS API restart", e, string(out))
		}
		t.Logf("PLN_CONVERSION_PROCESS round=%d %s", round, string(out))
		if before != f.publicDomainDigest() {
			t.Fatal("restart GET or rejected old preview altered domain")
		}
	}
}
func TestIntentConversionHTTPRestartChild(t *testing.T) {
	if os.Getenv("BIRDTIE_CONVERSION_CHILD") == "" {
		return
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" || cfg.ConnConfig.Database != os.Getenv("BIRDTIE_CONVERSION_DB") || !strings.HasPrefix(cfg.ConnConfig.Database, "birdtie_saf001_http_") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("owned native child")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	s := postgres.New(pool, false)
	g, e := postgres.NewHumanIntentConversions(s)
	if e != nil {
		t.Fatal(e)
	}
	h := New(s, s, nil, s, s, s, s, s, s, s, s, nil, false, nil, nil, nil, WithHumanIntentConversions(g))
	srv := httptest.NewServer(h)
	defer srv.Close()
	path := "/v1/me/social-intents/" + os.Getenv("BIRDTIE_CONVERSION_INTENT") + "/activity-conversion"
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+os.Getenv("BIRDTIE_CONVERSION_TOKEN"))
	req.Header.Set("X-Request-ID", fmt.Sprintf("pln_conversion_restart_%d", os.Getpid()))
	res, e := srv.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	var v struct{ Data ic.List }
	e = json.NewDecoder(res.Body).Decode(&v)
	res.Body.Close()
	if e != nil || res.StatusCode != 200 || v.Data.Intent.ConvertedActivityID == nil || *v.Data.Intent.ConvertedActivityID != os.Getenv("BIRDTIE_CONVERSION_ACTIVITY") || v.Data.Intent.ConvertedParticipationID == nil || *v.Data.Intent.ConvertedParticipationID != os.Getenv("BIRDTIE_CONVERSION_RSVP") {
		t.Fatal("durable native association", e, res.StatusCode, v)
	}
	raw, _ := json.Marshal(ic.Approval{PreviewID: os.Getenv("BIRDTIE_CONVERSION_PREVIEW")})
	req, _ = http.NewRequestWithContext(ctx, "POST", srv.URL+path+"/approve", strings.NewReader(string(raw)))
	req.Header.Set("Authorization", "Bearer "+os.Getenv("BIRDTIE_CONVERSION_TOKEN"))
	req.Header.Set("Content-Type", "application/json")
	res, e = srv.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("restarted process accepted old ephemeral key", res.StatusCode)
	}
	t.Logf("PLN registered API child pid=%d requestId=pln_conversion_restart_%d durableGET200 oldPreview403", os.Getpid(), os.Getpid())
}

type conversionEncodingHook struct {
	ic.Gateway
	change func()
}

func (g conversionEncodingHook) ListOwn(c context.Context, a ic.Access, id string) (ic.List, error) {
	v, e := g.Gateway.ListOwn(c, a, id)
	if e != nil {
		return v, e
	}
	check := v.Revalidate
	v.Revalidate = func(c context.Context) error { g.change(); return check(c) }
	return v, nil
}
func TestIntentConversionHTTPNativeEncodingActualReturnedChoicesCurrent(t *testing.T) {
	for _, kind := range []string{"title_aba", "cancel_rsvp", "city_expired", "session_revoked"} {
		t.Run(kind, func(t *testing.T) {
			f, id, activity, g := conversionHTTPNative(t)
			h := conversionEncodingHook{Gateway: g, change: func() {
				switch kind {
				case "title_aba":
					f.exec(`UPDATE activities SET title='B' WHERE id=$1`, activity)
					f.exec(`UPDATE activities SET title='异地合成线上活动' WHERE id=$1`, activity)
				case "cancel_rsvp":
					if _, e := f.store.CancelParticipation(f.ctx, f.accountIDs[1], activity); e != nil {
						t.Fatal(e)
					}
				case "city_expired":
					f.exec(`UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.cities[0])
				case "session_revoked":
					f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[1])
				}
			}}
			f.handler = New(f.store, f.store, nil, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil, WithHumanIntentConversions(h))
			want := 409
			if kind == "session_revoked" {
				want = 403
			}
			raw := f.call("GET", "/v1/me/social-intents/"+id+"/activity-conversion", nil, 1, want)
			if strings.Contains(raw.Body.String(), activity) || strings.Contains(raw.Body.String(), "线上活动") {
				t.Fatal("late private payload leaked", raw.Body.String())
			}
		})
	}
}

func TestIntentConversionHTTPNativePreviouslyPublicIntentTerminalDoesNotDiscloseAssociation(t *testing.T) {
	f, id, activity, _ := conversionHTTPNative(t)
	// Explicit synthetic actor's public source before the original concrete
	// preview; not a real public declaration or a production trial.
	f.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, f.accountIDs[1])
	f.exec(`UPDATE social_intents SET audience='PUBLIC',updated_at=clock_timestamp() WHERE id=$1`, id)
	visible := f.call("GET", "/v1/social-intents/"+id, nil, 0, 200)
	if strings.Contains(visible.Body.String(), "convertedParticipationId") {
		t.Fatal("active original source leaked association")
	}
	p := conversionHTTPPreview(t, f, id, activity)
	var r struct{ Data ic.Receipt }
	raw := f.call("POST", "/v1/me/social-intents/"+id+"/activity-conversion/approve", ic.Approval{PreviewID: p.PreviewID}, 1, 200)
	json.Unmarshal(raw.Body.Bytes(), &r)
	for _, path := range []string{"/v1/social-intents", "/v1/social-intents/" + id} {
		want := 200
		if strings.HasSuffix(path, id) {
			want = 404
		}
		v := f.call("GET", path, nil, 0, want)
		if strings.Contains(v.Body.String(), id) || strings.Contains(v.Body.String(), r.Data.ParticipationID) {
			t.Fatal("previous PUBLIC terminal source exposed private conversion", v.Body.String())
		}
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, req)
		if w.Code != want || strings.Contains(w.Body.String(), id) || strings.Contains(w.Body.String(), r.Data.ParticipationID) {
			t.Fatal("anonymous terminal disclosure", w.Code, w.Body.String())
		}
	}
}
