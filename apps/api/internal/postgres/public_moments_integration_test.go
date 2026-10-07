package postgres

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func TestPublicMomentNativeExplicitProjection(t *testing.T) {
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	author := momentPublicationOrdinary(t, f)
	reader := momentPublicationOrdinary(t, f)
	m := momentPublicationDraft(t, f, author)
	if _, e := b.store.GetPublicMoment(b.ctx, content.PublicMomentAccess{}, m.ID); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("private draft", e)
	}
	r := momentPublicationPublish(t, f, author, m)
	access := content.PublicMomentAccess{Actor: reader.actor, SessionDigest: reader.digest}
	for _, a := range []content.PublicMomentAccess{{}, access} {
		got, e := b.store.GetPublicMoment(b.ctx, a, m.ID)
		if e != nil || got.ID != m.ID || got.Revision != r.Revision || got.PlaceID != f.place || got.Body != m.Body || got.SourceBinding == "" || got.PublishedAt.Location().String() != "UTC" {
			t.Fatal("actual current public projection", got, e)
		}
		raw, e := json.Marshal(got)
		if e != nil {
			t.Fatal(e)
		}
		for _, canary := range []string{"occurredAt", "1999-01-01", "authorAccountId", author.actor.ID, "activityIds", "communityId", "organizationId", "sourceBinding", "Context", "Evidence"} {
			if strings.Contains(string(raw), canary) {
				t.Fatal("private metadata in public detail", canary, string(raw))
			}
		}
		var fields map[string]any
		if json.Unmarshal(raw, &fields) != nil || len(fields) != 9 {
			t.Fatal("public closed shape", string(raw))
		}
	}
	// Time-limited discovery is not authority to erase a stable public object.
	b.exec(`UPDATE moments SET published_at=clock_timestamp()-interval '31 days',author_confirmed_at=clock_timestamp()-interval '32 days' WHERE id=$1`, m.ID)
	if got, e := b.store.GetPublicMoment(b.ctx, access, m.ID); e != nil || got.ID != m.ID {
		t.Fatal("older explicitly public stable detail", e)
	}
	if e := b.store.WithdrawHumanMoment(b.ctx, author.digest, author.actor, m.ID, r.Revision); e != nil {
		t.Fatal(e)
	}
	if _, e := b.store.GetPublicMoment(b.ctx, access, m.ID); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("withdrawn public detail", e)
	}
}

func TestPublicMomentNativeCurrentAuthority(t *testing.T) {
	for _, mode := range []string{"author_suspended", "block_by_reader", "block_by_author", "place_hidden", "city_hidden", "place_expired", "city_expired", "session_revoked", "session_expired", "wrong_actor", "organization_reader"} {
		t.Run(mode, func(t *testing.T) {
			f := placeMemoryNativeFixture(t)
			b := f.private.base
			author := momentPublicationOrdinary(t, f)
			reader := momentPublicationOrdinary(t, f)
			m := momentPublicationDraft(t, f, author)
			momentPublicationPublish(t, f, author, m)
			a := content.PublicMomentAccess{Actor: reader.actor, SessionDigest: reader.digest}
			want := content.ErrNotFound
			switch mode {
			case "author_suspended":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, author.actor.ID)
			case "block_by_reader":
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, reader.actor.ID, author.actor.ID)
			case "block_by_author":
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, author.actor.ID, reader.actor.ID)
			case "place_hidden":
				b.exec(`UPDATE places SET publication_status='draft' WHERE id=$1`, f.place)
			case "city_hidden":
				b.exec(`UPDATE cities SET publication_status='draft' WHERE id=$1`, f.city)
			case "place_expired":
				b.exec(`UPDATE places SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.place)
			case "city_expired":
				b.exec(`UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.city)
			case "session_revoked":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, reader.digest[:])
				want = identity.ErrUnauthorized
			case "session_expired":
				b.exec(`UPDATE sessions SET created_at=clock_timestamp()-interval '1 day',expires_at=clock_timestamp()-interval '1 second',idle_expires_at=clock_timestamp()-interval '2 seconds' WHERE token_sha256=$1`, reader.digest[:])
				want = identity.ErrUnauthorized
			case "wrong_actor":
				a.Actor = author.actor
				want = identity.ErrUnauthorized
			case "organization_reader":
				a.Actor.AccountType = "organization"
				want = identity.ErrUnauthorized
			}
			if got, e := b.store.GetPublicMoment(b.ctx, a, m.ID); !errors.Is(e, want) || got.ID != "" {
				t.Fatal("current source/session refusal", mode, got, e)
			}
		})
	}
}
