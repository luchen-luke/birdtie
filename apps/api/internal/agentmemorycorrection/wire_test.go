package agentmemorycorrection

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMemoryCorrectionStrictWire(t *testing.T) {
	v := Input{ID: testID, TargetKind: "MEMORY", TargetID: testID, ExpectedVersion: 1, Action: "NEGATE", Category: "hiking"}
	raw, _ := json.Marshal(v)
	if _, e := DecodeInput(raw); e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{string(raw) + `{}`, `null`, `[]`, strings.Replace(string(raw), `"id":`, `"ID":`, 1), strings.Replace(string(raw), `"action":"NEGATE"`, `"action":"NEGATE","action":"DELETE"`, 1), strings.Replace(string(raw), `"action":"NEGATE"`, `"action":"NEGATE","confirmed":true`, 1)} {
		t.Run(s, func(t *testing.T) {
			if _, e := DecodeInput([]byte(s)); e == nil {
				t.Fatal("unsafe input")
			}
		})
	}
	if _, e := DecodeConfirm([]byte(`{"planDigest":"` + strings.Repeat("a", 64) + `"}`)); e != nil {
		t.Fatal(e)
	}
	if _, e := DecodeConfirm([]byte(`{"planDigest":"` + strings.Repeat("a", 64) + `","confirmed":true}`)); e == nil {
		t.Fatal("self authority")
	}
}

func TestMemoryCorrectionWireReplacementClosed(t *testing.T) {
	raw := []byte(`{"id":"` + testID + `","targetKind":"MEMORY","targetId":"` + testID + `","expectedVersion":1,"action":"EDIT","replacement":{"expectedVersion":1,"memoryType":"PREFERENCE","memoryKey":"activity_category:hiking","summary":"我偏好徒步活动","structuredValue":{"activityCategory":"hiking"},"visibility":"PRIVATE","validUntil":"2026-10-05T12:00:00Z"}}`)
	if _, e := DecodeInput(raw); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{strings.Replace(string(raw), `"summary":`, `"Summary":`, 1), strings.Replace(string(raw), `"activityCategory":"hiking"`, `"activityCategory":"hiking","activityCategory":"sports"`, 1), strings.Replace(string(raw), `"visibility":"PRIVATE"`, `"visibility":"PUBLIC"`, 1), strings.Replace(string(raw), `"expectedVersion":1,"memoryType"`, `"expectedVersion":2,"memoryType"`, 1)} {
		if _, e := DecodeInput([]byte(bad)); e == nil {
			t.Fatal("unsafe nested replacement accepted")
		}
	}
}
