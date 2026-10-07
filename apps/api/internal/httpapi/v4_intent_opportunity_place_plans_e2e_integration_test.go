package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	ai "github.com/birdtie/birdtie/apps/api/internal/activeintent"
	"github.com/birdtie/birdtie/apps/api/internal/activityplan"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/opportunity"
	"github.com/birdtie/birdtie/apps/api/internal/placematch"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/birdtie/birdtie/apps/api/internal/venue"
	"github.com/jackc/pgx/v5/pgxpool"
)

// These are independently owned synthetic domain fixtures, not real-world
// partner data or proof of physical attendance. All domain effects use the
// registered original human routes, with the original authorization.
type intentPlacePlansFixture struct {
	*crossCityOnlineFixture
	place, candidate, activity, intent string
	starts                             time.Time
}

func intentPlacePlansNative(t *testing.T) *intentPlacePlansFixture {
	t.Helper()
	f := &intentPlacePlansFixture{crossCityOnlineFixture: crossCityOnlineNative(t)}
	t.Cleanup(func() {
		ctx, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		// Remove only this fixture's rows before the parent removes its City.
		for _, q := range []string{
			`DELETE FROM activity_plans WHERE activity_id IN (SELECT id FROM activities WHERE created_by_account_id=ANY($1::uuid[]))`,
			`DELETE FROM activity_participations WHERE activity_id IN (SELECT id FROM activities WHERE created_by_account_id=ANY($1::uuid[]))`,
			`DELETE FROM activity_invitations WHERE activity_id IN (SELECT id FROM activities WHERE created_by_account_id=ANY($1::uuid[]))`,
			`DELETE FROM native_notification_decisions WHERE recipient_id=ANY($1::uuid[])`,
			`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM activities WHERE created_by_account_id=ANY($1::uuid[])`,
			`DELETE FROM venues WHERE source_candidate_id IN (SELECT id FROM venue_candidates WHERE submitted_by=ANY($1::uuid[]))`,
			`DELETE FROM venue_candidates WHERE submitted_by=ANY($1::uuid[])`,
			`DELETE FROM places WHERE maintainer_account_id=ANY($1::uuid[])`,
			`DELETE FROM city_editor_memberships WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`,
		} {
			if _, e := f.pool.Exec(ctx, q, f.accountIDs); e != nil {
				t.Error("E2E002 owned cleanup", e)
			}
		}
	})
	if e := f.pool.QueryRow(f.ctx, `INSERT INTO places(id,city_id,name,category_code,latitude,longitude,location_precision,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id)
 VALUES(gen_random_uuid(),$1,'合成端到端羽毛球馆','sports',57.15,-2.1,'point','published','LOCAL_SYNTHETIC','local:E2E002','合成维护者',$2) RETURNING id`, f.cities[0], f.accountIDs[0]).Scan(&f.place); e != nil {
		t.Fatal(e)
	}
	f.exec(`INSERT INTO city_editor_memberships(city_id,account_id,role,state) VALUES($1,$2,'contributor','active'),($1,$3,'reviewer','active')`, f.cities[0], f.accountIDs[0], f.accountIDs[1])
	var now time.Time
	if e := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	cap := 12
	w := f.request("POST", "/v1/cities/"+f.cities[0]+"/places/"+f.place+"/venue-candidates", venue.SubmitInput{Facts: venue.Facts{Capacity: &cap, ReservationSupport: "contact", Suitability: []string{"badminton"}, Amenities: []string{"indoor_court"}}, SourceURL: "https://example.org/local-synthetic-e2e002", RightsNote: "LOCAL_SYNTHETIC; not a real operator permission", ExpiresAt: now.Add(48 * time.Hour)}, 0, 201, nil)
	var candidate struct{ Data venue.Candidate }
	decodeIntentPlacePlans(t, w, &candidate)
	f.candidate = candidate.Data.ID
	f.request("POST", "/v1/venue-candidates/"+f.candidate+"/review", venue.ReviewInput{Decision: "approve", Note: "合成独立审核，不是现实授权"}, 1, 200, nil)
	f.starts = now.Add(time.Hour).Truncate(time.Second)
	w = f.request("POST", "/v1/me/activities", activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: f.accountIDs[0]}, CityID: f.cities[0], PlaceID: f.place, VenuePlaceID: f.place, Modality: "in_person", PhysicalPlaceStatus: "confirmed", Title: "合成端到端周末羽毛球", CategoryCode: "badminton", StartsAt: f.starts, EndsAt: f.starts.Add(time.Hour), TimeZone: "UTC", Visibility: "public", Capacity: &cap}, 0, 201, nil)
	var activity struct{ Data activitypublish.Activity }
	decodeIntentPlacePlans(t, w, &activity)
	f.activity = activity.Data.ID
	f.request("POST", "/v1/me/activities/"+f.activity+"/publish", nil, 0, 200, nil)
	constraints, _ := json.Marshal(map[string]any{"category": "badminton", "placeId": f.place})
	w = f.request("POST", "/v1/me/social-intents", socialintent.DraftInput{Type: "FIND_ACTIVITY", Title: "帮我找这里的羽毛球活动（合成）", Constraints: constraints, Audience: "PRIVATE", Modality: "IN_PERSON", ExpiresAt: now.Add(2 * time.Hour)}, 1, 201, nil)
	var draft struct{ Data socialintent.Record }
	decodeIntentPlacePlans(t, w, &draft)
	f.intent = draft.Data.ID
	w = f.request("GET", "/v1/me/active-social-intents/"+f.intent, nil, 1, 200, nil)
	var detail struct{ Data ai.Detail }
	decodeIntentPlacePlans(t, w, &detail)
	w = f.request("POST", "/v1/me/active-social-intents/"+f.intent+"/preview", ai.Input{Operation: "ACTIVATE", ExpectedVersion: detail.Data.Item.Version}, 1, 200, nil)
	var preview struct{ Data ai.Preview }
	decodeIntentPlacePlans(t, w, &preview)
	w = f.request("POST", "/v1/me/active-social-intents/"+f.intent+"/approve", map[string]string{"previewId": preview.Data.PreviewID}, 1, 200, nil)
	var receipt struct{ Data ai.Receipt }
	decodeIntentPlacePlans(t, w, &receipt)
	if receipt.Data.Item.Intent.ID != f.intent || receipt.Data.Item.Intent.Status != "ACTIVE" {
		t.Fatal("original concrete activation", w.Body.String())
	}
	return f
}

func decodeIntentPlacePlans(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if e := json.Unmarshal(w.Body.Bytes(), v); e != nil {
		t.Fatal("actual registered JSON", e)
	}
}

func (f *intentPlacePlansFixture) request(method, path string, body any, who, want int, proof *ea.View) *httptest.ResponseRecorder {
	f.t.Helper()
	var raw []byte
	var e error
	if body != nil {
		raw, e = json.Marshal(body)
		if e != nil {
			f.t.Fatal(e)
		}
	}
	r := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
	r.Header.Set("Content-Type", "application/json")
	if who >= 0 {
		r.Header.Set("Authorization", "Bearer "+f.tokens[who])
	}
	f.requestCount++
	rid := fmt.Sprintf("e2e002_%04d", f.requestCount)
	r.Header.Set("X-Request-ID", rid)
	if proof != nil {
		r.Header.Set("X-Birdtie-Action-Version", proof.SourceVersion)
		r.Header.Set("X-Birdtie-Action-Until", proof.ValidUntil.Format(time.RFC3339Nano))
		r.Header.Set("X-Birdtie-Action-Operation", "JOIN")
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	f.t.Logf("E2E002 registered request=%s method=%s path=%s subjectIndex=%d status=%d", rid, method, path, who, w.Code)
	if want != 0 && w.Code != want {
		f.t.Fatalf("%s want%d got%d %s", rid, want, w.Code, w.Body.String())
	}
	return w
}

func (f *intentPlacePlansFixture) actions() ea.View {
	f.t.Helper()
	w := f.request("GET", "/v1/me/entity-actions/activity/"+f.activity, nil, 1, 200, nil)
	var v struct{ Data ea.View }
	decodeIntentPlacePlans(f.t, w, &v)
	if !v.Data.Valid() || v.Data.Entity.ID != f.activity {
		f.t.Fatal("same Activity action source", w.Body.String())
	}
	for _, a := range v.Data.Actions {
		if a.Kind == ea.Join && a.Operation == "JOIN" && a.State == ea.Available && a.RequiresConfirmation {
			return v.Data
		}
	}
	f.t.Fatal("no real current confirmed JOIN")
	return ea.View{}
}

func (f *intentPlacePlansFixture) opportunity(found bool) {
	f.t.Helper()
	w := f.request("GET", "/v1/me/opportunities", nil, 1, 200, nil)
	var v struct {
		Data   []opportunity.Candidate
		Source string
	}
	decodeIntentPlacePlans(f.t, w, &v)
	matched := false
	for _, c := range v.Data {
		if c.IntentID == f.intent && c.Entity.ID == f.activity {
			if c.Entity.Type != "ACTIVITY" || c.Place.Type != "PLACE" || c.Place.ID != f.place || c.Action.Target.ID != f.activity || c.RuleVersion == "" || c.Reason == "" {
				f.t.Fatal("candidate ref consistency", w.Body.String())
			}
			matched = true
		}
	}
	if matched != found || v.Source != "RULE_BASED" {
		f.t.Fatal("native opportunity current supply", found, w.Body.String())
	}
}

func (f *intentPlacePlansFixture) plan() activityplan.Plan {
	f.t.Helper()
	w := f.request("GET", "/v1/me/activity-plans", nil, 1, 200, nil)
	var v struct{ Data []activityplan.Plan }
	decodeIntentPlacePlans(f.t, w, &v)
	for _, p := range v.Data {
		if p.ActivityID == f.activity {
			if !p.Available || p.PlaceID != f.place || p.PlaceName != "合成端到端羽毛球馆" || p.StartsAt == nil || !p.StartsAt.Equal(f.starts) || p.Modality != "in_person" || p.TimeZone != "UTC" {
				f.t.Fatal("original Plan time/place", w.Body.String())
			}
			return p
		}
	}
	f.t.Fatal("no persisted original Plan")
	return activityplan.Plan{}
}

func TestV4IntentOpportunityPlacePlansNativeRegisteredAndTwoProcessRestart(t *testing.T) {
	f := intentPlacePlansNative(t)
	before := f.identitySnapshot()
	readBefore := f.publicDomainDigest()
	f.opportunity(true)
	w := f.request("GET", "/v1/me/social-intents/"+f.intent+"/place-matches", nil, 1, 200, nil)
	var matches struct{ Data []placematch.Match }
	decodeIntentPlacePlans(t, w, &matches)
	found := false
	for _, m := range matches.Data {
		if m.Place.ID == f.place && m.IntentID == f.intent && m.HasReviewedVenue {
			found = true
		}
	}
	if !found {
		t.Fatal("selected original reviewed Place absent", w.Body.String())
	}
	for _, path := range []string{"/v1/places/" + f.place, "/v1/places/" + f.place + "/venue", "/v1/activities/" + f.activity} {
		w = f.request("GET", path, nil, 1, 200, nil)
		if !strings.Contains(w.Body.String(), f.place) {
			t.Fatal("Place/detail stable identity", path)
		}
	}
	proof := f.actions()
	if readBefore != f.publicDomainDigest() {
		t.Fatal("discovery/detail/action GET mutated domain")
	}
	w = f.request("POST", "/v1/activities/"+f.activity+"/participations", map[string]any{}, 1, 201, &proof)
	var p struct {
		Data struct{ ID, ActivityID, ParticipantAccountID, Status string }
	}
	decodeIntentPlacePlans(t, w, &p)
	if p.Data.ID == "" || p.Data.ActivityID != f.activity || p.Data.Status != "going" {
		t.Fatal("original RSVP not attendance", w.Body.String())
	}
	// Plans are an independent original domain action, not a duplicate RSVP.
	w = f.request("POST", "/v1/me/activity-plans", map[string]string{"activityId": f.activity}, 1, 201, nil)
	plan := f.plan()
	f.request("GET", "/v1/activities/"+f.activity+"/participations/me", nil, 1, 200, nil)
	if before != f.identitySnapshot() {
		t.Fatal("route implicitly mutated identity/Agent/contexts/Ties")
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "birdtie_saf001_http_") {
		t.Fatal("owned child boundary", e)
	}
	for round := 1; round <= 2; round++ {
		domainBefore := f.publicDomainDigest()
		cmd := exec.CommandContext(f.ctx, exe, "-test.run=^TestV4IntentOpportunityPlacePlansRestartChild$", "-test.v")
		cmd.Env = append(os.Environ(), "BIRDTIE_E2E002_CHILD=owned-native-http", "BIRDTIE_E2E002_DB="+cfg.ConnConfig.Database, "BIRDTIE_E2E002_TOKEN="+f.tokens[1], "BIRDTIE_E2E002_INTENT="+f.intent, "BIRDTIE_E2E002_ACTIVITY="+f.activity, "BIRDTIE_E2E002_PLACE="+f.place, "BIRDTIE_E2E002_PARTICIPATION="+p.Data.ID, "BIRDTIE_E2E002_PLAN="+plan.ID)
		var output bytes.Buffer
		cmd.Stdout, cmd.Stderr = &output, &output
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		pid := cmd.Process.Pid
		if e = cmd.Wait(); e != nil {
			t.Fatal("real API process restart", round, pid, e, output.String())
		}
		t.Logf("E2E002 actual independent API child round=%d PID=%d", round, pid)
		t.Log(strings.TrimSpace(output.String()))
		if domainBefore != f.publicDomainDigest() {
			t.Fatal("restart GET domain side effect; normal idle refresh is separately excluded")
		}
	}
	if before != f.identitySnapshot() {
		t.Fatal("API restart implicit identity/Tie effects")
	}
}

func TestV4IntentOpportunityPlacePlansNativeCurrentSourceGuard(t *testing.T) {
	for _, scenario := range []string{"hidden_place", "place_aba", "actor_blocks_host", "host_blocks_actor", "city_natural_expiry", "invite_only_uninvited", "session_revoked"} {
		t.Run(scenario, func(t *testing.T) {
			f := intentPlacePlansNative(t)
			f.opportunity(true)
			if scenario == "city_natural_expiry" {
				f.exec(`UPDATE cities SET expires_at=clock_timestamp()+interval '1 second' WHERE id=$1`, f.cities[0])
			}
			proof := f.actions()
			switch scenario {
			case "hidden_place":
				f.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place)
			case "place_aba":
				f.exec(`UPDATE places SET name='合成另一版本' WHERE id=$1`, f.place)
				f.exec(`UPDATE places SET name='合成端到端羽毛球馆' WHERE id=$1`, f.place)
			case "actor_blocks_host":
				f.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.accountIDs[1], f.accountIDs[0])
			case "host_blocks_actor":
				f.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.accountIDs[0], f.accountIDs[1])
			case "invite_only_uninvited":
				capacity := 12
				f.request("PUT", "/v1/me/activities/"+f.activity, activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: f.accountIDs[0]}, CityID: f.cities[0], PlaceID: f.place, VenuePlaceID: f.place, Modality: "in_person", PhysicalPlaceStatus: "confirmed", Title: "合成端到端周末羽毛球", CategoryCode: "badminton", StartsAt: f.starts, EndsAt: f.starts.Add(time.Hour), TimeZone: "UTC", Visibility: "invite_only", Capacity: &capacity}, 0, 200, nil)
				f.request("GET", "/v1/activities/"+f.activity, nil, 1, 404, nil)
			case "session_revoked":
				f.request("POST", "/v1/session/logout", nil, 1, http.StatusNoContent, nil)
			case "city_natural_expiry":
				for {
					var expired bool
					if e := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()>=expires_at FROM cities WHERE id=$1`, f.cities[0]).Scan(&expired); e != nil {
						t.Fatal(e)
					}
					if expired {
						break
					}
					select {
					case <-f.ctx.Done():
						t.Fatal("bounded native expiry wait")
					case <-time.After(20 * time.Millisecond):
					}
				}
			}
			before := f.publicDomainDigest()
			identity := f.identitySnapshot()
			want := http.StatusConflict
			// Original writer hides currently unauthorized/expired targets. A
			// visible target whose reviewed source version changed is a conflict.
			if scenario == "actor_blocks_host" || scenario == "host_blocks_actor" || scenario == "city_natural_expiry" || scenario == "invite_only_uninvited" {
				want = http.StatusNotFound
			}
			if scenario == "session_revoked" {
				want = http.StatusUnauthorized
			}
			f.request("POST", "/v1/activities/"+f.activity+"/participations", map[string]any{}, 1, want, &proof)
			if before != f.publicDomainDigest() || identity != f.identitySnapshot() {
				t.Fatal("denied stale approved proposal wrote domain/identity")
			}
			var count int
			if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM activity_participations WHERE activity_id=$1 AND participant_account_id=$2`, f.activity, f.accountIDs[1]).Scan(&count); e != nil || count != 0 {
				t.Fatal("negative JOIN side effects", count, e)
			}
			if scenario != "place_aba" && scenario != "session_revoked" {
				f.opportunity(false)
			}
		})
	}
}

func TestV4IntentOpportunityPlacePlansNativeOwnerAnonymousAndWorkspace(t *testing.T) {
	f := intentPlacePlansNative(t)
	before := f.publicDomainDigest()
	f.request("GET", "/v1/me/opportunities", nil, -1, 401, nil)
	f.request("GET", "/v1/me/social-intents/"+f.intent+"/place-matches", nil, 0, 404, nil)
	r := httptest.NewRequest("GET", "/v1/me/opportunities", nil)
	r.Header.Set("Authorization", "Bearer "+f.tokens[1])
	r.Header.Set("X-Birdtie-Organization-Workspace", f.accountIDs[0])
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("ORG workspace cannot inherit Person route", w.Code)
	}
	proof := f.actions()
	f.request("POST", "/v1/activities/"+f.activity+"/participations", map[string]any{}, 0, 409, &proof)
	if before != f.publicDomainDigest() {
		t.Fatal("authority boundary changed domain")
	}
}

func TestV4IntentOpportunityPlacePlansRestartChild(t *testing.T) {
	if os.Getenv("BIRDTIE_E2E002_CHILD") == "" {
		return
	}
	if os.Getenv("BIRDTIE_E2E002_CHILD") != "owned-native-http" || os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Fatal("explicit child gate")
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || cfg.ConnConfig.Database != os.Getenv("BIRDTIE_E2E002_DB") || !strings.HasPrefix(cfg.ConnConfig.Database, "birdtie_saf001_http_") || cfg.ConnConfig.Host != "127.0.0.1" {
		t.Fatal("owned loopback database only")
	}
	ctx, stop := context.WithTimeout(context.Background(), 20*time.Second)
	defer stop()
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	s := postgres.New(pool, false)
	g, e := postgres.NewHumanActiveIntents(s)
	if e != nil {
		t.Fatal(e)
	}
	h := New(s, s, nil, s, s, s, s, s, s, s, s, nil, false, nil, nil, nil, WithHumanActiveIntents(g))
	srv := httptest.NewServer(h)
	defer srv.Close()
	activity, place, intent := os.Getenv("BIRDTIE_E2E002_ACTIVITY"), os.Getenv("BIRDTIE_E2E002_PLACE"), os.Getenv("BIRDTIE_E2E002_INTENT")
	for i, path := range []string{"/v1/me/active-social-intents/" + intent, "/v1/me/opportunities", "/v1/me/social-intents/" + intent + "/place-matches", "/v1/places/" + place, "/v1/places/" + place + "/venue", "/v1/activities/" + activity, "/v1/activities/" + activity + "/participations/me", "/v1/me/activity-plans", "/v1/me/ties"} {
		r, e := http.NewRequestWithContext(ctx, "GET", srv.URL+path, nil)
		if e != nil {
			t.Fatal(e)
		}
		r.Header.Set("Authorization", "Bearer "+os.Getenv("BIRDTIE_E2E002_TOKEN"))
		r.Header.Set("X-Request-ID", fmt.Sprintf("e2e002_restart_%d_%d", os.Getpid(), i))
		resp, e := srv.Client().Do(r)
		if e != nil {
			t.Fatal(e)
		}
		var body json.RawMessage
		e = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if e != nil || resp.StatusCode != 200 {
			t.Fatal("registered restart GET", path, resp.StatusCode, e, string(body))
		}
		switch i {
		case 0:
			if !strings.Contains(string(body), intent) || !strings.Contains(string(body), "ACTIVE") {
				t.Fatal("original Intent persisted")
			}
		case 1:
			if !strings.Contains(string(body), activity) || !strings.Contains(string(body), place) {
				t.Fatal("original candidate source persisted")
			}
		case 6:
			if !strings.Contains(string(body), os.Getenv("BIRDTIE_E2E002_PARTICIPATION")) || !strings.Contains(string(body), "going") {
				t.Fatal("stable RSVP persisted not attendance")
			}
		case 7:
			if !strings.Contains(string(body), os.Getenv("BIRDTIE_E2E002_PLAN")) || !strings.Contains(string(body), place) {
				t.Fatal("original Plan/place persisted")
			}
		case 8:
			var v struct{ Data []any }
			if json.Unmarshal(body, &v) != nil || len(v.Data) != 0 {
				t.Fatal("no implicit tie")
			}
		}
		t.Logf("E2E002 OS PID=%d request=e2e002_restart_%d_%d GET=%s status=200", os.Getpid(), os.Getpid(), i, path)
	}
}
