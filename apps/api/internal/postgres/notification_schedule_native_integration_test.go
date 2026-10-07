package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	ns "github.com/birdtie/birdtie/apps/api/internal/agentnotificationschedule"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This suite executes the real099 functions, Store and original typed Inbox.
// The acceptance runner supplies a new exclusively owned migrated local DB;
// global schedulers must not be run against a phone or another worker's DB.
func notificationScheduleNativeClock(t *testing.T, ctx context.Context, p *pgxpool.Pool) time.Time {
	t.Helper()
	var at time.Time
	if e := p.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
		t.Fatal(e)
	}
	return at.UTC()
}
func notificationScheduleNativeDue(t *testing.T, ctx context.Context, p *pgxpool.Pool) time.Time {
	t.Helper()
	n := notificationScheduleNativeClock(t, ctx, p)
	due := n.Truncate(time.Minute).Add(time.Minute)
	if due.Sub(n) < 10*time.Second {
		due = due.Add(time.Minute)
	}
	return due
}
func notificationScheduleNativeWait(t *testing.T, ctx context.Context, p *pgxpool.Pool, due time.Time) {
	t.Helper()
	start := time.Now()
	for {
		now := notificationScheduleNativeClock(t, ctx, p)
		if !now.Before(due) {
			t.Logf("ACTUAL_PG_DUE_REACHED due=%s database_clock=%s elapsed=%s", due.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), time.Since(start))
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("bounded real due wait", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}
func notificationScheduleNativePut(t *testing.T, ctx context.Context, p *pgxpool.Pool, s *Store, a agentprofile.PrivateAccess, version uint64, zone string, due time.Time, enabled bool, quiet *ns.QuietWindow) ns.Policy {
	t.Helper()
	loc, e := ns.Location(zone)
	if e != nil {
		t.Fatal(e)
	}
	local := due.In(loc)
	input := ns.PutInput{ExpectedVersion: version, Settings: ns.Settings{Enabled: enabled, TimeZone: zone, LocalMinute: local.Hour()*60 + local.Minute(), GapPolicy: ns.GapSkip, FoldPolicy: ns.FoldEarlierOnce, Quiet: quiet, MaxContactsPerDay: 1, Categories: []agentnotification.Category{agentnotification.CategoryMessage}}, ExpiresAt: notificationScheduleNativeClock(t, ctx, p).Add(10 * time.Minute).Truncate(time.Microsecond)}
	out, e := s.PutOwnNotificationSchedule(ctx, a, input)
	if e != nil {
		t.Fatal("actual current schedule PUT", e)
	}
	if out.Version != version+1 || out.ValidFrom == nil || !out.ValidFrom.Equal(*out.UpdatedAt) || !out.ValidFrom.Before(due) {
		t.Fatal("actual legal future schedule", out, due)
	}
	return out
}
func notificationScheduleNativeProtected(t *testing.T, ctx context.Context, p *pgxpool.Pool, accounts []string) string {
	t.Helper()
	var out string
	e := p.QueryRow(ctx, `SELECT jsonb_build_object(
 'public',COALESCE((SELECT jsonb_agg(to_jsonb(x)||jsonb_build_object('xmin',x.xmin::text) ORDER BY account_id) FROM user_profiles x WHERE account_id=ANY($1::uuid[])),'[]'::jsonb),
 'metadata',COALESCE((SELECT jsonb_agg(to_jsonb(x)||jsonb_build_object('xmin',x.xmin::text) ORDER BY agent_id) FROM agent_profiles x WHERE owner_id=ANY($1::uuid[])),'[]'::jsonb),
 'private',COALESCE((SELECT jsonb_agg(to_jsonb(x)||jsonb_build_object('xmin',x.xmin::text) ORDER BY agent_id) FROM agent_private_profiles x WHERE owner_id=ANY($1::uuid[])),'[]'::jsonb),
 'visibility',COALESCE((SELECT jsonb_agg(to_jsonb(x)||jsonb_build_object('xmin',x.xmin::text) ORDER BY agent_id) FROM agent_profile_field_visibility x WHERE owner_id=ANY($1::uuid[])),'[]'::jsonb),
 'memory',COALESCE((SELECT jsonb_agg(to_jsonb(x)||jsonb_build_object('xmin',x.xmin::text) ORDER BY id) FROM agent_memories x WHERE owner_id=ANY($1::uuid[])),'[]'::jsonb),
 'decisions',COALESCE((SELECT jsonb_agg(to_jsonb(x)||jsonb_build_object('xmin',x.xmin::text) ORDER BY id) FROM native_notification_decisions x WHERE recipient_id=ANY($1::uuid[])),'[]'::jsonb),
 'tasks',COALESCE((SELECT jsonb_agg(to_jsonb(x)||jsonb_build_object('xmin',x.xmin::text) ORDER BY id) FROM agent_tasks x WHERE owner_account_id=ANY($1::uuid[])),'[]'::jsonb)
 )::text`, accounts).Scan(&out)
	if e != nil {
		t.Fatal("owned exact source snapshot", e)
	}
	return out
}
func notificationScheduleNativeCounts(t *testing.T, ctx context.Context, p *pgxpool.Pool, owner string) (slots, deliveries, inbox int) {
	t.Helper()
	e := p.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM native_notification_schedule_slots WHERE owner_id=$1),
 (SELECT count(*) FROM native_notification_schedule_deliveries WHERE owner_id=$1 AND delivered),
 (SELECT count(*) FROM inbox_items i JOIN native_notification_decisions d ON d.id=i.routing_decision_id WHERE d.recipient_id=$1 AND d.disposition='DIGEST')`, owner).Scan(&slots, &deliveries, &inbox)
	if e != nil {
		t.Fatal(e)
	}
	return
}

func TestNotificationScheduleNativeDSTSQL(t *testing.T) {
	f := nativeNotificationFixture(t)
	p := f.base.pool
	for _, tc := range []struct {
		name, zone, day, expected string
		minute                    int
	}{
		{"London_gap", "Europe/London", "2026-03-29", "", 90},
		{"London_fold_earlier_once", "Europe/London", "2026-10-25", "2026-10-25T00:30:00Z", 90},
		{"NY_gap", "America/New_York", "2026-03-08", "", 150},
		{"NY_fold_earlier_once", "America/New_York", "2026-11-01", "2026-11-01T05:30:00Z", 90},
		{"Lord_Howe_half_hour", "Australia/Lord_Howe", "2026-04-05", "2026-04-04T14:45:00Z", 105},
		{"Apia_skipped_date", "Pacific/Apia", "2011-12-30", "", 720},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"timeZone": tc.zone, "localMinute": tc.minute})
			var actual *time.Time
			if e := p.QueryRow(f.base.ctx, `SELECT birdtie_notification_schedule_wall_time($1::jsonb,$2::date)`, raw, tc.day).Scan(&actual); e != nil {
				t.Fatal("actual099 wall time", e)
			}
			day, e := time.Parse("2006-01-02", tc.day)
			if e != nil {
				t.Fatal(e)
			}
			loc, e := ns.Location(tc.zone)
			if e != nil {
				t.Fatal(e)
			}
			goAt, ok := ns.WallInstant(loc, day.Year(), day.Month(), day.Day(), tc.minute)
			if (actual != nil) != ok || (actual != nil && (!actual.Equal(goAt) || actual.UTC().Format(time.RFC3339) != tc.expected)) || (actual == nil && tc.expected != "") {
				t.Fatal("PG vs Go DST mismatch", actual, goAt, ok, tc.expected)
			}
			t.Logf("ACTUAL_PG_WALL_TIME_ONLY local_day=%s zone=%s result=%v; not historical scheduler execution", tc.day, tc.zone, actual)
		})
	}
}

func TestNotificationScheduleNativeRealLifecycle(t *testing.T) {
	f := newNotificationDeliveryFixture(t)
	b := f.private.base
	off := newNotificationDeliveryFixture(t)
	paused := newNotificationDeliveryFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	// Finish inherited45s fixture preparation before waiting on actual PG time.
	cv, _ := f.friend(t)
	f.policy(t, f.private.peer, agentnotification.Digest)
	m1 := f.message(t, cv)
	m2 := f.message(t, cv)
	offcv, _ := off.friend(t)
	off.policy(t, off.private.peer, agentnotification.Digest)
	off.message(t, offcv)
	pausedcv, _ := paused.friend(t)
	paused.policy(t, paused.private.owner, agentnotification.Digest)
	if _, e := paused.private.base.store.SendMessage(paused.private.base.ctx, paused.private.base.other.ID, pausedcv.ID, "SCHEDULE_PRIVATE_BODY paused original source"); e != nil {
		t.Fatal(e)
	}
	accounts := append(append(append([]string{}, b.accounts...), off.private.base.accounts...), paused.private.base.accounts...)
	protected := notificationScheduleNativeProtected(t, ctx, b.pool, accounts)
	due := notificationScheduleNativeDue(t, ctx, b.pool)
	var active, offPolicy ns.Policy
	t.Run("unconfigured_GET_does_not_create_daily_work", func(t *testing.T) {
		p, e := b.store.GetOwnNotificationSchedule(ctx, f.private.peer)
		if e != nil || p.Configured || p.Version != 0 || p.Enabled || p.MaxContactsPerDay != 0 {
			t.Fatal(p, e)
		}
		var n int
		if e = b.pool.QueryRow(ctx, `SELECT count(*) FROM native_notification_schedules WHERE owner_id=ANY($1::uuid[])`, accounts).Scan(&n); e != nil || n != 0 {
			t.Fatal("read made plan", n, e)
		}
	})
	t.Run("actual_PUT_CAS_and_independent_pool_read", func(t *testing.T) {
		active = notificationScheduleNativePut(t, ctx, b.pool, b.store, f.private.peer, 0, "Pacific/Pago_Pago", due, true, nil)
		loc, _ := ns.Location(active.TimeZone)
		if computed, ok, _ := ns.Due(active, due); !ok || !computed.DueAt.Equal(due) || computed.LocalDate != due.In(loc).Format("2006-01-02") {
			t.Fatal("legal real due mismatch", computed, active)
		}
		second, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
		if e != nil {
			t.Fatal(e)
		}
		defer second.Close()
		read, e := New(second, false).GetOwnNotificationSchedule(ctx, f.private.peer)
		if e != nil || !reflect.DeepEqual(active, read) {
			t.Fatal("independent pool persistence", e)
		}
		bad := ns.PutInput{ExpectedVersion: 0, Settings: active.Settings, ExpiresAt: *active.ExpiresAt}
		if got, e := b.store.PutOwnNotificationSchedule(ctx, f.private.peer, bad); !errors.Is(e, ns.ErrChanged) || got.Version != 0 {
			t.Fatal("stale CAS", got, e)
		}
	})
	t.Run("off_quiet_and_origin_revoked_before_real_due", func(t *testing.T) {
		// Empty sender, disabled recipient, quiet sender and revoked sender are
		// separate owners. The disabled/revoked recipients have real pending input.
		notificationScheduleNativePut(t, ctx, b.pool, b.store, f.private.owner, 0, "UTC", due, true, nil)
		offPolicy = notificationScheduleNativePut(t, ctx, b.pool, off.private.base.store, off.private.peer, 0, "UTC", due, true, nil)
		offPolicy = notificationScheduleNativePut(t, ctx, b.pool, off.private.base.store, off.private.peer, offPolicy.Version, "UTC", due, false, nil)
		minute := due.Hour()*60 + due.Minute()
		quiet := &ns.QuietWindow{StartMinute: minute, EndMinute: (minute + 30) % 1440}
		notificationScheduleNativePut(t, ctx, b.pool, off.private.base.store, off.private.owner, 0, "UTC", due, true, quiet)
		notificationScheduleNativePut(t, ctx, b.pool, paused.private.base.store, paused.private.owner, 0, "UTC", due, true, nil)
		if _, e := b.pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, paused.private.ownerSession); e != nil {
			t.Fatal(e)
		}
		_, d, _ := notificationScheduleNativeCounts(t, ctx, b.pool, b.other.ID)
		if d != 0 {
			t.Fatal("delivered before due")
		}
	})
	notificationScheduleNativeWait(t, ctx, b.pool, due)
	t.Run("two_original_Store_workers_one_slot_one_Inbox_and_EMPTY", func(t *testing.T) {
		second, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
		if e != nil {
			t.Fatal(e)
		}
		defer second.Close()
		stores := []*Store{b.store, New(second, false)}
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for _, s := range stores {
			wg.Add(1)
			go func(s *Store) {
				defer wg.Done()
				<-start
				_, e := s.ProcessNotificationSchedules(ctx, ns.MaxOwnersPerRun)
				results <- e
			}(s)
		}
		close(start)
		wg.Wait()
		close(results)
		for e := range results {
			if e != nil {
				t.Fatal("real concurrent processing", e)
			}
		}
		if s, d, i := notificationScheduleNativeCounts(t, ctx, b.pool, b.other.ID); s != 1 || d != 1 || i != 1 {
			t.Fatal("duplicate/missing actual effect", s, d, i)
		}
		if s, d, i := notificationScheduleNativeCounts(t, ctx, b.pool, b.person.ID); s != 1 || d != 0 || i != 0 {
			t.Fatal("real EMPTY slot", s, d, i)
		}
		var state string
		if e = b.pool.QueryRow(ctx, `SELECT state FROM native_notification_schedule_slots WHERE owner_id=$1`, b.person.ID).Scan(&state); e != nil || state != "EMPTY" {
			t.Fatal(state, e)
		}
		out, e := New(second, false).ProcessNotificationSchedules(ctx, ns.MaxOwnersPerRun)
		if e != nil || out.Slots != 0 || out.Delivered != 0 {
			t.Fatal("repeat process not idempotent", out, e)
		}
		items, e := b.store.ListInbox(ctx, b.other.ID)
		if e != nil {
			t.Fatal(e)
		}
		matched := 0
		var deliveredID string
		if e = b.pool.QueryRow(ctx, `SELECT i.id FROM inbox_items i JOIN native_notification_decisions d ON d.id=i.routing_decision_id WHERE d.source_id=$1 AND d.recipient_id=$2`, m1.ID, b.other.ID).Scan(&deliveredID); e != nil {
			t.Fatal("actual delivered message receipt", e)
		}
		for _, item := range items {
			if item.ID == deliveredID && item.TargetConversationID != nil && *item.TargetConversationID == cv.ID && item.NotificationRoute == string(agentnotification.Digest) {
				matched++
				assertNotificationDeliveryNoPayload(t, item)
			}
		}
		if matched != 1 {
			t.Fatal("real original typed Inbox target missing", matched)
		}
	})
	t.Run("disabled_quiet_and_revoked_have_zero_slots_and_zero_delivery", func(t *testing.T) {
		for _, owner := range []string{off.private.base.other.ID, off.private.base.person.ID, paused.private.base.person.ID} {
			if s, d, i := notificationScheduleNativeCounts(t, ctx, b.pool, owner); s != 0 || d != 0 || i != 0 {
				t.Fatal("closed source had effects", s, d, i)
			}
		}
		if p, e := off.private.base.store.GetOwnNotificationSchedule(ctx, off.private.peer); e != nil || p.Status != "DISABLED" || p.Version != offPolicy.Version {
			t.Fatal("off persistence", p, e)
		}
		_, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		if _, e = b.pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, paused.private.base.person.ID, digest[:]); e != nil {
			t.Fatal(e)
		}
		newAccess := paused.private.owner
		newAccess.SessionDigest = digest
		if p, e := paused.private.base.store.GetOwnNotificationSchedule(ctx, newAccess); e != nil || p.Status != "PAUSED_SESSION" {
			t.Fatal("fresh session silently rebound old plan", p, e)
		}
	})
	t.Run("used099_down_rejects_without_erasing_history", func(t *testing.T) {
		sql, e := os.ReadFile("../../migrations/099_native_notification_schedules.down.sql")
		if e != nil {
			t.Fatal(e)
		}
		conn, e := b.pool.Acquire(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer conn.Release()
		_, e = conn.Exec(ctx, string(sql))
		var pe *pgconn.PgError
		if !errors.As(e, &pe) || pe.Code != "P0001" || !strings.Contains(pe.Message, "down refuses to erase") {
			t.Fatal("actual used down did not reject", e)
		}
		if _, e = conn.Exec(ctx, "ROLLBACK"); e != nil {
			t.Fatal(e)
		}
		if s, d, i := notificationScheduleNativeCounts(t, ctx, b.pool, b.other.ID); s != 1 || d != 1 || i != 1 {
			t.Fatal("down destroyed used data", s, d, i)
		}
	})
	nextDue := notificationScheduleNativeDue(t, ctx, b.pool)
	t.Run("off_on_zone_edit_and_Inbox_delete_do_not_reset_receipt", func(t *testing.T) {
		active = notificationScheduleNativePut(t, ctx, b.pool, b.store, f.private.peer, active.Version, "Pacific/Pago_Pago", nextDue, false, nil)
		active = notificationScheduleNativePut(t, ctx, b.pool, b.store, f.private.peer, active.Version, "Pacific/Kiritimati", nextDue, true, nil)
		var oldDay string
		if e := b.pool.QueryRow(ctx, `SELECT local_date::text FROM native_notification_schedule_slots WHERE owner_id=$1`, b.other.ID).Scan(&oldDay); e != nil {
			t.Fatal(e)
		}
		newLoc, _ := ns.Location(active.TimeZone)
		if oldDay == nextDue.In(newLoc).Format("2006-01-02") {
			t.Fatal("budget control must use a genuinely new local date")
		}
		if _, e := b.pool.Exec(ctx, `DELETE FROM inbox_items WHERE id IN(SELECT inbox_item_id FROM native_notification_schedule_deliveries WHERE owner_id=$1 AND delivered)`, b.other.ID); e != nil {
			t.Fatal(e)
		}
		var retained int
		if e := b.pool.QueryRow(ctx, `SELECT count(*) FROM native_notification_schedule_deliveries WHERE owner_id=$1 AND delivered AND inbox_item_id IS NULL AND created_at>clock_timestamp()-interval '24 hours'`, b.other.ID).Scan(&retained); e != nil || retained != 1 {
			t.Fatal("actual receipt reset on delete", retained, e)
		}
	})
	notificationScheduleNativeWait(t, ctx, b.pool, nextDue)
	t.Run("new_date_slot_remains_BUDGET_FULL_with_pending_original_decision", func(t *testing.T) {
		second, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
		if e != nil {
			t.Fatal(e)
		}
		defer second.Close()
		out, e := New(second, false).ProcessNotificationSchedules(ctx, ns.MaxOwnersPerRun)
		if e != nil || out.Delivered != 0 || out.Slots != 1 {
			t.Fatal("budget native process", out, e)
		}
		if s, d, i := notificationScheduleNativeCounts(t, ctx, b.pool, b.other.ID); s != 2 || d != 1 || i != 0 {
			t.Fatal("budget reset via zone/session/pool", s, d, i)
		}
		var state string
		if e = b.pool.QueryRow(ctx, `SELECT state FROM native_notification_schedule_slots WHERE owner_id=$1 ORDER BY created_at DESC LIMIT 1`, b.other.ID).Scan(&state); e != nil || state != "BUDGET_FULL" {
			t.Fatal(state, e)
		}
		var pending int
		if e = b.pool.QueryRow(ctx, `SELECT count(*) FROM native_notification_decisions d WHERE d.source_id=$1 AND d.recipient_id=$2 AND birdtie_notification_digest_current(d) AND NOT EXISTS(SELECT 1 FROM native_notification_schedule_deliveries r WHERE r.decision_id=d.id)`, m2.ID, b.other.ID).Scan(&pending); e != nil || pending != 1 {
			t.Fatal("quota control lost pending input", pending, e)
		}
	})
	if after := notificationScheduleNativeProtected(t, ctx, b.pool, accounts); after != protected {
		t.Fatal("schedule altered original Profile/Memory/Task/decision rows or xmin")
	}
	t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY actual099 Store/PG/Inbox, real database due waits, no provider/model/production")
}

func TestNotificationScheduleNativeOriginalReminderIndependent(t *testing.T) {
	f := newNotificationDeliveryFixture(t)
	b := f.private.base
	a, _ := f.activity(t, activitypublish.Organizer{Type: "PERSON", ID: b.person.ID})
	before := notificationDeliveryProtectedSnapshot(t, f)
	out, e := ns.RunIndependentCycle(b.ctx, b.store, nil, 1, time.Second*20)
	if !errors.Is(e, ns.ErrUnavailable) || !out.RemindersOK || out.DigestOK {
		t.Fatal("digest unavailable stopped original real reminder", out, e)
	}
	f.delivered(t, agentnotification.KindActivityReminder, a.ID, b.other.ID, agentnotification.Normal, 1)
	n, e := b.store.EnqueueStartsSoonReminders(b.ctx)
	if e != nil || n != 0 {
		t.Fatal("original reminder duplicated", n, e)
	}
	if after := notificationDeliveryProtectedSnapshot(t, f); after != before {
		t.Fatal("reminder changed private source")
	}
	t.Log("actual original Enqueue then unavailable digest; not deployment/provider evidence")
}
