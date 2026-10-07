package modelegressbudget

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/birdtie/birdtie/apps/api/internal/modelresilience"
)

func TestModelEgressLocalRetryAbsentNativePortFailsClosed(t *testing.T) {
	if NewLocalRetryRunner(nil, nil, modelresilience.DefaultPolicy()) != nil {
		t.Fatal("missing native port accepted")
	}
}

func TestModelEgressLocalRetryAttemptDeadlineCanOnlyTighten(t *testing.T) {
	var out LocalAttemptOutcome
	short := time.Now().Add(time.Second)
	out.tightenDeadline(short)
	out.tightenDeadline(short.Add(time.Hour))
	out.tightenDeadline(time.Time{})
	if !out.monotonicDeadline.Equal(short) {
		t.Fatal("attempt bound extended")
	}
	out.tightenDeadline(short.Add(-time.Millisecond))
	if !out.monotonicDeadline.Before(short) {
		t.Fatal("earlier bound discarded")
	}
	if _, e := json.Marshal(out); !errors.Is(e, ErrServerOnly) {
		t.Fatal("private deadline became reconstructible", e)
	}
}

type localRetryTestPort struct {
	LocalRetryPort
	captures int
}

func (p *localRetryTestPort) CaptureOwnLocalRetryPlan(context.Context, agentevent.Access, []LocalRetryBinding, *agentfeature.Controller) (LocalRetryProof, error) {
	p.captures++
	return nil, ErrUnavailable
}
func TestModelEgressLocalRetryInvalidPlanStopsBeforeNativeCapture(t *testing.T) {
	for _, kind := range []string{"duplicate", "crossTask", "wrongController", "count", "sameRouteNoSwitchAllowed"} {
		t.Run(kind, func(t *testing.T) {
			gate := localTestGate(t, true)
			port := &localRetryTestPort{}
			adapter := &localTestAdapter{}
			d := NewLocalAttemptDriver(port, adapter, gate)
			policy := modelresilience.DefaultPolicy()
			policy.MaxAttempts = 3
			policy.SameRouteAttempts = 1
			policy.MaxProviderSwitches = 0
			steps := []LocalRetryStep{{Driver: d, Input: ReserveInput{OperationID: "a", RootTraceID: "root", TaskID: "task"}}, {Driver: d, Input: ReserveInput{OperationID: "b", RootTraceID: "root", TaskID: "task"}}}
			switch kind {
			case "duplicate":
				steps[1].Input.OperationID = "a"
			case "crossTask":
				steps[1].Input.TaskID = "other"
			case "wrongController":
				steps[1].Driver = NewLocalAttemptDriver(port, adapter, localTestGate(t, true))
			case "count":
				policy.MaxAttempts = 1
			}
			r := NewLocalRetryRunner(port, gate, policy)
			if r == nil {
				t.Fatal("valid policy constructor denied")
			}
			_, e := r.Run(context.Background(), agentevent.Access{}, steps)
			if kind == "sameRouteNoSwitchAllowed" {
				if !errors.Is(e, ErrUnavailable) || port.captures != 1 {
					t.Fatal("old010 same route compatibility lost", e)
				}
			} else if e == nil || port.captures != 0 {
				t.Fatal("invalid plan reached native", e, port.captures)
			}
		})
	}
}
func TestModelEgressLocalRetryFailureIsExactAndGettersCopy(t *testing.T) {
	request := modelgateway.Request{SchemaVersion: modelgateway.RequestVersion, RunID: "74000000-0000-4000-8000-000000000001",
		Agent:    agentcognitive.AgentReference{AgentID: "74000000-0000-4000-8000-000000000003", Principal: actorref.PrincipalRef{Type: actorref.Person, ID: "74000000-0000-4000-8000-000000000002"}, Role: agentruntime.PersonalAgent},
		TaskKind: modelgateway.ActivityQuery, PromptVersion: "activity_query.v1", InputSchemaVersion: "air.messages.v1", OutputSchemaVersion: "air.answer.v1",
		ContextSnapshotRef: "74000000-0000-4000-8000-000000000004", DataPolicyRef: "74000000-0000-4000-8000-000000000005", BudgetRef: "74000000-0000-4000-8000-000000000006", Budget: modelgateway.Budget{MaxOutputTokens: 128},
		Messages: []modelgateway.Message{{Role: "user", Content: "original private query"}}, OutputMode: modelgateway.Text, ToolAllowlist: []string{}, CapabilitiesRequired: []string{"text"}, DeadlineAt: time.Now().UTC().Add(time.Minute)}
	f := newLocalRetryFailure("operation", request, "RATE_LIMIT", time.Millisecond, true)
	if f == nil {
		t.Fatal("valid request did not sign classification")
	}
	request.Messages[0].Content = "changed"
	op, got, ok := f.OperationRequest()
	if !ok || op != "operation" || got.Messages[0].Content != "original private query" {
		t.Fatal("input mutation changed signed failure")
	}
	got.Messages[0].Content = "caller change"
	_, again, ok := f.OperationRequest()
	if !ok || again.Messages[0].Content != "original private query" {
		t.Fatal("getter exposed mutable failure")
	}
	if _, e := json.Marshal(f); !errors.Is(e, ErrServerOnly) {
		t.Fatal(e)
	}
	if _, _, ok = (LocalRetryFailure{}).OperationRequest(); ok {
		t.Fatal("empty failure accepted")
	}
	if newLocalRetryFailure("operation", modelgateway.Request{RunID: "original"}, "RATE_LIMIT", 0, false) != nil {
		t.Fatal("malformed request signed a failure classification")
	}
}
func TestModelEgressLocalRetryJitterUsesExistingBoundedPolicy(t *testing.T) {
	p := modelresilience.DefaultPolicy()
	r := NewLocalRetryRunner(&localRetryTestPort{}, localTestGate(t, true), p)
	for i := 0; i < 30; i++ {
		unit := r.jitter()
		if unit < 0 || unit > 1000 {
			t.Fatal(unit)
		}
	}
	r.jitter = func() int { return 500 }
	delay, e := modelresilience.RetryDelay(p, 1, 0, false, r.jitter())
	if e != nil || delay != 275*time.Millisecond {
		t.Fatal("existing bounded jitter arithmetic not reused", delay, e)
	}
}

func TestModelEgressLocalRetryControlObjectsCannotDecodeOrSerialize(t *testing.T) {
	if _, e := json.Marshal(LocalRetryOutcome{}); !errors.Is(e, ErrServerOnly) {
		t.Fatal(e)
	}
	var failure LocalRetryFailure
	if e := json.Unmarshal([]byte(`{"code":"RATE_LIMIT","operation":"forged"}`), &failure); !errors.Is(e, ErrServerOnly) {
		t.Fatal(e)
	}
}
