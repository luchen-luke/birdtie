package postgres

import (
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	review "github.com/birdtie/birdtie/apps/api/internal/agentmemoryreview"
	"strings"
	"testing"
	"time"
)

func TestMemoryReviewCursorSealedBoundedAndTamperRejected(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	c := memoryReviewCursor{Schema: review.Schema, Collection: review.Memories, Owner: "23000000-0000-4000-8000-000000000001", Agent: "23000000-0000-4000-8000-000000000002", Session: "23000000-0000-4000-8000-000000000003", Authority: strings.Repeat("a", 64), Generation: 2, Ceiling: now, After: now.Add(-time.Hour), Until: now.Add(time.Minute), ID: "23000000-0000-4000-8000-000000000004"}
	sealed, e := encodeMemoryReviewCursor(c)
	if e != nil {
		t.Fatal(e)
	}
	decoded, e := decodeMemoryReviewCursor(sealed)
	if e != nil || decoded != c {
		t.Fatal("sealed cursor lost native scope", e)
	}
	tamper := []byte(sealed)
	if tamper[20] == 'a' {
		tamper[20] = 'b'
	} else {
		tamper[20] = 'a'
	}
	if _, e = decodeMemoryReviewCursor(string(tamper)); !errors.Is(e, agentmemory.ErrConflict) {
		t.Fatal("tamper accepted", e)
	}
	if _, e = decodeMemoryReviewCursor(sealed + "="); !errors.Is(e, agentmemory.ErrInvalid) {
		t.Fatal("noncanonical encoding accepted", e)
	}
}
