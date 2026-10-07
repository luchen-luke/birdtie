package agentnotificationschedule

import (
	"sort"
	"time"
)

// ScheduledEvent is native metadata, not an AIR UserQuery, grant, model input
// or client-chosen dispatch. One owner/local date has one slot across revisions.
type ScheduledEvent struct {
	LocalDate       string
	DueAt           time.Time
	ScheduleVersion uint64
}

func (ScheduledEvent) MarshalJSON() ([]byte, error)  { return nil, ErrServerOnly }
func (e *ScheduledEvent) UnmarshalJSON([]byte) error { *e = ScheduledEvent{}; return ErrServerOnly }

// Exact wall-time candidates handle gaps and folds without Go's unspecified
// time.Date choice. Offset samples cover half-hour shifts and skipped dates.
func WallInstant(loc *time.Location, y int, m time.Month, day, minute int) (time.Time, bool) {
	if loc == nil || minute < 0 || minute >= 1440 {
		return time.Time{}, false
	}
	nominal := time.Date(y, m, day, minute/60, minute%60, 0, 0, time.UTC)
	offsets := map[int]bool{}
	for h := -36; h <= 36; h++ {
		_, o := nominal.Add(time.Duration(h) * time.Hour).In(loc).Zone()
		offsets[o] = true
	}
	var values []time.Time
	for o := range offsets {
		v := nominal.Add(-time.Duration(o) * time.Second)
		w := v.In(loc)
		if w.Year() == y && w.Month() == m && w.Day() == day && w.Hour() == minute/60 && w.Minute() == minute%60 && w.Second() == 0 {
			values = append(values, v)
		}
	}
	if len(values) == 0 {
		return time.Time{}, false
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Before(values[j]) })
	return values[0].UTC(), true
}
func IsQuiet(s Settings, now time.Time) bool {
	if s.Quiet == nil {
		return false
	}
	loc, e := Location(s.TimeZone)
	if e != nil {
		return true
	}
	w := now.In(loc)
	n := w.Hour()*60 + w.Minute()
	a, b := s.Quiet.StartMinute, s.Quiet.EndMinute
	if a < b {
		return n >= a && n < b
	}
	return n >= a || n < b
}

// No catch-up before the latest explicit edit, no gap normalization, no second
// fold trigger, and no implied plan for an unconfigured/expired identity.
func Due(p Policy, now time.Time) (ScheduledEvent, bool, error) {
	if e := ValidatePolicy(p); e != nil {
		return ScheduledEvent{}, false, e
	}
	if now.IsZero() {
		return ScheduledEvent{}, false, ErrInvalid
	}
	if !p.Configured || p.Status != "ACTIVE" || !p.Enabled || p.MaxContactsPerDay == 0 || now.Before(*p.ValidFrom) || !p.ExpiresAt.After(now) {
		return ScheduledEvent{}, false, nil
	}
	loc, e := Location(p.TimeZone)
	if e != nil {
		return ScheduledEvent{}, false, e
	}
	w := now.In(loc)
	instant, ok := WallInstant(loc, w.Year(), w.Month(), w.Day(), p.LocalMinute)
	if !ok || instant.Before(*p.ValidFrom) || now.Before(instant) || IsQuiet(p.Settings, instant) || IsQuiet(p.Settings, now) {
		return ScheduledEvent{}, false, nil
	}
	return ScheduledEvent{LocalDate: w.Format("2006-01-02"), DueAt: instant, ScheduleVersion: p.Version}, true, nil
}
