package agentnotification

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

const policyTestAgent = "a1000000-0000-4000-8000-000000000001"

func notificationTestNow() time.Time { return time.Date(2026, 10, 3, 2, 0, 0, 0, time.UTC) }
func notificationTestInput() PutInput {
	return PutInput{Enabled: true, DefaultRoute: Normal, Rules: []Rule{{Category: CategoryMessage, Route: Immediate}}, ExpiresAt: notificationTestNow().Add(time.Hour)}
}
func notificationTestPolicy() Policy {
	now := notificationTestNow()
	expires := now.Add(time.Hour)
	return Policy{SchemaVersion: SchemaVersion, AgentID: policyTestAgent, Version: 1, Enabled: true, DefaultRoute: Normal, Rules: []Rule{{Category: CategoryMessage, Route: Immediate}}, UpdatedAt: &now, ExpiresAt: &expires}
}

func TestNotificationPolicyFiveRoutesAndEightCategories(t *testing.T) {
	for _, category := range []Category{CategoryMessage, CategoryActivity, CategoryCommunity, CategoryOrganization, CategoryBusiness, CategorySystem, CategoryAgent, CategorySocial} {
		for _, route := range []Route{Immediate, Normal, Digest, Silent, Block} {
			t.Run(string(category)+"/"+string(route), func(t *testing.T) {
				p := notificationTestPolicy()
				p.Rules = []Rule{{Category: category, Route: route}}
				d, err := ChooseRoute(p, category, notificationTestNow())
				priority, _ := RoutePriority(route)
				if err != nil || d != (Decision{route, priority, "exact_rule"}) {
					t.Fatal("route classification mismatch")
				}
			})
		}
	}
}

func TestNotificationPolicyDefaultDisabledExpiredAndPause(t *testing.T) {
	now := notificationTestNow()
	cases := []struct {
		name   string
		change func(*Policy)
		at     time.Time
		route  Route
		reason string
	}{
		{"default", func(p *Policy) { p.Rules = []Rule{} }, now, Normal, "default_rule"},
		{"disabled", func(p *Policy) { p.Enabled = false; p.DefaultRoute = Block }, now, Normal, "disabled"},
		{"expiry_boundary", func(*Policy) {}, now.Add(time.Hour), Normal, "expired"},
		{"after_expiry", func(*Policy) {}, now.Add(2 * time.Hour), Normal, "expired"},
		{"pause", func(p *Policy) { at := now.Add(time.Minute); p.PauseUntil = &at }, now, Silent, "attention_paused"},
		{"pause_exact_end", func(p *Policy) { at := now.Add(time.Minute); p.PauseUntil = &at }, now.Add(time.Minute), Immediate, "exact_rule"},
		{"matched_block_wins_pause", func(p *Policy) { at := now.Add(time.Minute); p.PauseUntil = &at; p.Rules[0].Route = Block }, now, Block, "exact_rule"},
		{"default_block_overridden_by_exact", func(p *Policy) { p.DefaultRoute = Block }, now, Immediate, "exact_rule"},
		{"unmatched_default_block_wins_pause", func(p *Policy) {
			at := now.Add(time.Minute)
			p.PauseUntil = &at
			p.DefaultRoute = Block
			p.Rules = []Rule{}
		}, now, Block, "default_rule"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := notificationTestPolicy()
			c.change(&p)
			d, err := ChooseRoute(p, CategoryMessage, c.at)
			if err != nil || d.Route != c.route || d.Reason != c.reason {
				t.Fatal("classification boundary mismatch")
			}
		})
	}
	p, err := DefaultPolicy(policyTestAgent)
	if err != nil {
		t.Fatal(err)
	}
	d, err := ChooseRoute(p, CategoryBusiness, now)
	if err != nil || d != (Decision{Normal, 50, "not_configured"}) {
		t.Fatal("unconfigured must use ordinary default only")
	}
	// Category preference does not create a publication source or Agent permission.
	if descriptor, err := LookupKind(KindBusinessUpdate); err != nil || descriptor.Category != CategoryBusiness {
		t.Fatal("public publication category mapping unavailable")
	}
}

func TestNotificationPolicyInputBoundsAndIsolation(t *testing.T) {
	now := notificationTestNow()
	valid := notificationTestInput()
	valid.Rules = []Rule{{Category: CategorySocial, Route: Block}, {Category: CategoryActivity, Route: Digest}}
	pause := now.Add(time.Minute)
	valid.PauseUntil = &pause
	normalized, err := NormalizePutInput(valid, now)
	if err != nil || normalized.Rules[0].Category != CategoryActivity {
		t.Fatal("normalization failed")
	}
	valid.Rules[0].Route = Immediate
	*valid.PauseUntil = now.Add(2 * time.Hour)
	if normalized.Rules[1].Route != Block || !normalized.PauseUntil.Equal(now.Add(time.Minute)) {
		t.Fatal("caller mutation changed normalized preferences")
	}
	cases := []struct {
		name   string
		change func(*PutInput)
		at     time.Time
	}{
		{"expired", func(in *PutInput) { in.ExpiresAt = now }, now},
		{"past", func(in *PutInput) { in.ExpiresAt = now.Add(-time.Nanosecond) }, now},
		{"over_30_days", func(in *PutInput) { in.ExpiresAt = now.Add(MaxPolicyDuration + time.Microsecond) }, now},
		{"zero_expiry", func(in *PutInput) { in.ExpiresAt = time.Time{} }, now},
		{"year10000", func(in *PutInput) { in.ExpiresAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }, now},
		{"max_version_over", func(in *PutInput) { in.ExpectedVersion = MaxVersion + 1 }, now},
		{"unknown_default", func(in *PutInput) { in.DefaultRoute = "SEND" }, now},
		{"unknown_category", func(in *PutInput) { in.Rules[0].Category = "PRIVATE" }, now},
		{"unknown_route", func(in *PutInput) { in.Rules[0].Route = "PUSH" }, now},
		{"duplicate_rule", func(in *PutInput) { in.Rules = append(in.Rules, in.Rules[0]) }, now},
		{"too_many", func(in *PutInput) { in.Rules = make([]Rule, 9) }, now},
		{"pause_beyond_expiry", func(in *PutInput) { p := in.ExpiresAt.Add(time.Microsecond); in.PauseUntil = &p }, now},
		{"zero_pause", func(in *PutInput) { p := time.Time{}; in.PauseUntil = &p }, now},
		{"zero_clock", func(*PutInput) {}, time.Time{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := notificationTestInput()
			c.change(&in)
			out, err := NormalizePutInput(in, c.at)
			if err != ErrInvalid || !reflect.DeepEqual(out, PutInput{}) {
				t.Fatal("invalid input did not fail empty")
			}
		})
	}
	for _, version := range []uint64{0, 1, MaxVersion} {
		in := notificationTestInput()
		in.ExpectedVersion = version
		if _, err := NormalizePutInput(in, now); err != nil {
			t.Fatal("legal CAS value shape rejected")
		}
	}
	input := notificationTestInput()
	input.ExpiresAt = now.Add(MaxPolicyDuration)
	if _, err := NormalizePutInput(input, now); err != nil {
		t.Fatal("exact30day rejected")
	}
	input = notificationTestInput()
	input.ExpiresAt = input.ExpiresAt.Add(123 * time.Nanosecond)
	normalized, err = NormalizePutInput(input, now)
	if err != nil || normalized.ExpiresAt.Nanosecond()%1000 != 0 {
		t.Fatal("PG precision not normalized")
	}
	input = notificationTestInput()
	input.Rules = nil
	normalized, err = NormalizePutInput(input, now)
	if err != nil || normalized.Rules == nil {
		t.Fatal("omitted typed slice must become empty")
	}
}

func TestNotificationPolicyResponseShape(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Policy)
	}{
		{"schema", func(p *Policy) { p.SchemaVersion = "other" }},
		{"owner_not_uuid", func(p *Policy) { p.AgentID = "person" }},
		{"zero_uuid", func(p *Policy) { p.AgentID = "00000000-0000-0000-0000-000000000000" }},
		{"uppercase_uuid", func(p *Policy) { p.AgentID = "A1000000-0000-4000-8000-000000000001" }},
		{"padded_uuid", func(p *Policy) { p.AgentID = " " + policyTestAgent }},
		{"nil_rules", func(p *Policy) { p.Rules = nil }},
		{"unsorted_rules", func(p *Policy) {
			p.Rules = []Rule{{Category: CategorySocial, Route: Normal}, {Category: CategoryActivity, Route: Normal}}
		}},
		{"duplicate_rules", func(p *Policy) { p.Rules = append(p.Rules, p.Rules[0]) }},
		{"unknown_category", func(p *Policy) { p.Rules[0].Category = "CUSTOM" }},
		{"unknown_default", func(p *Policy) { p.DefaultRoute = "SEND" }},
		{"missing_expiry", func(p *Policy) { p.ExpiresAt = nil }},
		{"missing_updated", func(p *Policy) { p.UpdatedAt = nil }},
		{"version_over", func(p *Policy) { p.Version = MaxVersion + 1 }},
		{"reverse_time", func(p *Policy) { at := p.UpdatedAt.Add(-time.Hour); p.ExpiresAt = &at }},
		{"over30days", func(p *Policy) { at := p.UpdatedAt.Add(MaxPolicyDuration + time.Microsecond); p.ExpiresAt = &at }},
		{"pause_after_expiry", func(p *Policy) { at := p.ExpiresAt.Add(time.Microsecond); p.PauseUntil = &at }},
		{"v0_configured", func(p *Policy) { p.Version = 0 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := notificationTestPolicy()
			c.change(&p)
			if ValidatePolicy(p) != ErrInvalid {
				t.Fatal("bad response accepted")
			}
			if d, err := ChooseRoute(p, CategoryMessage, notificationTestNow()); err != ErrInvalid || d != (Decision{}) {
				t.Fatal("bad response produced route")
			}
		})
	}
	for _, change := range []func(*Policy){func(p *Policy) { p.Enabled = true }, func(p *Policy) { p.DefaultRoute = Block }, func(p *Policy) { p.Rules = []Rule{{Category: CategoryAgent, Route: Normal}} }, func(p *Policy) { at := notificationTestNow(); p.UpdatedAt = &at }, func(p *Policy) { at := notificationTestNow(); p.ExpiresAt = &at }, func(p *Policy) { at := notificationTestNow(); p.PauseUntil = &at }} {
		p, _ := DefaultPolicy(policyTestAgent)
		change(&p)
		if ValidatePolicy(p) != ErrInvalid {
			t.Fatal("V0 invented configuration")
		}
	}
	p := notificationTestPolicy()
	for _, category := range []Category{"", "message", " UNKNOWN", "PRIVATE"} {
		if d, err := ChooseRoute(p, category, notificationTestNow()); err != ErrInvalid || d != (Decision{}) {
			t.Fatal("unknown category defaulted")
		}
	}
	if d, err := ChooseRoute(p, CategoryMessage, notificationTestNow().Add(-time.Second)); err != ErrInvalid || d != (Decision{}) {
		t.Fatal("future policy used")
	}
	if _, err := RoutePriority("UNKNOWN"); err != ErrInvalid {
		t.Fatal("unknown priority")
	}
	encoded, err := json.Marshal(p)
	if err != nil || !json.Valid(encoded) {
		t.Fatal("human response encoding")
	}
}
