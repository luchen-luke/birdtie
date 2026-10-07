package agentcontextrelevance

import (
	"context"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

type Service struct{ builder *acb.Service }

func NewService(store acb.Store) (*Service, error) {
	b, e := acb.NewService(store)
	if e != nil {
		return nil, e
	}
	return &Service{builder: b}, nil
}
func (s *Service) Retrieve(ctx context.Context, r acb.Request) (Result, error) {
	if s == nil || s.builder == nil {
		return Result{}, acb.ErrUnavailable
	}
	if r.Mode != acb.MachineTaskContext || r.Selection != acb.ExactTaskContext {
		return Result{}, acb.ErrDenied
	}
	b, e := s.builder.Build(ctx, r)
	if e != nil {
		return Result{}, e
	}
	v, e := project(b.Bundle)
	if e != nil {
		return Result{}, e
	}
	if _, e = s.builder.RevalidateOwn(ctx, r.Access, b); e != nil {
		return Result{}, e
	}
	return Result{service: s, original: b, view: v}, nil
}
func (s *Service) Revalidate(ctx context.Context, a agentprofile.PrivateAccess, r Result) error {
	if s == nil || s.builder == nil || r.service != s {
		return acb.ErrDenied
	}
	_, e := s.builder.RevalidateOwn(ctx, a, r.original)
	return e
}
