package httpapi

import (
	"context"
	"errors"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
)

// Reuses the original HTTP encoded-buffer final revalidation. This wrapper is
// constructed only from the server's current native read, never a wire grant.
type currentToolSearchResponseStore struct {
	port    agenttool.CurrentSearchPort
	input   agenttool.CurrentSearch
	receipt agenttool.CurrentSearchReceipt
}

func (*currentToolSearchResponseStore) ReadAgentResultProjection(context.Context, arp.Access, arp.Query) (arp.Receipt, error) {
	return arp.Receipt{}, arp.ErrDenied
}
func (s *currentToolSearchResponseStore) RevalidateAgentResultProjection(ctx context.Context, a arp.Access, q arp.Query, r arp.Receipt) error {
	if s == nil || agenttool.CurrentSearchDigest(agenttool.CurrentSearch{Access: a, Query: q}) != agenttool.CurrentSearchDigest(s.input) || r.Seal != s.receipt.Source.Seal {
		return arp.ErrDenied
	}
	return currentToolReadError(s.port.RevalidateOwnCurrentSearch(ctx, s.input, s.receipt))
}
func currentToolReadError(e error) error {
	switch {
	case errors.Is(e, agenttool.ErrInvalid), errors.Is(e, newpeople.ErrInvalid):
		return arp.ErrInvalid
	case errors.Is(e, agenttool.ErrDenied), errors.Is(e, newpeople.ErrForbidden):
		return arp.ErrDenied
	case errors.Is(e, agenttool.ErrChanged), errors.Is(e, agenttool.ErrLimit), errors.Is(e, newpeople.ErrChanged):
		return arp.ErrChanged
	case errors.Is(e, agenttool.ErrUnavailable):
		return arp.ErrUnavailable
	case errors.Is(e, newpeople.ErrNotFound):
		return agentworkspace.ErrNotFound
	}
	return e
}
