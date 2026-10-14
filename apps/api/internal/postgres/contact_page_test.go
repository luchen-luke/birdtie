package postgres

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/follow"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func contactPageNativeWalk[T any](t *testing.T, s *Store, ctx context.Context, a ea.Access, kind, cursor string, load func(context.Context, ea.Access, connection.PageRequest) (connection.ContactPage[T], error), id func(T) string) []string {
	t.Helper()
	seen := []string{}
	for turn := 0; turn < 12; turn++ {
		in := connection.PageRequest{Limit: 20, Cursor: cursor}
		page, e := load(ctx, a, in)
		if e != nil {
			t.Fatal("native page", kind, e)
		}
		if e = s.ValidateContactPageResponse(ctx, a, kind, in, page); e != nil {
			t.Fatal("native final source proof", kind, e)
		}
		for _, row := range page.Items {
			seen = append(seen, id(row))
		}
		if page.NextCursor == nil {
			return seen
		}
		if *page.NextCursor == cursor {
			t.Fatal("cursor loop")
		}
		cursor = *page.NextCursor
	}
	t.Fatal("bounded fixture did not terminate")
	return nil
}

// The factory creates a random owned child and migrates only that child. The
// supplied runtime parent is read solely by its existing schema guard checks.
func TestContactPageNativeBeyond100CurrentPermissionsAndDirectRequest(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newNativeEntityShareFixture(t)
	b := f.f.private.base
	a := ea.Access{Actor: f.a.actor, SessionDigest: f.a.digest}
	other := ea.Access{Actor: f.b.actor, SessionDigest: f.b.digest}
	old := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	peers, requests, ties, follows := []string{}, []string{}, []string{}, []string{}
	t.Cleanup(func() {
		for _, v := range []struct {
			sql string
			ids []string
		}{
			{`DELETE FROM follows WHERE id=ANY($1::uuid[])`, follows},
			{`DELETE FROM person_ties WHERE id=ANY($1::uuid[])`, ties},
			{`DELETE FROM connection_requests WHERE id=ANY($1::uuid[])`, requests},
			{`DELETE FROM account_blocks WHERE blocker_account_id=$2 AND blocked_account_id=ANY($1::uuid[])`, peers},
		} {
			var e error
			if v.sql == `DELETE FROM account_blocks WHERE blocker_account_id=$2 AND blocked_account_id=ANY($1::uuid[])` {
				_, e = b.pool.Exec(context.Background(), v.sql, v.ids, a.Actor.ID)
			} else {
				_, e = b.pool.Exec(context.Background(), v.sql, v.ids)
			}
			if e != nil {
				t.Error("owned contacts cleanup", e)
			}
		}
	})
	for i := 0; i < 107; i++ {
		var peer, request, tie, following string
		if e := b.pool.QueryRow(b.ctx, `INSERT INTO accounts(id,account_type,status) VALUES(gen_random_uuid(),'person','active') RETURNING id`).Scan(&peer); e != nil {
			t.Fatal(e)
		}
		b.accounts = append(b.accounts, peer)
		peers = append(peers, peer)
		b.exec(`INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,$2,'public')`, peer, fmt.Sprintf("本地联系人分页%03d", i))
		// Adjacent creation times tie; UUID ordering must survive page boundaries.
		at := old.Add(time.Duration(i/2) * time.Second)
		if e := b.pool.QueryRow(b.ctx, `INSERT INTO connection_requests(sender_account_id,recipient_account_id,note,scope,state,expires_at,created_at,decided_at) VALUES($1,$2,'本地明确好友许可','friend','accepted',clock_timestamp()+interval '1 hour',$3,$3) RETURNING id`, a.Actor.ID, peer, at).Scan(&request); e != nil {
			t.Fatal(e)
		}
		requests = append(requests, request)
		if e := b.pool.QueryRow(b.ctx, `INSERT INTO person_ties(person_a_account_id,person_b_account_id,request_id,created_at) VALUES(LEAST($1::uuid,$2::uuid),GREATEST($1::uuid,$2::uuid),$3,$4) RETURNING id`, a.Actor.ID, peer, request, at).Scan(&tie); e != nil {
			t.Fatal(e)
		}
		ties = append(ties, tie)
		if e := b.pool.QueryRow(b.ctx, `INSERT INTO follows(follower_account_id,person_account_id,created_at) VALUES($1,$2,$3) RETURNING id`, a.Actor.ID, peer, at).Scan(&following); e != nil {
			t.Fatal(e)
		}
		follows = append(follows, following)
	}
	expected := func(sql string) []string {
		rows, e := b.pool.Query(b.ctx, sql, a.Actor.ID)
		if e != nil {
			t.Fatal(e)
		}
		defer rows.Close()
		ids := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				t.Fatal(e)
			}
			ids = append(ids, id)
		}
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
		return ids
	}
	tieExpected := expected(`SELECT id FROM person_ties WHERE person_a_account_id=$1 OR person_b_account_id=$1 ORDER BY created_at DESC,id DESC`)
	followExpected := expected(`SELECT id FROM follows WHERE follower_account_id=$1 ORDER BY created_at DESC,id DESC`)
	requestExpected := expected(`SELECT id FROM connection_requests WHERE sender_account_id=$1 OR recipient_account_id=$1 ORDER BY created_at DESC,id DESC`)
	tieID := func(v connection.Tie) string { return v.ID }
	followID := func(v follow.Record) string { return v.ID }
	requestID := func(v connection.Request) string { return v.ID }
	for _, v := range []struct{ got, want []string }{
		{contactPageNativeWalk(t, b.store, b.ctx, a, connection.ContactTies, "", b.store.ListContactTiesPage, tieID), tieExpected},
		{contactPageNativeWalk(t, b.store, b.ctx, a, connection.ContactFollows, "", b.store.ListContactFollowsPage, followID), followExpected},
		{contactPageNativeWalk(t, b.store, b.ctx, a, connection.ContactRequests, "", b.store.ListContactRequestsPage, requestID), requestExpected},
	} {
		if len(v.got) <= 100 || !reflect.DeepEqual(v.got, v.want) {
			t.Fatal("real keyset lost, duplicated or reordered contacts beyond100", len(v.got), len(v.want))
		}
	}

	in := connection.PageRequest{Limit: 20}
	first, e := b.store.ListContactTiesPage(b.ctx, a, in)
	if e != nil || first.NextCursor == nil {
		t.Fatal("tie first page", e)
	}
	firstFollow, e := b.store.ListContactFollowsPage(b.ctx, a, in)
	if e != nil || firstFollow.NextCursor == nil {
		t.Fatal("follow first page", e)
	}
	firstRequest, e := b.store.ListContactRequestsPage(b.ctx, a, in)
	if e != nil || firstRequest.NextCursor == nil {
		t.Fatal("request first page", e)
	}
	continued := connection.PageRequest{Limit: 20, Cursor: *first.NextCursor}
	if _, e = b.store.ListContactTiesPage(b.ctx, other, continued); !errors.Is(e, connection.ErrInvalidContactPage) {
		t.Fatal("foreign owner cursor", e)
	}
	if _, e = b.store.ListContactFollowsPage(b.ctx, a, continued); !errors.Is(e, connection.ErrInvalidContactPage) {
		t.Fatal("foreign kind cursor", e)
	}
	// A new row belongs to a new refresh, not this frozen upper-bound scan.
	var newestFollow string
	if e = b.pool.QueryRow(b.ctx, `INSERT INTO follows(follower_account_id,person_account_id) VALUES($1,$2) RETURNING id`, a.Actor.ID, other.Actor.ID).Scan(&newestFollow); e != nil {
		t.Fatal(e)
	}
	follows = append(follows, newestFollow)
	remainder := contactPageNativeWalk(t, b.store, b.ctx, a, connection.ContactFollows, *firstFollow.NextCursor, b.store.ListContactFollowsPage, followID)
	if !reflect.DeepEqual(remainder, followExpected[20:]) {
		t.Fatal("snapshot admitted later insertion or skipped existing row")
	}

	// This original request is beyond the initial100, yet directly addressable.
	oldest := requests[0]
	row, e := b.store.ReadContactRequestCurrent(b.ctx, a, oldest)
	if e != nil || row.ID != oldest {
		t.Fatal("old request direct read", e)
	}
	if e = b.store.ValidateContactRequestResponse(b.ctx, a, row); e != nil {
		t.Fatal("fresh direct proof", e)
	}
	if _, e = b.store.ReadContactRequestCurrent(b.ctx, other, oldest); !errors.Is(e, connection.ErrNotFound) {
		t.Fatal("foreign request disclosed", e)
	}
	b.exec(`UPDATE connection_requests SET note='本地变更后的申请文字' WHERE id=$1`, oldest)
	if e = b.store.ValidateContactRequestResponse(b.ctx, a, row); !errors.Is(e, connection.ErrConflict) {
		t.Fatal("changed direct projection leaked", e)
	}
	b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, a.Actor.ID, peers[0])
	for _, v := range []struct {
		got       []string
		forbidden string
	}{
		{contactPageNativeWalk(t, b.store, b.ctx, a, connection.ContactTies, *first.NextCursor, b.store.ListContactTiesPage, tieID), ties[0]},
		{contactPageNativeWalk(t, b.store, b.ctx, a, connection.ContactFollows, *firstFollow.NextCursor, b.store.ListContactFollowsPage, followID), follows[0]},
	} {
		for _, id := range v.got {
			if id == v.forbidden {
				t.Fatal("continued cursor revived blocked contact")
			}
		}
	}
	if _, e = b.store.ReadContactRequestCurrent(b.ctx, a, oldest); !errors.Is(e, connection.ErrNotFound) {
		t.Fatal("blocked request direct read", e)
	}
	// Historical request rows preserve the original redacted shape, not private
	// names/notes and not a new permission to review the blocked person.
	cursor := *firstRequest.NextCursor
	foundRedacted := false
	for turn := 0; turn < 8; turn++ {
		page, e := b.store.ListContactRequestsPage(b.ctx, a, connection.PageRequest{Cursor: cursor})
		if e != nil {
			t.Fatal(e)
		}
		for _, v := range page.Items {
			if v.ID == oldest {
				foundRedacted = true
				if v.Note != "" || v.OtherName != "账号暂不可用" {
					t.Fatal("blocked request private fields leaked")
				}
			}
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if !foundRedacted {
		t.Fatal("historical redacted request lost")
	}
	b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1 AND token_sha256=$2`, a.Actor.ID, a.SessionDigest[:])
	if _, e = b.store.ListContactTiesPage(b.ctx, a, continued); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("revoked continuation", e)
	}
	if _, e = b.store.ListContactFollowsPage(b.ctx, a, connection.PageRequest{Cursor: *firstFollow.NextCursor}); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("revoked follow continuation", e)
	}
	if _, e = b.store.ListContactRequestsPage(b.ctx, a, connection.PageRequest{Cursor: *firstRequest.NextCursor}); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("revoked request continuation", e)
	}
	if e = b.store.ValidateContactPageResponse(b.ctx, a, connection.ContactTies, in, first); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("revoked final page", e)
	}
	if _, e = b.store.ReadContactRequestCurrent(b.ctx, a, requests[1]); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("revoked direct request", e)
	}
}
