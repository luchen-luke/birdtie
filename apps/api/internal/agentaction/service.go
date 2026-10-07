package agentaction

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"reflect"
)

// Service has no model/provider caller and cannot approve using a JSON flag.
// Confirm is an explicit authenticated owner operation, separate from Commit.
type Service struct{ port Port }

func actionPortMissing(p any) bool {
	if p == nil {
		return true
	}
	v := reflect.ValueOf(p)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

func NewService(p Port) *Service {
	if actionPortMissing(p) {
		p = nil
	}
	return &Service{p}
}

func (s *Service) available(c context.Context) error {
	if c == nil {
		return ErrInvalid
	}
	if e := c.Err(); e != nil {
		return e
	}
	if s == nil || actionPortMissing(s.port) {
		return ErrUnavailable
	}
	return nil
}
func (s *Service) Preview(c context.Context, a agentevent.Access, g agentplanner.PreparedGoal, p agenttool.SandboxProposal, f *agentfeature.Controller) (Preview, error) {
	if e := s.available(c); e != nil {
		return Preview{}, e
	}
	return s.port.PreviewOwnSandboxAction(c, a, g, p, f)
}
func (s *Service) Confirm(c context.Context, a agentevent.Access, id, digest string, f *agentfeature.Controller) (Preview, error) {
	if e := s.available(c); e != nil {
		return Preview{}, e
	}
	return s.port.ApproveOwnSandboxAction(c, a, id, digest, f)
}
func (s *Service) Commit(c context.Context, a agentevent.Access, id string, p agenttool.SandboxProposal, f *agentfeature.Controller) (Dispatch, Commitment, error) {
	if e := s.available(c); e != nil {
		return Dispatch{}, nil, e
	}
	return s.port.CommitOwnSandboxAction(c, a, id, p, f)
}
func (s *Service) Reconcile(c context.Context, a agentevent.Access, id string, f *agentfeature.Controller) (Dispatch, error) {
	if e := s.available(c); e != nil {
		return Dispatch{}, e
	}
	return s.port.ReconcileOwnSandboxDispatch(c, a, id, f)
}
