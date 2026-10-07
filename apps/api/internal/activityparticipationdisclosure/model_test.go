package activityparticipationdisclosure

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParticipationDisclosureClosedInput(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	for _, raw := range []string{`null`, `{}`, `{"participationId":"` + id + `","operation":"PUBLIC"}`, `{"participationId":"` + id + `","operation":"PRIVATE","confirmed":true}`, `{"participationId":"` + id + `","operation":"PRIVATE","operation":"PUBLIC"}`, `{"participationId":"` + id + `","operation":"PRIVATE"} {}`, `{"participationId":"` + id + `","operation":"PRIVATE","disclosureExpiresAt":null}`, `{"participationId":"` + id + `","operation":"PUBLIC","disclosureExpiresAt":"2026-02-30T00:00:00Z"}`} {
		if _, _, e := DecodeBody([]byte(raw), false); e == nil {
			t.Fatal(raw)
		}
	}
	for _, at := range []string{"2026-10-04T13:00:00+08:00", "2026-10-04T05:00:00Z", "2026-10-04T03:00:00-02:00"} {
		if _, _, e := DecodeBody([]byte(`{"participationId":"`+id+`","operation":"PUBLIC","disclosureExpiresAt":"`+at+`"}`), false); e != nil {
			t.Fatal(at, e)
		}
	}
	if _, _, e := DecodeBody([]byte(`{"participationId":"`+id+`","operation":"PRIVATE"}`), false); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{`{"preview":"opaqueServerOwnedPreview0123456789","owner":"` + id + `"}`, `{"preview":"opaqueServerOwnedPreview0123456789","preview":"opaqueServerOwnedPreview0123456789"}`, `{"preview":null}`, `{"preview":"short"}`} {
		if _, _, e := DecodeBody([]byte(raw), true); e == nil {
			t.Fatal(raw)
		}
	}
}
func TestParticipationDisclosureOutputNoEffectsAndHiddenSource(t *testing.T) {
	now := time.Now().UTC()
	id := "11111111-1111-4111-8111-111111111111"
	agent := "22222222-2222-4222-8222-222222222222"
	v := View{SchemaVersion: Schema, OwnerID: id, AgentID: agent, ObservedAt: now, Records: []Record{{ParticipationID: id, ActivityID: agent, Title: "已不可公开展示的活动报名", Status: "cancelled", Visibility: "PRIVATE", Attendance: "UNKNOWN"}}, Limit: 100}
	if ValidateView(v, id) != nil {
		t.Fatal(v)
	}
	raw, _ := json.Marshal(v)
	for _, field := range []string{"ownerId", "agentId", "attendance", "modelAccess"} {
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil || len(m) == 0 || field == "" {
			t.Fatal(field)
		}
	}
	for _, mode := range []string{"wrongowner", "effect", "attendance", "hiddenname", "hidden_time", "missingagent", "duplicate", "status", "limit"} {
		t.Run(mode, func(t *testing.T) {
			b := v
			b.Records = append([]Record{}, v.Records...)
			switch mode {
			case "wrongowner":
				b.OwnerID = agent
			case "effect":
				b.SendAllowed = true
			case "attendance":
				b.Records[0].Attendance = "PRESENT"
			case "hiddenname":
				b.Records[0].Title = "私密活动"
			case "hidden_time":
				b.Records[0].StartsAt = &now
			case "missingagent":
				b.AgentID = ""
			case "duplicate":
				b.Records = append(b.Records, b.Records[0])
			case "status":
				b.Records[0].Status = "attended"
			case "limit":
				b.Limit = 200
			}
			if ValidateView(b, id) == nil {
				t.Fatal(mode)
			}
		})
	}
}
