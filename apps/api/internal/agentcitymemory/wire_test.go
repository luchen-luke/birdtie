package agentcitymemory

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestCityMemoryStrictWire(t *testing.T) {
	raw, _ := json.Marshal(testInput(Visited))
	if _, e := DecodePut(raw, testNow()); e != nil {
		t.Fatal(e)
	}
	base := string(raw)
	cases := map[string]string{"null": "null", "array": "[]", "trailing": base + base, "oversize": strings.Repeat(" ", 2049) + base, "missing": "{}", "nested": strings.Replace(base, `"cityId":"aberdeen-gb"`, `"cityId":{}`, 1), "bad_type": strings.Replace(base, `"expectedVersion":0`, `"expectedVersion":"0"`, 1), "duplicate": strings.Replace(base, `"kind":"VISITED"`, `"kind":"VISITED","kind":"VISITED"`, 1), "case": strings.Replace(base, `"cityId"`, `"CityId"`, 1), "fake_dates": strings.TrimSuffix(base, "}") + `,"visitedAt":"2020-01-01"}`, "null_deadline": strings.Replace(base, `"validUntil":"2026-10-03T02:00:00Z"`, `"validUntil":null`, 1)}
	for _, field := range []string{"owner", "agentId", "confirmed", "verified", "confidence", "sourceType", "status", "consent", "grant", "location", "current"} {
		cases["unknown_"+field] = strings.TrimSuffix(base, "}") + fmt.Sprintf(`,"%s":true}`, field)
	}
	for _, field := range []string{"expectedVersion", "cityId", "kind", "visibility", "validUntil"} {
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		fields[field] = json.RawMessage(`null`)
		v, _ := json.Marshal(fields)
		cases["null_"+field] = string(v)
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			got, e := DecodePut([]byte(v), testNow())
			if !errors.Is(e, ErrInvalid) || got != (PutDeclarationInput{}) {
				t.Fatalf("strict decoder accepted %s: %v", name, e)
			}
		})
	}
}
