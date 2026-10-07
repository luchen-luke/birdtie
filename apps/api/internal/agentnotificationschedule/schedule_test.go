package agentnotificationschedule

import (
	"testing"
	"time"
)

func TestNotificationScheduleUnitTimeZonesDST(t *testing.T) {
	for _, tc := range []struct {
		name, zone string
		y          int
		m          time.Month
		d, minute  int
		want       string
		ok         bool
	}{{"London_spring_gap", "Europe/London", 2026, 3, 29, 90, "", false}, {"London_autumn_fold", "Europe/London", 2026, 10, 25, 90, "2026-10-25T00:30:00Z", true}, {"NY_spring_gap", "America/New_York", 2026, 3, 8, 150, "", false}, {"NY_fold", "America/New_York", 2026, 11, 1, 90, "2026-11-01T05:30:00Z", true}, {"half_hour_fold", "Australia/Lord_Howe", 2026, 4, 5, 105, "2026-04-04T14:45:00Z", true}, {"skipped_date", "Pacific/Apia", 2011, 12, 30, 12 * 60, "", false}, {"Shanghai", "Asia/Shanghai", 2026, 10, 6, 18 * 60, "2026-10-06T10:00:00Z", true}} {
		t.Run(tc.name, func(t *testing.T) {
			loc, e := Location(tc.zone)
			if e != nil {
				t.Fatal(e)
			}
			at, ok := WallInstant(loc, tc.y, tc.m, tc.d, tc.minute)
			if ok != tc.ok || ok && at.Format(time.RFC3339) != tc.want {
				t.Fatal(at, ok, tc.want)
			}
		})
	}
}
func TestNotificationScheduleUnitDueAndQuiet(t *testing.T) {
	now := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)
	p := scheduleTestPolicy(now)
	start := now.Add(-24 * time.Hour)
	p.ValidFrom = &start
	p.UpdatedAt = &start
	p.LocalMinute = 90
	a, ok, e := Due(p, now)
	if e != nil || !ok {
		t.Fatal(e)
	}
	b, ok, e := Due(p, now.Add(time.Hour))
	if e != nil || !ok || a.LocalDate != b.LocalDate || !a.DueAt.Equal(b.DueAt) {
		t.Fatal("fold must yield same native logical slot", a, b, e)
	}
	for _, tc := range []struct {
		name string
		edit func(*Policy)
		at   time.Time
	}{{"unconfigured", func(v *Policy) { *v, _ = DefaultPolicy(scheduleTestAgent) }, now}, {"off", func(v *Policy) { v.Enabled = false; v.Status = "DISABLED" }, now}, {"zero_budget", func(v *Policy) { v.MaxContactsPerDay = 0 }, now}, {"expired", func(v *Policy) { n := now; v.ExpiresAt = &n }, now}, {"origin_session_paused", func(v *Policy) { v.Status = "PAUSED_SESSION" }, now}, {"edit_after_slot_no_backfill", func(v *Policy) { n := now.Add(time.Minute); v.ValidFrom = &n; v.UpdatedAt = &n }, now.Add(2 * time.Minute)}, {"not_due", func(v *Policy) {}, now.Add(-time.Minute)}, {"quiet_at_slot", func(v *Policy) { v.Quiet = &QuietWindow{60, 120} }, now}, {"quiet_now", func(v *Policy) { v.Quiet = &QuietWindow{120, 180} }, now.Add(2 * time.Hour)}} {
		t.Run(tc.name, func(t *testing.T) {
			v := p
			tc.edit(&v)
			_, ok, e := Due(v, tc.at)
			if e != nil || ok {
				t.Fatal(ok, e)
			}
		})
	}
	s := scheduleTestSettings()
	s.TimeZone = "UTC"
	s.Quiet = &QuietWindow{22 * 60, 7 * 60}
	for _, tc := range []struct {
		h    int
		want bool
	}{{21, false}, {22, true}, {0, true}, {6, true}, {7, false}} {
		if IsQuiet(s, time.Date(2026, 10, 6, tc.h, 0, 0, 0, time.UTC)) != tc.want {
			t.Fatal(tc)
		}
	}
}
