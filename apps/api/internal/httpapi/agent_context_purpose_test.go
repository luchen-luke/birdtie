package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
)

func TestContextPurposeClosedControlObjects(t *testing.T) {
	if _, e := contextPurposeObject([]byte(`{"previewId":"id"}`), "previewId"); e != nil {
		t.Fatal(e)
	}
	for name, raw := range map[string]string{
		"duplicate": `{"previewId":"one","previewId":"two"}`, "case": `{"PreviewId":"one"}`, "null": `{"previewId":null}`,
		"owner": `{"previewId":"one","ownerId":"somebody"}`, "confirmed": `{"previewId":"one","confirmed":true}`,
		"missing": `{}`, "trailing": `{"previewId":"one"}{}`, "huge": strings.Repeat("x", 8193),
	} {
		t.Run(name, func(t *testing.T) {
			if _, e := contextPurposeObject([]byte(raw), "previewId"); e == nil {
				t.Fatal("invalid control object accepted")
			}
		})
	}
}
func TestContextPurposeRuntimeConsumesSelectedNativeViewWithoutNewAuthority(t *testing.T) {
	b := acb.Bundle{TaskID: "task", CurrentQuery: "帮我找周末的羽毛球", City: &acb.ContextCity{ID: "city", TimeZone: "Europe/London"},
		Profile: map[string]json.RawMessage{"availability": json.RawMessage(`"周末上午"`)}, Memories: []acb.ReviewMemory{{ID: "memory", Summary: "我明确偏好室内活动"}},
		Places: []acb.PublicPlace{{ID: "place", Name: "当前地点", Category: "sports"}}, Activities: []acb.PublicActivity{{ID: "activity", Title: "当前活动", Category: "badminton"}},
		Relationships: []acb.ContextTie{{ID: "tie", PeerAccountID: "peer", State: "ACCEPTED"}}, Sources: []acb.Source{{Kind: "PURPOSE_PRIVATE_PROFILE", ID: "agent", RowToken: "INTERNAL_ROW_CANARY"}}}
	out := consumeLocalTaskContext(b)
	encoded, e := json.Marshal(out)
	if e != nil {
		t.Fatal(e)
	}
	raw := string(encoded)
	for _, value := range []string{b.CurrentQuery, "周末上午", "我明确偏好室内活动", "当前地点", "当前活动", "Europe/London", "ACCEPTED"} {
		if !strings.Contains(raw, value) {
			t.Fatal("selected native content was not consumed", value)
		}
	}
	if out.ModelAccess != "UNAVAILABLE" || out.MemoryPromotionAllowed || strings.Contains(raw, "INTERNAL_ROW_CANARY") || b.Sources[0].RowToken != "INTERNAL_ROW_CANARY" {
		t.Fatal("runtime output acquired authority or mutated native source")
	}
}
