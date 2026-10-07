package agentcognitive

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentpurpose"
)

func TestPurposeLimitationActualPortsAndGatedWrapper(t *testing.T) {
	for _, recipient := range []actorref.Type{actorref.Person, actorref.Organization, actorref.Business} {
		request := ReadRequest{Agent: AgentReference{Principal: actorref.PrincipalRef{Type: recipient}}}
		for _, wrapper := range []bool{false, true} {
			t.Run(string(recipient)+map[bool]string{false: "/port", true: "/wrapper_ON"}[wrapper], func(t *testing.T) {
				var reader MemoryReader = UnavailableCognitivePorts{}
				var submitter CandidateSubmitter = UnavailableCognitivePorts{}
				if wrapper {
					g, _, _, _ := gateFixture(t, agentfeature.Enrichment, agentfeature.Memory)
					reader, submitter = g, g
				}
				view, err := reader.ReadMemory(context.Background(), request)
				if !errors.Is(err, ErrUnavailable) || !errors.Is(err, agentpurpose.ErrUnavailable) || !reflect.DeepEqual(view, KnowledgeView{}) {
					t.Fatal("actual read did not apply missing provenance guard")
				}
				receipt, err := submitter.SubmitMemoryCandidate(context.Background(), CandidateSubmission{Request: request, PayloadDigest: "not-approval"})
				want, reason := agentpurpose.ErrUnavailable, "memory_candidate_port_unavailable"
				if recipient != actorref.Person {
					want, reason = agentpurpose.ErrProhibited, "memory_purpose_retention_prohibited"
				}
				if !errors.Is(err, ErrUnavailable) || !errors.Is(err, want) || receipt.Status != Unavailable || receipt.Reason != reason {
					t.Fatal("actual candidate boundary lost purpose classification")
				}
			})
		}
	}
}

func TestPurposeLimitationNilCancelledAndDisabledWrapper(t *testing.T) {
	g, controller, spy, access := gateFixture(t, agentfeature.Memory, agentfeature.Enrichment)
	for _, ctx := range []context.Context{nil, func() context.Context { c, x := context.WithCancel(context.Background()); x(); return c }()} {
		if view, err := g.ReadMemory(ctx, ReadRequest{}); err == nil || !reflect.DeepEqual(view, KnowledgeView{}) {
			t.Fatal("nil/cancel read survived")
		}
		if receipt, err := g.SubmitMemoryCandidate(ctx, CandidateSubmission{}); err == nil || receipt.Status != Unavailable {
			t.Fatal("nil/cancel candidate survived")
		}
		if profile, err := g.ReadOwnProfile(ctx, access); err == nil || !reflect.ValueOf(profile).IsZero() {
			t.Fatal("nil/cancel self read survived")
		}
		if items, err := g.ReadOwnContextDeclarations(ctx, access); err == nil || items != nil {
			t.Fatal("nil/cancel context read survived")
		}
	}
	if err := controller.Disable(agentfeature.Memory); err != nil {
		t.Fatal(err)
	}
	receipt, err := g.SubmitMemoryCandidate(context.Background(), CandidateSubmission{Request: ReadRequest{Agent: AgentReference{Principal: actorref.PrincipalRef{Type: actorref.Organization}}}})
	if !errors.Is(err, ErrUnavailable) || receipt.Reason != "memory_candidate_port_unavailable" || spy.authCalls+spy.profileCalls+spy.contextCalls != 0 {
		t.Fatal("feature OFF traversed source or retained stale purpose receipt")
	}
}
