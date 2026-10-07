package agentmemory

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func memoryWirePut() string {
	return `{"expectedVersion":0,"memoryType":"PREFERENCE","memoryKey":"activity.hiking",` +
		`"summary":"本人主动声明喜欢徒步","structuredValue":{"category":"hiking"},` +
		`"visibility":"PRIVATE","validUntil":"2026-10-03T12:00:00Z"}`
}

func TestMemoryWireValidManualInputs(t *testing.T) {
	for _, item := range []struct{ name, body string }{
		{"create", memoryWirePut()},
		{"update", strings.Replace(memoryWirePut(), `"expectedVersion":0`, `"expectedVersion":14`, 1)},
		{"owner_review_agent_only", strings.Replace(memoryWirePut(), `"PRIVATE"`, `"AGENT_ONLY"`, 1)},
		{"empty_value_object", strings.Replace(memoryWirePut(), `{"category":"hiking"}`, `{}`, 1)},
		{"timezone_normalizes", strings.Replace(memoryWirePut(), `2026-10-03T12:00:00Z`, `2026-10-03T20:00:00+08:00`, 1)},
		{"nested_typed_data", strings.Replace(memoryWirePut(), `{"category":"hiking"}`, `{"list":["徒步",true,1,null],"nested":{"enabled":false}}`, 1)},
	} {
		t.Run(item.name, func(t *testing.T) {
			decoded, err := DecodePutInput([]byte(item.body))
			if err != nil || decoded.MemoryType != TypePreference || !decoded.ValidUntil.Equal(memoryTestNow().Add(24*time.Hour)) {
				t.Fatal("bounded explicit input wire decoding failed")
			}
			var ordinary PutInput
			if err := json.Unmarshal([]byte(item.body), &ordinary); err != nil || !reflect.DeepEqual(decoded, ordinary) {
				t.Fatal("ordinary unmarshal bypassed domain normalization")
			}
		})
	}
}

func TestMemoryWireRejectsAuthorityAndPrivacyOverrides(t *testing.T) {
	for _, key := range []string{"ownerId", "ownerType", "agentId", "id", "schemaVersion", "version",
		"sourceType", "confidence", "status", "validFrom", "lastReinforcedAt", "createdAt", "updatedAt",
		"sourceId", "sourceVersion", "sourceKind", "evidence", "candidateId", "confirmed", "consent",
		"consentEpoch", "analysisAuthorized", "verified", "modelUse", "recipientId", "purpose", "scope"} {
		t.Run(key, func(t *testing.T) {
			body := strings.TrimSuffix(memoryWirePut(), "}") + `,"` + key + `":true}`
			actual, err := DecodePutInput([]byte(body))
			if !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(actual, PutInput{}) || err.Error() != ErrInvalid.Error() {
				t.Fatal("client authority override returned data or payload-dependent errors")
			}
		})
	}
}

func TestMemoryWireRejectsMalformedInputs(t *testing.T) {
	base := memoryWirePut()
	cases := []struct{ name, body string }{
		{"empty", ""}, {"null", "null"}, {"array", "[]"}, {"scalar", `"statement"`},
		{"second_json", base + "{}"}, {"trailing_garbage", base + "x"},
		{"duplicate_version", strings.TrimSuffix(base, "}") + `,"expectedVersion":1}`},
		{"escaped_duplicate", strings.TrimSuffix(base, "}") + `,"expected\u0056ersion":1}`},
		{"case_alias", strings.Replace(base, `"expectedVersion"`, `"ExpectedVersion"`, 1)},
		{"missing_version", strings.Replace(base, `"expectedVersion":0,`, "", 1)},
		{"null_version", strings.Replace(base, `"expectedVersion":0`, `"expectedVersion":null`, 1)},
		{"string_version", strings.Replace(base, `"expectedVersion":0`, `"expectedVersion":"0"`, 1)},
		{"bool_version", strings.Replace(base, `"expectedVersion":0`, `"expectedVersion":false`, 1)},
		{"negative_version", strings.Replace(base, `"expectedVersion":0`, `"expectedVersion":-1`, 1)},
		{"fractional_version", strings.Replace(base, `"expectedVersion":0`, `"expectedVersion":0.5`, 1)},
		{"exponent_version", strings.Replace(base, `"expectedVersion":0`, `"expectedVersion":1e0`, 1)},
		{"overflow_version", strings.Replace(base, `"expectedVersion":0`, `"expectedVersion":9223372036854775808`, 1)},
		{"null_summary", strings.Replace(base, `"summary":"本人主动声明喜欢徒步"`, `"summary":null`, 1)},
		{"empty_summary", strings.Replace(base, `本人主动声明喜欢徒步`, " ", 1)},
		{"invalid_surrogate_summary", strings.Replace(base, `本人主动声明喜欢徒步`, `\ud800`, 1)},
		{"null_structured", strings.Replace(base, `{"category":"hiking"}`, `null`, 1)},
		{"array_structured", strings.Replace(base, `{"category":"hiking"}`, `[]`, 1)},
		{"duplicate_structured", strings.Replace(base, `{"category":"hiking"}`, `{"x":1,"x":2}`, 1)},
		{"nested_duplicate", strings.Replace(base, `{"category":"hiking"}`, `{"x":{"k":1,"\u006b":2}}`, 1)},
		{"null_until", strings.Replace(base, `"2026-10-03T12:00:00Z"`, `null`, 1)},
		{"number_until", strings.Replace(base, `"2026-10-03T12:00:00Z"`, `1`, 1)},
		{"invalid_until", strings.Replace(base, `2026-10-03T12:00:00Z`, `tomorrow`, 1)},
		{"zero_until", strings.Replace(base, `2026-10-03T12:00:00Z`, `0001-01-01T00:00:00Z`, 1)},
		{"public", strings.Replace(base, `"PRIVATE"`, `"PUBLIC"`, 1)},
		{"oversize_raw_whitespace", base + strings.Repeat(" ", MaxBodyBytes)},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			actual, err := DecodePutInput([]byte(item.body))
			if !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(actual, PutInput{}) {
				t.Fatal("invalid body returned a partly populated declaration")
			}
			// encoding/json strips outer whitespace before UnmarshalJSON.
			// The HTTP gateway must enforce its raw-body bound and call the
			// domain decoder; a custom method cannot see discarded bytes.
			if item.name == "oversize_raw_whitespace" {
				return
			}
			ordinary := memoryTestPut()
			if err := json.Unmarshal([]byte(item.body), &ordinary); err == nil {
				t.Fatal("ordinary JSON unmarshalling bypassed strict input rejection")
			}
			// encoding/json can reject malformed syntax before calling an
			// UnmarshalJSON method; domain DecodePutInput always zeroes errors.
			if json.Valid([]byte(item.body)) && !reflect.DeepEqual(ordinary, PutInput{}) {
				t.Fatal("domain UnmarshalJSON kept stale successful input after an error")
			}
		})
	}
	for _, key := range []string{"memoryType", "memoryKey", "summary", "structuredValue", "visibility", "validUntil"} {
		t.Run("missing_"+key, func(t *testing.T) {
			var object map[string]json.RawMessage
			if json.Unmarshal([]byte(base), &object) != nil {
				t.Fatal("test input setup failed")
			}
			delete(object, key)
			body, _ := json.Marshal(object)
			if _, err := DecodePutInput(body); !errors.Is(err, ErrInvalid) {
				t.Fatal("required declaration field missing without rejection")
			}
		})
	}
}

func TestMemoryWireExpiryRequiresActualWriteClock(t *testing.T) {
	for _, item := range []struct {
		name  string
		clock time.Time
		ok    bool
	}{
		{"current", memoryTestNow(), true},
		{"expired", memoryTestNow().Add(48 * time.Hour), false},
		{"outside_retention", memoryTestNow().Add(-MaxValidity), false},
	} {
		t.Run(item.name, func(t *testing.T) {
			decoded, err := DecodePutInput([]byte(memoryWirePut()))
			if err != nil {
				t.Fatal("syntax decoding incorrectly depends on an invented current time")
			}
			_, err = NormalizePutInput(decoded, item.clock)
			if (err == nil) != item.ok {
				t.Fatal("write clock did not enforce future bounded retention")
			}
		})
	}
}

func TestMemoryStructuredValueBoundsAndTypes(t *testing.T) {
	cases := []struct {
		name, raw string
		ok        bool
	}{
		{"object", `{"declared":true}`, true},
		{"typed_values", `{"a":[1,false,null,"徒步"],"b":{"x":"text"}}`, true},
		{"empty_object", `{}`, true},
		{"nonobject", `[{}]`, false},
		{"duplicate", `{"x":1,"x":2}`, false},
		{"decoded_duplicate", `{"x":1,"\u0078":2}`, false},
		{"duplicate_in_array", `{"a":[{"x":1,"x":2}]}`, false},
		{"empty_key", `{"":1}`, false},
		{"oversize_key", `{"` + strings.Repeat("a", 101) + `":1}`, false},
		{"control_key", `{"a\nb":1}`, false},
		{"null_control_string", `{"x":"a\u0000b"}`, false},
		{"unpaired_surrogate", `{"x":"\ud800"}`, false},
		{"unpaired_low_surrogate", `{"x":"\udc00"}`, false},
		{"paired_surrogate", `{"x":"\ud83d\ude80"}`, true},
		{"huge_number", `{"x":1e999}`, false},
		{"trailing_json", `{} {}`, false},
		{"canonical_8k", `{"x":"` + strings.Repeat("a", MaxStructuredValueBytes-8) + `"}`, true},
		{"canonical_over8k", `{"x":"` + strings.Repeat("a", MaxStructuredValueBytes-7) + `"}`, false},
		{"formatted_jsonb_8k", ` { "x" : "` + strings.Repeat("a", MaxStructuredValueBytes-8) + `" } `, true},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			actual, err := NormalizeStructuredValue(json.RawMessage(item.raw))
			if (err == nil) != item.ok || (err != nil && (actual != nil || !errors.Is(err, ErrInvalid))) {
				t.Fatal("structured value did not enforce bounded strict JSON object contract")
			}
		})
	}
	t.Run("depth_exact", func(t *testing.T) {
		value := `1`
		for i := 0; i < MaxStructuredDepth-1; i++ {
			value = `{"x":` + value + `}`
		}
		if _, err := NormalizeStructuredValue(json.RawMessage(value)); err != nil {
			t.Fatal("maximum allowed JSON depth rejected")
		}
		if _, err := NormalizeStructuredValue(json.RawMessage(`{"x":` + value + `}`)); !errors.Is(err, ErrInvalid) {
			t.Fatal("excess JSON depth accepted")
		}
	})
	for _, count := range []int{MaxArrayItems, MaxArrayItems + 1} {
		t.Run(fmt.Sprintf("array_%d", count), func(t *testing.T) {
			raw := `{"a":[` + strings.TrimSuffix(strings.Repeat("1,", count), ",") + `]}`
			_, err := NormalizeStructuredValue(json.RawMessage(raw))
			if (err == nil) != (count <= MaxArrayItems) {
				t.Fatal("array item bound violated")
			}
		})
	}
	for _, count := range []int{MaxObjectMembers, MaxObjectMembers + 1} {
		t.Run(fmt.Sprintf("members_%d", count), func(t *testing.T) {
			members := []string{}
			for i := 0; i < count; i++ {
				members = append(members, fmt.Sprintf(`"k%d":1`, i))
			}
			_, err := NormalizeStructuredValue(json.RawMessage(`{` + strings.Join(members, ",") + `}`))
			if (err == nil) != (count <= MaxObjectMembers) {
				t.Fatal("object member bound violated")
			}
		})
	}
	t.Run("total_nodes", func(t *testing.T) {
		members := []string{}
		for i := 0; i < 8; i++ {
			members = append(members, fmt.Sprintf(`"k%d":[%s]`, i, strings.TrimSuffix(strings.Repeat("0,", 32), ",")))
		}
		if _, err := NormalizeStructuredValue(json.RawMessage(`{` + strings.Join(members, ",") + `}`)); !errors.Is(err, ErrInvalid) {
			t.Fatal("bounded containers collectively exceeded total node budget")
		}
	})
}

func TestMemoryDeleteInputStrictCASOnly(t *testing.T) {
	for _, version := range []int64{1, 12, math.MaxInt64} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"expectedVersion":%d}`, version))
			actual, err := DecodeDeleteInput(body)
			if err != nil || actual.ExpectedVersion != version {
				t.Fatal("positive delete CAS shape rejected")
			}
		})
	}
	for _, item := range []struct{ name, body string }{
		{"empty", ""}, {"null", "null"}, {"zero", `{"expectedVersion":0}`},
		{"negative", `{"expectedVersion":-1}`}, {"null_version", `{"expectedVersion":null}`},
		{"string", `{"expectedVersion":"1"}`}, {"fraction", `{"expectedVersion":1.5}`},
		{"exponent", `{"expectedVersion":1e0}`}, {"extra", `{"expectedVersion":1,"confirmed":true}`},
		{"duplicate", `{"expectedVersion":1,"expectedVersion":2}`},
		{"case", `{"ExpectedVersion":1}`}, {"second_json", `{"expectedVersion":1}{}`},
	} {
		t.Run(item.name, func(t *testing.T) {
			actual, err := DecodeDeleteInput([]byte(item.body))
			if !errors.Is(err, ErrInvalid) || actual != (DeleteInput{}) {
				t.Fatal("unsafe delete input returned data")
			}
		})
	}
}
