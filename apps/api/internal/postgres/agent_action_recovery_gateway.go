package postgres

import (
	"context"

	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

type humanSandboxRecovery struct {
	service *aa.Service
	flags   *agentfeature.Controller
}

// Borrow the SAME startup Controller. This adapter neither creates/enables a
// controller nor exposes any of the Service's approval/dispatch methods.
func NewHumanSandboxRecovery(s *Store, c *agentfeature.Controller) aa.HumanRecoveryGateway {
	return &humanSandboxRecovery{service: aa.NewService(s), flags: c}
}

func (g *humanSandboxRecovery) RecoverOwn(ctx context.Context, a agentprofile.PrivateAccess, approvalID string) (aa.HumanRecoveryReceipt, error) {
	if ctx == nil || ctx.Err() != nil || g == nil || g.service == nil {
		return aa.HumanRecoveryReceipt{}, aa.ErrUnavailable
	}
	if agentprofile.ValidatePrivateAccess(a) != nil {
		return aa.HumanRecoveryReceipt{}, aa.ErrDenied
	}
	if !agentplanner.ValidID(approvalID) {
		return aa.HumanRecoveryReceipt{}, aa.ErrInvalid
	}
	ticket, e := g.flags.Capture(agentfeature.Enrichment)
	if e != nil {
		return aa.HumanRecoveryReceipt{}, aa.ErrUnavailable
	}
	d, e := g.service.RecoverApproval(ctx, agentevent.Access{SessionDigest: a.SessionDigest}, approvalID, g.flags)
	if e != nil {
		return aa.HumanRecoveryReceipt{}, e
	}
	if ctx.Err() != nil || !g.flags.Current(ticket) {
		return aa.HumanRecoveryReceipt{}, aa.ErrUnknown
	}
	return aa.NewHumanRecoveryReceipt(d, a.WorkspacePrincipal, approvalID)
}
