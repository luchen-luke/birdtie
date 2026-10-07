package agenttool

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"reflect"
)

type Service struct{ plan Plan }

func missing(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	if r.Kind() == reflect.Pointer || r.Kind() == reflect.Interface {
		return r.IsNil()
	}
	return false
}
func NewService(plan Plan) *Service {
	if missing(plan) {
		plan = nil
	}
	return &Service{plan}
}
func (s *Service) Check(ctx context.Context, a agentevent.Access, p agentplanner.ActionProposal, c *agentfeature.Controller) (Decision, Call, error) {
	if ctx == nil {
		return Decision{}, nil, ErrInvalid
	}
	if e := ctx.Err(); e != nil {
		return Decision{}, nil, e
	}
	if s == nil || s.plan == nil {
		return Decision{}, nil, ErrUnavailable
	}
	return s.plan.Check(ctx, a, p, c)
}
func (s *Service) Read(ctx context.Context, a agentevent.Access, p agentplanner.ActionProposal, c *agentfeature.Controller) (Decision, Result, error) {
	d, call, e := s.Check(ctx, a, p, c)
	if e != nil {
		return d, Result{}, e
	}
	if d.Disposition != Allow || missing(call) {
		return d, Result{}, ErrDenied
	}
	result, e := call.Read(ctx, a, c)
	if e != nil {
		return d, Result{}, e
	}
	return Clone(d), result, nil
}
