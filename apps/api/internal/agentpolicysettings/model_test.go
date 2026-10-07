package agentpolicysettings

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentautonomy"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
)

var policyTestNow = time.Date(2026, 10, 3, 0, 0, 0, 123456000, time.UTC)

func policyRaw(f Family) json.RawMessage {
	switch f {
	case Attention:
		return json.RawMessage(`{"defaultRoute":"BLOCK","rules":[]}`)
	case Social:
		return json.RawMessage(`{"rules":[]}`)
	}
	return json.RawMessage(`{"level":"LEVEL_0_OBSERVE"}`)
}
func policyBundle() Bundle {
	return Bundle{SchemaVersion: SchemaVersion, OwnerType: actorref.Person, OwnerID: "85000000-0000-4000-8000-000000000001", AgentID: "85000000-0000-4000-8000-000000000002", ObservedAt: policyTestNow, Attention: DefaultRecord(Attention), Social: DefaultRecord(Social), Autonomy: DefaultRecord(Autonomy)}
}
func TestPolicySettingsNormalize(t *testing.T) {
	for _, f := range Families() {
		t.Run(string(f), func(t *testing.T) {
			raw := policyRaw(f)
			original := string(raw)
			in := PutInput{Settings: raw, ExpiresAt: policyTestNow.Add(time.Hour)}
			got, err := NormalizeInput(f, in, policyTestNow)
			if err != nil || got.ExpectedVersion != 0 || string(raw) != original {
				t.Fatal("valid closed human settings rejected or mutated", err)
			}
			got.Settings[0] = 'x'
			if string(raw) != original {
				t.Fatal("input aliases normalized output")
			}
		})
	}
	t.Run("AttentionPauseAndSort", func(t *testing.T) {
		pause := policyTestNow.Add(time.Minute).Format(time.RFC3339Nano)
		raw := json.RawMessage(fmt.Sprintf(`{"defaultRoute":"DIGEST","rules":[{"eventType":"UserQuery","route":"IMMEDIATE"},{"eventType":"MomentCreated","route":"BLOCK"}],"pauseUntil":%q}`, pause))
		got, err := NormalizeSettings(Attention, raw, policyTestNow, policyTestNow.Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		var d AttentionSettings
		json.Unmarshal(got, &d)
		if d.Rules[0].EventType != "MomentCreated" || d.PauseUntil.Location() != time.UTC {
			t.Fatal("not canonical")
		}
	})
	t.Run("WhitespaceRules", func(t *testing.T) {
		if _, err := NormalizeSettings(Social, json.RawMessage("{\"rules\": \n [ ]}"), policyTestNow, policyTestNow.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("SocialMissingExplicitDisabled", func(t *testing.T) {
		got, err := NormalizeSettings(Social, json.RawMessage(`{"rules":[{"category":"UNKNOWN_PERSON","preference":"REVIEW_REQUIRED"}]}`), policyTestNow, policyTestNow.Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		var d SocialSettings
		json.Unmarshal(got, &d)
		if len(d.Rules) != 7 {
			t.Fatal("categories not closed")
		}
		for _, r := range d.Rules {
			if r.Category != "UNKNOWN_PERSON" && r.Preference != "DISABLED" {
				t.Fatal("missing preference granted")
			}
		}
	})
	for level := 0; level <= 2; level++ {
		t.Run(fmt.Sprintf("Autonomy%d", level), func(t *testing.T) {
			if _, err := NormalizeSettings(Autonomy, json.RawMessage(fmt.Sprintf(`{"level":%q}`, agentautonomy.Levels()[level])), policyTestNow, policyTestNow.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestPolicySettingsStrictWire(t *testing.T) {
	exp := policyTestNow.Add(time.Hour).Format(time.RFC3339Nano)
	valid := fmt.Sprintf(`{"expectedVersion":0,"settings":{"level":"LEVEL_0_OBSERVE"},"expiresAt":%q}`, exp)
	bad := map[string]string{
		"Unknown":          strings.TrimSuffix(valid, "}") + `,"confirmed":true}`,
		"Owner":            strings.TrimSuffix(valid, "}") + `,"ownerId":"85000000-0000-4000-8000-000000000001"}`,
		"Duplicate":        strings.Replace(valid, `"expectedVersion":0`, `"expectedVersion":0,"expectedVersion":1`, 1),
		"EscapedDuplicate": strings.Replace(valid, `"expectedVersion":0`, `"expectedVersion":0,"expected\u0056ersion":1`, 1),
		"Case":             strings.Replace(valid, "expectedVersion", "ExpectedVersion", 1),
		"Null":             strings.Replace(valid, `"settings":{"level":"LEVEL_0_OBSERVE"}`, `"settings":null`, 1),
		"Missing":          fmt.Sprintf(`{"settings":{"level":"LEVEL_0_OBSERVE"},"expiresAt":%q}`, exp),
		"Array":            `[]`, "Scalar": `1`, "Trailing": valid + `{}`, "InvalidUTF8": valid + string([]byte{0xff}), "Oversize": strings.Repeat(" ", MaxBodyBytes) + valid,
		"Negative":        strings.Replace(valid, `"expectedVersion":0`, `"expectedVersion":-1`, 1),
		"Fraction":        strings.Replace(valid, `"expectedVersion":0`, `"expectedVersion":0.0`, 1),
		"Exponent":        strings.Replace(valid, `"expectedVersion":0`, `"expectedVersion":0e0`, 1),
		"StringVersion":   strings.Replace(valid, `"expectedVersion":0`, `"expectedVersion":"0"`, 1),
		"Overflow":        strings.Replace(valid, `"expectedVersion":0`, `"expectedVersion":9223372036854775808`, 1),
		"Exhausted":       strings.Replace(valid, `"expectedVersion":0`, `"expectedVersion":9223372036854775807`, 1),
		"TooPrecise":      strings.Replace(valid, exp, policyTestNow.Add(time.Hour+time.Nanosecond).Format(time.RFC3339Nano), 1),
		"Autonomous":      strings.Replace(valid, `"level":"LEVEL_0_OBSERVE"`, `"level":"LEVEL_3_DELEGATE"`, 1),
		"NestedDuplicate": strings.Replace(valid, `"level":"LEVEL_0_OBSERVE"`, `"level":"LEVEL_0_OBSERVE","level":1`, 1),
		"NestedCase":      strings.Replace(valid, `"level":"LEVEL_0_OBSERVE"`, `"Level":"LEVEL_0_OBSERVE"`, 1),
		"NestedNull":      strings.Replace(valid, `"level":"LEVEL_0_OBSERVE"`, `"level":null`, 1),
		"ProviderClaim":   strings.Replace(valid, `"level":"LEVEL_0_OBSERVE"`, `"level":"LEVEL_0_OBSERVE","purpose":"model"`, 1),
	}
	for name, raw := range bad {
		t.Run(name, func(t *testing.T) {
			if got, err := DecodePutInput(Autonomy, []byte(raw)); !errors.Is(err, ErrInvalid) || len(got.Settings) != 0 {
				t.Fatal("invalid wire accepted")
			}
		})
	}
	t.Run("ValidShapeNotCurrent", func(t *testing.T) {
		in, err := DecodePutInput(Autonomy, []byte(valid))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = NormalizeInput(Autonomy, in, policyTestNow.Add(2*time.Hour)); !errors.Is(err, ErrInvalid) {
			t.Fatal("wire expiry became current permission")
		}
	})
	t.Run("UnknownFamily", func(t *testing.T) {
		if _, err := DecodePutInput("OTHER", []byte(valid)); !errors.Is(err, ErrInvalid) {
			t.Fatal("unknown family accepted")
		}
	})
	t.Run("TimezoneCanonical", func(t *testing.T) {
		raw := strings.Replace(valid, exp, policyTestNow.Add(time.Hour).In(time.FixedZone("test", 8*3600)).Format(time.RFC3339Nano), 1)
		in, err := DecodePutInput(Autonomy, []byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		n, err := NormalizeInput(Autonomy, in, policyTestNow)
		if err != nil || n.ExpiresAt.Location() != time.UTC {
			t.Fatal("timezone not canonical")
		}
	})
}
func TestPolicySettingsRejectClosedSettings(t *testing.T) {
	cases := []struct {
		name string
		f    Family
		raw  string
	}{
		{"UnknownRoute", Attention, `{"defaultRoute":"ALLOW","rules":[]}`}, {"NullRules", Attention, `{"defaultRoute":"BLOCK","rules":null}`}, {"UnknownEvent", Attention, `{"defaultRoute":"BLOCK","rules":[{"eventType":"AttendanceConfirmed","route":"BLOCK"}]}`}, {"DuplicateEvent", Attention, `{"defaultRoute":"BLOCK","rules":[{"eventType":"MomentCreated","route":"BLOCK"},{"eventType":"MomentCreated","route":"BLOCK"}]}`}, {"NestedAuthority", Attention, `{"defaultRoute":"BLOCK","rules":[{"eventType":"UserQuery","route":"BLOCK","allowed":true}]}`}, {"NullPause", Attention, `{"defaultRoute":"BLOCK","rules":[],"pauseUntil":null}`}, {"ExpiredPause", Attention, `{"defaultRoute":"BLOCK","rules":[],"pauseUntil":"2026-10-01T00:00:00Z"}`}, {"LatePause", Attention, `{"defaultRoute":"BLOCK","rules":[],"pauseUntil":"2026-11-01T00:00:00Z"}`},
		{"UnknownCategory", Social, `{"rules":[{"category":"FRIEND","preference":"DISABLED"}]}`}, {"AutonomousSocial", Social, `{"rules":[{"category":"BUSINESS","preference":"ALLOW"}]}`}, {"DuplicateCategory", Social, `{"rules":[{"category":"BUSINESS","preference":"DISABLED"},{"category":"BUSINESS","preference":"DISABLED"}]}`}, {"CaseCategory", Social, `{"rules":[{"Category":"BUSINESS","preference":"DISABLED"}]}`}, {"ClaimConsent", Social, `{"rules":[],"consent":true}`}, {"NoArray", Social, `{"rules":{}}`},
		{"NegativeLevel", Autonomy, `{"level":-1}`}, {"FloatingLevel", Autonomy, `{"level":1.0}`}, {"UnknownLevel", Autonomy, `{"level":4}`}, {"HardOff3", Autonomy, `{"level":"LEVEL_3_DELEGATE"}`}, {"StringLevel", Autonomy, `{"level":"1"}`}, {"NullLevel", Autonomy, `{"level":null}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := NormalizeSettings(c.f, json.RawMessage(c.raw), policyTestNow, policyTestNow.Add(time.Hour)); !errors.Is(err, ErrInvalid) {
				t.Fatal("invalid settings accepted", err)
			}
		})
	}
	for name, in := range map[string]PutInput{"Expired": {Settings: policyRaw(Autonomy), ExpiresAt: policyTestNow}, "Long": {Settings: policyRaw(Autonomy), ExpiresAt: policyTestNow.Add(MaxValidity + time.Microsecond)}, "Zero": {Settings: policyRaw(Autonomy)}, "Nanosecond": {Settings: policyRaw(Autonomy), ExpiresAt: policyTestNow.Add(time.Hour + time.Nanosecond)}} {
		t.Run(name, func(t *testing.T) {
			if _, err := NormalizeInput(Autonomy, in, policyTestNow); !errors.Is(err, ErrInvalid) {
				t.Fatal("invalid expiry accepted")
			}
		})
	}
}
func TestPolicySettingsBundle(t *testing.T) {
	b := policyBundle()
	if ValidateBundle(b) != nil {
		t.Fatal("default invalid")
	}
	for name, mutate := range map[string]func(*Bundle){"WrongType": func(b *Bundle) { b.OwnerType = actorref.Organization }, "ZeroOwner": func(b *Bundle) { b.OwnerID = "00000000-0000-0000-0000-000000000000" }, "InvalidAgent": func(b *Bundle) { b.AgentID = "model-provider" }, "WrongSchema": func(b *Bundle) { b.SchemaVersion = "v2" }, "MissingFamily": func(b *Bundle) { b.Social.Family = Attention }, "DefaultPretendsVersion": func(b *Bundle) { b.Social.NativeRevision = 1 }, "DefaultClaimConfigured": func(b *Bundle) { b.Autonomy.Configured = true }, "DefaultClaimAuthority": func(b *Bundle) { b.Autonomy.Settings = json.RawMessage(`{"level":"LEVEL_3_DELEGATE"}`) }, "ZeroClock": func(b *Bundle) { b.ObservedAt = time.Time{} }} {
		t.Run(name, func(t *testing.T) {
			copy := policyBundle()
			mutate(&copy)
			if ValidateBundle(copy) == nil {
				t.Fatal("wrong bundle accepted")
			}
		})
	}
	from, expires := policyTestNow, policyTestNow.Add(time.Hour)
	raw, _ := NormalizeSettings(Social, policyRaw(Social), from, expires)
	r := Record{Family: Social, Configured: true, NativeRevision: 7, Status: "ACTIVE", Settings: raw, ValidFrom: &from, ExpiresAt: &expires, UpdatedAt: &from}
	if ValidateRecord(r, from) != nil {
		t.Fatal("active invalid")
	}
	r.Status = "EXPIRED"
	if ValidateRecord(r, expires) != nil {
		t.Fatal("expired retained row invalid")
	}
	if ValidateRecord(r, from) == nil {
		t.Fatal("expired status lied")
	}
	t.Run("NoPermissionFromLevel", func(t *testing.T) {
		limits := agentfeature.Limits()
		if limits.AutonomousAction {
			t.Fatal("hard brake changed")
		}
		raw, _ := NormalizeSettings(Autonomy, json.RawMessage(`{"level":"LEVEL_2_PREPARE"}`), from, expires)
		var s AutonomySettings
		json.Unmarshal(raw, &s)
		if s.Level != agentautonomy.LevelPrepare {
			t.Fatal("level mismatch")
		}
	})
}
