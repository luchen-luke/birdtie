// Package agentmemoryreview is a bounded human self-management projection.
// A page/cursor is neither an analysis permission nor a Memory write grant.
package agentmemoryreview

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"time"
)

const Schema = "agent-memory-review-page-v1"
const PageSize = 20
const MaxCursor = 1024
const Lease = 2 * time.Minute
const Memories = "MEMORIES"
const Candidates = "CANDIDATES"

type Page[T any] struct {
	Schema      string                `json:"schema"`
	Collection  string                `json:"collection"`
	Owner       actorref.PrincipalRef `json:"owner"`
	AgentID     string                `json:"agentId"`
	Items       []T                   `json:"items"`
	NextCursor  *string               `json:"nextCursor"`
	ObservedAt  time.Time             `json:"observedAt"`
	ExpiresAt   time.Time             `json:"expiresAt"`
	ModelAccess bool                  `json:"modelAccess"`
}
type MemoryPage = Page[agentmemory.Record]
type CandidatePage = Page[agentmemorycandidate.Record]
type MemoryPager interface {
	ReadOwnMemoryPage(context.Context, agentprofile.PrivateAccess, string) (MemoryPage, error)
}
type CandidatePager interface {
	ReadOwnCandidatePage(context.Context, agentprofile.PrivateAccess, string) (CandidatePage, error)
}

func CursorShape(v string) bool {
	if len(v) == 0 || len(v) > MaxCursor {
		return false
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func Validate[T any](p Page[T], owner, collection string, now time.Time) error {
	id, e := agentmemory.NormalizeMemoryID(p.AgentID)
	if p.Schema != Schema || p.Collection != collection || p.Owner.Type != actorref.Person || p.Owner.ID != owner || e != nil || id != p.AgentID || p.Items == nil || len(p.Items) > PageSize || p.ModelAccess ||
		p.ObservedAt.IsZero() || p.ExpiresAt.IsZero() || p.ObservedAt.After(now) || !p.ExpiresAt.After(now) || p.ExpiresAt.After(p.ObservedAt.Add(Lease)) || p.NextCursor != nil && (!CursorShape(*p.NextCursor) || len(p.Items) != PageSize) {
		return agentmemory.ErrInvalid
	}
	return nil
}
