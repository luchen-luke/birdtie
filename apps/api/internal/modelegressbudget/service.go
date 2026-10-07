package modelegressbudget

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

// The persistent local accounting/owner approval does not install an approved
// provider, tokenizer, paid tariff, service secret or AgentRun execution path.
type Service struct{ gateway *modelgateway.Gateway }

func NewService(gate modelgateway.LiveGate) *Service {
	return &Service{gateway: modelgateway.NewGateway(gate)}
}
func (s *Service) Complete(ctx context.Context, r modelgateway.Request) (modelgateway.Result, error) {
	if s == nil || s.gateway == nil {
		return modelgateway.NewGateway(nil).Complete(ctx, r)
	}
	return s.gateway.Complete(ctx, r)
}

var _ modelgateway.ModelGateway = (*Service)(nil)
