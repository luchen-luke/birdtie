package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ai "github.com/birdtie/birdtie/apps/api/internal/activeintent"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/follow"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type visibilityMatrixFixture struct {
	t      *testing.T
	ctx    context.Context
	pool   *pgxpool.Pool
	store  *Store
	ids    []string // owner, one-way follower, selected Close friend, ordinary friend, explicit grant, stranger, reverse follower.
	access ea.Access
	ties   map[int]string
}

func visibilityMatrixNative(t *testing.T) *visibilityMatrixFixture {
	t.Helper()
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" || os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("matrix requires an explicitly owned disposable database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var schema bool
	if err = pool.QueryRow(ctx, `SELECT to_regprocedure('public.birdtie_profile_visible_to(uuid,uuid)') IS NOT NULL AND to_regclass('public.person_close_connections') IS NOT NULL`).Scan(&schema); err != nil || !schema {
		t.Fatal("119 and118 required", err)
	}
	ids, cleanup := ownedSocialPeopleFixture(t, ctx, pool, 7)
	t.Cleanup(cleanup)
	f := &visibilityMatrixFixture{t: t, ctx: ctx, pool: pool, store: New(pool, false), ids: ids, ties: map[int]string{}}
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM follows WHERE follower_account_id=ANY($1::uuid[]) OR person_account_id=ANY($1::uuid[])`,
			`DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`,
			`DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[])`,
			`DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`,
			`DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`,
		} {
			ownedSocialFixtureCleanup(t, ctx, pool, q, ids)
		}
	})
	f.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=ANY($1::uuid[])`, ids)
	for _, pair := range [][2]int{{1, 0}, {0, 6}} {
		if _, err = f.store.Follow(ctx, ids[pair[0]], follow.Target{Type: follow.Person, ID: ids[pair[1]]}); err != nil {
			t.Fatal(err)
		}
	}
	for _, index := range []int{2, 3} {
		r, e := f.store.CreateFriendRequest(ctx, ids[0], ids[index], "合成明确好友关系")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.store.DecideRequest(ctx, ids[index], r.ID, "accept"); e != nil {
			t.Fatal(e)
		}
		var tie string
		if e = pool.QueryRow(ctx, `SELECT id FROM person_ties WHERE request_id=$1 AND status='active'`, r.ID).Scan(&tie); e != nil {
			t.Fatal(e)
		}
		f.ties[index] = tie
	}
	_, digest, err := identity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '2 hours',clock_timestamp()+interval '1 hour')`, ids[0], digest[:])
	f.access = ea.Access{Actor: identity.Actor{ID: ids[0], AccountType: "person"}, SessionDigest: digest}
	if _, err = f.store.GrantProfileRead(ctx, ids[0], ids[4], time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	f.chooseClose(2)
	return f
}

func (f *visibilityMatrixFixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(f.ctx, q, args...); err != nil {
		f.t.Fatal(err)
	}
}
func (f *visibilityMatrixFixture) chooseClose(index int) {
	f.t.Helper()
	view, e := f.store.ListOwnCloseConnectionsCurrent(f.ctx, f.access)
	if e != nil {
		f.t.Fatal(e)
	}
	for _, entry := range view.Connections {
		if entry.PersonID == f.ids[index] {
			r, e := f.store.PutOwnCloseConnectionCurrent(f.ctx, f.access, entry.PersonID, connection.ClosePut{TieID: entry.TieID, RequestID: entry.RequestID, SourceVersion: entry.SourceVersion, ValidUntil: view.ValidUntil, Confirmed: true})
			if e != nil || !r.Connection.IsClose {
				f.t.Fatal("explicit current Close choice", e)
			}
			return
		}
	}
	f.t.Fatal("selected accepted friend missing from current Close choices")
}
func (f *visibilityMatrixFixture) intent(audience string) socialintent.Record {
	f.t.Helper()
	r, e := f.store.CreateSocialIntentDraft(f.ctx, f.ids[0], socialintent.DraftInput{Type: "FIND_COMPANION", Title: "本人选择的" + audience, Audience: audience, Modality: "ONLINE", Constraints: json.RawMessage(`{}`), ExpiresAt: time.Now().Add(time.Hour)})
	if e != nil {
		f.t.Fatal(e)
	}
	r, e = f.store.ActivateSocialIntent(f.ctx, f.ids[0], r.ID)
	if e != nil {
		f.t.Fatal(e)
	}
	return r
}
func (f *visibilityMatrixFixture) profile(viewer string) bool {
	f.t.Helper()
	var permitted bool
	var input any
	if viewer != "" {
		input = viewer
	}
	if e := f.pool.QueryRow(f.ctx, `SELECT birdtie_profile_visible_to($1,$2::uuid)`, f.ids[0], input).Scan(&permitted); e != nil {
		f.t.Fatal(e)
	}
	return permitted
}
func (f *visibilityMatrixFixture) visible(viewer, id string) bool {
	f.t.Helper()
	_, e := f.store.GetVisibleSocialIntent(f.ctx, viewer, id)
	if e != nil && !errors.Is(e, socialintent.ErrNotFound) {
		f.t.Fatal(e)
	}
	return e == nil
}

func TestVisibilityMatrixNativeFiveAudiencesAndLegacyGrant(t *testing.T) {
	f := visibilityMatrixNative(t)
	intents := map[string]socialintent.Record{}
	for _, audience := range []string{"PUBLIC", "FOLLOWERS", "CONNECTIONS", "CLOSE", "PRIVATE", "FRIENDS"} {
		intents[audience] = f.intent(audience)
	}
	for _, profile := range []string{"public", "followers", "connections", "close", "private"} {
		f.exec(`UPDATE user_profiles SET visibility=$2 WHERE account_id=$1`, f.ids[0], profile)
		for _, viewer := range []struct {
			name, id string
			index    int
		}{{"anonymous", "", -1}, {"owner", f.ids[0], 0}, {"follower", f.ids[1], 1}, {"close", f.ids[2], 2}, {"friend", f.ids[3], 3}, {"grant", f.ids[4], 4}, {"stranger", f.ids[5], 5}, {"reverseFollow", f.ids[6], 6}} {
			t.Run(profile+"_"+viewer.name, func(t *testing.T) {
				wantProfile := viewer.index == 0 || viewer.index == 4 || profile == "public" || (profile == "followers" && viewer.index == 1) || (profile == "connections" && (viewer.index == 2 || viewer.index == 3)) || (profile == "close" && viewer.index == 2)
				if got := f.profile(viewer.id); got != wantProfile {
					t.Fatalf("Profile matrix got=%t want=%t", got, wantProfile)
				}
				for audience, intent := range intents {
					want := (audience == "PUBLIC" && profile == "public") || (audience == "FOLLOWERS" && viewer.index == 1) || ((audience == "CONNECTIONS" || audience == "FRIENDS") && (viewer.index == 2 || viewer.index == 3)) || (audience == "CLOSE" && viewer.index == 2)
					if got := f.visible(viewer.id, intent.ID); got != want {
						t.Fatalf("Intent=%s got=%t want=%t", audience, got, want)
					}
				}
			})
		}
	}
	// The original owner read is independent of public discovery: even a
	// private declaration stays readable only through its own owner endpoint.
	if _, e := f.store.GetOwnSocialIntent(f.ctx, f.ids[0], intents["PRIVATE"].ID); e != nil {
		t.Fatal(e)
	}
	if f.profile(f.ids[5]) || f.visible(f.ids[4], intents["CLOSE"].ID) {
		t.Fatal("grant was interpreted as Close authority")
	}
}

func TestVisibilityMatrixNativeCurrentRevocationAndFollowerBootstrap(t *testing.T) {
	f := visibilityMatrixNative(t)
	followers, connections, close := f.intent("FOLLOWERS"), f.intent("CONNECTIONS"), f.intent("CLOSE")
	f.exec(`UPDATE user_profiles SET visibility='followers' WHERE account_id=$1`, f.ids[0])
	if _, e := f.store.Follow(f.ctx, f.ids[5], follow.Target{Type: follow.Person, ID: f.ids[0]}); !errors.Is(e, follow.ErrUnavailable) {
		t.Fatal("new Follow bootstrapped its own access", e)
	}
	if _, e := f.pool.Exec(f.ctx, `INSERT INTO follows(follower_account_id,person_account_id) VALUES($1,$2)`, f.ids[5], f.ids[0]); e == nil {
		t.Fatal("SQL bypass bootstrapped follower")
	}
	if _, e := f.store.Follow(f.ctx, f.ids[1], follow.Target{Type: follow.Person, ID: f.ids[0]}); e != nil {
		t.Fatal("existing follower lost idempotent follow", e)
	}
	if e := f.store.Unfollow(f.ctx, f.ids[1], follow.Target{Type: follow.Person, ID: f.ids[0]}); e != nil {
		t.Fatal(e)
	}
	if f.profile(f.ids[1]) || f.visible(f.ids[1], followers.ID) {
		t.Fatal("unfollow retained follower read")
	}
	f.exec(`UPDATE user_profiles SET visibility='close' WHERE account_id=$1`, f.ids[0])
	if !f.profile(f.ids[2]) || f.profile(f.ids[3]) {
		t.Fatal("Close is not directional subset")
	}
	if e := f.store.RemoveTie(f.ctx, f.ids[0], f.ties[2]); e != nil {
		t.Fatal(e)
	}
	if f.profile(f.ids[2]) || f.visible(f.ids[2], connections.ID) || f.visible(f.ids[2], close.ID) {
		t.Fatal("removed accepted Tie retained access")
	}
	f.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, f.ids[0])
	r, e := f.store.CreateFriendRequest(f.ctx, f.ids[0], f.ids[2], "本人重新申请好友")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.DecideRequest(f.ctx, f.ids[2], r.ID, "accept"); e != nil {
		t.Fatal(e)
	}
	if !f.visible(f.ids[2], connections.ID) || f.visible(f.ids[2], close.ID) {
		t.Fatal("new accepted relation silently revived old Close")
	}
	f.chooseClose(2)
	if !f.visible(f.ids[2], close.ID) {
		t.Fatal("fresh explicit Close selection missing")
	}
	if e = f.store.BlockAccount(f.ctx, f.ids[2], f.ids[0]); e != nil {
		t.Fatal(e)
	}
	if f.profile(f.ids[2]) || f.visible(f.ids[2], connections.ID) || f.visible(f.ids[2], close.ID) {
		t.Fatal("reverse Block did not revoke")
	}
	// Restore only the block edge: explicit Close remains retired.
	if e = f.store.UnblockAccount(f.ctx, f.ids[2], f.ids[0]); e != nil {
		t.Fatal(e)
	}
	if f.visible(f.ids[2], close.ID) {
		t.Fatal("unblock revived Close")
	}
	f.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.ids[3])
	if f.profile(f.ids[3]) || f.visible(f.ids[3], connections.ID) {
		t.Fatal("inactive viewer retained relationship read")
	}
	f.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.ids[0])
	if f.profile("") || f.profile(f.ids[4]) || f.visible(f.ids[3], connections.ID) {
		t.Fatal("inactive owner/grant retained read")
	}
}

func TestVisibilityMatrixNative119GuardedDownAndOriginalDefinitions(t *testing.T) {
	ownedMigrationDatabase(t)
	pool := newPeopleTestPool(t)
	ctx := context.Background()
	independentClose := visibilityMatrixNative(t)
	var closeBefore string
	if e := pool.QueryRow(ctx, `SELECT row_to_json(c)::text FROM person_close_connections c WHERE owner_account_id=$1 AND person_account_id=$2`, independentClose.ids[0], independentClose.ids[2]).Scan(&closeBefore); e != nil {
		t.Fatal(e)
	}
	down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "119_visibility_matrix.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	up, e := os.ReadFile(filepath.Join("..", "..", "migrations", "119_visibility_matrix.sql"))
	if e != nil {
		t.Fatal(e)
	}
	conn, e := pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	exec := func(raw []byte) error {
		_, err := conn.Exec(ctx, string(raw))
		if err != nil {
			_, _ = conn.Exec(ctx, "ROLLBACK")
		}
		return err
	}
	if e = exec(down); e != nil {
		t.Fatal("empty119 down", e)
	}
	var closeAfter string
	if e = pool.QueryRow(ctx, `SELECT row_to_json(c)::text FROM person_close_connections c WHERE owner_account_id=$1 AND person_account_id=$2`, independentClose.ids[0], independentClose.ids[2]).Scan(&closeAfter); e != nil || closeBefore != closeAfter {
		t.Fatal("119 down changed independent118 Close selection", e)
	}
	var oldIntent, oldFollow string
	if e = conn.QueryRow(ctx, `SELECT pg_get_functiondef('birdtie_social_intent_visible_to(uuid,uuid)'::regprocedure),pg_get_functiondef('birdtie_validate_follow()'::regprocedure)`).Scan(&oldIntent, &oldFollow); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(oldIntent, "WHEN 'FOLLOWERS'") || strings.Contains(oldIntent, "birdtie_close_connection_visible") || !strings.Contains(oldFollow, "p.visibility='public'") {
		t.Fatal("119 did not restore original037/047 semantics")
	}
	if e = exec(up); e != nil {
		t.Fatal("119 reapply", e)
	}
	ids, cleanup := ownedSocialPeopleFixture(t, ctx, pool, 1)
	defer cleanup()
	for _, profile := range []string{"followers", "connections", "close"} {
		if _, e = pool.Exec(ctx, `UPDATE user_profiles SET visibility=$2 WHERE account_id=$1`, ids[0], profile); e != nil {
			t.Fatal(e)
		}
		if e = exec(down); e == nil {
			t.Fatal("used Profile tier down accepted", profile)
		}
		var actual string
		if e = pool.QueryRow(ctx, `SELECT visibility FROM user_profiles WHERE account_id=$1`, ids[0]).Scan(&actual); e != nil || actual != profile {
			t.Fatal("used down coerced actual data", e)
		}
	}
	if _, e = pool.Exec(ctx, `UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, ids[0]); e != nil {
		t.Fatal(e)
	}
	for _, audience := range []string{"FOLLOWERS", "CONNECTIONS", "CLOSE"} {
		var id string
		if e = pool.QueryRow(ctx, `INSERT INTO social_intents(creator_account_id,intent_type,title,audience,modality,expires_at) VALUES($1,'FIND_COMPANION','合成真实档位',$2,'ONLINE',now()+interval '1 hour') RETURNING id`, ids[0], audience).Scan(&id); e != nil {
			t.Fatal(e)
		}
		if e = exec(down); e == nil {
			t.Fatal("used Intent tier down accepted", audience)
		}
		if _, e = pool.Exec(ctx, `DELETE FROM social_intents WHERE id=$1`, id); e != nil {
			t.Fatal(e)
		}
	}
	// Source guards reject tiers for organization owners even via direct SQL.
	var organization string
	if e = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'organization') RETURNING id`).Scan(&organization); e != nil {
		t.Fatal(e)
	}
	defer ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM accounts WHERE id=$1`, organization)
	if _, e = pool.Exec(ctx, `INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'合成组织','close')`, organization); e == nil {
		t.Fatal("organization directSQL gained relationship tier")
	}
	if _, e = pool.Exec(ctx, `UPDATE user_profiles SET visibility='close' WHERE account_id=$1`, ids[0]); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `UPDATE accounts SET account_type='organization' WHERE id=$1`, ids[0]); e == nil {
		t.Fatal("owner type change bypassed visibility guard")
	}
}

func TestVisibilityMatrixNativeFollowerSourceABARejectsOldPreviewAndReceipt(t *testing.T) {
	t.Run("owner publication preview", func(t *testing.T) {
		f, a, g, r := activeNative(t)
		b := f.private.base
		b.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=ANY($1::uuid[])`, []string{b.person.ID, b.other.ID})
		target := follow.Target{Type: follow.Person, ID: b.person.ID}
		if _, e := b.store.Follow(b.ctx, b.other.ID, target); e != nil {
			t.Fatal(e)
		}
		d, e := g.ReadOwn(b.ctx, a, r.ID)
		if e != nil {
			t.Fatal(e)
		}
		edit := activeDraft(d.Item.Intent)
		edit.Audience = "FOLLOWERS"
		p, e := g.PreviewOwn(b.ctx, a, r.ID, ai.Input{Operation: "EDIT", ExpectedVersion: d.Item.Version, Edit: &edit})
		if e != nil {
			t.Fatal(e)
		}
		v, e := g.ApproveOwn(b.ctx, a, r.ID, p.PreviewID)
		if e != nil {
			t.Fatal(e)
		}
		p, e = g.PreviewOwn(b.ctx, a, r.ID, ai.Input{Operation: "ACTIVATE", ExpectedVersion: v.Item.Version})
		if e != nil {
			t.Fatal(e)
		}
		if e = b.store.Unfollow(b.ctx, b.other.ID, target); e != nil {
			t.Fatal(e)
		}
		if _, e = b.store.Follow(b.ctx, b.other.ID, target); e != nil {
			t.Fatal(e)
		}
		if _, e = g.ApproveOwn(b.ctx, a, r.ID, p.PreviewID); !errors.Is(e, ai.ErrConflict) {
			t.Fatal("changed audience recipient source accepted old approval", e)
		}
		current, e := g.ReadOwn(b.ctx, a, r.ID)
		if e != nil || current.Item.Intent.Status != "DRAFT" {
			t.Fatal("stale audience publication effected", e)
		}
	})
	t.Run("current candidate receipt", func(t *testing.T) {
		f, a, rows := humanNewPeopleFixture(t)
		target := follow.Target{Type: follow.Person, ID: f.ids[1]}
		if _, e := f.store.Follow(f.ctx, f.ids[0], target); e != nil {
			t.Fatal(e)
		}
		f.exec(`UPDATE social_intents SET audience='FOLLOWERS',updated_at=clock_timestamp() WHERE id=$1`, rows[1].ID)
		r, e := f.store.ReadHumanNewPeople(f.ctx, a.Actor, a.Digest, rows[0].ID)
		if e != nil || len(r.Response.Candidates) != 1 {
			t.Fatal("actual follower candidate missing", e)
		}
		if e = f.store.Unfollow(f.ctx, f.ids[0], target); e != nil {
			t.Fatal(e)
		}
		if _, e = f.store.Follow(f.ctx, f.ids[0], target); e != nil {
			t.Fatal(e)
		}
		if e = f.store.RevalidateHumanNewPeople(f.ctx, a.Actor, a.Digest, r); e == nil {
			t.Fatal("Follow ABA retained old candidate source")
		}
		fresh, e := f.store.ReadHumanNewPeople(f.ctx, a.Actor, a.Digest, rows[0].ID)
		if e != nil || len(fresh.Response.Candidates) != 1 {
			t.Fatal("fresh follower candidate read failed", e)
		}
	})
}

func TestVisibilityMatrixNativeExpirationAfterActualStatementWait(t *testing.T) {
	f := visibilityMatrixNative(t)
	r := f.intent("PUBLIC")
	// Both checks execute after a real database wait in the same SQL statement.
	// Transaction/statement start timestamps would preserve already expired ACL.
	f.exec(`UPDATE consent_grants SET expires_at=clock_timestamp()+interval '150 milliseconds' WHERE owner_account_id=$1 AND recipient_account_id=$2 AND purpose='profile_view'`, f.ids[0], f.ids[4])
	f.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, f.ids[0])
	if !f.profile(f.ids[4]) {
		t.Fatal("unexpired grant positive control")
	}
	var granted bool
	if e := f.pool.QueryRow(f.ctx, `WITH actual_wait AS MATERIALIZED(SELECT pg_sleep(0.22)) SELECT birdtie_profile_visible_to($1,$2) FROM actual_wait`, f.ids[0], f.ids[4]).Scan(&granted); e != nil || granted {
		t.Fatal("grant expired during statement wait retained access", e)
	}
	f.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, f.ids[0])
	f.exec(`UPDATE social_intents SET expires_at=clock_timestamp()+interval '150 milliseconds' WHERE id=$1`, r.ID)
	if !f.visible("", r.ID) {
		t.Fatal("unexpired Intent positive control")
	}
	var visible bool
	if e := f.pool.QueryRow(f.ctx, `WITH actual_wait AS MATERIALIZED(SELECT pg_sleep(0.22)) SELECT birdtie_social_intent_visible_to($1,NULL) FROM actual_wait`, r.ID).Scan(&visible); e != nil || visible {
		t.Fatal("Intent expired during statement wait retained access", e)
	}
}

func TestVisibilityMatrixNativeCloseRecipientABARejectsOldPublication(t *testing.T) {
	f, a, g, r := activeNative(t)
	b := f.private.base
	b.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=ANY($1::uuid[])`, []string{b.person.ID, b.other.ID})
	request, e := b.store.CreateFriendRequest(b.ctx, b.person.ID, b.other.ID, "本人明确的当前好友")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.DecideRequest(b.ctx, b.other.ID, request.ID, "accept"); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM person_ties WHERE request_id=$1`, `DELETE FROM inbox_items WHERE resource_id=$1::uuid`, `DELETE FROM connection_requests WHERE id=$1`} {
			if _, e := b.pool.Exec(b.ctx, q, request.ID); e != nil {
				t.Error(e)
			}
		}
	})
	access := ea.Access{Actor: a.Actor, SessionDigest: a.SessionDigest}
	selectCurrent := func() connection.CloseEntry {
		v, e := b.store.ListOwnCloseConnectionsCurrent(b.ctx, access)
		if e != nil {
			t.Fatal(e)
		}
		for _, entry := range v.Connections {
			if entry.PersonID == b.other.ID {
				r, e := b.store.PutOwnCloseConnectionCurrent(b.ctx, access, entry.PersonID, connection.ClosePut{TieID: entry.TieID, RequestID: entry.RequestID, SourceVersion: entry.SourceVersion, ValidUntil: v.ValidUntil, Confirmed: true})
				if e != nil {
					t.Fatal(e)
				}
				return r.Connection
			}
		}
		t.Fatal("accepted friend absent")
		return connection.CloseEntry{}
	}
	selected := selectCurrent()
	d, e := g.ReadOwn(b.ctx, a, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	edit := activeDraft(d.Item.Intent)
	edit.Audience = "CLOSE"
	p, e := g.PreviewOwn(b.ctx, a, r.ID, ai.Input{Operation: "EDIT", ExpectedVersion: d.Item.Version, Edit: &edit})
	if e != nil {
		t.Fatal(e)
	}
	v, e := g.ApproveOwn(b.ctx, a, r.ID, p.PreviewID)
	if e != nil {
		t.Fatal(e)
	}
	p, e = g.PreviewOwn(b.ctx, a, r.ID, ai.Input{Operation: "ACTIVATE", ExpectedVersion: v.Item.Version})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.DeleteOwnCloseConnectionCurrent(b.ctx, access, selected.PersonID, connection.CloseDelete{TieID: selected.TieID, RequestID: selected.RequestID, ExpectedRevision: selected.NativeRevision, Confirmed: true}); e != nil {
		t.Fatal(e)
	}
	selectCurrent()
	if _, e = g.ApproveOwn(b.ctx, a, r.ID, p.PreviewID); !errors.Is(e, ai.ErrConflict) {
		t.Fatal("Close unselect/reselect accepted old concrete publication", e)
	}
	current, e := g.ReadOwn(b.ctx, a, r.ID)
	if e != nil || current.Item.Intent.Status != "DRAFT" {
		t.Fatal("stale Close publication effected", e)
	}
}

func TestVisibilityMatrixNativeCurrentHumanIntentOwnerType(t *testing.T) {
	f := visibilityMatrixNative(t)
	r := f.intent("PUBLIC")
	if !f.visible("", r.ID) {
		t.Fatal("actual Person public Intent positive control")
	}
	f.exec(`UPDATE accounts SET account_type='organization' WHERE id=$1`, f.ids[0])
	if f.visible("", r.ID) || f.visible(f.ids[5], r.ID) {
		t.Fatal("current non-Person retained old human Intent visibility")
	}
	if !f.profile("") || !f.profile(f.ids[4]) {
		t.Fatal("human Intent owner eligibility incorrectly removed independent organization public/granted Profile access")
	}
}

// These are real independent SQL transactions, with a native lock-wait
// positive control. A Person without an Agent composite FK must still preserve
// the ordinary Profile relationship audience invariant in either lock order.
func TestVisibilityMatrixNativeRelationshipOwnerConcurrentGuard(t *testing.T) {
	for _, order := range []string{"profile_first", "account_first"} {
		t.Run(order, func(t *testing.T) {
			f := visibilityMatrixNative(t)
			var agents int
			if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agents WHERE principal_account_id=$1`, f.ids[0]).Scan(&agents); e != nil || agents != 0 {
				t.Fatal("independent owned Profile owner must have no Agent binding", e, agents)
			}
			holder, e := f.pool.BeginTx(f.ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			if e != nil {
				t.Fatal(e)
			}
			defer holder.Rollback(context.Background())
			profileSQL := `UPDATE user_profiles SET visibility='followers' WHERE account_id=$1`
			accountSQL := `UPDATE accounts SET account_type='organization' WHERE id=$1`
			first, second := profileSQL, accountSQL
			if order == "account_first" {
				first, second = accountSQL, profileSQL
			}
			if _, e = holder.Exec(f.ctx, first, f.ids[0]); e != nil {
				t.Fatal("first ordinary identity/Profile transaction", e)
			}
			workerCtx, cancelWorker := context.WithTimeout(f.ctx, 15*time.Second)
			defer cancelWorker()
			waiter, e := f.pool.BeginTx(workerCtx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			if e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			received := false
			defer func() {
				cancelWorker()
				_ = holder.Rollback(context.Background())
				if !received {
					select {
					case <-done:
					case <-time.After(3 * time.Second):
						t.Error("owned waiting statement did not terminate after cancellation")
					}
				}
				_ = waiter.Rollback(context.Background())
			}()
			go func() {
				_, e := waiter.Exec(workerCtx, second, f.ids[0])
				done <- e
			}()
			waitCtx, cancelWait := context.WithTimeout(f.ctx, 8*time.Second)
			defer cancelWait()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case e = <-done:
					received = true
					t.Fatal("concurrent write did not reach the required native lock wait", order, e)
				default:
				}
				var waiting bool
				e = f.pool.QueryRow(waitCtx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a
				  WHERE a.datname=current_database() AND a.pid=$1 AND a.wait_event_type='Lock'
				  AND $2=ANY(pg_blocking_pids(a.pid))
				  AND EXISTS(SELECT 1 FROM pg_locks l WHERE l.pid=a.pid AND NOT l.granted))`,
					int(waiter.Conn().PgConn().PID()), int(holder.Conn().PgConn().PID())).Scan(&waiting)
				if e != nil {
					t.Fatal("native wait observation failed", e)
				}
				if waiting {
					t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY actual identity-key lock wait observed", order)
					break
				}
				select {
				case <-waitCtx.Done():
					t.Fatal("required native identity-key lock wait not observed", order)
				case <-ticker.C:
				}
			}
			if e = holder.Commit(f.ctx); e != nil {
				t.Fatal("first native write commit", e)
			}
			select {
			case e = <-done:
				received = true
			case <-workerCtx.Done():
				t.Fatal("waiting native guard did not finish", order)
			}
			var pgErr *pgconn.PgError
			if !errors.As(e, &pgErr) || pgErr.Code != "23514" {
				t.Fatal("current relationship owner guard did not reject after actual wait", order, e)
			}
			var accountType, visibility string
			if e = f.pool.QueryRow(f.ctx, `SELECT a.account_type,p.visibility FROM accounts a JOIN user_profiles p ON p.account_id=a.id WHERE a.id=$1`, f.ids[0]).Scan(&accountType, &visibility); e != nil {
				t.Fatal(e)
			}
			wantType, wantVisibility := "person", "followers"
			if order == "account_first" {
				wantType, wantVisibility = "organization", "public"
			}
			if accountType != wantType || visibility != wantVisibility {
				t.Fatal("ordinary Profile identity invariant corrupted", accountType, visibility)
			}
		})
	}
}
