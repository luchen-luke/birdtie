package agentaction

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

func TestSandboxRecoveryHumanReceiptIsClosedHistoricalView(t *testing.T) {
	owner := actorref.PrincipalRef{Type: actorref.Person, ID: "38000000-0000-4000-8000-000000000001"}
	at := time.Date(2026, 10, 7, 1, 0, 0, 0, time.UTC)
	d := Dispatch{Schema: Schema, ID: "38000000-0000-4000-8000-000000000002", ApprovalID: "38000000-0000-4000-8000-000000000003", TenantID: owner.ID, EffectKey: strings.Repeat("a", 64), CommittedAt: at}
	for _, state := range []string{Committed, InFlight, Unknown, Succeeded, NoEffect} {
		x := d
		x.State = state
		if state == InFlight {
			until := at.Add(time.Second)
			x.LeaseUntil = &until
		}
		if state == Succeeded {
			effect := "38000000-0000-4000-8000-000000000004"
			applied := at.Add(time.Microsecond)
			x.EffectID = &effect
			x.AppliedAt = &applied
		}
		v, e := NewHumanRecoveryReceipt(x, owner, d.ApprovalID)
		if e != nil || v.Status != state {
			t.Fatal(state, e, v)
		}
		raw, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		for _, hidden := range []string{"effect_key", "effectKey", "binding", "payload", "session", "authority", "source", "claim", "value"} {
			if strings.Contains(string(raw), hidden) {
				t.Fatal("private authority exported", hidden)
			}
		}
		if state == Succeeded {
			*x.EffectID = owner.ID
			*x.AppliedAt = at
			if *v.EffectID == owner.ID || v.AppliedAt.Equal(at) {
				t.Fatal("borrowed mutable output")
			}
		}
	}
	for _, edit := range []func(*Dispatch){
		func(x *Dispatch) { x.TenantID = d.ID }, func(x *Dispatch) { x.ApprovalID = d.ID }, func(x *Dispatch) { x.ID = "unknown" },
		func(x *Dispatch) { x.State = "FAILED" }, func(x *Dispatch) { x.EffectKey = "confirmed" }, func(x *Dispatch) { x.CommittedAt = time.Time{} },
		func(x *Dispatch) { x.State = Succeeded }, func(x *Dispatch) { x.State = InFlight },
	} {
		x := d
		x.State = Unknown
		edit(&x)
		if v, e := NewHumanRecoveryReceipt(x, owner, d.ApprovalID); e == nil || v.SchemaVersion != "" {
			t.Fatal("bad native receipt accepted", x, v, e)
		}
	}
}
