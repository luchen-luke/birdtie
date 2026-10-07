package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/activityparticipation"
	"github.com/birdtie/birdtie/apps/api/internal/activityplan"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestPlansCurrentHTTPNativeJoinedOnlineAndReminderIndependent(t *testing.T) {
	f := crossCityOnlineNative(t)
	id := f.activityOf(activitypublish.Organizer{Type: "PERSON", ID: f.accountIDs[0]}, "public")
	f.call("POST", "/v1/activities/"+id+"/participations", nil, 1, http.StatusCreated)
	f.call("POST", "/v1/me/activity-plans", map[string]string{"activityId": id}, 1, http.StatusCreated)
	joined := f.call("GET", "/v1/me/participations", nil, 1, 200)
	if joined.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private cache")
	}
	var p struct {
		Data []activityparticipation.Overview
	}
	if json.Unmarshal(joined.Body.Bytes(), &p) != nil || len(p.Data) != 1 || p.Data[0].ActivityID != id || p.Data[0].Status != "going" || p.Data[0].Modality != "online" || p.Data[0].PlaceID != "" || p.Data[0].StartsAt == nil {
		t.Fatal("real native online RSVP", joined.Body.String())
	}
	t.Logf("PLN_ACTUAL_REGISTERED_WIRE_PARTICIPATIONS=%s", joined.Body.String())
	reminder := f.call("GET", "/v1/me/activity-plans", nil, 1, 200)
	t.Logf("PLN_ACTUAL_REGISTERED_WIRE_PLANS=%s", reminder.Body.String())
	var v struct{ Data []activityplan.Plan }
	if json.Unmarshal(reminder.Body.Bytes(), &v) != nil || len(v.Data) != 1 || v.Data[0].ActivityID != id || v.Data[0].Modality != "online" || v.Data[0].PlaceName != "" || v.Data[0].TimeZone != "UTC" {
		t.Fatal("native independent reminder", reminder.Body.String())
	}
	f.call("DELETE", "/v1/me/activity-plans/"+v.Data[0].ID, nil, 1, 204)
	joined = f.call("GET", "/v1/me/participations", nil, 1, 200)
	if json.Unmarshal(joined.Body.Bytes(), &p) != nil || len(p.Data) != 1 || p.Data[0].Status != "going" {
		t.Fatal("removing reminder cancelled RSVP")
	}
}

// Native store is unchanged; the test hook changes real native source after the
// endpoint has encoded data but before the original native validator executes.
type plansCurrentEncodingHook struct {
	*postgres.Store
	change func()
}

func (s *plansCurrentEncodingHook) ListActivityPlansCurrent(ctx context.Context, a activityplan.CurrentAccess) ([]activityplan.Plan, activityplan.CurrentValidation, error) {
	rows, check, e := s.Store.ListActivityPlansCurrent(ctx, a)
	if e != nil {
		return nil, nil, e
	}
	return rows, func(c context.Context) error { s.change(); return check(c) }, nil
}
func (s *plansCurrentEncodingHook) ListParticipationsCurrent(ctx context.Context, a activityplan.CurrentAccess) ([]activityparticipation.Overview, activityplan.CurrentValidation, error) {
	rows, check, e := s.Store.ListParticipationsCurrent(ctx, a)
	if e != nil {
		return nil, nil, e
	}
	return rows, func(c context.Context) error { s.change(); return check(c) }, nil
}
func TestPlansCurrentHTTPNativeAfterEncodingRejectsSourceAndSession(t *testing.T) {
	for _, path := range []string{"/v1/me/activity-plans", "/v1/me/participations"} {
		for _, kind := range []string{"source_aba", "session_revoked", "source_expiry"} {
			t.Run(path+"/"+kind, func(t *testing.T) {
				f := crossCityOnlineNative(t)
				id := f.activityOf(activitypublish.Organizer{Type: "PERSON", ID: f.accountIDs[0]}, "public")
				f.call("POST", "/v1/activities/"+id+"/participations", nil, 1, 201)
				f.call("POST", "/v1/me/activity-plans", map[string]string{"activityId": id}, 1, 201)
				before := f.identitySnapshot()
				want := 409
				hook := &plansCurrentEncodingHook{Store: f.store, change: func() {
					switch kind {
					case "source_aba":
						f.exec(`UPDATE activities SET publication_status='hidden' WHERE id=$1`, id)
						f.exec(`UPDATE activities SET publication_status='published' WHERE id=$1`, id)
					case "session_revoked":
						f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[1])
					case "source_expiry":
						f.exec(`UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.cities[0])
					}
					before = f.identitySnapshot() // includes deliberate fixture Session revoke, never an HTTP effect.
				}}
				f.handler = New(hook, f.store, nil, f.store, f.store, f.store, f.store, f.store, f.store, hook, f.store, nil, false, nil, nil, nil)
				if kind == "session_revoked" {
					want = 401
				}
				got := f.call("GET", path, nil, 1, want)
				if strings.Contains(got.Body.String(), "异地") || strings.Contains(got.Body.String(), id) || before != f.identitySnapshot() {
					t.Fatal("encoded private payload/identity leak", got.Body.String())
				}
				var count int
				if e := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM activity_plans WHERE owner_account_id=$1)+(SELECT count(*) FROM activity_participations WHERE participant_account_id=$1)`, f.accountIDs[1]).Scan(&count); e != nil || count != 2 {
					t.Fatal("GET altered original RSVP/reminder", e, count)
				}
			})
		}
	}
}
func TestPlansCurrentHTTPNativeOriginalIDsAndFactsAfterProcessRestart(t *testing.T) {
	f := crossCityOnlineNative(t)
	id := f.activityOf(activitypublish.Organizer{Type: "PERSON", ID: f.accountIDs[0]}, "public")
	f.call("POST", "/v1/activities/"+id+"/participations", nil, 1, 201)
	f.call("POST", "/v1/me/activity-plans", map[string]string{"activityId": id}, 1, 201)
	var v struct{ Data []activityplan.Plan }
	json.Unmarshal(f.call("GET", "/v1/me/activity-plans", nil, 1, 200).Body.Bytes(), &v)
	var p struct {
		Data []activityparticipation.Overview
	}
	json.Unmarshal(f.call("GET", "/v1/me/participations", nil, 1, 200).Body.Bytes(), &p)
	if len(v.Data) != 1 || len(p.Data) != 1 {
		t.Fatal("original native IDs absent")
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "birdtie_saf001_http_") {
		t.Fatal("owned restart boundary", e)
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	for round := 0; round < 2; round++ {
		before := f.publicDomainDigest()
		cmd := exec.CommandContext(f.ctx, exe, "-test.run=^TestPlansCurrentRestartChild$", "-test.v")
		cmd.Env = append(os.Environ(), "BIRDTIE_PLANS_CHILD=owned-native-http", "BIRDTIE_PLANS_DB="+cfg.ConnConfig.Database, "BIRDTIE_PLANS_TOKEN="+f.tokens[1], "BIRDTIE_PLANS_ACTIVITY="+id, "BIRDTIE_PLANS_REMINDER="+v.Data[0].ID, "BIRDTIE_PLANS_RSVP="+p.Data[0].ID)
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatal("actual restart", e, string(out))
		}
		t.Logf("PLN native independent OS process round=%d %s", round, string(out))
		if before != f.publicDomainDigest() {
			t.Fatal("restart GET wrote domain; only legitimate Session idle excluded")
		}
	}
}
func TestPlansCurrentRestartChild(t *testing.T) {
	if os.Getenv("BIRDTIE_PLANS_CHILD") == "" {
		return
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" || os.Getenv("BIRDTIE_PLANS_CHILD") != "owned-native-http" || cfg.ConnConfig.Database != os.Getenv("BIRDTIE_PLANS_DB") || !strings.HasPrefix(cfg.ConnConfig.Database, "birdtie_saf001_http_") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("explicit owned loopback restart boundary")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	s := postgres.New(pool, false)
	h := New(s, s, nil, s, s, s, s, s, s, s, s, nil, false, nil, nil, nil)
	srv := httptest.NewServer(h)
	defer srv.Close()
	for _, path := range []string{"/v1/me/participations", "/v1/me/activity-plans"} {
		req, e := http.NewRequestWithContext(ctx, "GET", srv.URL+path, nil)
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Authorization", "Bearer "+os.Getenv("BIRDTIE_PLANS_TOKEN"))
		req.Header.Set("X-Request-ID", fmt.Sprintf("pln_restart_%d", os.Getpid()))
		res, e := srv.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		var raw json.RawMessage
		e = json.NewDecoder(res.Body).Decode(&raw)
		res.Body.Close()
		if e != nil || res.StatusCode != 200 || res.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("registered own restart GET", e, res.StatusCode)
		}
		if path == "/v1/me/participations" {
			var v struct {
				Data []activityparticipation.Overview
			}
			if json.Unmarshal(raw, &v) != nil || len(v.Data) != 1 || v.Data[0].ID != os.Getenv("BIRDTIE_PLANS_RSVP") || v.Data[0].ActivityID != os.Getenv("BIRDTIE_PLANS_ACTIVITY") || v.Data[0].Status != "going" || v.Data[0].Modality != "online" || v.Data[0].StartsAt == nil || v.Data[0].EndsAt == nil || v.Data[0].TimeZone != "UTC" || v.Data[0].PlaceName != "" {
				t.Fatal("RSVP native durable facts", string(raw))
			}
		} else {
			var v struct{ Data []activityplan.Plan }
			if json.Unmarshal(raw, &v) != nil || len(v.Data) != 1 || v.Data[0].ID != os.Getenv("BIRDTIE_PLANS_REMINDER") || v.Data[0].ActivityID != os.Getenv("BIRDTIE_PLANS_ACTIVITY") || v.Data[0].Modality != "online" || v.Data[0].StartsAt == nil || v.Data[0].EndsAt == nil || v.Data[0].TimeZone != "UTC" || v.Data[0].PlaceName != "" {
				t.Fatal("Reminder native durable facts", string(raw))
			}
		}
		t.Logf("PLN restart pid=%d requestId=pln_restart_%d %s status=200", os.Getpid(), os.Getpid(), path)
	}
}
