package httpapi

import (
	"context"
	"encoding/json"
	apd "github.com/birdtie/birdtie/apps/api/internal/activityparticipationdisclosure"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParticipationDisclosureHTTPNativeActualRegisteredLifecycle(t *testing.T) {
	pool := interestHTTPOwnedDB(t)
	ctx, c := context.WithTimeout(context.Background(), 45*time.Second)
	defer c()
	s := postgres.New(pool, false)
	var owner, other string
	for _, p := range []*string{&owner, &other} {
		if e := pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(p); e != nil {
			t.Fatal(e)
		}
		if _, e := pool.Exec(ctx, `INSERT INTO agents(agent_type,principal_account_id) VALUES('personal',$1)`, *p); e != nil {
			t.Fatal(e)
		}
	}
	token, d, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '2 hours',clock_timestamp()+interval '1 hour')`, owner, d[:]); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label) VALUES('disclosure-http','合成City','test','GB','Europe/London','published','合成','disposable://activity-disclosure','合成')`); e != nil {
		t.Fatal(e)
	}
	act, e := s.CreateSocialDraft(ctx, other, activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: other}, CityID: "disclosure-http", Title: "合成HTTP报名活动", Summary: "不是实际到场", Visibility: "public", StartsAt: time.Now().UTC().Add(time.Hour), EndsAt: time.Now().UTC().Add(3 * time.Hour), TimeZone: "Europe/London"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.PublishSocialActivity(ctx, other, act.ID); e != nil {
		t.Fatal(e)
	}
	p, _, e := s.JoinActivity(ctx, owner, act.ID)
	if e != nil {
		t.Fatal(e)
	}
	h := New(s, s, nil, nil, nil, s, nil, s, nil, nil, nil, nil, false, nil, nil, nil)
	call := func(method, path, body string, want int, workspace bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if workspace {
			r.Header.Set("X-Birdtie-Organization-Workspace", owner)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s status%d want%d %s", method, path, w.Code, want, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cache")
		}
		if want == 200 {
			t.Log("NATIVE_PARTICIPATION_WIRE", path, w.Body.String())
		}
		return w
	}
	read := func(w *httptest.ResponseRecorder) apd.View {
		var v struct {
			Data apd.View `json:"data"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil || apd.ValidateView(v.Data, owner) != nil {
			t.Fatal(e, w.Body.String())
		}
		return v.Data
	}
	prefix := "/v1/me/activity-participation-disclosures"
	old, e := s.GetParticipation(ctx, owner, act.ID)
	if e != nil {
		t.Fatal(e)
	}
	call("GET", prefix+"?ownerId="+other, "", 400, false)
	call("GET", prefix, "", 403, true)
	call("GET", prefix, `{"owner":"x"}`, 400, false)
	before := read(call("GET", prefix, "", 200, false))
	if len(before.Records) != 1 || before.Records[0].Visibility != "PRIVATE" {
		t.Fatal(before)
	}
	read(call("GET", prefix+"/options", "", 200, false))
	call("POST", prefix+"/preview", `{"participationId":"`+p.ID+`","operation":"PUBLIC","confirmed":true}`, 400, false)
	expiry := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	raw, _ := json.Marshal(apd.Input{ParticipationID: p.ID, Operation: "PUBLIC", DisclosureExpiresAt: &expiry})
	var pv struct {
		Data apd.Preview `json:"data"`
	}
	if e = json.Unmarshal(call("POST", prefix+"/preview", string(raw), 200, false).Body.Bytes(), &pv); e != nil || apd.ValidatePreview(pv.Data, owner, apd.Input{ParticipationID: p.ID, Operation: "PUBLIC", DisclosureExpiresAt: &expiry}) != nil {
		t.Fatal(e, pv)
	}
	body, _ := json.Marshal(map[string]string{"preview": pv.Data.Preview})
	got := read(call("POST", prefix+"/approve", string(body), 200, false))
	if !got.Records[0].EffectivePublic || got.Records[0].ActivityID != act.ID || got.Records[0].ParticipationID != p.ID {
		t.Fatal(got)
	}
	call("POST", prefix+"/approve", string(body), 409, false)
	if _, e = pool.Exec(ctx, `UPDATE activities SET visibility='private' WHERE id=$1`, act.ID); e != nil {
		t.Fatal(e)
	}
	hidden := read(call("GET", prefix, "", 200, false))
	if hidden.Records[0].SourceAvailable || hidden.Records[0].StartsAt != nil {
		t.Fatal(hidden)
	}
	raw, _ = json.Marshal(apd.Input{ParticipationID: p.ID, Operation: "PRIVATE"})
	if e = json.Unmarshal(call("POST", prefix+"/preview", string(raw), 200, false).Body.Bytes(), &pv); e != nil {
		t.Fatal(e)
	}
	body, _ = json.Marshal(map[string]string{"preview": pv.Data.Preview})
	read(call("POST", prefix+"/approve", string(body), 200, false))
	now, e := s.GetParticipation(ctx, owner, act.ID)
	if e != nil || now.ID != old.ID || now.Status != old.Status || !now.UpdatedAt.Equal(old.UpdatedAt) {
		t.Fatal("disclosure mutated original RSVP", e, old, now)
	}
	if _, e = pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, d[:]); e != nil {
		t.Fatal(e)
	}
	call("GET", prefix, "", 401, false)
}
