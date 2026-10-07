package httpapi

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	ns "github.com/birdtie/birdtie/apps/api/internal/agentnotificationschedule"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
)

func notificationScheduleNativeHTTP(t *testing.T) *privateProfileHTTPDBFixture {
	t.Helper()
	f := notificationPolicyHTTPDBNew(t)
	for i := 0; i < 2; i++ {
		p, e := actorref.ParsePrincipal("PERSON", f.accountIDs[i])
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.store.EnsureAgentProfile(f.ctx, f.agentIDs[i], p); e != nil {
			t.Fatal(e)
		}
	}
	return f
}
func notificationScheduleNativeInput(t *testing.T, f *privateProfileHTTPDBFixture) ns.PutInput {
	t.Helper()
	var at time.Time
	if e := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
		t.Fatal(e)
	}
	return ns.PutInput{Settings: ns.Settings{Enabled: true, TimeZone: "UTC", LocalMinute: 720, GapPolicy: ns.GapSkip, FoldPolicy: ns.FoldEarlierOnce, Quiet: nil, MaxContactsPerDay: 1, Categories: []agentnotification.Category{agentnotification.CategoryMessage}}, ExpiresAt: at.Add(10 * time.Minute).UTC().Truncate(time.Microsecond)}
}
func notificationScheduleNativeBody(t *testing.T, in ns.PutInput) string {
	t.Helper()
	b, e := json.Marshal(in)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func notificationScheduleNativeRecord(t *testing.T, b []byte) ns.Policy {
	t.Helper()
	var v struct {
		Data ns.Policy `json:"data"`
	}
	if e := json.Unmarshal(b, &v); e != nil || ns.ValidatePolicy(v.Data) != nil {
		t.Fatal("actual schedule wire invalid", e)
	}
	return v.Data
}
func TestNotificationScheduleNativeHTTPCurrentLifecycle(t *testing.T) {
	f := notificationScheduleNativeHTTP(t)
	before := notificationPolicyHTTPDBSourceSnapshot(t, f)
	var p ns.Policy
	const path = "/v1/me/notification-schedule"
	in := notificationScheduleNativeInput(t, f)
	t.Run("unconfigured_read_no_default_write", func(t *testing.T) {
		w := f.request(t, f.handler, "GET", path, "", f.tokens[0], 200, nil)
		p = notificationScheduleNativeRecord(t, w.Body.Bytes())
		var n int
		if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM native_notification_schedules WHERE owner_id=ANY($1::uuid[])`, f.accountIDs).Scan(&n); e != nil || n != 0 || p.Configured || p.Version != 0 || p.MaxContactsPerDay != 0 {
			t.Fatal("GET created a schedule", n, e, p)
		}
	})
	t.Run("explicit_current_version_save_and_read", func(t *testing.T) {
		p = notificationScheduleNativeRecord(t, f.request(t, f.handler, "PUT", path, notificationScheduleNativeBody(t, in), f.tokens[0], 200, nil).Body.Bytes())
		read := notificationScheduleNativeRecord(t, f.request(t, f.handler, "GET", path, "", f.tokens[0], 200, nil).Body.Bytes())
		if p.Version != 1 || p.AgentID != f.agentIDs[0] || !reflect.DeepEqual(p, read) || !p.ValidFrom.Equal(*p.UpdatedAt) {
			t.Fatal("native CAS/current read mismatch", p, read)
		}
	})
	t.Run("stale_cas_peer_and_org_do_not_replace", func(t *testing.T) {
		f.request(t, f.handler, "PUT", path, notificationScheduleNativeBody(t, in), f.tokens[0], 409, nil)
		foreign := in
		foreign.ExpectedVersion = p.Version
		f.request(t, f.handler, "PUT", path, notificationScheduleNativeBody(t, foreign), f.tokens[1], 409, nil)
		f.request(t, f.handler, "GET", path, "", f.tokens[2], 403, nil)
		if read := notificationScheduleNativeRecord(t, f.request(t, f.handler, "GET", path, "", f.tokens[0], 200, nil).Body.Bytes()); !reflect.DeepEqual(read, p) {
			t.Fatal("denied request changed native schedule")
		}
	})
	t.Run("explicit_off_preserves_origin_rule_and_no_inference", func(t *testing.T) {
		in.ExpectedVersion = p.Version
		in.Enabled = false
		p = notificationScheduleNativeRecord(t, f.request(t, f.handler, "PUT", path, notificationScheduleNativeBody(t, in), f.tokens[0], 200, nil).Body.Bytes())
		if p.Status != "DISABLED" || p.Version != 2 || p.Enabled {
			t.Fatal("explicit off not stored", p)
		}
		var n int
		if e := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM native_notification_schedule_slots WHERE owner_id=ANY($1::uuid[]))+(SELECT count(*) FROM native_notification_schedule_deliveries WHERE owner_id=ANY($1::uuid[]))+(SELECT count(*) FROM agent_tasks WHERE owner_account_id=ANY($1::uuid[]))`, f.accountIDs).Scan(&n); e != nil || n != 0 {
			t.Fatal("settings created tasks/delivery", n, e)
		}
	})
	if notificationPolicyHTTPDBSourceSnapshot(t, f) != before {
		t.Fatal("schedule settings changed original Profile/Memory/source")
	}
	t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY actual registered099 HTTP CAS, not IdP or delivery")
}

// Hook is invoked by the original response handler after it has encoded the
// actual native policy. It mutates the owned Agent, not the result or session.
type notificationScheduleNativeEncodingHook struct {
	*postgres.Store
	change func()
}

func (s *notificationScheduleNativeEncodingHook) ValidateHumanSocialResponse(ctx context.Context, d [32]byte, a identity.Actor) error {
	s.change()
	return s.Store.ValidateHumanSocialResponse(ctx, d, a)
}
func TestNotificationScheduleNativeHTTPAfterEncodingAgentRetired(t *testing.T) {
	for _, method := range []string{"GET", "PUT"} {
		t.Run(method, func(t *testing.T) {
			f := notificationScheduleNativeHTTP(t)
			body := ""
			if method == "PUT" {
				body = notificationScheduleNativeBody(t, notificationScheduleNativeInput(t, f))
			}
			hook := &notificationScheduleNativeEncodingHook{Store: f.store, change: func() { f.exec(`UPDATE agents SET status='retired' WHERE id=$1`, f.agentIDs[0]) }}
			h := New(hook, hook, nil, hook, hook, hook, hook, hook, hook, hook, hook, nil, false, nil, nil, nil)
			w := f.request(t, h, method, "/v1/me/notification-schedule", body, f.tokens[0], 403, nil)
			if strings.Contains(w.Body.String(), `"data"`) || strings.Contains(w.Body.String(), f.agentIDs[0]) {
				t.Fatal("retired Agent metadata released")
			}
			var count int
			if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM native_notification_schedules WHERE owner_id=$1`, f.accountIDs[0]).Scan(&count); e != nil {
				t.Fatal(e)
			}
			want := 0
			if method == "PUT" {
				want = 1
			}
			if count != want {
				t.Fatal("response rejection cannot undo an already committed PUT", count, want)
			}
		})
	}
}
