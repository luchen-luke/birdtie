package agentcandidatepipeline

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"testing"
)

type fakeExecutor struct {
	calls, reads int
	receipt      Receipt
	err          error
	after        func()
}

func (f *fakeExecutor) StageOwnCandidatePipeline(_ context.Context, _ agentprofile.PrivateAccess, _ string, _ Handler, _ *agentfeature.Controller, _ agentfeature.Ticket, _ *agentcognitive.CandidateSubmission) (Receipt, error) {
	f.calls++
	if f.after != nil {
		f.after()
	}
	return f.receipt, f.err
}
func (f *fakeExecutor) ReadOwnCandidatePipeline(context.Context, agentprofile.PrivateAccess, string) (Receipt, error) {
	f.reads++
	return f.receipt, f.err
}
func accessFixture() agentprofile.PrivateAccess {
	return agentprofile.PrivateAccess{WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: ownID}, SessionDigest: [32]byte{1}}
}
func featureFixture(t *testing.T, on bool) *agentfeature.Controller {
	t.Helper()
	cfg := agentfeature.DefaultConfig()
	if on {
		var e error
		cfg, e = agentfeature.ParseConfig([]byte(`{"schemaVersion":"agent-feature-flags-v1","flags":{"agent_enrichment":true,"agent_memory":true,"agent_attention_policy":false,"agent_social_policy":false,"life_map":false},"pilot":{"memory":"basic","inference":"conservative","autonomousAction":false,"sensitiveInference":false}}`))
		if e != nil {
			t.Fatal(e)
		}
	}
	c, e := agentfeature.NewController(cfg)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestCandidatePipelineDefaultOffAndNilPorts(t *testing.T) {
	f := &fakeExecutor{receipt: receiptFixture()}
	for _, s := range []*Service{nil, NewService(nil, nil), NewService(f, featureFixture(t, false)), NewServiceWithHandler(f, featureFixture(t, true), "client-handler")} {
		if _, e := s.StageOwnMomentCandidate(context.Background(), accessFixture(), grantID); !errors.Is(e, ErrUnavailable) {
			t.Fatal(e)
		}
	}
	if f.calls != 0 {
		t.Fatal("OFF reached writer")
	}
}
func TestCandidatePipelineServiceCurrentControllerAndReceipt(t *testing.T) {
	c := featureFixture(t, true)
	f := &fakeExecutor{receipt: receiptFixture()}
	s := NewService(f, c)
	r, e := s.StageOwnMomentCandidate(context.Background(), accessFixture(), grantID)
	if e != nil || !r.Committed || f.calls != 1 {
		t.Fatal(r, e)
	}
	f.after = func() { c.Disable(agentfeature.Memory) }
	if r, e = s.StageOwnMomentCandidate(context.Background(), accessFixture(), grantID); !errors.Is(e, ErrUnavailable) || r.Committed {
		t.Fatal("late controller", r, e)
	}
	// Receipt recovery is read-only even while the write rollout brake is OFF.
	f.after = nil
	if _, e = s.ReadOwnMomentCandidateReceipt(context.Background(), accessFixture(), grantID); e != nil || f.reads != 1 {
		t.Fatal(e)
	}
}
func TestCandidatePipelineServiceNeverAcceptsPortWrongOwnerOrFalseSuccess(t *testing.T) {
	for _, which := range []string{"owner", "grant", "false", "model"} {
		t.Run(which, func(t *testing.T) {
			r := receiptFixture()
			switch which {
			case "owner":
				r.Owner.ID = grantID
			case "grant":
				r.RetentionGrantID = ownID
			case "false":
				r.Committed = false
			case "model":
				r.ModelAccess = true
			}
			f := &fakeExecutor{receipt: r}
			s := NewService(f, featureFixture(t, true))
			if out, e := s.StageOwnMomentCandidate(context.Background(), accessFixture(), grantID); !errors.Is(e, ErrUnavailable) || out.Committed {
				t.Fatal(out, e)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fakeExecutor{}
	s := NewService(f, featureFixture(t, true))
	if _, e := s.StageOwnMomentCandidate(ctx, accessFixture(), grantID); !errors.Is(e, context.Canceled) || f.calls != 0 {
		t.Fatal(e)
	}
	if _, e := s.StageOwnMomentCandidate(context.Background(), accessFixture(), "notUUID"); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
}
