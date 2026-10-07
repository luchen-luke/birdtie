package agentplanner

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"reflect"
	"time"
)

// PreparedGoal is a process-local native association. A JSON View cannot be
// supplied in its place; the actual PostgreSQL ModelRun checks its own type.
type PreparedGoal interface {
	View() View
	NeedsModel() bool
	Remaining(time.Time) time.Duration
}
type TaskPort interface {
	PrepareOwnReadonlyPlan(context.Context, agentevent.Access, string, string) (PreparedGoal, error)
}
type CandidateSelection struct {
	ID                 string
	ExpectedVersion    int64
	LogicalOperationID string
}
type CandidatePort interface {
	PlanOwnCandidateReview(context.Context, agentprofile.PrivateAccess, CandidateSelection) (View, error)
}
type Service struct {
	tasks      TaskPort
	candidates CandidatePort
}

func missing(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Func, reflect.Chan, reflect.Slice:
		return r.IsNil()
	}
	return false
}
func NewService(tasks TaskPort, candidates CandidatePort) *Service {
	if missing(tasks) {
		tasks = nil
	}
	if missing(candidates) {
		candidates = nil
	}
	return &Service{tasks, candidates}
}
func (s *Service) Prepare(ctx context.Context, a agentevent.Access, task, operation string) (PreparedGoal, error) {
	if ctx == nil || !ValidID(task) || !ValidID(operation) {
		return nil, ErrInvalid
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if s == nil || s.tasks == nil {
		return nil, ErrUnavailable
	}
	return s.tasks.PrepareOwnReadonlyPlan(ctx, a, task, operation)
}
func (s *Service) Review(ctx context.Context, a agentprofile.PrivateAccess, input CandidateSelection) (View, error) {
	if ctx == nil || !ValidID(input.LogicalOperationID) || (input.ID == "" && input.ExpectedVersion != 0) || (input.ID != "" && (!ValidID(input.ID) || input.ExpectedVersion < 1)) {
		return View{}, ErrInvalid
	}
	if e := ctx.Err(); e != nil {
		return View{}, e
	}
	if s == nil || s.candidates == nil {
		return View{}, ErrUnavailable
	}
	c, cancel := context.WithTimeout(ctx, MaxElapsed)
	defer cancel()
	started := time.Now()
	v, e := s.candidates.PlanOwnCandidateReview(c, a, input)
	if e != nil {
		return View{}, e
	}
	if c.Err() != nil || !v.ValidElapsed(started, time.Now()) {
		return View{}, ErrChanged
	}
	return Clone(v), nil
}

type UnavailablePorts struct{}

func (UnavailablePorts) PrepareOwnReadonlyPlan(context.Context, agentevent.Access, string, string) (PreparedGoal, error) {
	return nil, ErrUnavailable
}
func (UnavailablePorts) PlanOwnCandidateReview(context.Context, agentprofile.PrivateAccess, CandidateSelection) (View, error) {
	return View{}, ErrUnavailable
}
