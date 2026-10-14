package agentmemoryreview

import (
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"strings"
	"testing"
	"time"
)

func TestMemoryReviewPageBoundedPrivateEnvelope(t *testing.T) {
	now := time.Now().UTC()
	owner := "23000000-0000-4000-8000-000000000001"
	p := Page[string]{Schema: Schema, Collection: Memories, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: owner}, AgentID: "23000000-0000-4000-8000-000000000002", Items: []string{}, ObservedAt: now, ExpiresAt: now.Add(Lease)}
	if Validate(p, owner, Memories, now) != nil {
		t.Fatal("valid human page denied")
	}
	for _, mutate := range []func(*Page[string]){func(p *Page[string]) { p.ModelAccess = true }, func(p *Page[string]) { p.Items = make([]string, 21) }, func(p *Page[string]) { p.Collection = Candidates }, func(p *Page[string]) { p.ExpiresAt = now.Add(Lease + time.Second) }, func(p *Page[string]) { v := "safe_cursor"; p.NextCursor = &v }} {
		bad := p
		mutate(&bad)
		if Validate(bad, owner, Memories, now) == nil {
			t.Fatal("invalid page accepted")
		}
	}
	for _, v := range []string{"", strings.Repeat("a", 1025), "https://example.invalid", "a=b", "a+b"} {
		if CursorShape(v) {
			t.Fatal("invalid cursor shape")
		}
	}
}
