package agentaction

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
)

type actionRecoveryBoundaryPort struct {
	Port
	calls int
}

const recoveryApprovalID = "11111111-1111-4111-8111-111111111111"

type actionRecoveryReceiptPort struct {
	Port
	calls          int
	lastID         string
	lastAccess     agentevent.Access
	lastController *agentfeature.Controller
	dispatch       Dispatch
	err            error
}

type actionRecoveryForwardPort struct {
	Port
	access     agentevent.Access
	controller *agentfeature.Controller
	proposal   agenttool.SandboxProposal
	id, digest string
	calls      int
}

func (p *actionRecoveryForwardPort) PreviewOwnSandboxAction(_ context.Context, a agentevent.Access, _ agentplanner.PreparedGoal, q agenttool.SandboxProposal, f *agentfeature.Controller) (Preview, error) {
	p.calls++
	p.access, p.proposal, p.controller = a, q, f
	return Preview{State: Pending}, nil
}
func (p *actionRecoveryForwardPort) ApproveOwnSandboxAction(_ context.Context, a agentevent.Access, id, digest string, f *agentfeature.Controller) (Preview, error) {
	p.calls++
	p.access, p.id, p.digest, p.controller = a, id, digest, f
	return Preview{State: Approved}, nil
}
func (p *actionRecoveryForwardPort) CommitOwnSandboxAction(_ context.Context, a agentevent.Access, id string, q agenttool.SandboxProposal, f *agentfeature.Controller) (Dispatch, Commitment, error) {
	p.calls++
	p.access, p.id, p.proposal, p.controller = a, id, q, f
	return Dispatch{State: Committed}, nil, nil
}
func (p *actionRecoveryForwardPort) ReconcileOwnSandboxDispatch(_ context.Context, a agentevent.Access, id string, f *agentfeature.Controller) (Dispatch, error) {
	p.calls++
	p.access, p.id, p.controller = a, id, f
	return Dispatch{State: Unknown}, nil
}

func TestActionRecoveryServiceOriginalCallsRemainSeparate(t *testing.T) {
	// Each spy supplies an explicit native-port response. The Service does not
	// turn a preview into approval, a committed receipt into success or an
	// unknown receipt into a new call. No fixture is a production authority.
	a := zeroAccess()
	q := agenttool.SandboxProposal{ActionID: recoveryApprovalID, Value: "private fixture"}
	for _, name := range []string{"preview", "confirm", "commit", "reconcile"} {
		t.Run(name, func(t *testing.T) {
			p := &actionRecoveryForwardPort{}
			s := NewService(p)
			c := context.Background()
			switch name {
			case "preview":
				v, e := s.Preview(c, a, nil, q, nil)
				if e != nil || v.State != Pending || p.proposal != q {
					t.Fatal(e, v)
				}
			case "confirm":
				v, e := s.Confirm(c, a, recoveryApprovalID, "exact-inspected-digest", nil)
				if e != nil || v.State != Approved || p.id != recoveryApprovalID || p.digest != "exact-inspected-digest" {
					t.Fatal(e, v)
				}
			case "commit":
				v, h, e := s.Commit(c, a, recoveryApprovalID, q, nil)
				if e != nil || h != nil || v.State != Committed || p.id != recoveryApprovalID || p.proposal != q {
					t.Fatal(e, v)
				}
			case "reconcile":
				v, e := s.Reconcile(c, a, recoveryApprovalID, nil)
				if e != nil || v.State != Unknown || p.id != recoveryApprovalID {
					t.Fatal(e, v)
				}
			}
			if p.calls != 1 || p.access != a || p.controller != nil {
				t.Fatal("port input/call count changed", p.calls)
			}
		})
	}
}

func (p *actionRecoveryReceiptPort) ReconcileOwnSandboxApproval(_ context.Context, a agentevent.Access, id string, c *agentfeature.Controller) (Dispatch, error) {
	p.calls++
	p.lastID, p.lastAccess, p.lastController = id, a, c
	return p.dispatch, p.err
}

func TestActionRecoveryServiceBoundaryAppliesToEveryOperation(t *testing.T) {
	methods := []struct {
		name string
		call func(*Service, context.Context) error
	}{
		{"preview", func(s *Service, c context.Context) error {
			_, e := s.Preview(c, zeroAccess(), agentplanner.PreparedGoal(nil), agenttool.SandboxProposal{}, nil)
			return e
		}},
		{"confirm", func(s *Service, c context.Context) error {
			_, e := s.Confirm(c, zeroAccess(), recoveryApprovalID, "not-authority", nil)
			return e
		}},
		{"commit", func(s *Service, c context.Context) error {
			_, h, e := s.Commit(c, zeroAccess(), recoveryApprovalID, agenttool.SandboxProposal{}, nil)
			if h != nil {
				t.Fatal("unexpected capability")
			}
			return e
		}},
		{"reconcile", func(s *Service, c context.Context) error {
			_, e := s.Reconcile(c, zeroAccess(), recoveryApprovalID, nil)
			return e
		}},
		{"recover", func(s *Service, c context.Context) error {
			_, e := s.RecoverApproval(c, zeroAccess(), recoveryApprovalID, nil)
			return e
		}},
	}
	for _, method := range methods {
		t.Run(method.name, func(t *testing.T) {
			if e := method.call(nil, context.Background()); !errors.Is(e, ErrUnavailable) {
				t.Fatal("nil service", e)
			}
			if e := method.call(NewService(&actionRecoveryReceiptPort{}), nil); !errors.Is(e, ErrInvalid) {
				t.Fatal("nil context", e)
			}
			c, cancel := context.WithCancel(context.Background())
			cancel()
			if e := method.call(NewService(&actionRecoveryReceiptPort{}), c); !errors.Is(e, context.Canceled) {
				t.Fatal("cancelled", e)
			}
		})
	}
}

func TestActionRecoveryServiceMissingPortAndInvalidAddressNeverCommit(t *testing.T) {
	if _, e := NewService(&actionRecoveryBoundaryPort{}).RecoverApproval(context.Background(), zeroAccess(), recoveryApprovalID, nil); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	for _, id := range []string{"", "confirmed", "../dispatch", "https://example.invalid/receipt", "11111111-1111-4111-8111-111111111111 ", "model.approved"} {
		t.Run(id, func(t *testing.T) {
			p := &actionRecoveryReceiptPort{}
			d, e := NewService(p).RecoverApproval(context.Background(), zeroAccess(), id, nil)
			if !errors.Is(e, ErrInvalid) || p.calls != 0 || d != (Dispatch{}) {
				t.Fatal("untrusted address reached recovery", e, p.calls, d)
			}
		})
	}
}

func TestActionRecoveryServiceLostResponseReadsSameReceiptWithoutResending(t *testing.T) {
	// Port spy models a persisted result after a lost reply. It is not a real
	// database/provider result and supplies no execution or approval capability.
	effect := "22222222-2222-4222-8222-222222222222"
	at := time.Date(2026, 10, 6, 1, 2, 3, 0, time.UTC)
	p := &actionRecoveryReceiptPort{dispatch: Dispatch{Schema: Schema, ID: effect, ApprovalID: recoveryApprovalID, TenantID: effect, EffectKey: "original_stable_effect_key", State: Succeeded, CommittedAt: at, EffectID: &effect, AppliedAt: &at}}
	s := NewService(p)
	a := zeroAccess()
	first, e := s.RecoverApproval(context.Background(), a, recoveryApprovalID, nil)
	if e != nil || first != p.dispatch {
		t.Fatal(e, first)
	}
	second, e := s.RecoverApproval(context.Background(), a, recoveryApprovalID, nil)
	if e != nil || second != first || p.calls != 2 || p.lastID != recoveryApprovalID || p.lastAccess != a || p.lastController != nil {
		t.Fatal("receipt/source changed or not forwarded", e, p.calls)
	}
	// All unimplemented embedded approval/dispatch methods would panic if
	// recovery attempted to create a second effect.
}

func TestActionRecoveryServiceUnknownDeniedAndChangedStayDistinct(t *testing.T) {
	for _, err := range []error{ErrUnknown, ErrDenied, ErrChanged, ErrExpired, ErrUnavailable, context.DeadlineExceeded} {
		t.Run(err.Error(), func(t *testing.T) {
			p := &actionRecoveryReceiptPort{err: err}
			d, e := NewService(p).RecoverApproval(context.Background(), zeroAccess(), recoveryApprovalID, nil)
			if !errors.Is(e, err) || d != (Dispatch{}) || p.calls != 1 {
				t.Fatal("error became success or was retried", e, d, p.calls)
			}
		})
	}
}

func (p *actionRecoveryBoundaryPort) ReconcileOwnSandboxDispatch(context.Context, agentevent.Access, string, *agentfeature.Controller) (Dispatch, error) {
	if p == nil {
		panic("typed nil port was called")
	}
	p.calls++
	return Dispatch{State: Succeeded}, nil
}

func TestActionRecoveryServiceTypedNilPortUnavailable(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("service called typed nil port: %v", r)
		}
	}()
	var p *actionRecoveryBoundaryPort
	d, e := NewService(p).Reconcile(context.Background(), zeroAccess(), "x", nil)
	if !errors.Is(e, ErrUnavailable) || d != (Dispatch{}) {
		t.Fatalf("typed nil must fail without an effect: %v, %#v", e, d)
	}
}

func TestActionRecoveryServiceNilContextNoRead(t *testing.T) {
	p := &actionRecoveryBoundaryPort{}
	d, e := NewService(p).Reconcile(nil, zeroAccess(), "x", nil)
	if !errors.Is(e, ErrInvalid) || p.calls != 0 || d != (Dispatch{}) {
		t.Fatalf("nil context reached recovery port: error=%v calls=%d result=%#v", e, p.calls, d)
	}
}

func TestActionRecoveryServiceCancelledContextNoRead(t *testing.T) {
	p := &actionRecoveryBoundaryPort{}
	c, cancel := context.WithCancel(context.Background())
	cancel()
	d, e := NewService(p).Reconcile(c, zeroAccess(), "x", nil)
	if !errors.Is(e, context.Canceled) || p.calls != 0 || d != (Dispatch{}) {
		t.Fatalf("cancelled context reached recovery port: error=%v calls=%d result=%#v", e, p.calls, d)
	}
}
