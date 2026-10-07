package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentplacememory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type placeMemoryFixture struct {
	private                 *agentPrivateFixture
	city, place, otherPlace string
}

func placeMemoryNativeFixture(t *testing.T) *placeMemoryFixture {
	t.Helper()
	f := &placeMemoryFixture{private: agentMemoryTestFixture(t)}
	b := f.private.base
	var installed bool
	if e := b.pool.QueryRow(b.ctx, `SELECT to_regclass('agent_domain_outbox') IS NOT NULL AND to_regclass('agent_memory_candidates') IS NOT NULL`).Scan(&installed); e != nil || !installed {
		t.Fatal("Place Memory requires final native001-064 fixture baseline")
	}
	f.city = "place027-" + strings.ReplaceAll(agentMemoryID(t, f.private), "-", "")
	b.exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id)
	 VALUES($1,'合成027城市','LOCAL','GB','Europe/London','published','LOCAL_SYNTHETIC_FIXTURE','local:AGE027','合成维护者',$2)`, f.city, b.other.ID)
	for _, target := range []*string{&f.place, &f.otherPlace} {
		if e := b.pool.QueryRow(b.ctx, `INSERT INTO places(id,city_id,name,category_code,summary,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id)
		 VALUES(gen_random_uuid(),$1,'合成027地点','sports','不能复制的地点描述','published','LOCAL_SYNTHETIC_FIXTURE','local:AGE027','合成维护者',$2) RETURNING id`, f.city, b.other.ID).Scan(target); e != nil {
			t.Fatal(e)
		}
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{
			`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM saved_items WHERE owner_account_id=ANY($1::uuid[])`,
			`DELETE FROM moments WHERE author_account_id=ANY($1::uuid[])`,
			`DELETE FROM activities WHERE created_by_account_id=ANY($1::uuid[])`,
			`DELETE FROM places WHERE maintainer_account_id=ANY($1::uuid[])`,
			`DELETE FROM cities WHERE maintainer_account_id=ANY($1::uuid[])`,
		} {
			if _, e := b.pool.Exec(ctx, q, b.accounts); e != nil {
				t.Error("owned Place source cleanup failed", e)
			}
		}
	})
	return f
}
func (f *placeMemoryFixture) moment(t *testing.T) content.Moment {
	t.Helper()
	b := f.private.base
	claimed := time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
	m, e := b.store.CreateMomentDraft(b.ctx, b.person.ID, content.MomentInput{CityID: f.city, PlaceID: f.place, Title: "合成027私人标题", Body: "合成027不能泄漏正文或推断到访", OccurredAt: &claimed, TimePrecision: "day", LocationPrecision: "place"})
	if e != nil {
		t.Fatal("native private Moment source", e)
	}
	return m
}
func (f *placeMemoryFixture) read(t *testing.T) agentplacememory.Projection {
	t.Helper()
	b := f.private.base
	p, e := b.store.ReadOwnPlaceMemory(b.ctx, f.private.owner, f.place)
	if e != nil || agentplacememory.Validate(p, p.ObservedAt) != nil || p.Owner != b.person || p.AgentID != b.personID {
		t.Fatal("actual current Place projection", e)
	}
	encoded, _ := json.Marshal(p)
	for _, private := range []string{"不能泄漏正文", "私人标题", "不能复制的地点描述", "latitude", "longitude", "occurredAt", "photo", "confidence", "activityId"} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("Place projection copied contents or invented experience", private)
		}
	}
	return p
}
func placeDeclarationInput(place string, kind agentplacememory.Kind) agentplacememory.PutDeclarationInput {
	return agentplacememory.PutDeclarationInput{PlaceID: place, Kind: kind, Visibility: agentmemory.VisibilityPrivate, ValidUntil: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}
}
func (f *placeMemoryFixture) declare(t *testing.T, kind agentplacememory.Kind) agentmemory.Record {
	t.Helper()
	b := f.private.base
	r, e := b.store.PutOwnPlaceDeclaration(b.ctx, f.private.owner, agentMemoryID(t, f.private), placeDeclarationInput(f.place, kind))
	if e != nil || r.SourceType != agentmemory.SourceExplicit || r.MemoryType != agentmemory.TypePlace || r.Confidence != 1 || r.Version != 1 {
		t.Fatal("native typed human declaration", e)
	}
	return r
}
func requirePlaceReadError(t *testing.T, p agentplacememory.Projection, e, want error) {
	t.Helper()
	if !errors.Is(e, want) || !reflect.DeepEqual(p, agentplacememory.Projection{}) {
		t.Fatalf("current Place read must reject with zero payload: got %v want %v", e, want)
	}
}
func requirePlaceWriteError(t *testing.T, r agentmemory.Record, e, want error) {
	t.Helper()
	if !errors.Is(e, want) || !reflect.DeepEqual(r, agentmemory.Record{}) {
		t.Fatalf("typed declaration must reject without contents: got %v want %v", e, want)
	}
}

func TestPlaceMemoryNativeCurrentSourcesAndDeclarations(t *testing.T) {
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	initial := f.read(t)
	if initial.Signals == nil || len(initial.Signals) != 0 {
		t.Fatal("initial Place read did not remain empty")
	}
	save, e := b.store.Save(b.ctx, b.person.ID, "place", f.place)
	if e != nil {
		t.Fatal(e)
	}
	m := f.moment(t)
	p := f.read(t)
	if len(p.Signals) != 2 || p.Signals[0].Kind != agentplacememory.CreatedMomentAt || p.Signals[1].Kind != agentplacememory.Saved {
		t.Fatal("native sources or deterministic order lost")
	}
	if p.Signals[0].Source.ID != m.ID || p.Signals[0].Source.Version.Revision != m.Revision || p.Signals[1].Source.ID != save || p.VerifiedVisit != "UNAVAILABLE" || p.Attendance != "UNAVAILABLE" {
		t.Fatal("source incorrectly certified experience")
	}
	before := agentMemoryOwnedSourceSnapshot(t, f.private)
	liked := f.declare(t, agentplacememory.Liked)
	visited := f.declare(t, agentplacememory.Visited)
	if agentMemoryOwnedSourceSnapshot(t, f.private) != before {
		t.Fatal("Place declaration changed identity, Profile, visibility, source grants or Tasks")
	}
	p = f.read(t)
	if len(p.Signals) != 4 {
		t.Fatal("distinct native sources and own statements were not combined")
	}
	for _, s := range p.Signals {
		if (s.Kind == agentplacememory.Liked || s.Kind == agentplacememory.Visited) && s.Basis != agentplacememory.SelfDeclaration {
			t.Fatal("direct statement became objective fact")
		}
	}
	pool, e := pgxpool.New(b.ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	read, e := New(pool, false).ReadOwnPlaceMemory(b.ctx, f.private.owner, f.place)
	if e != nil || read.SnapshotID != p.SnapshotID {
		t.Fatal("persistent data not retained across new Store/connection", e)
	}
	rows, e := b.store.ReadOwnMemories(b.ctx, f.private.owner)
	if e != nil || len(rows) != 2 || rows[0].ID != visited.ID && rows[1].ID != visited.ID || rows[0].ID != liked.ID && rows[1].ID != liked.ID {
		t.Fatal("Place statements bypassed the single Memory ledger")
	}
	ports := agentcognitive.UnavailableCognitivePorts{}
	if value, e := ports.ReadMemory(b.ctx, agentcognitive.ReadRequest{}); !errors.Is(e, agentcognitive.ErrUnavailable) || !reflect.ValueOf(value).IsZero() {
		t.Fatal("human Place memory enabled machine access")
	}
}
func TestPlaceMemoryNativeDeclarationCASRetryAndTombstone(t *testing.T) {
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	id := agentMemoryID(t, f.private)
	in := placeDeclarationInput(f.place, agentplacememory.Liked)
	r, e := b.store.PutOwnPlaceDeclaration(b.ctx, f.private.owner, id, in)
	if e != nil {
		t.Fatal(e)
	}
	for _, version := range []int64{0, 1} {
		t.Run("create_retry_"+string(rune('0'+version)), func(t *testing.T) {
			input := in
			input.ExpectedVersion = version
			again, e := b.store.PutOwnPlaceDeclaration(b.ctx, f.private.owner, id, input)
			if e != nil || !reflect.DeepEqual(again, r) {
				t.Fatal("create retry changed canonical version", e)
			}
		})
	}
	old := f.read(t)
	in.ExpectedVersion = 1
	in.Visibility = agentmemory.VisibilityAgentOnly
	updated, e := b.store.PutOwnPlaceDeclaration(b.ctx, f.private.owner, id, in)
	if e != nil || updated.Version != 2 {
		t.Fatal("native replacement", e)
	}
	if e = b.store.RevalidateOwnPlaceMemory(b.ctx, f.private.owner, old); !errors.Is(e, agentplacememory.ErrForbidden) {
		t.Fatal("old Memory version revalidated", e)
	}
	stale := in
	stale.ExpectedVersion = 1
	stale.ValidUntil = stale.ValidUntil.Add(time.Minute)
	got, e := b.store.PutOwnPlaceDeclaration(b.ctx, f.private.owner, id, stale)
	requirePlaceWriteError(t, got, e, agentplacememory.ErrConflict)
	for _, change := range []string{"kind", "place"} {
		t.Run("stable_selector_"+change, func(t *testing.T) {
			changed := in
			changed.ExpectedVersion = 2
			if change == "kind" {
				changed.Kind = agentplacememory.Visited
			} else {
				changed.PlaceID = f.otherPlace
			}
			got, e := b.store.PutOwnPlaceDeclaration(b.ctx, f.private.owner, id, changed)
			requirePlaceWriteError(t, got, e, agentplacememory.ErrConflict)
		})
	}
	deleted, e := b.store.DeleteOwnPlaceDeclaration(b.ctx, f.private.owner, id, 2)
	if e != nil || deleted.Status != agentmemory.StatusDeleted || deleted.Version != 3 || deleted.Summary != "" || string(deleted.StructuredValue) != "{}" {
		t.Fatal("single-ledger deletion did not scrub", e)
	}
	again, e := b.store.DeleteOwnPlaceDeclaration(b.ctx, f.private.owner, id, 2)
	if e != nil || !reflect.DeepEqual(again, deleted) {
		t.Fatal("tombstone deletion retry changed version", e)
	}
	got, e = b.store.PutOwnPlaceDeclaration(b.ctx, f.private.owner, id, in)
	requirePlaceWriteError(t, got, e, agentplacememory.ErrConflict)
	if p := f.read(t); len(p.Signals) != 0 {
		t.Fatal("deleted declaration remained a signal")
	}
}
func TestPlaceMemoryNativeSourceChangeInvalidation(t *testing.T) {
	t.Run("bookmark_remove_readd", func(t *testing.T) {
		f := placeMemoryNativeFixture(t)
		b := f.private.base
		id, e := b.store.Save(b.ctx, b.person.ID, "place", f.place)
		if e != nil {
			t.Fatal(e)
		}
		old := f.read(t)
		if e = b.store.RemoveSaved(b.ctx, b.person.ID, id); e != nil {
			t.Fatal(e)
		}
		if e = b.store.RevalidateOwnPlaceMemory(b.ctx, f.private.owner, old); !errors.Is(e, agentplacememory.ErrForbidden) {
			t.Fatal("removed bookmark revalidated", e)
		}
		newID, e := b.store.Save(b.ctx, b.person.ID, "place", f.place)
		if e != nil || newID == id {
			t.Fatal("re-added bookmark invented old source")
		}
		current := f.read(t)
		if current.SnapshotID == old.SnapshotID || current.Signals[0].Source.Version == old.Signals[0].Source.Version {
			t.Fatal("new source resurrected old snapshot")
		}
	})
	t.Run("moment_edit_move_withdraw", func(t *testing.T) {
		f := placeMemoryNativeFixture(t)
		b := f.private.base
		m := f.moment(t)
		old := f.read(t)
		m, e := b.store.UpdateMomentDraft(b.ctx, b.person.ID, m.ID, m.Revision, content.MomentInput{CityID: f.city, PlaceID: f.otherPlace, Title: "合成027编辑", Body: "仍不能推断到访", TimePrecision: "unknown", LocationPrecision: "place"})
		if e != nil {
			t.Fatal(e)
		}
		if e = b.store.RevalidateOwnPlaceMemory(b.ctx, f.private.owner, old); !errors.Is(e, agentplacememory.ErrForbidden) {
			t.Fatal("edited source revalidated", e)
		}
		if len(f.read(t).Signals) != 0 {
			t.Fatal("old Place retained moved Moment")
		}
		other, e := b.store.ReadOwnPlaceMemory(b.ctx, f.private.owner, f.otherPlace)
		if e != nil || len(other.Signals) != 1 || other.Signals[0].Source.Version.Revision != m.Revision {
			t.Fatal("real updated link missing", e)
		}
		if e = b.store.WithdrawMoment(b.ctx, b.person.ID, m.ID, m.Revision); e != nil {
			t.Fatal(e)
		}
		if e = b.store.RevalidateOwnPlaceMemory(b.ctx, f.private.owner, other); !errors.Is(e, agentplacememory.ErrForbidden) {
			t.Fatal("withdrawn Moment revalidated", e)
		}
	})
}
func TestPlaceMemoryNativeIdentityAndTargetBoundaries(t *testing.T) {
	t.Run("cross_owner_typed_principals", func(t *testing.T) {
		f := placeMemoryNativeFixture(t)
		b := f.private.base
		r := f.declare(t, agentplacememory.Liked)
		old := f.read(t)
		peer, e := b.store.ReadOwnPlaceMemory(b.ctx, f.private.peer, f.place)
		if e != nil || len(peer.Signals) != 0 {
			t.Fatal("public Place revealed another owner's statements", e)
		}
		if e = b.store.RevalidateOwnPlaceMemory(b.ctx, f.private.peer, old); !errors.Is(e, agentplacememory.ErrForbidden) {
			t.Fatal("peer validated owner's snapshot", e)
		}
		got, e := b.store.DeleteOwnPlaceDeclaration(b.ctx, f.private.peer, r.ID, 1)
		requirePlaceWriteError(t, got, e, agentplacememory.ErrNotFound)
		for _, a := range []agentprofile.PrivateAccess{f.private.org, f.private.biz, {}, {SessionDigest: f.private.owner.SessionDigest, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Organization, ID: b.person.ID}}, {SessionDigest: f.private.peer.SessionDigest, WorkspacePrincipal: b.person}} {
			p, e := b.store.ReadOwnPlaceMemory(b.ctx, a, f.place)
			requirePlaceReadError(t, p, e, agentplacememory.ErrForbidden)
			got, e := b.store.PutOwnPlaceDeclaration(b.ctx, a, agentMemoryID(t, f.private), placeDeclarationInput(f.place, agentplacememory.Liked))
			requirePlaceWriteError(t, got, e, agentplacememory.ErrForbidden)
		}
	})
	for _, change := range []string{"place_hidden", "city_hidden", "place_expired", "city_expired", "metadata_missing", "session_revoked", "account_suspended", "agent_retired"} {
		t.Run(change, func(t *testing.T) {
			f := placeMemoryNativeFixture(t)
			b := f.private.base
			f.declare(t, agentplacememory.Liked)
			old := f.read(t)
			want := agentplacememory.ErrNotFound
			switch change {
			case "place_hidden":
				b.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place)
			case "city_hidden":
				b.exec(`UPDATE cities SET publication_status='hidden' WHERE id=$1`, f.city)
			case "place_expired":
				b.exec(`UPDATE places SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.place)
			case "city_expired":
				b.exec(`UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.city)
			case "metadata_missing":
				b.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, b.personID)
			case "session_revoked":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.private.ownerSession)
				want = agentplacememory.ErrForbidden
			case "account_suspended":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
				want = agentplacememory.ErrForbidden
			case "agent_retired":
				b.exec(`UPDATE agents SET status='retired' WHERE id=$1`, b.personID)
				want = agentplacememory.ErrForbidden
			}
			p, e := b.store.ReadOwnPlaceMemory(b.ctx, f.private.owner, f.place)
			requirePlaceReadError(t, p, e, want)
			if e = b.store.RevalidateOwnPlaceMemory(b.ctx, f.private.owner, old); !errors.Is(e, want) {
				t.Fatal("old response survived current boundary", e)
			}
			got, e := b.store.PutOwnPlaceDeclaration(b.ctx, f.private.owner, agentMemoryID(t, f.private), placeDeclarationInput(f.place, agentplacememory.Visited))
			requirePlaceWriteError(t, got, e, want)
		})
	}
}
func TestPlaceMemoryNativeRestorationAndDeletionAfterHidden(t *testing.T) {
	for _, boundary := range []string{"account", "agent", "place", "city", "metadata"} {
		t.Run(boundary, func(t *testing.T) {
			f := placeMemoryNativeFixture(t)
			b := f.private.base
			old := f.read(t)
			switch boundary {
			case "account":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
				b.exec(`UPDATE accounts SET status='active' WHERE id=$1`, b.person.ID)
			case "agent":
				b.exec(`UPDATE agents SET status='retired' WHERE id=$1`, b.personID)
				b.exec(`UPDATE agents SET status='active' WHERE id=$1`, b.personID)
			case "place":
				b.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place)
				b.exec(`UPDATE places SET publication_status='published' WHERE id=$1`, f.place)
			case "city":
				b.exec(`UPDATE cities SET publication_status='hidden' WHERE id=$1`, f.city)
				b.exec(`UPDATE cities SET publication_status='published' WHERE id=$1`, f.city)
			case "metadata":
				b.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, b.personID)
				b.exec(`INSERT INTO agent_profiles(agent_id,owner_type,owner_id) VALUES($1,'PERSON',$2)`, b.personID, b.person.ID)
			}
			if e := b.store.RevalidateOwnPlaceMemory(b.ctx, f.private.owner, old); !errors.Is(e, agentplacememory.ErrForbidden) {
				t.Fatal("restored boundary revived historical snapshot", e)
			}
			f.read(t)
		})
	}
	t.Run("hidden_place_deletion", func(t *testing.T) {
		f := placeMemoryNativeFixture(t)
		b := f.private.base
		r := f.declare(t, agentplacememory.Visited)
		b.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place)
		gone, e := b.store.DeleteOwnPlaceDeclaration(b.ctx, f.private.owner, r.ID, 1)
		if e != nil || gone.Status != agentmemory.StatusDeleted || gone.Summary != "" || string(gone.StructuredValue) != "{}" {
			t.Fatal("hidden target prevented own scrub", e)
		}
	})
}
func TestPlaceMemoryNativeGenericAndExpiredMemoryNeverPromoted(t *testing.T) {
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	in := agentMemoryInput("generic.place")
	in.MemoryType = agentmemory.TypePlace
	in.StructuredValue = json.RawMessage(`{"visited":true,"liked":true,"confirmed":true,"placeId":"` + f.place + `"}`)
	mustPutAgentMemory(t, f.private, agentMemoryID(t, f.private), in)
	if len(f.read(t).Signals) != 0 {
		t.Fatal("generic EXPLICIT JSON became typed evidence")
	}
	inDeclare := placeDeclarationInput(f.place, agentplacememory.Visited)
	inDeclare.ValidUntil = time.Now().UTC().Add(800 * time.Millisecond).Truncate(time.Microsecond)
	r, e := b.store.PutOwnPlaceDeclaration(b.ctx, f.private.owner, agentMemoryID(t, f.private), inDeclare)
	if e != nil {
		t.Fatal("real short-lived declaration", e)
	}
	time.Sleep(time.Second)
	if len(f.read(t).Signals) != 0 {
		t.Fatal("expired declaration remained current")
	}
	got, e := b.store.DeleteOwnPlaceDeclaration(b.ctx, f.private.owner, r.ID, 1)
	if e != nil || got.Status != agentmemory.StatusDeleted {
		t.Fatal("expired record cannot be deleted", e)
	}
}

func TestPlaceMemoryNativeConcurrentRetryAndUnsupportedClaims(t *testing.T) {
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	id := agentMemoryID(t, f.private)
	in := placeDeclarationInput(f.place, agentplacememory.Liked)
	var wg sync.WaitGroup
	failures := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := b.store.PutOwnPlaceDeclaration(b.ctx, f.private.owner, id, in)
			if e != nil {
				failures <- e
				return
			}
			if r.ID != id || r.Version != 1 {
				failures <- errors.New("duplicate native declaration side effect")
			}
		}()
	}
	wg.Wait()
	close(failures)
	for e := range failures {
		t.Fatal(e)
	}
	var count int
	if e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memories WHERE owner_id=$1`, b.person.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal("native retry created duplicates", e)
	}
	for _, kind := range []agentplacememory.Kind{agentplacememory.Saved, agentplacememory.CreatedMomentAt, agentplacememory.AttendedActivityAt, "VERIFIED_VISIT"} {
		t.Run(string(kind), func(t *testing.T) {
			bad := in
			bad.Kind = kind
			r, e := b.store.PutOwnPlaceDeclaration(b.ctx, f.private.owner, agentMemoryID(t, f.private), bad)
			requirePlaceWriteError(t, r, e, agentplacememory.ErrInvalid)
		})
	}
	// A typed endpoint cannot silently replace an unrelated legacy declaration.
	ordinary := mustPutAgentMemory(t, f.private, agentMemoryID(t, f.private), agentMemoryInput("ordinary.preference"))
	changed := in
	changed.ExpectedVersion = 1
	r, e := b.store.PutOwnPlaceDeclaration(b.ctx, f.private.owner, ordinary.ID, changed)
	requirePlaceWriteError(t, r, e, agentplacememory.ErrConflict)
	if _, e = b.store.DeleteOwnPlaceDeclaration(b.ctx, f.private.owner, ordinary.ID, 1); !errors.Is(e, agentplacememory.ErrConflict) {
		t.Fatal("typed path deleted unrelated Memory", e)
	}
	// Same native object address cannot transfer a declaration across owners.
	r, e = b.store.PutOwnPlaceDeclaration(b.ctx, f.private.peer, id, in)
	requirePlaceWriteError(t, r, e, agentplacememory.ErrConflict)
}

func TestPlaceMemoryNativeRSVPEndedActivityIsNotAttendance(t *testing.T) {
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	in := activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: b.other.ID}, CityID: f.city, PlaceID: f.place,
		Title: "合成027到场负例", Summary: "明确本地合成RSVP不证明到场", StartsAt: time.Now().Add(time.Second), EndsAt: time.Now().Add(1500 * time.Millisecond), TimeZone: "Europe/London", Visibility: "public"}
	a, e := b.store.CreateSocialDraft(b.ctx, b.other.ID, in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.PublishSocialActivity(b.ctx, b.other.ID, a.ID); e != nil {
		t.Fatal(e)
	}
	if _, _, e = b.store.JoinActivity(b.ctx, b.person.ID, a.ID); e != nil {
		t.Fatal(e)
	}
	if len(f.read(t).Signals) != 0 {
		t.Fatal("going RSVP became Place attendance")
	}
	// The native organizer writer refuses changing a published start into the
	// past. Wait for this real short local Activity instead of fabricating an
	// unsupported past-time mutation or an attendance row.
	time.Sleep(time.Until(a.EndsAt) + 100*time.Millisecond)
	if p := f.read(t); len(p.Signals) != 0 || p.Attendance != "UNAVAILABLE" || p.VerifiedVisit != "UNAVAILABLE" {
		t.Fatal("ended activity invented real attendance or visit")
	}
	if _, e = b.store.CancelParticipation(b.ctx, b.person.ID, a.ID); e != nil {
		t.Fatal(e)
	}
	if len(f.read(t).Signals) != 0 {
		t.Fatal("cancelled RSVP became visit")
	}
}

func TestPlaceMemoryNativeSessionRefreshAndReplacement(t *testing.T) {
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	old := f.read(t)
	if _, e := b.store.Authenticate(b.ctx, f.private.owner.SessionDigest); e != nil {
		t.Fatal("actual native Authenticate idle refresh", e)
	}
	if e := b.store.RevalidateOwnPlaceMemory(b.ctx, f.private.owner, old); e != nil {
		t.Fatal("ordinary session refresh revoked own read", e)
	}
	_, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	b.exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)
	 VALUES($1,$2,'test',clock_timestamp()+interval '2 hours',clock_timestamp()+interval '1 hour')`, b.person.ID, digest[:])
	b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.private.ownerSession)
	newAccess := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: b.person}
	if e = b.store.RevalidateOwnPlaceMemory(b.ctx, newAccess, old); !errors.Is(e, agentplacememory.ErrForbidden) {
		t.Fatal("replacement session revived old revoked read snapshot", e)
	}
	fresh, e := b.store.ReadOwnPlaceMemory(b.ctx, newAccess, f.place)
	if e != nil || fresh.SnapshotID == old.SnapshotID {
		t.Fatal("fresh owner session cannot get an independent current snapshot", e)
	}
}

func TestPlaceMemoryNativeBoundedCollectionAndIndependentWrite(t *testing.T) {
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	// All these synthetic source rows are created through the actual native
	// Moment writer (including064 capture), rather than fixture "visited" rows.
	for range agentplacememory.MaxSignals {
		f.moment(t)
	}
	p := f.read(t)
	if len(p.Signals) != agentplacememory.MaxSignals {
		t.Fatal("complete legal source cap not returned")
	}
	f.moment(t)
	p, e := b.store.ReadOwnPlaceMemory(b.ctx, f.private.owner, f.place)
	requirePlaceReadError(t, p, e, agentplacememory.ErrUnavailable)
	// Collection overflow must not disable the independent canonical human
	// declaration writer or deletion; their final guards do not aggregate data.
	r := f.declare(t, agentplacememory.Liked)
	if gone, e := b.store.DeleteOwnPlaceDeclaration(b.ctx, f.private.owner, r.ID, 1); e != nil || gone.Status != agentmemory.StatusDeleted {
		t.Fatal("collection overflow prevented canonical deletion", e)
	}
}

type placeMemoryReadBarrier struct {
	mu             sync.Mutex
	calls          int
	ready, release chan struct{}
}

func (b *placeMemoryReadBarrier) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.Contains(d.SQL, "own_place_memory_current_v1") {
		b.mu.Lock()
		b.calls++
		second := b.calls == 2
		b.mu.Unlock()
		if second {
			close(b.ready)
			select {
			case <-b.release:
			case <-ctx.Done():
			}
		}
	}
	return ctx
}
func (*placeMemoryReadBarrier) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
func TestPlaceMemoryNativeFinalSnapshotUnderRepeatableDefault(t *testing.T) {
	for _, mutation := range []string{"bookmark_remove", "moment_edit", "city_hidden", "context_cancel"} {
		t.Run(mutation, func(t *testing.T) {
			f := placeMemoryNativeFixture(t)
			b := f.private.base
			save, e := b.store.Save(b.ctx, b.person.ID, "place", f.place)
			if e != nil {
				t.Fatal(e)
			}
			m := f.moment(t)
			cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
			if e != nil {
				t.Fatal(e)
			}
			cfg.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"
			barrier := &placeMemoryReadBarrier{ready: make(chan struct{}), release: make(chan struct{})}
			cfg.ConnConfig.Tracer = barrier
			pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			store := New(pool, false)
			ctx, cancel := context.WithCancel(b.ctx)
			defer cancel()
			type outcome struct {
				p agentplacememory.Projection
				e error
			}
			done := make(chan outcome, 1)
			go func() { p, e := store.ReadOwnPlaceMemory(ctx, f.private.owner, f.place); done <- outcome{p, e} }()
			select {
			case <-barrier.ready:
			case <-time.After(5 * time.Second):
				t.Fatal("actual second SQL snapshot barrier not reached")
			}
			want := agentplacememory.ErrForbidden
			switch mutation {
			case "bookmark_remove":
				if e = b.store.RemoveSaved(b.ctx, b.person.ID, save); e != nil {
					t.Fatal(e)
				}
			case "moment_edit":
				if _, e = b.store.UpdateMomentDraft(b.ctx, b.person.ID, m.ID, m.Revision, content.MomentInput{CityID: f.city, PlaceID: f.place, Title: "合成027迟到源更新", TimePrecision: "unknown", LocationPrecision: "place"}); e != nil {
					t.Fatal(e)
				}
			case "city_hidden":
				b.exec(`UPDATE cities SET publication_status='hidden' WHERE id=$1`, f.city)
				want = agentplacememory.ErrNotFound
			case "context_cancel":
				cancel()
				want = context.Canceled
			}
			close(barrier.release)
			select {
			case out := <-done:
				requirePlaceReadError(t, out.p, out.e, want)
			case <-time.After(5 * time.Second):
				t.Fatal("late current snapshot did not resolve")
			}
		})
	}
}
func placeMemoryWaitForBlock(t *testing.T, f *placeMemoryFixture, pid uint32) {
	t.Helper()
	b := f.private.base
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		var blocked bool
		if e := b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_pids(pid)))`, int(pid)).Scan(&blocked); e != nil {
			t.Fatal(e)
		}
		if blocked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("native row wait not observed")
}
func TestPlaceMemoryNativeExpiryAcrossActualRowWaits(t *testing.T) {
	t.Run("place_expiry_while_memory_locked", func(t *testing.T) {
		f := placeMemoryNativeFixture(t)
		b := f.private.base
		r := f.declare(t, agentplacememory.Liked)
		b.exec(`UPDATE places SET expires_at=clock_timestamp()+interval '800 milliseconds' WHERE id=$1`, f.place)
		block, e := b.pool.Begin(b.ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer block.Rollback(context.Background())
		var id string
		if e = block.QueryRow(b.ctx, `SELECT id FROM agent_memories WHERE id=$1 FOR UPDATE`, r.ID).Scan(&id); e != nil {
			t.Fatal(e)
		}
		in := placeDeclarationInput(f.place, agentplacememory.Liked)
		in.ExpectedVersion = 1
		in.Visibility = agentmemory.VisibilityAgentOnly
		type out struct {
			r agentmemory.Record
			e error
		}
		done := make(chan out, 1)
		go func() {
			record, e := b.store.PutOwnPlaceDeclaration(b.ctx, f.private.owner, r.ID, in)
			done <- out{record, e}
		}()
		placeMemoryWaitForBlock(t, f, block.Conn().PgConn().PID())
		time.Sleep(time.Second)
		if e = block.Commit(b.ctx); e != nil {
			t.Fatal(e)
		}
		got := <-done
		requirePlaceWriteError(t, got.r, got.e, agentplacememory.ErrNotFound)
		current, e := scanAgentMemory(b.pool.QueryRow(b.ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE id=$1`, r.ID))
		if e != nil || !reflect.DeepEqual(current, r) {
			t.Fatal("late target expiry committed declaration change", e)
		}
	})
	for _, expiry := range []string{"city", "session"} {
		t.Run(expiry+"_expiry_while_metadata_locked", func(t *testing.T) {
			f := placeMemoryNativeFixture(t)
			b := f.private.base
			if expiry == "city" {
				b.exec(`UPDATE cities SET expires_at=clock_timestamp()+interval '800 milliseconds' WHERE id=$1`, f.city)
			} else {
				b.exec(`UPDATE sessions SET expires_at=clock_timestamp()+interval '800 milliseconds',idle_expires_at=clock_timestamp()+interval '700 milliseconds' WHERE id=$1`, f.private.ownerSession)
			}
			block, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer block.Rollback(context.Background())
			var id string
			if e = block.QueryRow(b.ctx, `SELECT agent_id FROM agent_profiles WHERE agent_id=$1 FOR UPDATE`, b.personID).Scan(&id); e != nil {
				t.Fatal(e)
			}
			type out struct {
				p agentplacememory.Projection
				e error
			}
			done := make(chan out, 1)
			go func() { p, e := b.store.ReadOwnPlaceMemory(b.ctx, f.private.owner, f.place); done <- out{p, e} }()
			placeMemoryWaitForBlock(t, f, block.Conn().PgConn().PID())
			time.Sleep(time.Second)
			if e = block.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			got := <-done
			// Identity was acquired before the wait. The final single current SQL
			// refuses either expiry without exposing Place/source metadata.
			requirePlaceReadError(t, got.p, got.e, agentplacememory.ErrNotFound)
		})
	}
}
