package postgres

import (
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/saved"
	"strings"
	"testing"
	"time"
)

func TestBatchASavedSearchBeyondHundredLiteralAndPrivateIsolation(t *testing.T) {
	f := savedPageNative(t)
	var oldest, place string
	for i := 0; i < 121; i++ {
		id, p := f.bookmark(t)
		if i == 0 {
			oldest, place = id, p
		}
	}
	f.exec(t, `UPDATE places SET name='跨页唯一%_地点' WHERE id=$1`, place)
	if _, e := f.store.UpdateHumanSavedNote(f.ctx, f.digest, f.actor, oldest, saved.NoteInput{OperationID: f.id(t), ExpectedRevision: 0, Note: "NEVER_SEARCH_PRIVATE_NOTE"}); e != nil {
		t.Fatal(e)
	}
	q := saved.Query{Text: "%_", Kind: "place", CityID: "aberdeen-gb"}
	page, e := f.store.SearchHumanSavedPage(f.ctx, f.digest, f.actor, q, "")
	if e != nil || len(page.Items) != 1 || page.Items[0].ID != oldest || page.Items[0].Latitude == nil || page.Items[0].SourceExpiresAt == nil {
		t.Fatal("server search missed older visible point", e)
	}
	if e = f.store.ValidateHumanSavedSearchPage(f.ctx, f.digest, f.actor, q, "", page); e != nil {
		t.Fatal("fixed readAt current proof", e)
	}
	private, e := f.store.SearchHumanSavedPage(f.ctx, f.digest, f.actor, saved.Query{Text: "NEVER_SEARCH_PRIVATE_NOTE"}, "")
	if e != nil || len(private.Items) != 0 {
		t.Fatal("note entered query", e)
	}
	all := saved.Query{Kind: "place"}
	first, e := f.store.SearchHumanSavedPage(f.ctx, f.digest, f.actor, all, "")
	if e != nil || first.NextCursor == nil || len(first.Items) != 20 {
		t.Fatal("filtered bounded first page", e)
	}
	if _, e = f.store.SearchHumanSavedPage(f.ctx, f.digest, f.actor, saved.Query{Kind: "group"}, *first.NextCursor); !errors.Is(e, saved.ErrPageChanged) {
		t.Fatal("cursor changed query", e)
	}
	seen := map[string]bool{}
	cursor := ""
	for n := 0; n < 8; n++ {
		p, e := f.store.SearchHumanSavedPage(f.ctx, f.digest, f.actor, all, cursor)
		if e != nil {
			t.Fatal(e)
		}
		for _, i := range p.Items {
			if seen[i.ID] {
				t.Fatal("duplicate across pages")
			}
			seen[i.ID] = true
		}
		if p.NextCursor == nil {
			break
		}
		cursor = *p.NextCursor
	}
	if len(seen) != 121 {
		t.Fatal("whole saved scope truncated", len(seen))
	}
	f.exec(t, `UPDATE places SET publication_status='draft' WHERE id=$1`, place)
	if e = f.store.ValidateHumanSavedSearchPage(f.ctx, f.digest, f.actor, q, "", page); !errors.Is(e, saved.ErrPageChanged) {
		t.Fatal("retired point released", e)
	}
	f.exec(t, `UPDATE places SET publication_status='published' WHERE id=$1`, place)
	if e = f.store.ValidateHumanSavedSearchPage(f.ctx, f.digest, f.actor, q, "", page); !errors.Is(e, saved.ErrPageChanged) {
		t.Fatal("source ABA revived proof", e)
	}
	legacy, e := f.store.ListSaved(f.ctx, f.actor.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, i := range legacy {
		if i.Note != "" || i.Latitude != nil || strings.Contains(i.Summary, "NEVER_SEARCH") {
			t.Fatal("private/mapped projection entered Agent legacy")
		}
	}
}
func TestBatchASavedSearchPointDeadlineAndSessionRetirement(t *testing.T) {
	f := savedPageNative(t)
	_, place := f.bookmark(t)
	f.exec(t, `UPDATE places SET expires_at=clock_timestamp()+interval '30 seconds' WHERE id=$1`, place)
	q := saved.Query{Kind: "place"}
	p, e := f.store.SearchHumanSavedPage(f.ctx, f.digest, f.actor, q, "")
	if e != nil || len(p.Items) != 1 || p.Items[0].SourceExpiresAt == nil || p.Items[0].SourceExpiresAt.After(time.Now().Add(31*time.Second)) {
		t.Fatal("actual source deadline not retained", e)
	}
	f.exec(t, `UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '35 minutes' WHERE id=$1`, f.session)
	if e = f.store.ValidateHumanSavedSearchPage(f.ctx, f.digest, f.actor, q, "", p); e != nil {
		t.Fatal("ordinary idle renewal retired source", e)
	}
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
	if e = f.store.ValidateHumanSavedSearchPage(f.ctx, f.digest, f.actor, q, "", p); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("revoked Session released point", e)
	}
	f.exec(t, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.session)
	if e = f.store.ValidateHumanSavedSearchPage(f.ctx, f.digest, f.actor, q, "", p); !errors.Is(e, saved.ErrPageChanged) {
		t.Fatal("Session ABA revived old point", e)
	}
}
