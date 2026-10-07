package postgres

import (
	"context"
	"encoding/json"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"testing"
)

func TestAgentActionNativeLateClaimIdentityGateAndJSONNeverExecute(t *testing.T) {
	for _, kind := range []string{"peer", "other-controller", "off-on", "cancel", "json"} {
		t.Run(kind, func(t *testing.T) {
			x := actionNativeFixture(t)
			f := x.f
			b := f.f.native.private.base
			actionApprove(t, x)
			_, h := actionCommitNative(t, x)
			_, cl, e := h.Begin(b.ctx, f.f.native.access, f.gate)
			if e != nil {
				t.Fatal(e)
			}
			a := f.f.native.access
			c := f.gate
			ctx := b.ctx
			switch kind {
			case "peer":
				a = agentevent.Access{SessionDigest: f.f.native.private.peer.SessionDigest}
			case "other-controller":
				c = egressGate(t, true)
			case "off-on":
				retryGateRestore(t, f)
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "json":
				var fake nativeActionClaim
				if json.Unmarshal([]byte(`{"approved":true,"fence":1,"owner":"fake"}`), &fake) == nil {
					t.Fatal("JSON made claim")
				}
				cl = &fake
			}
			if d, e := cl.Execute(ctx, a, c); e == nil || d.State == aa.Succeeded || d.EffectID != nil {
				t.Fatal("late/wrong caller escaped", kind, e, d)
			}
			if _, w := actionRows(t, x); w != 0 {
				t.Fatal("late caller wrote")
			}
		})
	}
}

func TestAgentActionNativeOneActualPrivateSandboxEffectAndOldDomainUnchanged(t *testing.T) {
	x := actionNativeFixture(t)
	f := x.f
	b := f.f.native.private.base
	before := toolBusinessSnapshot(t, f)
	if d, h, e := b.store.CommitOwnSandboxAction(b.ctx, f.f.native.access, x.p.Binding.ApprovalID, x.p.Proposal, f.gate); e == nil || h != nil || d.ID != "" {
		t.Fatal("pending/model fields became approval")
	}
	actionApprove(t, x)
	d, h := actionCommitNative(t, x)
	if _, w := actionRows(t, x); w != 0 || d.EffectID != nil {
		t.Fatal("consumed approval falsely executed")
	}
	if _, e := json.Marshal(h); e == nil {
		t.Fatal("commitment transferred via JSON")
	}
	flight, claim, e := h.Begin(b.ctx, f.f.native.access, f.gate)
	if e != nil || claim == nil || flight.State != aa.InFlight {
		t.Fatal(e, flight)
	}
	if _, e := json.Marshal(claim); e == nil {
		t.Fatal("claim transferable")
	}
	result, e := claim.Execute(b.ctx, f.f.native.access, f.gate)
	if e != nil || result.State != aa.Succeeded || result.EffectID == nil {
		t.Fatal("no actual sandbox effect receipt", e, result)
	}
	next, e := claim.Execute(b.ctx, f.f.native.access, f.gate)
	if e != nil || next.EffectID == nil || *next.EffectID != *result.EffectID {
		t.Fatal("same original claim did not return actual immutable receipt", e)
	}
	duplicate, again, e := b.store.CommitOwnSandboxAction(b.ctx, f.f.native.access, x.p.Binding.ApprovalID, x.p.Proposal, f.gate)
	if e != nil || again != nil || duplicate.State != aa.Succeeded {
		t.Fatal("duplicate acquired another send capability", e, duplicate)
	}
	if _, claim, e := h.Begin(b.ctx, f.f.native.access, f.gate); e == nil || claim != nil {
		t.Fatal("commitment consumed twice")
	}
	dc, w := actionRows(t, x)
	if dc != 1 || w != 1 {
		t.Fatal("duplicate actual effect", dc, w)
	}
	if before != toolBusinessSnapshot(t, f) {
		t.Fatal("sandbox touched ordinary business/memory/model rows")
	}
	retryAssertOriginalBudgets(t, f, 0)
	actionWire(t, "sandbox-native-preview", x.p)
	actionWire(t, "sandbox-native-dispatch-not-success", d)
	actionWire(t, "sandbox-native-actual-effect", result)
}
