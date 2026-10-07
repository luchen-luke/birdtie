package agentcognitive

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/agentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
)

// KnowledgeView/CandidateSubmission are bounded references for future ports,
// not new Memory entities or a second truth store. Sources must be resolved from
// current AGE/domain owners. A candidate is neither approved nor committed.
type KnowledgeView struct {
	Agent   AgentReference
	Scope   agentruntime.Scope
	Sources []SourceReference
}

type CandidateSubmission struct {
	Request       ReadRequest
	Sources       []SourceReference
	PayloadDigest string // Review binding; never an operation/effect identifier.
}

type CandidateReceipt struct {
	Status Availability
	Reason string
}

type MemoryReader interface {
	ReadMemory(context.Context, ReadRequest) (KnowledgeView, error)
}

type CandidateSubmitter interface {
	SubmitMemoryCandidate(context.Context, CandidateSubmission) (CandidateReceipt, error)
}

// UnavailableCognitivePorts is an explicit failure implementation, not a Memory
// adapter. No fake records, candidate writes, approved receipts or effects are
// returned. No HTTP handler invokes these ports in Phase 0.
type UnavailableCognitivePorts struct{}

var _ MemoryReader = UnavailableCognitivePorts{}
var _ CandidateSubmitter = UnavailableCognitivePorts{}

func (UnavailableCognitivePorts) ReadMemory(ctx context.Context, request ReadRequest) (KnowledgeView, error) {
	err := agentpurpose.Check(ctx, agentpurpose.Request{Origin: agentpurpose.OriginUnknown, Operation: agentpurpose.Read, Retention: agentpurpose.Transient, Recipient: request.Agent.Principal.Type})
	return KnowledgeView{}, errors.Join(ErrUnavailable, err)
}

func (UnavailableCognitivePorts) SubmitMemoryCandidate(ctx context.Context, request CandidateSubmission) (CandidateReceipt, error) {
	// Even staging is durable. The current reference cannot prove source purpose
	// or independent retention permission; never infer it from MemoryEvidence.
	err := agentpurpose.Check(ctx, agentpurpose.Request{Origin: agentpurpose.OriginUnknown, Operation: agentpurpose.StageMemory, Retention: agentpurpose.Persistent, Recipient: request.Request.Agent.Principal.Type})
	reason := "memory_candidate_port_unavailable"
	if errors.Is(err, agentpurpose.ErrProhibited) {
		reason = "memory_purpose_retention_prohibited"
	}
	return CandidateReceipt{Unavailable, reason}, errors.Join(ErrUnavailable, err)
}
