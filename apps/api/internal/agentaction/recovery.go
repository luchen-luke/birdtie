package agentaction

import (
	"context"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
)

// ApprovalRecoveryPort only finds an existing dispatch and reconciles its
// original private sandbox effect. It neither creates nor returns a sending
// capability. The native port must check the current owner and session even
// when the original approval has expired or its source has since changed.
// External tools cannot use sandbox absence to infer that nothing happened.
type ApprovalRecoveryPort interface {
	ReconcileOwnSandboxApproval(context.Context, agentevent.Access, string, *agentfeature.Controller) (Dispatch, error)
}

// RecoverApproval uses the stable approval address already shown to its owner
// before Commit. A lost commit response may not contain the new dispatch ID.
// Recovery never calls Confirm, Commit, Begin or Execute, including on errors.
func (s *Service) RecoverApproval(c context.Context, a agentevent.Access, approvalID string, f *agentfeature.Controller) (Dispatch, error) {
	if e := s.available(c); e != nil {
		return Dispatch{}, e
	}
	if !agentplanner.ValidID(approvalID) {
		return Dispatch{}, ErrInvalid
	}
	p, ok := s.port.(ApprovalRecoveryPort)
	if !ok || actionPortMissing(p) {
		return Dispatch{}, ErrUnavailable
	}
	return p.ReconcileOwnSandboxApproval(c, a, approvalID, f)
}
