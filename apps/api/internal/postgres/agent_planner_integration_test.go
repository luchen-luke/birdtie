package postgres

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"testing"
	"time"
)

func TestBoundedPlannerNativeCurrentGoalClarificationAndSourceBinding(t *testing.T) {
	for _, kind := range []string{"unknown", "refine-no-previous", "compare-ambiguous", "area-no-bounds", "completed"} {
		t.Run(kind, func(t *testing.T) {
			f := outputActivityFixture(t, 4)
			b := f.f.native.private.base
			intent, status, filters := agentworkspace.UnsupportedIntent, "ACTIVE", map[string]string{}
			switch kind {
			case "refine-no-previous":
				intent = agentworkspace.RefineResults
			case "compare-ambiguous":
				intent = agentworkspace.CompareResults
				filters["targetIntent"] = agentworkspace.FindActivity
			case "area-no-bounds":
				intent = agentworkspace.AreaDiscovery
			case "completed":
				intent = agentworkspace.FindActivity
				status = "COMPLETED"
			}
			b.exec(`UPDATE agent_tasks SET intent=$2,status=$3,filters=$4::jsonb WHERE id=$1`, f.f.native.task.ID, intent, status, mustPlannerJSON(t, filters))
			goal, e := b.store.PrepareOwnReadonlyPlan(b.ctx, f.f.native.access, f.f.native.task.ID, egressID(t, f))
			if e != nil || goal == nil || goal.NeedsModel() || goal.View().Status != agentplanner.Clarification || len(goal.View().Clarifications) < 1 || len(goal.View().Clarifications) > 2 {
				t.Fatal("unknown/ambiguous source was not clarified", e)
			}
			if raw, e := json.Marshal(goal); e == nil || len(raw) != 0 {
				t.Fatal("private native goal serialized")
			}
			retryAssertOriginalBudgets(t, f, 0)
		})
	}
}
func mustPlannerJSON(t *testing.T, v any) string {
	t.Helper()
	raw, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return string(raw)
}
func TestBoundedPlannerNativeGoalRejectsAnonymousForeignOrgAndExpired(t *testing.T) {
	for _, kind := range []string{"anonymous", "peer", "org", "revoked", "expired", "agent-aba", "context-cancel"} {
		t.Run(kind, func(t *testing.T) {
			f := outputActivityFixture(t, 4)
			b := f.f.native.private.base
			a := f.f.native.access
			ctx := b.ctx
			switch kind {
			case "anonymous":
				a = agentevent.Access{}
			case "peer":
				a = agentevent.Access{SessionDigest: f.f.native.private.peer.SessionDigest}
			case "org":
				a = agentevent.Access{SessionDigest: f.f.native.private.org.SessionDigest}
			case "revoked":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:])
			case "expired":
				b.exec(`UPDATE sessions SET created_at=statement_timestamp()-interval '1 hour',idle_expires_at=statement_timestamp()-interval '1 second' WHERE token_sha256=$1`, a.SessionDigest[:])
			case "agent-aba":
				b.exec(`UPDATE agents SET status='retired' WHERE id=$1`, b.personID)
			case "context-cancel":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			goal, e := b.store.PrepareOwnReadonlyPlan(ctx, a, f.f.native.task.ID, egressID(t, f))
			if e == nil || goal != nil {
				t.Fatal("foreign/invalid native goal accepted", kind, e)
			}
		})
	}
}
func TestBoundedPlannerNativePreparedGoalCannotAttachAfterTaskABA(t *testing.T) {
	f := outputActivityFixture(t, 4)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	goal, e := b.store.PrepareOwnReadonlyPlan(b.ctx, f.f.native.access, f.f.native.task.ID, egressID(t, f))
	if e != nil {
		t.Fatal(e)
	}
	b.exec(`UPDATE agent_tasks SET query=query WHERE id=$1`, f.f.native.task.ID)
	c, cancel := context.WithTimeout(b.ctx, 10*time.Second)
	defer cancel()
	ticket, e := f.gate.Capture(agentfeature.Enrichment)
	if e != nil {
		t.Fatal(e)
	}
	// Native original preview revalidation rejects the ABA before any reserve.
	h, _, e := b.store.CreateOwnLocalPlannerRun(c, f.f.native.access, modelRunID(t, f), plannerBindings(t, p, f), f.gate, ticket, goal)
	if e == nil || h != nil {
		t.Fatal("stale goal acquired native handle", e)
	}
	retryAssertOriginalBudgets(t, f, 0)
}
func plannerBindings(t *testing.T, p modelegressbudget.Preview, f *egressFixture) []modelegressbudget.LocalRetryBinding {
	t.Helper()
	a := &nativeLocalAttemptAdapter{}
	return []modelegressbudget.LocalRetryBinding{{Input: modelegressbudget.ReserveInput{OperationID: egressID(t, f), PreviewID: p.ID, RootTraceID: f.root, TaskID: p.TaskID}, Destination: a.LocalDestination()}}
}

type plannerForgedGoal struct{ view agentplanner.View }

func (g plannerForgedGoal) View() agentplanner.View         { return g.view }
func (plannerForgedGoal) NeedsModel() bool                  { return true }
func (plannerForgedGoal) Remaining(time.Time) time.Duration { return time.Hour }

func TestBoundedPlannerNativeGoalIsNotTransferableApproval(t *testing.T) {
	for _, kind := range []string{"foreign-session", "other-store", "wire-forgery", "missing-deadline", "too-long-deadline", "source-revoked", "flag-off-on"} {
		t.Run(kind, func(t *testing.T) {
			f := outputActivityFixture(t, 4)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			goal, e := b.store.PrepareOwnReadonlyPlan(b.ctx, f.f.native.access, f.f.native.task.ID, egressID(t, f))
			if e != nil {
				t.Fatal(e)
			}
			ticket, e := f.gate.Capture(agentfeature.Enrichment)
			if e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithTimeout(b.ctx, 10*time.Second)
			defer cancel()
			a, store := f.f.native.access, b.store
			switch kind {
			case "foreign-session":
				a = agentevent.Access{SessionDigest: f.f.native.private.peer.SessionDigest}
			case "other-store":
				store = New(b.pool, false)
			case "wire-forgery":
				goal = plannerForgedGoal{goal.View()}
			case "missing-deadline":
				ctx = b.ctx
			case "too-long-deadline":
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(b.ctx, time.Minute)
				defer stop()
			case "source-revoked":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:])
			case "flag-off-on":
				retryGateRestore(t, f)
			}
			h, _, e := store.CreateOwnLocalPlannerRun(ctx, a, modelRunID(t, f), plannerBindings(t, p, f), f.gate, ticket, goal)
			if e == nil || h != nil {
				t.Fatal("native prepared read crossed original association or hard cap", kind, e)
			}
			outputAssertRevokedBudgetSQL(t, f, 0)
		})
	}
}
