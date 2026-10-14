package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/jackc/pgx/v5"
)

var _ agentworkspace.EmptyPlaceReplyResultsPort = (*Store)(nil)
var errPlaceRefinementNonempty = errors.New("current native place refinement is not empty")

func requireEmptyPlaceRefinementSource(q arp.Query, source arp.Receipt) error {
	if q.Kind != "place" || !q.Valid() || !source.Valid() {
		return arp.ErrDenied
	}
	if len(source.Items) != 0 || len(source.Places) != 0 || len(source.Activities) != 0 || len(source.PublicCommercialRefs) != 0 {
		return errPlaceRefinementNonempty
	}
	return nil
}

func (s *Store) TryOwnEmptyPlaceRefinementReply(ctx context.Context, a arp.Access) (agentworkspace.Task, agentworkspace.EmptyPlaceReplyRead, bool, error) {
	if ctx == nil || !a.Valid() {
		return agentworkspace.Task{}, nil, false, arp.ErrDenied
	}
	var expected agentworkspace.Task
	if json.Unmarshal(a.ExpectedTask, &expected) != nil || expected.ID != a.TaskID || expected.PrincipalID != a.Actor.ID || expected.ActingUserID != a.Actor.ID {
		return agentworkspace.Task{}, nil, false, arp.ErrDenied
	}
	// ExpectedTask is the original sanitized CAS snapshot: internal reason and
	// old selection keys are deliberately absent. This fast shape check grants
	// nothing; captureOwnHumanReply checks the actual persisted reason/selection
	// only after original ownership, session/policy and exact Task CAS.
	if expected.Filters == nil {
		return expected, nil, false, nil
	}
	expected.Filters["reason"] = agentworkspace.ExplicitPlaceRefinement
	if !agentworkspace.IsExplicitPlaceRefinementTask(expected) {
		return expected, nil, false, nil
	}
	t, read, e := s.captureOwnHumanReply(ctx, a, true)
	if errors.Is(e, errPlaceRefinementNonempty) {
		return expected, nil, false, nil
	}
	return t, read, e == nil, e
}

// Reacquire the SAME current query after the original Task writer, inside its
// transaction. Historical empty refs alone cannot detect newly matching rows.
func (s *Store) readEmptyPlaceRefinementTx(ctx context.Context, tx pgx.Tx, a arp.Access, t agentworkspace.Task, q arp.Query, p *nativeToolPolicy) (*nativeEmptyPlaceReplyRead, error) {
	raw, e := json.Marshal(agentworkspace.SanitizeTaskForResponse(t))
	if e != nil {
		return nil, arp.ErrUnavailable
	}
	a.ExpectedTask = raw
	input := agenttool.CurrentSearch{Access: a, Query: q}
	receipt, e := s.readOwnCurrentSearchTx(ctx, tx, input, p)
	if e != nil {
		return nil, e
	}
	if e = requireEmptyPlaceRefinementSource(q, receipt.Source); e != nil {
		return nil, arp.ErrChanged
	}
	state := &nativeEmptyPlaceReplyState{port: s, query: input, receipt: receipt}
	return &nativeEmptyPlaceReplyRead{read: func() *nativeEmptyPlaceReplyState { return state }}, nil
}

type nativeEmptyPlaceReplyState struct {
	port    agenttool.CurrentSearchPort
	query   agenttool.CurrentSearch
	receipt agenttool.CurrentSearchReceipt
}
type nativeEmptyPlaceReplyRead struct {
	read func() *nativeEmptyPlaceReplyState
}

func (*nativeEmptyPlaceReplyRead) MarshalJSON() ([]byte, error) { return nil, agenttool.ErrServerOnly }
func (h *nativeEmptyPlaceReplyRead) UnmarshalJSON([]byte) error {
	if h != nil {
		*h = nativeEmptyPlaceReplyRead{}
	}
	return agenttool.ErrServerOnly
}
func (*nativeEmptyPlaceReplyRead) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("native empty place search read"))
}
func (h *nativeEmptyPlaceReplyRead) Revalidate(ctx context.Context) error {
	if ctx == nil || h == nil || h.read == nil {
		return arp.ErrDenied
	}
	s := h.read()
	if s == nil || s.port == nil || !s.receipt.Valid(s.query) || requireEmptyPlaceRefinementSource(s.query.Query, s.receipt.Source) != nil {
		return arp.ErrDenied
	}
	// Original native seal/task/session/policy/source and fixed lease remain the
	// final fence. No new receipt renews the captured response's lease.
	return s.port.RevalidateOwnCurrentSearch(ctx, s.query, s.receipt)
}
