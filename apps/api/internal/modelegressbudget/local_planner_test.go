package modelegressbudget

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/modelrequestrun"
	"testing"
	"time"
)

type localPlannerTestPort struct {
	*localRunTestPort
	prepares int
}

func (p *localPlannerTestPort) PrepareOwnReadonlyPlan(context.Context, agentevent.Access, string, string) (agentplanner.PreparedGoal, error) {
	p.prepares++
	return nil, agentplanner.ErrUnavailable
}
func (p *localPlannerTestPort) CreateOwnLocalPlannerRun(context.Context, agentevent.Access, string, []LocalRetryBinding, *agentfeature.Controller, agentfeature.Ticket, agentplanner.PreparedGoal) (LocalModelRunHandle, modelrequestrun.Control, error) {
	p.creates++
	return nil, modelrequestrun.Control{}, ErrUnavailable
}
func TestBoundedPlannerOriginalRunnerHardCapsAndMissingPort(t *testing.T) {
	gate := localTestGate(t, false)
	p := &localPlannerTestPort{localRunTestPort: &localRunTestPort{}}
	policy := DefaultPlannerPolicy()
	var absent *localPlannerTestPort
	if NewLocalPlannerRunner(absent, gate, policy) != nil || NewLocalPlannerRunner(nil, gate, policy) != nil {
		t.Fatal("missing native port accepted")
	}
	for _, change := range []func(){func() { policy.MaxAttempts = 3 }, func() { policy = DefaultPlannerPolicy(); policy.MaxElapsed = 31 * time.Second }} {
		change()
		if NewLocalPlannerRunner(p, gate, policy) != nil {
			t.Fatal("planner hard cap relaxed")
		}
	}
	r := NewLocalPlannerRunner(p, gate, DefaultPlannerPolicy())
	id := "80000000-0000-4000-8000-000000000001"
	if _, e := r.Run(context.Background(), agentevent.Access{}, id, id, id, make([]LocalRetryStep, 3)); !errors.Is(e, agentplanner.ErrLimit) || p.prepares != 0 || p.creates != 0 {
		t.Fatal("overlimit prepared/reserved", e)
	}
	if _, e := r.Run(context.Background(), agentevent.Access{}, id, id, id, nil); !errors.Is(e, agentplanner.ErrUnavailable) || p.prepares != 1 || p.creates != 0 {
		t.Fatal("OFF fallback invoked model", e)
	}
}
