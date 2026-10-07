package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	sc "github.com/birdtie/birdtie/apps/api/internal/socialcontext"
	"github.com/jackc/pgx/v5/pgxpool"
)

type sharedHistoryFixture struct {
	b                                         *agentProfileFixture
	access                                    sc.CurrentAccess
	session, city, place, activity, community string
}

func sharedHistoryNative(t *testing.T) *sharedHistoryFixture {
	t.Helper()
	ownedMigrationDatabase(t)
	// The child database's original full rows must survive every fixture cleanup.
	ctx := context.Background()
	check, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	digest := func() string {
		var v string
		e := check.QueryRow(ctx, `SELECT coalesce(jsonb_object_agg(tablename,body ORDER BY tablename),'{}')::text FROM (SELECT tablename, (xpath('/row/body/text()',query_to_xml(format('SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text),''[]'')::text body FROM public.%I x',tablename),false,true,'')))[1]::text body FROM pg_tables WHERE schemaname='public') x`).Scan(&v)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	before := digest()
	t.Cleanup(func() {
		if after := digest(); after != before {
			t.Error("child complete original public rows changed after owned cleanup")
		}
		check.Close()
	})
	p := agentPrivateTestFixture(t)
	b := p.base
	f := &sharedHistoryFixture{b: b, session: p.ownerSession, access: sc.CurrentAccess{ViewerID: b.person.ID, TargetID: b.other.ID, SessionDigest: p.owner.SessionDigest}}
	f.city = "shared-" + strings.ReplaceAll(b.person.ID, "-", "")
	b.exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES($1,'合成共同信息城市','LOCAL','GB','Europe/London','published','LOCAL_SYNTHETIC','local:ACTN003','合成维护者',$2)`, f.city, b.other.ID)
	if e = b.pool.QueryRow(b.ctx, `INSERT INTO places(id,city_id,name,category_code,latitude,longitude,location_precision,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES(gen_random_uuid(),$1,'活动关联公开地点','sports',57.15,-2.1,'point','published','LOCAL_SYNTHETIC','local:ACTN003','合成维护者',$2) RETURNING id`, f.city, b.other.ID).Scan(&f.place); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		c, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		for _, q := range []string{
			`DELETE FROM activity_plans WHERE owner_account_id=ANY($1::uuid[])`,
			`DELETE FROM activity_participations WHERE participant_account_id=ANY($1::uuid[])`,
			`DELETE FROM activity_invitations WHERE activity_id IN(SELECT id FROM activities WHERE created_by_account_id=ANY($1::uuid[]))`,
			`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM activities WHERE created_by_account_id=ANY($1::uuid[])`,
			`UPDATE communities SET lifecycle_status='archived' WHERE owner_account_id=ANY($1::uuid[])`,
			`DELETE FROM community_memberships WHERE community_id IN(SELECT id FROM communities WHERE owner_account_id=ANY($1::uuid[]))`,
			`DELETE FROM communities WHERE owner_account_id=ANY($1::uuid[])`,
			`DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`,
			`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM places WHERE maintainer_account_id=ANY($1::uuid[])`,
			`DELETE FROM cities WHERE maintainer_account_id=ANY($1::uuid[])`,
		} {
			if _, e := b.pool.Exec(c, q, b.accounts); e != nil {
				t.Error("shared history exclusive cleanup", e)
			}
		}
	})
	var now time.Time
	if e = b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	a, e := b.store.CreateSocialDraft(b.ctx, b.person.ID, activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: b.person.ID}, CityID: f.city, PlaceID: f.place, Modality: "in_person", PhysicalPlaceStatus: "confirmed", Title: "共同报名的合成活动", StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour), TimeZone: "Europe/London", Visibility: "public"})
	if e != nil {
		t.Fatal(e)
	}
	f.activity = a.ID
	if _, e = b.store.PublishSocialActivity(b.ctx, b.person.ID, a.ID); e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{b.person.ID, b.other.ID} {
		if _, _, e = b.store.JoinActivity(b.ctx, id, a.ID); e != nil {
			t.Fatal(e)
		}
	}
	comm, e := b.store.CreateSocialCommunity(b.ctx, b.person.ID, community.SocialInput{Name: "共同公开社群", Visibility: "public", JoinPolicy: "open"})
	if e != nil {
		t.Fatal(e)
	}
	f.community = comm.ID
	if _, e = b.store.JoinSocialCommunity(b.ctx, b.other.ID, comm.ID); e != nil {
		t.Fatal(e)
	}
	return f
}
func (f *sharedHistoryFixture) flags(t *testing.T, on bool) {
	t.Helper()
	for _, id := range []string{f.b.person.ID, f.b.other.ID} {
		if _, e := f.b.store.SetSocialDisclosure(f.b.ctx, id, sc.Disclosure{MutualTies: on, SharedCommunities: on, SharedActivities: on}); e != nil {
			t.Fatal(e)
		}
	}
}
func (f *sharedHistoryFixture) read(t *testing.T) (sc.Signals, sc.CurrentValidation) {
	t.Helper()
	v, c, e := f.b.store.SharedSocialContextCurrent(f.b.ctx, f.access)
	if e != nil || c == nil || sc.ValidateCurrent(v, f.access) != nil {
		t.Fatal("actual native history", e)
	}
	return v, c
}
func TestSharedHistoryNativeDerivedFactsDefaultPrivateAndNoWrites(t *testing.T) {
	f := sharedHistoryNative(t)
	v, c := f.read(t)
	if len(v.Activities) != 0 || len(v.Communities) != 0 || len(v.Places) != 0 {
		t.Fatal("default off leaked", v)
	}
	if e := c(f.b.ctx); e != nil {
		t.Fatal(e)
	}
	f.flags(t, true)
	v, c = f.read(t)
	if len(v.Activities) != 1 || v.Activities[0].ID != f.activity || len(v.Communities) != 1 || v.Communities[0].ID != f.community || len(v.Places) != 1 || v.Places[0].ID != f.place || v.Places[0].ActivityIDs[0] != f.activity || v.Attendance != "UNKNOWN" || v.Visit != "UNKNOWN" {
		t.Fatalf("native persisted facts: %+v", v)
	}
	var before, after string
	q := `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(p) ORDER BY p.id) FROM activity_participations p WHERE participant_account_id=ANY($1::uuid[])),(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM audit_events a WHERE actor_account_id=ANY($1::uuid[])))::text`
	if e := f.b.pool.QueryRow(f.b.ctx, q, f.b.accounts).Scan(&before); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		f.read(t)
		if e := c(f.b.ctx); e != nil {
			t.Fatal(e)
		}
	}
	if e := f.b.pool.QueryRow(f.b.ctx, q, f.b.accounts).Scan(&after); e != nil || after != before {
		t.Fatal("GET wrote domain", e)
	}
	raw, _ := json.Marshal(v)
	for _, bad := range []string{"latitude", "longitude", "session", "token", "参加过", "到访", "participantAccountId"} {
		if strings.Contains(string(raw), bad) {
			t.Fatal("projection leaked or invented", bad)
		}
	}
}
func TestSharedHistoryNativeChangesAndABA(t *testing.T) {
	for _, kind := range []string{"optout", "profile", "activity", "place", "membership", "cancel", "blockForward", "blockReverse", "session"} {
		t.Run(kind, func(t *testing.T) {
			f := sharedHistoryNative(t)
			f.flags(t, true)
			_, c := f.read(t)
			b := f.b
			switch kind {
			case "optout":
				f.flags(t, false)
				f.flags(t, true)
			case "profile":
				b.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, b.other.ID)
				b.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, b.other.ID)
			case "activity":
				b.exec(`UPDATE activities SET title=title||'B' WHERE id=$1`, f.activity)
				b.exec(`UPDATE activities SET title='共同报名的合成活动' WHERE id=$1`, f.activity)
			case "place":
				b.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place)
				b.exec(`UPDATE places SET publication_status='published' WHERE id=$1`, f.place)
			case "membership":
				b.exec(`UPDATE community_memberships SET status='pending' WHERE community_id=$1 AND user_account_id=$2`, f.community, b.other.ID)
			case "cancel":
				if _, e := b.store.CancelParticipation(b.ctx, b.other.ID, f.activity); e != nil {
					t.Fatal(e)
				}
			case "blockForward":
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, b.person.ID, b.other.ID)
			case "blockReverse":
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($2,$1)`, b.person.ID, b.other.ID)
			case "session":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
			}
			e := c(b.ctx)
			if !errors.Is(e, sc.ErrChanged) && !errors.Is(e, sc.ErrNotFound) && !errors.Is(e, identity.ErrUnauthorized) {
				t.Fatal("stale native frame accepted", kind, e)
			}
			if kind == "cancel" {
				v, _ := f.read(t)
				if len(v.Activities) != 0 || len(v.Places) != 0 {
					t.Fatal("cancel retained shared activity/place")
				}
			}
		})
	}
}
func TestSharedHistoryNativeNaturalCityExpiry(t *testing.T) {
	f := sharedHistoryNative(t)
	f.flags(t, true)
	var deadline time.Time
	if e := f.b.pool.QueryRow(f.b.ctx, `UPDATE cities SET expires_at=clock_timestamp()+interval '1 second' WHERE id=$1 RETURNING expires_at`, f.city).Scan(&deadline); e != nil {
		t.Fatal(e)
	}
	v, c := f.read(t)
	if v.ValidUntil.After(deadline) {
		t.Fatal("source deadline omitted")
	}
	for {
		var elapsed bool
		if e := f.b.pool.QueryRow(f.b.ctx, `SELECT clock_timestamp()>=$1`, deadline).Scan(&elapsed); e != nil {
			t.Fatal(e)
		}
		if elapsed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if e := c(f.b.ctx); !errors.Is(e, sc.ErrChanged) {
		t.Fatal("natural current expiry accepted", e)
	}
	v, _ = f.read(t)
	if len(v.Activities) != 0 || len(v.Places) != 0 || len(v.Communities) != 1 {
		t.Fatal("expired city projection", v)
	}
}
func TestSharedHistoryNativeFilteredSourceABA(t *testing.T) {
	f := sharedHistoryNative(t)
	f.flags(t, true)
	f.b.exec(`UPDATE activities SET visibility='invite_only' WHERE id=$1`, f.activity)
	v, c := f.read(t)
	if len(v.Activities) != 0 || len(v.Places) != 0 {
		t.Fatal("private participation leaked")
	}
	f.b.exec(`UPDATE activities SET title=title||' changed' WHERE id=$1`, f.activity)
	if e := c(f.b.ctx); !errors.Is(e, sc.ErrChanged) {
		t.Fatal("filtered candidate epoch omitted", e)
	}
}
func TestSharedHistoryNativeActualRelationWaitCurrentBoundaries(t *testing.T) {
	for _, kind := range []string{"revoke", "optoutABA", "placeABA", "cityExpiry", "sessionExpiry"} {
		t.Run(kind, func(t *testing.T) {
			f := sharedHistoryNative(t)
			f.flags(t, true)
			var deadline time.Time
			if kind == "cityExpiry" {
				if e := f.b.pool.QueryRow(f.b.ctx, `UPDATE cities SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING expires_at`, f.city).Scan(&deadline); e != nil {
					t.Fatal(e)
				}
			}
			if kind == "sessionExpiry" {
				if e := f.b.pool.QueryRow(f.b.ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() n) UPDATE sessions SET idle_expires_at=stamp.n+interval '2 seconds' FROM stamp WHERE id=$1 RETURNING idle_expires_at`, f.session).Scan(&deadline); e != nil {
					t.Fatal(e)
				}
			}
			_, check := f.read(t)
			ctx, stop := context.WithTimeout(f.b.ctx, 10*time.Second)
			defer stop()
			lock, e := f.b.pool.Acquire(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer lock.Release()
			tx, e := lock.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(context.Background())
			var pid int
			if e = tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
				t.Fatal(e)
			}
			if _, e = tx.Exec(ctx, `LOCK TABLE places IN ACCESS EXCLUSIVE MODE`); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() { done <- check(ctx) }()
			defer func() {
				stop()
				_ = tx.Rollback(context.Background())
				select {
				case <-done:
				case <-time.After(2 * time.Second):
				}
			}()
			for {
				var waiting bool
				e = f.b.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a WHERE a.datname=current_database() AND $1=ANY(pg_blocking_pids(a.pid)) AND a.query LIKE '%shared_history_relations%' AND EXISTS(SELECT 1 FROM pg_locks l WHERE l.pid=a.pid AND l.relation='places'::regclass AND NOT l.granted))`, pid).Scan(&waiting)
				if e != nil {
					t.Fatal(e)
				}
				if waiting {
					break
				}
				select {
				case e := <-done:
					t.Fatal("read ended before exact relation wait", e)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(5 * time.Millisecond):
				}
			}
			switch kind {
			case "revoke":
				f.b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
			case "optoutABA":
				f.flags(t, false)
				f.flags(t, true)
			case "placeABA":
				if _, e = tx.Exec(ctx, `UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place); e != nil {
					t.Fatal(e)
				}
				if _, e = tx.Exec(ctx, `UPDATE places SET publication_status='published' WHERE id=$1`, f.place); e != nil {
					t.Fatal(e)
				}
			case "cityExpiry", "sessionExpiry":
				for {
					var elapsed bool
					if e = f.b.pool.QueryRow(ctx, `SELECT clock_timestamp()>=$1`, deadline).Scan(&elapsed); e != nil {
						t.Fatal(e)
					}
					if elapsed {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			if e = tx.Commit(ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e = <-done:
				want := sc.ErrChanged
				if kind == "revoke" || kind == "sessionExpiry" {
					want = identity.ErrUnauthorized
				}
				if !errors.Is(e, want) {
					t.Fatal("current boundary after real relation wait", kind, e, want)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}
func TestSharedHistoryNativeOnlineAndTBDNotPhysicalPlace(t *testing.T) {
	for _, kind := range []string{"online", "tbd", "unspecified"} {
		t.Run(kind, func(t *testing.T) {
			f := sharedHistoryNative(t)
			f.flags(t, true)
			if kind == "tbd" {
				f.b.exec(`UPDATE activities SET physical_place_status='tbd',place_id=NULL,venue_place_id=NULL WHERE id=$1`, f.activity)
			} else {
				f.b.exec(`UPDATE activities SET modality=$2,physical_place_status=CASE WHEN $2='unspecified' THEN 'unknown' ELSE 'not_applicable' END,place_id=NULL,venue_place_id=NULL WHERE id=$1`, f.activity, kind)
			}
			v, _ := f.read(t)
			if len(v.Activities) != 1 || len(v.Places) != 0 {
				t.Fatal("unknown or online reference claimed physical place", kind, v)
			}
		})
	}
}
func TestSharedHistoryNativeLegalBlockUnblockABA(t *testing.T) {
	f := sharedHistoryNative(t)
	f.flags(t, true)
	v, c := f.read(t)
	if len(v.Activities) != 1 {
		t.Fatal("setup")
	}
	if e := f.b.store.BlockAccount(f.b.ctx, f.b.person.ID, f.b.other.ID); e != nil {
		t.Fatal(e)
	}
	if e := f.b.store.UnblockAccount(f.b.ctx, f.b.person.ID, f.b.other.ID); e != nil {
		t.Fatal(e)
	}
	if e := c(f.b.ctx); !errors.Is(e, sc.ErrChanged) {
		t.Fatal("old projection survived real BlockAccount/UnblockAccount ABA", e)
	}
	v, _ = f.read(t)
	if len(v.Activities) != 1 {
		t.Fatal("fresh reopened public source incorrectly rejected")
	}
}
