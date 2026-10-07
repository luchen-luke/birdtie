package modelegressbudget

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/modelrequestrun"
	"github.com/birdtie/birdtie/apps/api/internal/modelresilience"
	"testing"
)

type localRunTestPort struct {
	LocalRetryPort
	creates int
}

func (p *localRunTestPort) CreateOwnLocalModelRun(context.Context, agentevent.Access, string, []LocalRetryBinding, *agentfeature.Controller, agentfeature.Ticket) (LocalModelRunHandle, modelrequestrun.Control, error) {
	p.creates++
	return nil, modelrequestrun.Control{}, ErrUnavailable
}
func (p *localRunTestPort) ReadOwnLocalModelRun(context.Context, agentevent.Access, string) (modelrequestrun.Control, error) {
	return modelrequestrun.Control{}, ErrUnavailable
}
func (p *localRunTestPort) CancelOwnLocalModelRun(context.Context, agentevent.Access, string, int64) (modelrequestrun.Control, error) {
	return modelrequestrun.Control{}, ErrUnavailable
}
func TestModelRequestRunLocalMissingPortAndOFFNeverPlanOrCall(t *testing.T) {
	var missing *localRunTestPort
	gate := localTestGate(t, false)
	if NewLocalModelRunRunner(nil, gate, modelresilience.DefaultPolicy()) != nil || NewLocalModelRunRunner(missing, gate, modelresilience.DefaultPolicy()) != nil {
		t.Fatal("nil port accepted")
	}
	port := &localRunTestPort{}
	adapter := &localTestAdapter{}
	driver := NewLocalAttemptDriver(port, adapter, gate)
	runner := NewLocalModelRunRunner(port, gate, modelresilience.DefaultPolicy())
	_, e := runner.Run(context.Background(), agentevent.Access{}, "id", []LocalRetryStep{{Driver: driver, Input: ReserveInput{OperationID: "op"}}})
	if !errors.Is(e, ErrUnavailable) || port.creates != 0 || adapter.calls != 0 {
		t.Fatal("OFF created/attempted native run", e, port.creates, adapter.calls)
	}
}
