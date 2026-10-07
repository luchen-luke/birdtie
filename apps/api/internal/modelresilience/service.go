package modelresilience

import (
	"context"

	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

// Service deliberately has no provider, fake grant, clock or billing injection
// point. A rollout gate cannot enable missing source/egress/spend authority.
type Service struct{ gateway *modelgateway.Gateway }

var _ modelgateway.ModelGateway = (*Service)(nil)

func NewService(gate modelgateway.LiveGate) *Service {
	return &Service{gateway: modelgateway.NewGateway(gate)}
}

func (s *Service) Complete(ctx context.Context, r modelgateway.Request) (modelgateway.Result, error) {
	if s == nil || s.gateway == nil {
		return modelgateway.NewGateway(nil).Complete(ctx, r)
	}
	return s.gateway.Complete(ctx, r)
}
