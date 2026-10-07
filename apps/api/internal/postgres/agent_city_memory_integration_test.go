package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcitymemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type cityMemoryFixture struct {
	private                                         *agentPrivateFixture
	city, otherCity, contextID, otherContext, place string
}

func cityMemoryNativeFixture(t *testing.T) *cityMemoryFixture {
	t.Helper()
	f := &cityMemoryFixture{private: agentMemoryTestFixture(t)}
	b := f.private.base
	var installed bool
	if e := b.pool.QueryRow(b.ctx, `SELECT to_regclass('agent_domain_outbox') IS NOT NULL`).Scan(&installed); e != nil || !installed {
		t.Fatal("native city fixture requires001-064")
	}
	for _, pair := range []struct{ city, context *string }{{&f.city, &f.contextID}, {&f.otherCity, &f.otherContext}} {
		*pair.city = "city028-" + strings.ReplaceAll(agentMemoryID(t, f.private), "-", "")
		b.exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id)
  VALUES($1,'合成028城市','LOCAL','GB','Europe/London','published','LOCAL_SYNTHETIC_FIXTURE','local:AGE028','合成维护者',$2)`, *pair.city, b.other.ID)
		b.exec(`INSERT INTO city_contexts(city_id) VALUES($1)`, *pair.city)
		if e := b.pool.QueryRow(b.ctx, `SELECT id::text FROM contexts WHERE context_type='CITY' AND city_id=$1`, *pair.city).Scan(pair.context); e != nil {
			t.Fatal(e)
		}
	}
	if e := b.pool.QueryRow(b.ctx, `INSERT INTO places(id,city_id,name,category_code,summary,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id)
 VALUES(gen_random_uuid(),$1,'合成028地点','sports','不得推断到访的来源','published','LOCAL_SYNTHETIC_FIXTURE','local:AGE028','合成维护者',$2) RETURNING id`, f.city, b.other.ID).Scan(&f.place); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, `DELETE FROM saved_items WHERE owner_account_id=ANY($1::uuid[])`, `DELETE FROM moments WHERE author_account_id=ANY($1::uuid[])`, `DELETE FROM activities WHERE created_by_account_id=ANY($1::uuid[])`, `DELETE FROM person_contexts WHERE person_account_id=ANY($1::uuid[])`} {
			if _, e := b.pool.Exec(ctx, q, b.accounts); e != nil {
				t.Error("owned city native cleanup", e)
			}
		}
		for _, q := range []string{`DELETE FROM places WHERE city_id=ANY($1::text[])`, `DELETE FROM contexts WHERE city_id=ANY($1::text[])`, `DELETE FROM city_contexts WHERE city_id=ANY($1::text[])`, `DELETE FROM cities WHERE id=ANY($1::text[])`} {
			if _, e := b.pool.Exec(ctx, q, []string{f.city, f.otherCity}); e != nil {
				t.Error("owned city target cleanup", e)
			}
		}
		var n int
		if e := b.pool.QueryRow(ctx, `SELECT count(*) FROM cities WHERE id=ANY($1::text[])`, []string{f.city, f.otherCity}).Scan(&n); e != nil || n != 0 {
			t.Error("owned city residue", n, e)
		}
	})
	return f
}
func cityInput(city string, k agentcitymemory.Kind) agentcitymemory.PutDeclarationInput {
	return agentcitymemory.PutDeclarationInput{CityID: city, Kind: k, Visibility: agentmemory.VisibilityPrivate, ValidUntil: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}
}
func (f *cityMemoryFixture) read(t *testing.T) agentcitymemory.Projection {
	t.Helper()
	b := f.private.base
	p, e := b.store.ReadOwnCityMemory(b.ctx, f.private.owner, f.city)
	if e != nil || agentcitymemory.Validate(p, p.ObservedAt) != nil || p.Owner != b.person || p.AgentID != b.personID {
		t.Fatal("actual current city projection", e)
	}
	raw, _ := json.Marshal(p)
	for _, bad := range []string{"私人正文", "亲历", "occurredAt", "latitude", "longitude", "1999-01-01", "confidence", "displayName"} {
		if strings.Contains(string(raw), bad) {
			t.Fatal("city read invented/copied private facts", bad)
		}
	}
	return p
}
func (f *cityMemoryFixture) declare(t *testing.T, k agentcitymemory.Kind) agentmemory.Record {
	t.Helper()
	b := f.private.base
	r, e := b.store.PutOwnCityDeclaration(b.ctx, f.private.owner, agentMemoryID(t, f.private), cityInput(f.city, k))
	if e != nil || r.SourceType != agentmemory.SourceExplicit || r.MemoryType != agentmemory.TypeCity || r.Confidence != 1 || r.Version != 1 {
		t.Fatal("actual typed city declaration", e)
	}
	return r
}
func cityReadError(t *testing.T, p agentcitymemory.Projection, e, want error) {
	t.Helper()
	if !errors.Is(e, want) || !reflect.DeepEqual(p, agentcitymemory.Projection{}) {
		t.Fatalf("city read error %v expected %v zero=%v", e, want, reflect.DeepEqual(p, agentcitymemory.Projection{}))
	}
}
func cityWriteError(t *testing.T, r agentmemory.Record, e, want error) {
	t.Helper()
	if !errors.Is(e, want) || !reflect.DeepEqual(r, agentmemory.Record{}) {
		t.Fatalf("city write error %v expected %v", e, want)
	}
}

func TestCityMemoryNativeFourIndependentSignals(t *testing.T) {
	f := cityMemoryNativeFixture(t)
	b := f.private.base
	if len(f.read(t).Signals) != 0 {
		t.Fatal("unexpected initial facts")
	}
	d, e := b.store.DeclareContext(b.ctx, b.person.ID, contextgraph.DeclarationInput{Type: contextgraph.City, SourceKey: f.city, Relation: "current"})
	if e != nil || d.ContextID != f.contextID {
		t.Fatal("actual native current declaration", e)
	}
	for _, k := range []agentcitymemory.Kind{agentcitymemory.Lived, agentcitymemory.Visited, agentcitymemory.Interested} {
		f.declare(t, k)
	}
	before := agentMemoryOwnedSourceSnapshot(t, f.private)
	p := f.read(t)
	if len(p.Signals) != 4 {
		t.Fatal("four distinct sources not present")
	}
	seen := map[agentcitymemory.Kind]bool{}
	for _, s := range p.Signals {
		seen[s.Kind] = true
		if s.Kind == agentcitymemory.Current {
			if s.Source.ID != d.ContextID || s.Source.Revision != 0 || len(s.Source.Token) != 64 || s.SourceUpdatedAt != nil || s.ValidUntil != nil {
				t.Fatal("invented context version/time")
			}
		} else if s.Source.Revision != 1 || s.Source.Token != "" {
			t.Fatal("wrong native Memory version")
		}
	}
	for _, k := range agentcitymemory.Kinds() {
		if !seen[k] {
			t.Fatal("missing distinct kind", k)
		}
	}
	pool, e := pgxpool.New(b.ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	again, e := New(pool, false).ReadOwnCityMemory(b.ctx, f.private.owner, f.city)
	if e != nil || again.SnapshotID != p.SnapshotID {
		t.Fatal("reconnected native city state", e)
	}
	if agentMemoryOwnedSourceSnapshot(t, f.private) != before {
		t.Fatal("read altered Profile/identity/permissions")
	}
	_, e = b.store.DeclareContext(b.ctx, b.person.ID, contextgraph.DeclarationInput{Type: contextgraph.City, SourceKey: f.otherCity, Relation: "current"})
	if e != nil {
		t.Fatal(e)
	}
	if e = b.store.RevalidateOwnCityMemory(b.ctx, f.private.owner, p); !errors.Is(e, agentcitymemory.ErrForbidden) {
		t.Fatal("current switch retained old snapshot", e)
	}
	if len(f.read(t).Signals) != 3 {
		t.Fatal("current switch destroyed history or old current remained")
	}
	next, e := b.store.ReadOwnCityMemory(b.ctx, f.private.owner, f.otherCity)
	if e != nil || len(next.Signals) != 1 || next.Signals[0].Kind != agentcitymemory.Current {
		t.Fatal("current switch inferred new history", e)
	}
	ports := agentcognitive.UnavailableCognitivePorts{}
	value, e := ports.ReadMemory(b.ctx, agentcognitive.ReadRequest{})
	if !errors.Is(e, agentcognitive.ErrUnavailable) || !reflect.ValueOf(value).IsZero() {
		t.Fatal("city declaration enabled cognition")
	}
}
func TestCityMemoryNativeCASRetryAndDeletion(t *testing.T) {
	f := cityMemoryNativeFixture(t)
	b := f.private.base
	id := agentMemoryID(t, f.private)
	in := cityInput(f.city, agentcitymemory.Visited)
	r, e := b.store.PutOwnCityDeclaration(b.ctx, f.private.owner, id, in)
	if e != nil {
		t.Fatal(e)
	}
	for _, version := range []int64{0, 1} {
		retry := in
		retry.ExpectedVersion = version
		got, e := b.store.PutOwnCityDeclaration(b.ctx, f.private.owner, id, retry)
		if e != nil || !reflect.DeepEqual(got, r) {
			t.Fatal("exact retry lost native row", e)
		}
	}
	old := f.read(t)
	in.ExpectedVersion = 1
	in.Visibility = agentmemory.VisibilityAgentOnly
	updated, e := b.store.PutOwnCityDeclaration(b.ctx, f.private.owner, id, in)
	if e != nil || updated.Version != 2 {
		t.Fatal(e)
	}
	if e = b.store.RevalidateOwnCityMemory(b.ctx, f.private.owner, old); !errors.Is(e, agentcitymemory.ErrForbidden) {
		t.Fatal("old version accepted", e)
	}
	stale := in
	stale.ValidUntil = stale.ValidUntil.Add(time.Minute)
	got, e := b.store.PutOwnCityDeclaration(b.ctx, f.private.owner, id, stale)
	cityWriteError(t, got, e, agentcitymemory.ErrConflict)
	for _, selector := range []string{"kind", "city"} {
		t.Run(selector, func(t *testing.T) {
			changed := in
			changed.ExpectedVersion = 2
			if selector == "kind" {
				changed.Kind = agentcitymemory.Lived
			} else {
				changed.CityID = f.otherCity
			}
			got, e := b.store.PutOwnCityDeclaration(b.ctx, f.private.owner, id, changed)
			cityWriteError(t, got, e, agentcitymemory.ErrConflict)
		})
	}
	gone, e := b.store.DeleteOwnCityDeclaration(b.ctx, f.private.owner, id, 2)
	if e != nil || gone.Version != 3 || gone.Status != agentmemory.StatusDeleted || gone.Summary != "" || string(gone.StructuredValue) != "{}" {
		t.Fatal("native tombstone", e)
	}
	retry, e := b.store.DeleteOwnCityDeclaration(b.ctx, f.private.owner, id, 2)
	if e != nil || !reflect.DeepEqual(gone, retry) {
		t.Fatal("delete retry", e)
	}
	got, e = b.store.PutOwnCityDeclaration(b.ctx, f.private.owner, id, in)
	cityWriteError(t, got, e, agentcitymemory.ErrConflict)
	if len(f.read(t).Signals) != 0 {
		t.Fatal("deleted Memory still projected")
	}
	ordinary := mustPutAgentMemory(t, f.private, agentMemoryID(t, f.private), agentMemoryInput("ordinary.city.guard"))
	if _, e = b.store.DeleteOwnCityDeclaration(b.ctx, f.private.owner, ordinary.ID, 1); !errors.Is(e, agentcitymemory.ErrConflict) {
		t.Fatal("typed endpoint deleted unrelated Memory", e)
	}
}
func TestCityMemoryNativeIdentityAndCurrentTargetBoundary(t *testing.T) {
	t.Run("real_sessions_and_namespaces", func(t *testing.T) {
		f := cityMemoryNativeFixture(t)
		b := f.private.base
		r := f.declare(t, agentcitymemory.Lived)
		old := f.read(t)
		peer, e := b.store.ReadOwnCityMemory(b.ctx, f.private.peer, f.city)
		if e != nil || len(peer.Signals) != 0 {
			t.Fatal("cross Person leaked", e)
		}
		if e = b.store.RevalidateOwnCityMemory(b.ctx, f.private.peer, old); !errors.Is(e, agentcitymemory.ErrForbidden) {
			t.Fatal("cross owner revalidated", e)
		}
		got, e := b.store.DeleteOwnCityDeclaration(b.ctx, f.private.peer, r.ID, 1)
		cityWriteError(t, got, e, agentcitymemory.ErrNotFound)
		got, e = b.store.PutOwnCityDeclaration(b.ctx, f.private.peer, r.ID, cityInput(f.city, agentcitymemory.Lived))
		cityWriteError(t, got, e, agentcitymemory.ErrConflict)
		for _, a := range []agentprofile.PrivateAccess{{}, f.private.org, f.private.biz, {SessionDigest: f.private.owner.SessionDigest, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Organization, ID: b.person.ID}}, {SessionDigest: f.private.peer.SessionDigest, WorkspacePrincipal: b.person}} {
			p, e := b.store.ReadOwnCityMemory(b.ctx, a, f.city)
			cityReadError(t, p, e, agentcitymemory.ErrForbidden)
			got, e := b.store.PutOwnCityDeclaration(b.ctx, a, agentMemoryID(t, f.private), cityInput(f.city, agentcitymemory.Visited))
			cityWriteError(t, got, e, agentcitymemory.ErrForbidden)
		}
	})
	for _, boundary := range []string{"city_hidden", "city_expired", "session_revoked", "session_absolute", "session_idle", "account_suspended", "agent_retired", "agent_deleted", "metadata_missing"} {
		t.Run(boundary, func(t *testing.T) {
			f := cityMemoryNativeFixture(t)
			b := f.private.base
			f.declare(t, agentcitymemory.Lived)
			old := f.read(t)
			want := agentcitymemory.ErrNotFound
			switch boundary {
			case "city_hidden":
				b.exec(`UPDATE cities SET publication_status='hidden' WHERE id=$1`, f.city)
			case "city_expired":
				b.exec(`UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.city)
			case "session_revoked":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.private.ownerSession)
				want = agentcitymemory.ErrForbidden
			case "session_absolute":
				// All three values describe one actual PG instant. Independent
				// volatile clocks could violate idle <= absolute before testing
				// the reader's unchanged expired-session boundary.
				b.exec(`WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at)
 UPDATE sessions SET created_at=stamp.at-interval '2 hours',expires_at=stamp.at-interval '1 hour',idle_expires_at=stamp.at-interval '1 hour'
 FROM stamp WHERE id=$1`, f.private.ownerSession)
				want = agentcitymemory.ErrForbidden
			case "session_idle":
				b.exec(`UPDATE sessions SET created_at=clock_timestamp()-interval '2 hours',idle_expires_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, f.private.ownerSession)
				want = agentcitymemory.ErrForbidden
			case "account_suspended":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
				want = agentcitymemory.ErrForbidden
			case "agent_retired":
				b.exec(`UPDATE agents SET status='retired' WHERE id=$1`, b.personID)
				want = agentcitymemory.ErrForbidden
			case "agent_deleted":
				b.exec(`DELETE FROM agents WHERE id=$1`, b.personID)
				want = agentcitymemory.ErrForbidden
			case "metadata_missing":
				b.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, b.personID)
			}
			p, e := b.store.ReadOwnCityMemory(b.ctx, f.private.owner, f.city)
			cityReadError(t, p, e, want)
			if e = b.store.RevalidateOwnCityMemory(b.ctx, f.private.owner, old); !errors.Is(e, want) {
				t.Fatal("old source survived boundary", e)
			}
			got, e := b.store.PutOwnCityDeclaration(b.ctx, f.private.owner, agentMemoryID(t, f.private), cityInput(f.city, agentcitymemory.Visited))
			cityWriteError(t, got, e, want)
		})
	}
	for _, boundary := range []string{"city", "account", "agent", "metadata"} {
		t.Run("restored_"+boundary, func(t *testing.T) {
			f := cityMemoryNativeFixture(t)
			b := f.private.base
			old := f.read(t)
			switch boundary {
			case "city":
				b.exec(`UPDATE cities SET publication_status='hidden' WHERE id=$1`, f.city)
				b.exec(`UPDATE cities SET publication_status='published' WHERE id=$1`, f.city)
			case "account":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
				b.exec(`UPDATE accounts SET status='active' WHERE id=$1`, b.person.ID)
			case "agent":
				b.exec(`UPDATE agents SET status='retired' WHERE id=$1`, b.personID)
				b.exec(`UPDATE agents SET status='active' WHERE id=$1`, b.personID)
			case "metadata":
				b.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, b.personID)
				b.exec(`INSERT INTO agent_profiles(agent_id,owner_type,owner_id) VALUES($1,'PERSON',$2)`, b.personID, b.person.ID)
			}
			if e := b.store.RevalidateOwnCityMemory(b.ctx, f.private.owner, old); !errors.Is(e, agentcitymemory.ErrForbidden) {
				t.Fatal("restored state revived old snapshot", e)
			}
			f.read(t)
		})
	}
}
func TestCityMemoryNativeContextRemovalRecreateAndAmbiguity(t *testing.T) {
	f := cityMemoryNativeFixture(t)
	b := f.private.base
	_, e := b.store.DeclareContext(b.ctx, b.person.ID, contextgraph.DeclarationInput{Type: contextgraph.City, SourceKey: f.city, Relation: "current"})
	if e != nil {
		t.Fatal(e)
	}
	old := f.read(t)
	if e = b.store.RemoveContextDeclaration(b.ctx, b.person.ID, f.contextID, "current"); e != nil {
		t.Fatal(e)
	}
	if e = b.store.RevalidateOwnCityMemory(b.ctx, f.private.owner, old); !errors.Is(e, agentcitymemory.ErrForbidden) {
		t.Fatal("deleted declaration reused", e)
	}
	_, e = b.store.DeclareContext(b.ctx, b.person.ID, contextgraph.DeclarationInput{Type: contextgraph.City, SourceKey: f.city, Relation: "current"})
	if e != nil {
		t.Fatal(e)
	}
	if next := f.read(t); next.Signals[0].Source.Token == old.Signals[0].Source.Token {
		t.Fatal("same-address recreated declaration reused old token")
	}
	if e = b.store.RevalidateOwnCityMemory(b.ctx, f.private.owner, old); !errors.Is(e, agentcitymemory.ErrForbidden) {
		t.Fatal("recreated declaration revived old snapshot", e)
	}
	b.exec(`INSERT INTO person_contexts(person_account_id,context_id,relation) VALUES($1,$2,'current')`, b.person.ID, f.otherContext)
	p, e := b.store.ReadOwnCityMemory(b.ctx, f.private.owner, f.city)
	cityReadError(t, p, e, agentcitymemory.ErrUnavailable)
	b.exec(`UPDATE cities SET publication_status='hidden' WHERE id=$1`, f.otherCity)
	p, e = b.store.ReadOwnCityMemory(b.ctx, f.private.owner, f.city)
	cityReadError(t, p, e, agentcitymemory.ErrUnavailable)
}
func TestCityMemoryNativeNoImplicitExperienceOrDates(t *testing.T) {
	f := cityMemoryNativeFixture(t)
	b := f.private.base
	savePrivateCanaries(t, f.private)
	for _, relation := range []string{"home", "past", "destination"} {
		if _, e := b.store.DeclareContext(b.ctx, b.person.ID, contextgraph.DeclarationInput{Type: contextgraph.City, SourceKey: f.city, Relation: relation}); e != nil {
			t.Fatal(e)
		}
	}
	in := agentMemoryInput("ordinary.city.text")
	in.MemoryType = agentmemory.TypeCity
	in.StructuredValue = json.RawMessage(`{"visited":true,"lived":true,"confirmed":true,"cityId":"` + f.city + `"}`)
	mustPutAgentMemory(t, f.private, agentMemoryID(t, f.private), in)
	if _, e := b.store.Save(b.ctx, b.person.ID, "place", f.place); e != nil {
		t.Fatal(e)
	}
	claimed := time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, e := b.store.CreateMomentDraft(b.ctx, b.person.ID, content.MomentInput{CityID: f.city, PlaceID: f.place, Title: "合成028私人标题", Body: "合成028私人正文-不证明到访", OccurredAt: &claimed, TimePrecision: "day", LocationPrecision: "place"}); e != nil {
		t.Fatal(e)
	}
	activity, e := b.store.CreateSocialDraft(b.ctx, b.other.ID, activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: b.other.ID}, CityID: f.city, PlaceID: f.place, Title: "合成028活动", Summary: "本地报名不是到访", StartsAt: time.Now().Add(time.Hour), EndsAt: time.Now().Add(2 * time.Hour), TimeZone: "Europe/London", Visibility: "public"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.PublishSocialActivity(b.ctx, b.other.ID, activity.ID); e != nil {
		t.Fatal(e)
	}
	if _, _, e = b.store.JoinActivity(b.ctx, b.person.ID, activity.ID); e != nil {
		t.Fatal(e)
	}
	if p := f.read(t); len(p.Signals) != 0 || p.HistoricalDates != "NOT_PROVIDED" {
		t.Fatal("native text/media/RSVP/old context became city facts")
	}
}
func TestCityMemoryNativeConcurrentCASAndExpiryManagement(t *testing.T) {
	f := cityMemoryNativeFixture(t)
	b := f.private.base
	r := f.declare(t, agentcitymemory.Interested)
	var wg sync.WaitGroup
	out := make(chan error, 8)
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			in := cityInput(f.city, agentcitymemory.Interested)
			in.ExpectedVersion = 1
			in.ValidUntil = in.ValidUntil.Add(time.Duration(i) * time.Minute)
			_, e := b.store.PutOwnCityDeclaration(b.ctx, f.private.owner, r.ID, in)
			out <- e
		}(i)
	}
	wg.Wait()
	close(out)
	success, conflict := 0, 0
	for e := range out {
		if e == nil {
			success++
		} else if errors.Is(e, agentcitymemory.ErrConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 7 {
		t.Fatal("native CAS did not have sole winner", success, conflict)
	}
	id := agentMemoryID(t, f.private)
	short := cityInput(f.city, agentcitymemory.Visited)
	if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()+interval '600 milliseconds'`).Scan(&short.ValidUntil); e != nil {
		t.Fatal(e)
	}
	created, e := b.store.PutOwnCityDeclaration(b.ctx, f.private.owner, id, short)
	if e != nil {
		t.Fatal(e)
	}
	p := f.read(t)
	for {
		var now time.Time
		if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			t.Fatal(e)
		}
		if !now.Before(short.ValidUntil) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if e = b.store.RevalidateOwnCityMemory(b.ctx, f.private.owner, p); !errors.Is(e, agentcitymemory.ErrExpired) {
		t.Fatal("expired read lease renewed", e)
	}
	if len(f.read(t).Signals) != 1 {
		t.Fatal("expired declaration was current")
	}
	var version int64
	if e = b.pool.QueryRow(b.ctx, `SELECT version FROM agent_memories WHERE id=$1`, id).Scan(&version); e != nil || version != 1 {
		t.Fatal("logical read expiry wrote version", e)
	}
	b.exec(`UPDATE cities SET publication_status='hidden' WHERE id=$1`, f.city)
	gone, e := b.store.DeleteOwnCityDeclaration(b.ctx, f.private.owner, created.ID, 1)
	if e != nil || gone.Status != agentmemory.StatusDeleted {
		t.Fatal("hidden/expired target blocked human deletion", e)
	}
}
func TestCityMemoryNativeLateCityExpiryDuringMemoryRowWait(t *testing.T) {
	f := cityMemoryNativeFixture(t)
	b := f.private.base
	r := f.declare(t, agentcitymemory.Lived)
	blocker, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(context.Background())
	if _, e = blocker.Exec(b.ctx, `SELECT id FROM agent_memories WHERE id=$1 FOR UPDATE`, r.ID); e != nil {
		t.Fatal(e)
	}
	var cityDeadline time.Time
	if e = b.pool.QueryRow(b.ctx, `UPDATE cities SET expires_at=clock_timestamp()+interval '1500 milliseconds' WHERE id=$1 RETURNING expires_at`, f.city).Scan(&cityDeadline); e != nil {
		t.Fatal(e)
	}
	before := agentMemoryOwnedRowsSnapshot(t, f.private)
	in := cityInput(f.city, agentcitymemory.Lived)
	in.ExpectedVersion = 1
	in.Visibility = agentmemory.VisibilityAgentOnly
	type outcome struct {
		r agentmemory.Record
		e error
	}
	done := make(chan outcome, 1)
	go func() {
		got, e := b.store.PutOwnCityDeclaration(b.ctx, f.private.owner, r.ID, in)
		done <- outcome{got, e}
	}()
	waitUntil := time.Now().Add(4 * time.Second)
	reached := false
	for time.Now().Before(waitUntil) {
		if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_pids(pid)) AND query LIKE '%agent_memories%' AND query LIKE '%FOR UPDATE%')`, int(blocker.Conn().PgConn().PID())).Scan(&reached); e != nil {
			t.Fatal(e)
		}
		if reached {
			break
		}
		select {
		case <-done:
			t.Fatal("write never reached actual row wait")
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !reached {
		t.Fatal("actual owned Memory row wait not observed")
	}
	for {
		var now time.Time
		if e = b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			t.Fatal(e)
		}
		if !now.Before(cityDeadline) {
			break
		}
		if time.Now().After(waitUntil) {
			t.Fatal("PG clock failed to cross City deadline")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if e = blocker.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case got := <-done:
		cityWriteError(t, got.r, got.e, agentcitymemory.ErrForbidden)
	case <-time.After(4 * time.Second):
		t.Fatal("late write did not finish")
	}
	if agentMemoryOwnedRowsSnapshot(t, f.private) != before {
		t.Fatal("City expiry rejection changed complete native rows")
	}
	t.Log("ACTUAL_MEMORY_ROW_WAIT=true CITY_DEADLINE_CROSSED=true ROLLBACK_FULL_OWNED_ROWS=true")
}
func TestCityMemoryNativeNilContextPoolAndCancelledInputs(t *testing.T) {
	f := cityMemoryNativeFixture(t)
	b := f.private.base
	// A real pool isolates nil-context handling from unavailable storage.
	p, e := b.store.ReadOwnCityMemory(nil, f.private.owner, f.city)
	cityReadError(t, p, e, agentcitymemory.ErrInvalid)
	r, e := b.store.PutOwnCityDeclaration(nil, f.private.owner, agentMemoryID(t, f.private), cityInput(f.city, agentcitymemory.Lived))
	cityWriteError(t, r, e, agentcitymemory.ErrInvalid)
	for _, store := range []*Store{nil, New(nil, false)} {
		p, e := store.ReadOwnCityMemory(b.ctx, f.private.owner, f.city)
		cityReadError(t, p, e, agentcitymemory.ErrUnavailable)
	}
	ctx, cancel := context.WithCancel(b.ctx)
	cancel()
	p, e = b.store.ReadOwnCityMemory(ctx, f.private.owner, f.city)
	cityReadError(t, p, e, context.Canceled)
	p, e = b.store.ReadOwnCityMemory(b.ctx, f.private.owner, "unknown-city-028")
	cityReadError(t, p, e, agentcitymemory.ErrNotFound)
}

func TestCityMemoryNativeImmutableLeaseAndDeleteNamespaceVersion(t *testing.T) {
	f := cityMemoryNativeFixture(t)
	b := f.private.base
	r := f.declare(t, agentcitymemory.Lived)
	p := f.read(t)
	for _, change := range []string{"observed", "expires", "hash"} {
		t.Run(change, func(t *testing.T) {
			old := p
			switch change {
			case "observed":
				old.ObservedAt = old.ObservedAt.Add(time.Second)
			case "expires":
				old.ExpiresAt = old.ExpiresAt.Add(time.Second)
			case "hash":
				old.SnapshotID = strings.Repeat("a", 64)
			}
			if e := b.store.RevalidateOwnCityMemory(b.ctx, f.private.owner, old); !errors.Is(e, agentcitymemory.ErrInvalid) {
				t.Fatal("altered original lease revalidated", e)
			}
		})
	}
	before := agentMemoryOwnedRowsSnapshot(t, f.private)
	got, e := b.store.DeleteOwnCityDeclaration(b.ctx, f.private.owner, r.ID, 2)
	cityWriteError(t, got, e, agentcitymemory.ErrConflict)
	if agentMemoryOwnedRowsSnapshot(t, f.private) != before {
		t.Fatal("future expected version changed original Memory")
	}
	// The original human Memory API may replace its own type at a new version.
	in := agentMemoryInput("ordinary.preference.after.city")
	in.ExpectedVersion = 1
	changed, e := b.store.PutOwnMemory(b.ctx, f.private.owner, r.ID, in)
	if e != nil || changed.Version != 2 || changed.MemoryType != agentmemory.TypePreference {
		t.Fatal("native owner changed own source type", e)
	}
	before = agentMemoryOwnedRowsSnapshot(t, f.private)
	got, e = b.store.DeleteOwnCityDeclaration(b.ctx, f.private.owner, r.ID, 2)
	cityWriteError(t, got, e, agentcitymemory.ErrConflict)
	if agentMemoryOwnedRowsSnapshot(t, f.private) != before {
		t.Fatal("typed City path deleted a different namespace")
	}
}

func TestCityMemoryNativeLateSessionExpiryWhileMetadataWaits(t *testing.T) {
	f := cityMemoryNativeFixture(t)
	b := f.private.base
	blocker, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(context.Background())
	if _, e = blocker.Exec(b.ctx, `SELECT agent_id FROM agent_profiles WHERE agent_id=$1 FOR UPDATE`, b.personID); e != nil {
		t.Fatal(e)
	}
	var deadline time.Time
	if e = b.pool.QueryRow(b.ctx, `UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '1200 milliseconds' WHERE id=$1 RETURNING idle_expires_at`, f.private.ownerSession).Scan(&deadline); e != nil {
		t.Fatal(e)
	}
	before := agentMemoryOwnedRowsSnapshot(t, f.private)
	type outcome struct {
		p agentcitymemory.Projection
		e error
	}
	done := make(chan outcome, 1)
	go func() { p, e := b.store.ReadOwnCityMemory(b.ctx, f.private.owner, f.city); done <- outcome{p, e} }()
	reached := false
	until := time.Now().Add(4 * time.Second)
	for time.Now().Before(until) {
		if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_pids(pid)) AND query LIKE '%FROM agent_profiles%' AND query LIKE '%FOR SHARE%')`, int(blocker.Conn().PgConn().PID())).Scan(&reached); e != nil {
			t.Fatal(e)
		}
		if reached {
			break
		}
		select {
		case <-done:
			t.Fatal("read did not enter precise metadata wait")
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !reached {
		t.Fatal("metadata wait not observed")
	}
	for {
		var now time.Time
		if e = b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			t.Fatal(e)
		}
		if !now.Before(deadline) {
			break
		}
		if time.Now().After(until) {
			t.Fatal("deadline was not reached")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if e = blocker.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case result := <-done:
		cityReadError(t, result.p, result.e, agentcitymemory.ErrForbidden)
	case <-time.After(4 * time.Second):
		t.Fatal("late read did not finish")
	}
	if agentMemoryOwnedRowsSnapshot(t, f.private) != before {
		t.Fatal("expired session read changed full native rows")
	}
	t.Log("ACTUAL_METADATA_WAIT=true SESSION_DEADLINE_CROSSED=true ZERO_PAYLOAD=true")
}

func TestCityMemoryNativeRepeatableReadPoolStillUsesCurrentBridge(t *testing.T) {
	f := cityMemoryNativeFixture(t)
	b := f.private.base
	config, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	config.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"
	pool, e := pgxpool.NewWithConfig(b.ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	store := New(pool, false)
	var isolation string
	if e = pool.QueryRow(b.ctx, `SHOW default_transaction_isolation`).Scan(&isolation); e != nil || isolation != "repeatable read" {
		t.Fatal("real pool isolation setting not applied", e)
	}
	r, e := store.PutOwnCityDeclaration(b.ctx, f.private.owner, agentMemoryID(t, f.private), cityInput(f.city, agentcitymemory.Visited))
	if e != nil || r.Version != 1 {
		t.Fatal("typed writer did not override pool isolation", e)
	}
	p, e := store.ReadOwnCityMemory(b.ctx, f.private.owner, f.city)
	if e != nil || len(p.Signals) != 1 {
		t.Fatal("current human reader under actual RR pool", e)
	}
	b.exec(`UPDATE cities SET publication_status='hidden' WHERE id=$1`, f.city)
	if e = store.RevalidateOwnCityMemory(b.ctx, f.private.owner, p); !errors.Is(e, agentcitymemory.ErrNotFound) {
		t.Fatal("RR default retained hidden current City", e)
	}
}
