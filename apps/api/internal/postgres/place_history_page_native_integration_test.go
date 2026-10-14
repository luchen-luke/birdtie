package postgres

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	ph "github.com/birdtie/birdtie/apps/api/internal/placehistory"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

func pageNativeOwned(t *testing.T) {
	t.Helper()
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" || os.Getenv("BIRDTIE_DATABASE_URL") == "" {
		t.Skip("explicit owned pagination native runner required")
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Port != 55432 || !regexp.MustCompile(`^birdtie_nowv4_history_[0-9a-f]{12}$`).MatchString(cfg.ConnConfig.Database) || cfg.ConnConfig.Database != os.Getenv("BIRDTIE_PLACE_HISTORY_DATABASE") {
		t.Fatal("refuse non-owned pagination parent")
	}
	ownedMigrationDatabase(t)
}
func TestPlaceHistoryPageSQLACLFirstAndBoundedKeyset(t *testing.T) {
	sql := historyPageSQL()
	for _, fragment := range []string{"m.status='published'", "m.visibility='public'", "account_blocks", "author_confirmed_at", "m.published_at>=bound.asof-interval '30 days'", "(r.published_at,r.id)<($9::timestamptz,$10::uuid)", "LIMIT 6", "SELECT count(*) FROM recent", "authorization_generation", currentRetainedPlaceVisibilitySQL("p", "clock.at")} {
		if !strings.Contains(sql, fragment) {
			t.Fatal("missing original ACL/clock/keyset", fragment)
		}
	}
	if strings.Index(sql, "account_blocks") > strings.Index(sql, "page_rows AS MATERIALIZED") {
		t.Fatal("pagination occurs before ACL")
	}
	for _, forbidden := range []string{"OFFSET ", "UPDATE ", "INSERT INTO", "private_images", "media_url", "avatar"} {
		if strings.Contains(sql, forbidden) {
			t.Fatal("unsupported authority/offset/media", forbidden)
		}
	}
}
func TestPlaceHistoryPageNativeTwoPagesAndNoPrivateCounts(t *testing.T) {
	pageNativeOwned(t)
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	a := momentPublicationOrdinary(t, f)
	for i := 0; i < 7; i++ {
		m := momentPublicationDraft(t, f, a)
		momentPublicationPublish(t, f, a, m)
	}
	_ = momentPublicationDraft(t, f, a) // Actual private draft must affect neither count nor cursor.
	q := ph.PageQuery{PlaceID: f.place}
	first, e := b.store.ReadPublicPlaceHistoryPage(b.ctx, ph.Access{}, q)
	if e != nil || first.Count != 7 || len(first.Items) != 5 || first.NextCursor == nil {
		t.Fatal("actual first page", e)
	}
	if e = b.store.RevalidatePublicPlaceHistoryPage(b.ctx, ph.Access{}, q, first); e != nil {
		t.Fatal("actual final proof", e)
	}
	c, e := ph.DecodePageCursor(*first.NextCursor, f.place, false)
	if e != nil {
		t.Fatal(e)
	}
	q.Cursor = &c
	next, e := b.store.ReadPublicPlaceHistoryPage(b.ctx, ph.Access{}, q)
	if e != nil || next.Count != 7 || len(next.Items) != 2 || next.NextCursor != nil || !next.ValidUntil.Equal(first.ValidUntil) || next.SourceVersion != first.SourceVersion {
		t.Fatal("actual second page, fixed lease", e)
	}
	ids := map[string]bool{}
	for _, p := range []ph.Page{first, next} {
		for _, m := range p.Items {
			if ids[m.ID] {
				t.Fatal("duplicate boundary")
			}
			ids[m.ID] = true
			if m.AuthorID != nil || m.AuthorPublicProfile != nil {
				t.Fatal("non-optin author identity")
			}
		}
	}
	if len(ids) != 7 {
		t.Fatal("actual public membership incomplete")
	}
}
func TestPlaceHistoryPageNativeSourceWithdrawAndBlockABA(t *testing.T) {
	for _, mode := range []string{"withdraw", "reader_block_unblock", "author_block_unblock"} {
		t.Run(mode, func(t *testing.T) {
			pageNativeOwned(t)
			f := placeMemoryNativeFixture(t)
			b := f.private.base
			a := momentPublicationOrdinary(t, f)
			reader := momentPublicationOrdinary(t, f)
			var last content.Moment
			var revision int64
			for i := 0; i < 7; i++ {
				last = momentPublicationDraft(t, f, a)
				r := momentPublicationPublish(t, f, a, last)
				revision = r.Revision
			}
			access := ph.Access{Actor: reader.actor, SessionDigest: reader.digest}
			q := ph.PageQuery{PlaceID: f.place, WithAuthor: true}
			first, e := b.store.ReadPublicPlaceHistoryPage(b.ctx, access, q)
			if e != nil || first.NextCursor == nil {
				t.Fatal(e)
			}
			c, e := ph.DecodePageCursor(*first.NextCursor, f.place, true)
			if e != nil {
				t.Fatal(e)
			}
			q.Cursor = &c
			switch mode {
			case "withdraw":
				if e = b.store.WithdrawHumanMoment(b.ctx, a.digest, a.actor, last.ID, revision); e != nil {
					t.Fatal(e)
				}
			default:
				owner, target := reader.actor.ID, a.actor.ID
				if mode == "author_block_unblock" {
					owner, target = target, owner
				}
				if e = b.store.BlockAccount(b.ctx, owner, target); e != nil {
					t.Fatal(e)
				}
				if e = b.store.UnblockAccount(b.ctx, owner, target); e != nil {
					t.Fatal(e)
				}
			}
			out, e := b.store.ReadPublicPlaceHistoryPage(b.ctx, access, q)
			if !errors.Is(e, ph.ErrChanged) || out.Items != nil {
				t.Fatal("old generation returned mixed page", e)
			}
		})
	}
}
func TestPlaceHistoryPageNativeDeadlineNoRefreshExtension(t *testing.T) {
	pageNativeOwned(t)
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	a := momentPublicationOrdinary(t, f)
	reader := momentPublicationOrdinary(t, f)
	for i := 0; i < 7; i++ {
		momentPublicationPublish(t, f, a, momentPublicationDraft(t, f, a))
	}
	access := ph.Access{Actor: reader.actor, SessionDigest: reader.digest}
	q := ph.PageQuery{PlaceID: f.place}
	first, e := b.store.ReadPublicPlaceHistoryPage(b.ctx, access, q)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.Authenticate(b.ctx, reader.digest); e != nil {
		t.Fatal("original idle Authenticate", e)
	}
	if e = b.store.RevalidatePublicPlaceHistoryPage(b.ctx, access, q, first); e != nil {
		t.Fatal("harmless idle renewal changed source", e)
	}
	c, e := ph.DecodePageCursor(*first.NextCursor, f.place, false)
	if e != nil {
		t.Fatal(e)
	}
	q.Cursor = &c
	second, e := b.store.ReadPublicPlaceHistoryPage(b.ctx, access, q)
	if e != nil || !second.ValidUntil.Equal(first.ValidUntil) {
		t.Fatal("original page deadline moved", e)
	}
	// Narrowing a current source deadline is an actual negative fixture change.
	b.exec(`UPDATE places SET expires_at=clock_timestamp()+interval '1 second' WHERE id=$1`, f.place)
	if _, e = b.store.ReadPublicPlaceHistoryPage(b.ctx, access, q); !errors.Is(e, ph.ErrChanged) {
		t.Fatal("shortened source lease reused", e)
	}
	var expired bool
	stop := time.Now().Add(3 * time.Second)
	for !expired && time.Now().Before(stop) {
		if e = b.pool.QueryRow(b.ctx, `SELECT expires_at<=clock_timestamp() FROM places WHERE id=$1`, f.place).Scan(&expired); e != nil {
			t.Fatal(e)
		}
		if !expired {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !expired {
		t.Fatal("native expiry not observed")
	}
	if _, e = b.store.ReadPublicPlaceHistoryPage(b.ctx, access, q); !errors.Is(e, ph.ErrNotFound) {
		t.Fatal("expired target disclosed", e)
	}
}
func TestPlaceHistoryPageNativeNoMediaOrOwnerPrivateWire(t *testing.T) {
	pageNativeOwned(t)
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	a := momentPublicationOrdinary(t, f)
	momentPublicationPublish(t, f, a, momentPublicationDraft(t, f, a))
	p, e := b.store.ReadPublicPlaceHistoryPage(b.ctx, ph.Access{}, ph.PageQuery{PlaceID: f.place, WithAuthor: true})
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(p)
	for _, key := range []string{"occurredAt", "latitude", "privateImages", "mediaURL", "avatarURL", "SessionDigest", "source_type"} {
		if strings.Contains(string(raw), key) {
			t.Fatal("private/enriched field widened", key)
		}
	}
}
func TestPlaceHistoryPageNativeSessionRevocationABA(t *testing.T) {
	pageNativeOwned(t)
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	a := momentPublicationOrdinary(t, f)
	reader := momentPublicationOrdinary(t, f)
	for i := 0; i < 7; i++ {
		momentPublicationPublish(t, f, a, momentPublicationDraft(t, f, a))
	}
	access := ph.Access{Actor: reader.actor, SessionDigest: reader.digest}
	q := ph.PageQuery{PlaceID: f.place}
	first, e := b.store.ReadPublicPlaceHistoryPage(b.ctx, access, q)
	if e != nil || first.NextCursor == nil {
		t.Fatal(e)
	}
	c, e := ph.DecodePageCursor(*first.NextCursor, f.place, false)
	if e != nil {
		t.Fatal(e)
	}
	q.Cursor = &c
	if e = b.store.RevokeSession(b.ctx, reader.digest); e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.ReadPublicPlaceHistoryPage(b.ctx, access, q); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("supplied revoked session treated anonymous", e)
	}
	b.exec(`UPDATE sessions SET revoked_at=NULL WHERE token_sha256=$1`, reader.digest[:]) // Negative restore: native111 epoch must retire old cursor.
	if _, e = b.store.ReadPublicPlaceHistoryPage(b.ctx, access, q); !errors.Is(e, ph.ErrChanged) {
		t.Fatal("session ABA restored old page approval", e)
	}
}
func TestPlaceHistoryPageNativePublicAuthorSourceRetiresOldCursor(t *testing.T) {
	for _, mode := range []string{"private", "private_public_ABA"} {
		t.Run(mode, func(t *testing.T) {
			pageNativeOwned(t)
			f := placeMemoryNativeFixture(t)
			b := f.private.base
			a := momentPublicationNativeActor{actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, digest: f.private.owner.SessionDigest}
			// Original private fixture supplies an actual active personal Agent and
			// explicitly public Profile. No fake avatar/media or contact authority.
			for i := 0; i < 7; i++ {
				momentPublicationPublish(t, f, a, momentPublicationDraft(t, f, a))
			}
			q := ph.PageQuery{PlaceID: f.place, WithAuthor: true}
			first, e := b.store.ReadPublicPlaceHistoryPage(b.ctx, ph.Access{}, q)
			if e != nil || first.NextCursor == nil || first.Items[0].AuthorID == nil || *first.Items[0].AuthorID != b.person.ID {
				t.Fatal("actual allowed public label", e)
			}
			c, e := ph.DecodePageCursor(*first.NextCursor, f.place, true)
			if e != nil {
				t.Fatal(e)
			}
			q.Cursor = &c
			if _, e = b.store.UpdateOwnProfile(b.ctx, b.person.ID, identity.ProfileInput{DisplayName: "合成公开资料", Bio: "旧公开字段保持原值", Visibility: "private"}); e != nil {
				t.Fatal(e)
			}
			if mode == "private_public_ABA" {
				if _, e = b.store.UpdateOwnProfile(b.ctx, b.person.ID, identity.ProfileInput{DisplayName: "合成公开资料", Bio: "旧公开字段保持原值", Visibility: "public"}); e != nil {
					t.Fatal(e)
				}
			}
			if _, e = b.store.ReadPublicPlaceHistoryPage(b.ctx, ph.Access{}, q); !errors.Is(e, ph.ErrChanged) {
				t.Fatal("profile source ABA mixed old authors", e)
			}
			q.Cursor = nil
			fresh, e := b.store.ReadPublicPlaceHistoryPage(b.ctx, ph.Access{}, q)
			if e != nil || fresh.Count != 7 {
				t.Fatal("private profile changed public text count", e)
			}
			if mode == "private" {
				for _, m := range fresh.Items {
					if m.AuthorID != nil || m.AuthorPublicProfile != nil {
						t.Fatal("private profile label exposed")
					}
				}
			}
		})
	}
}

// Text regression only: this deliberately does not count as executed SQL or PG.
func TestPlaceHistoryPageSQLCheckedOriginalMarkers(t *testing.T) {
	for _, base := range []string{placeHistoryPayloadSQL, strings.ReplaceAll(placeHistoryPayloadSQL, "\r\n", "\n"), strings.ReplaceAll(strings.ReplaceAll(placeHistoryPayloadSQL, "\r\n", "\n"), "\n", "\r\n")} {
		if sql := historyPageSQLFromBase(base); sql == "" || !strings.Contains(sql, "CROSS JOIN clock CROSS JOIN bound") || strings.Contains(sql, "m.published_at>=clock.at-interval '30 days'") {
			t.Fatal("original CTE adaptation unavailable or moving window")
		}
	}
	for _, needle := range []string{" arrangements AS MATERIALIZED (", " target AS MATERIALIZED (", "m.published_at FROM moments m", "CROSS JOIN clock\n   WHERE m.status", "m.published_at>=clock.at-interval '30 days' AND m.published_at<=clock.at"} {
		base := strings.ReplaceAll(placeHistoryPayloadSQL, "\r\n", "\n")
		if sql := historyPageSQLFromBase(strings.Replace(base, needle, "DRIFT", 1)); sql != "" {
			t.Fatal("missing marker did not fail closed", needle)
		}
		if sql := historyPageSQLFromBase(strings.Replace(base, needle, needle+needle, 1)); sql != "" {
			t.Fatal("ambiguous marker did not fail closed", needle)
		}
	}
}

func TestPlaceHistoryPageSQLFinalAtomicSourceClock(t *testing.T) {
	sql := historyPageFinalSQL()
	for _, fragment := range []string{"WITH page_snapshot(", "ready AS MATERIALIZED(SELECT count(*) n FROM page_snapshot)", "wall AS MATERIALIZED(SELECT clock_timestamp() at FROM ready)", "snapshot.private_source", "se.authorization_generation=$12", "se.id::text=$11", "account_blocks", "author_public_profile", "FROM wall LEFT JOIN page_snapshot snapshot ON true"} {
		if !strings.Contains(sql, fragment) {
			t.Fatal("missing final native ACL/source/wall/identity fence", fragment)
		}
	}
	if strings.Contains(sql, "OFFSET ") || strings.Contains(sql, "se.xmin") || strings.Contains(sql, "UPDATE ") || strings.Contains(sql, "LOCK TABLE") {
		t.Fatal("final read introduces pagination/session/write/table-lock authority")
	}
}
