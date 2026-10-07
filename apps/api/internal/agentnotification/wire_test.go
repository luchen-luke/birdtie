package agentnotification

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const notificationTestBody = `{"expectedVersion":0,"enabled":true,"defaultRoute":"NORMAL","rules":[{"category":"MESSAGE","route":"IMMEDIATE"}],"expiresAt":"2026-10-03T03:00:00Z"}`

func TestNotificationPolicyStrictWire(t *testing.T) {
	cases := []struct{ name, body string }{
		{"empty", ""}, {"null", "null"}, {"array", "[]"}, {"trailing", notificationTestBody + "{}"}, {"garbage", notificationTestBody + "x"},
		{"unknown_owner", strings.Replace(notificationTestBody, `"expectedVersion":0`, `"ownerId":"secret","expectedVersion":0`, 1)},
		{"unknown_agent", strings.Replace(notificationTestBody, `"expectedVersion":0`, `"agentId":"secret","expectedVersion":0`, 1)},
		{"authority", strings.Replace(notificationTestBody, `"enabled":true`, `"enabled":true,"confirmed":true`, 1)},
		{"current_source", strings.Replace(notificationTestBody, `"enabled":true`, `"enabled":true,"currentSource":{"verified":true}`, 1)},
		{"duplicate_top", strings.Replace(notificationTestBody, `"enabled":true`, `"enabled":true,"enabled":false`, 1)},
		{"escaped_duplicate", strings.Replace(notificationTestBody, `"enabled":true`, `"enabled":true,"enabl\u0065d":false`, 1)},
		{"case_key", strings.Replace(notificationTestBody, `"enabled":true`, `"Enabled":true`, 1)},
		{"duplicate_nested", strings.Replace(notificationTestBody, `"category":"MESSAGE"`, `"category":"MESSAGE","category":"SOCIAL"`, 1)},
		{"case_nested", strings.Replace(notificationTestBody, `"category":"MESSAGE"`, `"Category":"MESSAGE"`, 1)},
		{"private_rule", strings.Replace(notificationTestBody, `"route":"IMMEDIATE"`, `"route":"IMMEDIATE","privateBody":"canary"`, 1)},
		{"missing_route", strings.Replace(notificationTestBody, `,"route":"IMMEDIATE"`, "", 1)},
		{"null_rule", strings.Replace(notificationTestBody, `[{"category":"MESSAGE","route":"IMMEDIATE"}]`, `[null]`, 1)},
		{"null_rules", strings.Replace(notificationTestBody, `[{"category":"MESSAGE","route":"IMMEDIATE"}]`, `null`, 1)},
		{"wrong_rules", strings.Replace(notificationTestBody, `[{"category":"MESSAGE","route":"IMMEDIATE"}]`, `{}`, 1)},
		{"nested_rules", strings.Replace(notificationTestBody, `[{"category":"MESSAGE","route":"IMMEDIATE"}]`, `[[{"category":"MESSAGE","route":"IMMEDIATE"}]]`, 1)},
		{"unknown_category", strings.Replace(notificationTestBody, `"MESSAGE"`, `"INFERRED"`, 1)},
		{"case_category", strings.Replace(notificationTestBody, `"MESSAGE"`, `"message"`, 1)},
		{"unknown_route", strings.Replace(notificationTestBody, `"IMMEDIATE"`, `"PUSH"`, 1)},
		{"duplicate_category", strings.Replace(notificationTestBody, `[{"category":"MESSAGE","route":"IMMEDIATE"}]`, `[{"category":"MESSAGE","route":"IMMEDIATE"},{"category":"MESSAGE","route":"NORMAL"}]`, 1)},
		{"number_route", strings.Replace(notificationTestBody, `"NORMAL"`, `1`, 1)},
		{"null_enabled", strings.Replace(notificationTestBody, `"enabled":true`, `"enabled":null`, 1)},
		{"string_enabled", strings.Replace(notificationTestBody, `"enabled":true`, `"enabled":"true"`, 1)},
		{"number_enabled", strings.Replace(notificationTestBody, `"enabled":true`, `"enabled":1`, 1)},
		{"bool_version", strings.Replace(notificationTestBody, `"expectedVersion":0`, `"expectedVersion":true`, 1)},
		{"fraction_version", strings.Replace(notificationTestBody, `"expectedVersion":0`, `"expectedVersion":1.0`, 1)},
		{"exponent_version", strings.Replace(notificationTestBody, `"expectedVersion":0`, `"expectedVersion":1e0`, 1)},
		{"negative_version", strings.Replace(notificationTestBody, `"expectedVersion":0`, `"expectedVersion":-1`, 1)},
		{"string_version", strings.Replace(notificationTestBody, `"expectedVersion":0`, `"expectedVersion":"0"`, 1)},
		{"overflow_version", strings.Replace(notificationTestBody, `"expectedVersion":0`, `"expectedVersion":9223372036854775808`, 1)},
		{"malformed_expiry", strings.Replace(notificationTestBody, `"2026-10-03T03:00:00Z"`, `"tomorrow"`, 1)},
		{"null_pause", strings.Replace(notificationTestBody, `"enabled":true`, `"enabled":true,"pauseUntil":null`, 1)},
		{"zero_pause", strings.Replace(notificationTestBody, `"enabled":true`, `"enabled":true,"pauseUntil":"0001-01-01T00:00:00Z"`, 1)},
		{"surrogate_route", strings.Replace(notificationTestBody, `"NORMAL"`, `"NOR\ud800MAL"`, 1)},
		{"over_body", strings.Repeat(" ", MaxBodyBytes) + notificationTestBody},
		{"invalid_utf8", notificationTestBody + string([]byte{0xff})},
		{"too_deep", strings.Replace(notificationTestBody, `"enabled":true`, `"enabled":`+strings.Repeat("[", 40)+`true`+strings.Repeat("]", 40), 1)},
	}
	for _, key := range []string{"expectedVersion", "enabled", "defaultRoute", "rules", "expiresAt"} {
		var values map[string]json.RawMessage
		if json.Unmarshal([]byte(notificationTestBody), &values) != nil {
			t.Fatal("fixture")
		}
		delete(values, key)
		raw, _ := json.Marshal(values)
		cases = append(cases, struct{ name, body string }{"missing_" + key, string(raw)})
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := DecodePutInput([]byte(c.body))
			if err != ErrInvalid || !reflect.DeepEqual(out, PutInput{}) {
				t.Fatal("unsafe wire input accepted")
			}
			assigned := notificationTestInput()
			// Direct custom invocation preserves raw whitespace bytes. The
			// standard json.Unmarshal trims them before invoking UnmarshalJSON;
			// the HTTP reader and DecodePutInput independently enforce raw size.
			if err := assigned.UnmarshalJSON([]byte(c.body)); err == nil {
				t.Fatal("Unmarshal bypassed strict decoder")
			}
		})
	}
}

func TestNotificationPolicyWirePositive(t *testing.T) {
	body := strings.Replace(notificationTestBody, `"enabled":true`, `"enabled":true,"pauseUntil":"2026-10-03T11:00:00+08:00"`, 1)
	input, err := DecodePutInput([]byte(body))
	if err != nil || input.ExpectedVersion != 0 || !input.Enabled || input.PauseUntil == nil || input.PauseUntil.Location().String() != "UTC" {
		t.Fatal("valid offset request rejected")
	}
	if _, err := NormalizePutInput(input, notificationTestNow()); err != nil {
		t.Fatal("valid wire normalization failed")
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var decoded PutInput
	if err = json.Unmarshal(encoded, &decoded); err != nil || !reflect.DeepEqual(input, decoded) {
		t.Fatal("strict input round trip failed")
	}
	input, err = DecodePutInput([]byte(strings.Replace(notificationTestBody, `[{"category":"MESSAGE","route":"IMMEDIATE"}]`, `[]`, 1)))
	if err != nil || input.Rules == nil || len(input.Rules) != 0 {
		t.Fatal("empty rule list")
	}
	// Decode is timeless shape validation; Store uses its refreshed PG clock.
	if _, err := DecodePutInput([]byte(strings.Replace(notificationTestBody, `2026-10-03T03:00:00Z`, `2025-10-03T03:00:00Z`, 1))); err != nil {
		t.Fatal("wire decoder invented current server time")
	}
}

func TestNotificationPolicyPersistedRulesStrictDecoder(t *testing.T) {
	cases := []string{"", "null", "{}", "[null]", `[{"category":"MESSAGE","route":"NORMAL","ownerId":"secret"}]`, `[{"Category":"MESSAGE","route":"NORMAL"}]`, `[{"category":"MESSAGE","category":"SOCIAL","route":"NORMAL"}]`, `[{"category":"MESSAGE","route":null}]`, `[{"category":"UNKNOWN","route":"NORMAL"}]`, `[{"category":"MESSAGE","route":"SEND"}]`, `[] []`, `[[[[[]]]]]`, strings.Repeat(" ", MaxBodyBytes+1)}
	for i, raw := range cases {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			rules, err := DecodeRules([]byte(raw))
			if err != ErrInvalid || rules != nil {
				t.Fatal("unsafe stored rules accepted")
			}
		})
	}
	rules, err := DecodeRules([]byte(`[{"category":"SOCIAL","route":"BLOCK"},{"category":"ACTIVITY","route":"DIGEST"}]`))
	if err != nil || len(rules) != 2 || rules[0].Category != CategoryActivity {
		t.Fatal("stored rule normalization")
	}
	rules, err = DecodeRules([]byte(`[]`))
	if err != nil || rules == nil {
		t.Fatal("stored rules empty must be []")
	}
}
