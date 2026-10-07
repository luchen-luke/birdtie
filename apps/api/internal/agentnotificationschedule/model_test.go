package agentnotificationschedule

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"math"
	"testing"
	"time"
)

const scheduleTestAgent = "82000000-0000-4000-8000-000000000001"

func scheduleTestSettings() Settings {
	return Settings{Enabled: true, TimeZone: "Europe/London", LocalMinute: 18 * 60, GapPolicy: GapSkip, FoldPolicy: FoldEarlierOnce, MaxContactsPerDay: 3, Categories: []agentnotification.Category{agentnotification.CategorySocial, agentnotification.CategoryActivity}}
}
func scheduleTestPolicy(at time.Time) Policy {
	start := at.Add(-time.Hour).UTC().Truncate(time.Microsecond)
	end := at.Add(48 * time.Hour).UTC().Truncate(time.Microsecond)
	s, _ := NormalizeSettings(scheduleTestSettings())
	return Policy{SchemaVersion: SchemaVersion, AgentID: scheduleTestAgent, Version: 1, Configured: true, Status: "ACTIVE", BudgetWindowHours: 24, Settings: s, ValidFrom: &start, UpdatedAt: &start, ExpiresAt: &end}
}
func TestNotificationScheduleUnitModel(t *testing.T) {
	now := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	p, e := DefaultPolicy(scheduleTestAgent)
	if e != nil || ValidatePolicy(p) != nil || p.Configured || p.Enabled || p.TimeZone != "" || p.MaxContactsPerDay != 0 {
		t.Fatal("GET must not create an implicit schedule")
	}
	for _, tc := range []struct {
		name string
		edit func(*Settings)
	}{{"unknown_zone", func(s *Settings) { s.TimeZone = "unknown/secret" }}, {"OS_local", func(s *Settings) { s.TimeZone = "Local" }}, {"fixed_abbreviation", func(s *Settings) { s.TimeZone = "BST" }}, {"path", func(s *Settings) { s.TimeZone = "../Europe/London" }}, {"minute", func(s *Settings) { s.LocalMinute = 1440 }}, {"gap", func(s *Settings) { s.GapPolicy = "MOVE_FORWARD" }}, {"fold", func(s *Settings) { s.FoldPolicy = "BOTH" }}, {"negative_budget", func(s *Settings) { s.MaxContactsPerDay = -1 }}, {"excess_budget", func(s *Settings) { s.MaxContactsPerDay = 21 }}, {"unknown_category", func(s *Settings) { s.Categories = []agentnotification.Category{"PRIVATE_RAW"} }}, {"duplicate_category", func(s *Settings) { s.Categories = []agentnotification.Category{"SOCIAL", "SOCIAL"} }}, {"no_category", func(s *Settings) { s.Categories = nil }}, {"quiet_equal", func(s *Settings) { s.Quiet = &QuietWindow{2, 2} }}, {"quiet_invalid", func(s *Settings) { s.Quiet = &QuietWindow{-1, 20} }}} {
		t.Run(tc.name, func(t *testing.T) {
			s := scheduleTestSettings()
			tc.edit(&s)
			if _, e := NormalizeSettings(s); !errors.Is(e, ErrInvalid) {
				t.Fatal(e)
			}
		})
	}
	for _, tc := range []struct {
		name    string
		version uint64
		at      time.Time
	}{{"expiry_exact", 0, now}, {"long_expiry", 0, now.Add(MaxDuration + time.Microsecond)}, {"precision", 0, now.Add(time.Second + time.Nanosecond)}, {"version_max", math.MaxInt64, now.Add(time.Hour)}} {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := NormalizePut(PutInput{tc.version, scheduleTestSettings(), tc.at}, now); e != ErrInvalid {
				t.Fatal(e)
			}
		})
	}
	in := scheduleTestSettings()
	q := &QuietWindow{22 * 60, 7 * 60}
	in.Quiet = q
	s, e := NormalizeSettings(in)
	if e != nil {
		t.Fatal(e)
	}
	q.StartMinute = 0
	in.Categories[0] = "PRIVATE_RAW"
	if s.Quiet.StartMinute != 22*60 || s.Categories[1] != agentnotification.CategorySocial {
		t.Fatal("caller mutated saved settings")
	}
	if _, e = json.Marshal(ScheduledEvent{}); e == nil {
		t.Fatal("metadata must not become client authority")
	}
	var ev ScheduledEvent
	if json.Unmarshal([]byte(`{}`), &ev) == nil {
		t.Fatal("cannot deserialize dispatch")
	}
	valid := scheduleTestPolicy(now)
	if ValidatePolicy(valid) != nil {
		t.Fatal("valid configured")
	}
	valid.BudgetWindowHours = 0
	if ValidatePolicy(valid) == nil {
		t.Fatal("must explain rolling window")
	}
}
