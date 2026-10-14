package activeintent

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"testing"
	"time"
)

func TestActiveIntentClosedDraftAndApprovalInput(t *testing.T) {
	d := socialintent.DraftInput{Type: "FIND_ACTIVITY", Title: "本轮真实意图", Constraints: json.RawMessage(`{"startsAt":"2030-01-01T18:00:00+08:00","endsAt":"2030-01-01T19:00:00+08:00"}`), Audience: "PRIVATE", Modality: "ONLINE", ExpiresAt: time.Now().Add(time.Hour)}
	if _, e := NormalizeDraft(d); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*socialintent.DraftInput){func(v *socialintent.DraftInput) { v.Audience = "SYSTEM" }, func(v *socialintent.DraftInput) { v.Audience = "PRIVATE"; v.InviteeIDs = []string{"bad"} }, func(v *socialintent.DraftInput) {
		v.Constraints = json.RawMessage(`{"startsAt":"2030-01-01T18:00:00Z"}`)
	}, func(v *socialintent.DraftInput) { v.ContextID = "bad" }, func(v *socialintent.DraftInput) { v.Modality = "IN_PERSON" }, func(v *socialintent.DraftInput) { v.Constraints = json.RawMessage(`{"preciseLocation":true}`) }} {
		v := d
		change(&v)
		if _, e := NormalizeDraft(v); e == nil {
			t.Fatal("invalid draft accepted", v)
		}
	}
	for _, in := range []Input{{Operation: "EDIT", ExpectedVersion: "x"}, {Operation: "CANCEL", ExpectedVersion: string(make([]byte, 64))}, {Operation: "AUTO", ExpectedVersion: "0123456789012345678901234567890123456789012345678901234567890123"}} {
		if ValidateInput(in) == nil {
			t.Fatal("unbound input accepted")
		}
	}
}

func TestActiveIntentRelationshipAudienceDrafts(t *testing.T) {
	d := socialintent.DraftInput{Type: "FIND_COMPANION", Title: "本人明确的受众", Constraints: json.RawMessage(`{}`), Audience: "PRIVATE", Modality: "ONLINE", ExpiresAt: time.Now().Add(time.Hour)}
	for _, audience := range []string{"PUBLIC", "FOLLOWERS", "CONNECTIONS", "CLOSE", "PRIVATE", "FRIENDS"} {
		t.Run(audience, func(t *testing.T) {
			v := d
			v.Audience = audience
			if _, e := NormalizeDraft(v); e != nil {
				t.Fatalf("current audience rejected: %v", e)
			}
			for _, field := range []string{"city", "community", "invitee"} {
				bad := v
				switch field {
				case "city":
					bad.CityID = "aberdeen-gb"
				case "community":
					bad.CommunityID = "33333333-3333-4333-8333-333333333333"
				case "invitee":
					bad.InviteeIDs = []string{"22222222-2222-4222-8222-222222222222"}
				}
				if _, e := NormalizeDraft(bad); e == nil {
					t.Fatal("relationship audience accepted unrelated target", field)
				}
			}
		})
	}
}
