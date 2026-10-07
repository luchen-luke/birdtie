package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func TestEntityShareNativeAuthorityAfterRealWait(t *testing.T) {
	for _, change := range []string{"revoke", "expiry", "remove-tie", "peer-block", "hidden-place"} {
		t.Run(change, func(t *testing.T) {
			f := newNativeEntityShareFixture(t)
			b := f.f.private.base
			locked, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer locked.Rollback(context.Background())
			if _, e = locked.Exec(b.ctx, `SELECT id FROM accounts WHERE id=$1 FOR NO KEY UPDATE`, f.a.actor.ID); e != nil {
				t.Fatal(e)
			}
			input := nativeShareInput(t, f)
			if change == "expiry" {
				// Use one actual timestamp for both deadlines: separately evaluated
				// clock_timestamp calls can violate idle_expires_at <= expires_at.
				b.exec(`WITH expiry AS MATERIALIZED (SELECT clock_timestamp()+interval '400ms' AS at)
					UPDATE sessions SET expires_at=expiry.at,idle_expires_at=expiry.at
					FROM expiry WHERE token_sha256=$1`, f.a.digest[:])
			}
			result := make(chan error, 1)
			go func() { _, err := b.store.ShareHumanEntity(b.ctx, f.access(), f.conversation, input); result <- err }()
			placeMemoryWaitForBlock(t, f.f, locked.Conn().PgConn().PID())
			want := connection.ErrNotFound
			switch change {
			case "revoke":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, f.a.digest[:])
				want = identity.ErrUnauthorized
			case "expiry":
				time.Sleep(550 * time.Millisecond)
				want = identity.ErrUnauthorized
			case "remove-tie":
				b.exec(`UPDATE person_ties SET status='removed' WHERE person_a_account_id=LEAST($1::uuid,$2::uuid) AND person_b_account_id=GREATEST($1::uuid,$2::uuid)`, f.a.actor.ID, f.b.actor.ID)
			case "peer-block":
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.b.actor.ID, f.a.actor.ID)
			case "hidden-place":
				b.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.f.place)
			}
			if e = locked.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			if e = <-result; !errors.Is(e, want) {
				t.Fatal("late authority reused", change, e, want)
			}
			var count int
			if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM conversation_messages WHERE conversation_id=$1`, f.conversation).Scan(&count); e != nil || count != 0 {
				t.Fatal("unauthorized late effect", count, e)
			}
		})
	}
}

func TestEntityShareNativeSevenKindsAndOldHistoryRecovery(t *testing.T) {
	f := newNativeEntityShareFixture(t)
	b := f.f.private.base
	var orgAccount, orgID, businessAccount, businessID string
	for _, kind := range []string{"organization", "business"} {
		var account string
		if e := b.pool.QueryRow(b.ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),$1) RETURNING id`, kind).Scan(&account); e != nil {
			t.Fatal(e)
		}
		if kind == "organization" {
			orgAccount = account
		} else {
			businessAccount = account
		}
	}
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM organizations WHERE account_id=$1`, `DELETE FROM businesses WHERE account_id=$1`, `DELETE FROM accounts WHERE id=$1`} {
			for _, id := range []string{orgAccount, businessAccount} {
				if _, e := b.pool.Exec(context.Background(), q, id); e != nil {
					t.Error(e)
				}
			}
		}
	})
	if e := b.pool.QueryRow(b.ctx, `INSERT INTO organizations(account_id,organization_type,name,verification_status) VALUES($1,'club','本地合成公开组织','verified') RETURNING id`, orgAccount).Scan(&orgID); e != nil {
		t.Fatal(e)
	}
	if e := b.pool.QueryRow(b.ctx, `INSERT INTO businesses(account_id,name,claim_status,claim_source_url,claim_reviewed_by,claim_reviewed_at) VALUES($1,'本地合成公开商户','verified','https://example.invalid/local-only',$2,now()) RETURNING id`, businessAccount, f.a.actor.ID).Scan(&businessID); e != nil {
		t.Fatal(e)
	}
	var original connection.EntityShareReceipt
	var originalInput connection.EntityShareInput
	for _, source := range []struct{ kind, id string }{
		{"activity", "b1700000-0000-4000-8000-000000000017"},
		{"place", f.f.place}, {"person", f.b.actor.ID},
		{"community", "b1700000-0000-4000-8000-000000000030"},
		{"organization", orgID}, {"business", businessID}, {"moment", f.moment},
	} {
		t.Run(source.kind, func(t *testing.T) {
			input := nativeShareInput(t, f)
			input.Entity.Type = source.kind
			input.Entity.ID = source.id
			r, e := b.store.ShareHumanEntity(b.ctx, f.access(), f.conversation, input)
			if e != nil || r.Message.Entity == nil || !r.Message.Entity.Available || r.Message.Entity.Type != source.kind || r.Message.Entity.ID != source.id {
				t.Fatal("stable native entity", r, e)
			}
			read, e := New(b.pool, false).GetHumanEntityShare(b.ctx, f.access(), f.conversation, input.OperationID)
			if e != nil || read.Message.ID != r.Message.ID {
				t.Fatal("exact operation recovery", read, e)
			}
			if source.kind == "activity" {
				original = r
				originalInput = input
			}
		})
	}
	// The keyed authority receipt is independent of the latest 100-message window.
	b.exec(`INSERT INTO conversation_messages(conversation_id,sender_account_id,body,created_at)
	 SELECT $1,$2,'本地合成旧消息',clock_timestamp()+n*interval '1 microsecond' FROM generate_series(1,110) n`, f.conversation, f.a.actor.ID)
	messages, e := b.store.ListMessages(b.ctx, f.a.actor.ID, f.conversation)
	if e != nil || len(messages) != 100 {
		t.Fatal("actual history window", len(messages), e)
	}
	for _, m := range messages {
		if m.ID == original.Message.ID {
			t.Fatal("original unexpectedly remained in latest100")
		}
	}
	read, e := New(b.pool, false).GetHumanEntityShare(b.ctx, f.access(), f.conversation, originalInput.OperationID)
	if e != nil || read.Message.ID != original.Message.ID {
		t.Fatal("old native keyed receipt lost", read, e)
	}
	var count, notifications int
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM conversation_messages WHERE conversation_id=$1`, f.conversation).Scan(&count); e != nil || count != 117 {
		t.Fatal(count, e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM native_notification_decisions WHERE source_id=$1`, original.Message.ID).Scan(&notifications); e != nil || notifications != 1 {
		t.Fatal("read recreated notification", notifications, e)
	}
}

type nativeEntityShareFixture struct {
	f                    *placeMemoryFixture
	a, b                 momentPublicationNativeActor
	conversation, moment string
	revision             int64
}

func newNativeEntityShareFixture(t *testing.T) *nativeEntityShareFixture {
	t.Helper()
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	a := momentPublicationOrdinary(t, f)
	peer := momentPublicationOrdinary(t, f)
	for _, id := range []string{a.actor.ID, peer.actor.ID} {
		b.exec(`INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'本地合成分享好友','public')`, id)
	}
	t.Cleanup(func() {
		ids := []string{a.actor.ID, peer.actor.ID}
		for _, q := range []string{`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`, `DELETE FROM native_notification_decisions WHERE recipient_id=ANY($1::uuid[]) OR actor_id=ANY($1::uuid[])`, `DELETE FROM conversation_messages WHERE sender_account_id=ANY($1::uuid[])`, `DELETE FROM conversation_member_states WHERE member_account_id=ANY($1::uuid[])`, `DELETE FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[])`, `DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, `DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`, `DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`} {
			if _, e := b.pool.Exec(context.Background(), q, ids); e != nil {
				t.Error("owned native share cleanup", e)
			}
		}
	})
	r, e := b.store.CreateFriendRequest(b.ctx, a.actor.ID, peer.actor.ID, "本地合成明确好友许可")
	if e != nil {
		t.Fatal("actual friend request", e)
	}
	if _, e = b.store.DecideRequest(b.ctx, peer.actor.ID, r.ID, "accept"); e != nil {
		t.Fatal("actual acceptance", e)
	}
	ties, e := b.store.ListTies(b.ctx, a.actor.ID)
	if e != nil || len(ties) != 1 {
		t.Fatal("actual ties", ties, e)
	}
	c, e := b.store.StartFriendConversation(b.ctx, a.actor.ID, ties[0].ID)
	if e != nil {
		t.Fatal("actual friend chat", e)
	}
	m := momentPublicationDraft(t, f, a)
	published := momentPublicationPublish(t, f, a, m)
	return &nativeEntityShareFixture{f, a, peer, c.ID, m.ID, published.Revision}
}
func nativeShareInput(t *testing.T, f *nativeEntityShareFixture) connection.EntityShareInput {
	in := connection.EntityShareInput{OperationID: agentMemoryID(t, f.f.private)}
	in.Entity.Type = "moment"
	in.Entity.ID = f.moment
	return in
}
func (f *nativeEntityShareFixture) access() connection.EntityShareAccess {
	return connection.EntityShareAccess{Actor: f.a.actor, SessionDigest: f.a.digest}
}
func TestEntityShareNativeIdempotentRecovery(t *testing.T) {
	f := newNativeEntityShareFixture(t)
	b := f.f.private.base
	in := nativeShareInput(t, f)
	first, e := b.store.ShareHumanEntity(b.ctx, f.access(), f.conversation, in)
	if e != nil || first.Message.Entity == nil || !first.Message.Entity.Available {
		t.Fatal("native share", first, e)
	}
	for i := 0; i < 3; i++ {
		again, e := New(b.pool, false).ShareHumanEntity(b.ctx, f.access(), f.conversation, in)
		if e != nil || again.Message.ID != first.Message.ID {
			t.Fatal("same operation duplicated across Store restart", again, e)
		}
	}
	read, e := b.store.GetHumanEntityShare(b.ctx, f.access(), f.conversation, in.OperationID)
	if e != nil || read.Message.ID != first.Message.ID {
		t.Fatal("exact current recovery", read, e)
	}
	var count, notifications int
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM conversation_messages WHERE conversation_id=$1`, f.conversation).Scan(&count); e != nil || count != 1 {
		t.Fatal("native message effects", count, e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM native_notification_decisions WHERE source_id=$1`, first.Message.ID).Scan(&notifications); e != nil || notifications != 1 {
		t.Fatal("duplicate native notification", notifications, e)
	}
	conflict := in
	conflict.Entity.Type = "place"
	conflict.Entity.ID = f.f.place
	if _, e = b.store.ShareHumanEntity(b.ctx, f.access(), f.conversation, conflict); !errors.Is(e, connection.ErrConflict) {
		t.Fatal("same key changed target", e)
	}
	if e = b.store.WithdrawHumanMoment(b.ctx, f.a.digest, f.a.actor, f.moment, f.revision); e != nil {
		t.Fatal(e)
	}
	read, e = b.store.GetHumanEntityShare(b.ctx, f.access(), f.conversation, in.OperationID)
	if e != nil || read.Message.ID != first.Message.ID || read.Message.Entity.Available || read.Message.Entity.ID != "" || read.Message.Entity.Title != "" {
		t.Fatal("committed but withdrawn source recovery", read, e)
	}
	if replay, e := b.store.ShareHumanEntity(b.ctx, f.access(), f.conversation, in); e != nil || replay.Message.ID != first.Message.ID || replay.Message.Entity.Available {
		t.Fatal("withdrawn replay recreated effect or hid committed receipt", replay, e)
	}
	newIntent := nativeShareInput(t, f)
	if _, e = b.store.ShareHumanEntity(b.ctx, f.access(), f.conversation, newIntent); !errors.Is(e, connection.ErrNotFound) {
		t.Fatal("new hidden source intent", e)
	}
}
func TestEntityShareNativeConcurrentAndBidirectional(t *testing.T) {
	f := newNativeEntityShareFixture(t)
	b := f.f.private.base
	in := nativeShareInput(t, f)
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := b.store.ShareHumanEntity(b.ctx, f.access(), f.conversation, in)
			ids <- r.Message.ID
			errs <- e
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal("concurrent exact operation", e)
		}
	}
	id := ""
	for v := range ids {
		if id != "" && v != id {
			t.Fatal("duplicate concurrent IDs", id, v)
		}
		id = v
	}
	var count int
	if e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM conversation_messages WHERE conversation_id=$1`, f.conversation).Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
	// Opposite directions must not deadlock via recipient account FK locks.
	for i := 0; i < 2; i++ {
		access := f.access()
		if i == 1 {
			access = connection.EntityShareAccess{Actor: f.b.actor, SessionDigest: f.b.digest}
		}
		next := nativeShareInput(t, f)
		wg.Add(1)
		go func(a connection.EntityShareAccess, input connection.EntityShareInput) {
			defer wg.Done()
			_, e := b.store.ShareHumanEntity(b.ctx, a, f.conversation, input)
			if e != nil {
				t.Error("bidirectional send", e)
			}
		}(access, next)
	}
	wg.Wait()
}

func TestEntityShareNativeRecipientBlockAndPrivate(t *testing.T) {
	f := newNativeEntityShareFixture(t)
	b := f.f.private.base
	b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.b.actor.ID, f.a.actor.ID)
	if _, e := b.store.ShareHumanEntity(b.ctx, f.access(), f.conversation, nativeShareInput(t, f)); !errors.Is(e, connection.ErrNotFound) {
		t.Fatal("blocked relationship", e)
	}
	b.exec(`DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, f.b.actor.ID, f.a.actor.ID)
	private := momentPublicationDraft(t, f.f, f.a)
	in := nativeShareInput(t, f)
	in.Entity.ID = private.ID
	if _, e := b.store.ShareHumanEntity(b.ctx, f.access(), f.conversation, in); !errors.Is(e, connection.ErrNotFound) {
		t.Fatal("private draft shared", e)
	}
	if _, e := b.store.GetPublicMoment(b.ctx, content.PublicMomentAccess{}, private.ID); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("private public detail", e)
	}
}
