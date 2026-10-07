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
