package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	review "github.com/birdtie/birdtie/apps/api/internal/agentmemoryreview"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"testing"
)

func TestMemoryReviewPageNativeBeyondFiftyAndSessionABA(t *testing.T) {
	ownedMigrationDatabase(t)
	f, service := memoryCandidateFixture(t)
	b := f.base.private.base
	ctx := b.ctx
	a := f.base.private.owner
	g := NewMemoryCandidateHumanGateway(b.store, service.flags).(review.CandidatePager)
	gateway := NewMemoryCandidateHumanGateway(b.store, service.flags)
	draft := memoryCandidateDraft(t, f)
	ids := map[string]bool{}
	var oldest agentmemorycandidate.Record
	for i := 0; i < 61; i++ {
		m, e := b.store.CreateMomentDraft(ctx, b.person.ID, content.MomentInput{CityID: "aberdeen-gb", Title: fmt.Sprintf("分页独占合成来源%d", i), TimePrecision: "unknown", LocationPrecision: "city"})
		if e != nil {
			t.Fatal(e)
		}
		r, e := gateway.Save(ctx, a, agentmemorycandidate.HumanDraft{Category: "sports", Sources: []agentmemorycandidate.Selector{{Type: agentevent.MomentSource, ID: m.ID}, {Type: agentevent.SavedPlaceSource, ID: f.sources[agentevent.PlaceSaved]}}, ValidUntil: draft.ValidUntil})
		if e != nil {
			t.Fatal(e)
		}
		ids[r.ID] = true
		if i == 0 {
			oldest = r
		}
		if _, e = b.store.PutOwnMemory(ctx, a, agentMemoryID(t, f.base.private), agentMemoryInput(fmt.Sprintf("review-%d", i))); e != nil {
			t.Fatal(e)
		}
	}
	seen := map[string]bool{}
	cursor := ""
	var firstCursor string
	for pages := 0; pages < 5; pages++ {
		p, e := g.ReadOwnCandidatePage(ctx, a, cursor)
		if e != nil || len(p.Items) > 20 || p.ModelAccess {
			t.Fatal("candidate page", e)
		}
		for _, r := range p.Items {
			if seen[r.ID] || !ids[r.ID] {
				t.Fatal("duplicate or foreign candidate")
			}
			seen[r.ID] = true
		}
		if p.NextCursor == nil {
			break
		}
		cursor = *p.NextCursor
		if firstCursor == "" {
			firstCursor = cursor
		}
	}
	if len(seen) != 61 || !seen[oldest.ID] {
		t.Fatal("candidate beyond old50 unreachable", len(seen))
	}
	if r, e := gateway.Reject(ctx, a, oldest.ID, oldest.Version); e != nil || r.Status != agentmemorycandidate.Rejected {
		t.Fatal("oldest current source not actionable", e)
	}
	memoryIDs := map[string]bool{}
	cursor = ""
	var memoryCursor string
	for pages := 0; pages < 5; pages++ {
		p, e := b.store.ReadOwnMemoryPage(ctx, a, cursor)
		if e != nil || len(p.Items) > 20 || p.ModelAccess {
			t.Fatal("memory page", e)
		}
		for _, r := range p.Items {
			if memoryIDs[r.ID] {
				t.Fatal("duplicate memory")
			}
			memoryIDs[r.ID] = true
		}
		if p.NextCursor == nil {
			break
		}
		cursor = *p.NextCursor
		if memoryCursor == "" {
			memoryCursor = cursor
		}
	}
	if len(memoryIDs) != 61 {
		t.Fatal("unbounded memory path or missing page", len(memoryIDs))
	}
	if _, e := g.ReadOwnCandidatePage(ctx, a, memoryCursor); !errors.Is(e, agentmemory.ErrConflict) {
		t.Fatal("cursor crossed collection", e)
	}
	if _, e := b.store.ReadOwnMemoryPage(ctx, f.base.private.peer, memoryCursor); !errors.Is(e, agentmemory.ErrConflict) {
		t.Fatal("cursor crossed owner", e)
	}
	if _, e := b.pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:]); e != nil {
		t.Fatal(e)
	}
	if _, e := b.pool.Exec(ctx, `UPDATE sessions SET revoked_at=NULL WHERE token_sha256=$1`, a.SessionDigest[:]); e != nil {
		t.Fatal(e)
	}
	if _, e := b.store.ReadOwnMemoryPage(ctx, a, memoryCursor); !errors.Is(e, agentmemory.ErrConflict) {
		t.Fatal("Session restore revived memory cursor", e)
	}
	if _, e := g.ReadOwnCandidatePage(ctx, a, firstCursor); !errors.Is(e, agentmemory.ErrConflict) {
		t.Fatal("Session restore revived candidate cursor", e)
	}
	if _, e := b.store.ReadOwnMemoryPage(context.Background(), a, ""); e != nil {
		t.Fatal("fresh homepage after ABA", e)
	}
}
