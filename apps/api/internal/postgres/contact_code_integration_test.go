package postgres

import (
	"context"
	"errors"
	cc "github.com/birdtie/birdtie/apps/api/internal/contactcode"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"testing"
	"time"
)

func TestContactCodeNativeLifecycleAndPermission(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newNativeEntityShareFixture(t)
	b := f.f.private.base
	a := identity.ProfileAccess{Actor: f.a.actor, SessionDigest: f.a.digest}
	peer := identity.ProfileAccess{Actor: f.b.actor, SessionDigest: f.b.digest}
	t.Cleanup(func() {
		for _, sql := range []string{`DELETE FROM person_contact_code_operations WHERE owner_account_id=ANY($1::uuid[])`, `DELETE FROM person_contact_codes WHERE owner_account_id=ANY($1::uuid[])`} {
			if _, e := b.pool.Exec(context.Background(), sql, []string{a.Actor.ID, peer.Actor.ID}); e != nil {
				t.Error(e)
			}
		}
	})
	initial, e := b.store.ReadOwnContactCode(b.ctx, a)
	if e != nil || initial.Version != 0 || initial.CodeID != nil {
		t.Fatal("implicit creation", e)
	}
	in := cc.Input{OperationID: agentMemoryID(t, f.f.private), ExpectedVersion: 0}
	v, e := b.store.ChangeOwnContactCode(b.ctx, a, in, false)
	if e != nil || v.CodeID == nil || v.Version != 1 || v.ExpiresAt == nil {
		t.Fatal("create", e)
	}
	if v.ExpiresAt.Sub(time.Now().UTC()) > cc.Lifetime+time.Second || v.ExpiresAt.Sub(time.Now().UTC()) < cc.Lifetime-time.Minute {
		t.Fatal("wrong expiry")
	}
	repeated, e := b.store.ChangeOwnContactCode(b.ctx, a, in, false)
	if e != nil || !sameCode(v, repeated) {
		t.Fatal("retry changed code", e)
	}
	r, e := b.store.ResolveContactCode(b.ctx, peer, *v.CodeID)
	if e != nil || r.AccountID != a.Actor.ID {
		t.Fatal("resolve public target", e)
	}
	if e = b.store.ValidateContactResolution(b.ctx, peer, *v.CodeID, r); e != nil {
		t.Fatal("current resolution", e)
	}
	next, e := b.store.ChangeOwnContactCode(b.ctx, a, cc.Input{OperationID: agentMemoryID(t, f.f.private), ExpectedVersion: v.Version}, false)
	if e != nil || *next.CodeID == *v.CodeID {
		t.Fatal("rotate", e)
	}
	if _, e = b.store.ResolveContactCode(b.ctx, peer, *v.CodeID); !errors.Is(e, cc.ErrNotFound) {
		t.Fatal("old code valid", e)
	}
	if e = b.store.ValidateContactResolution(b.ctx, peer, *v.CodeID, r); e == nil {
		t.Fatal("late rotated proof released")
	}
	if _, e = b.store.ChangeOwnContactCode(b.ctx, a, cc.Input{OperationID: agentMemoryID(t, f.f.private), ExpectedVersion: v.Version}, true); !errors.Is(e, cc.ErrConflict) {
		t.Fatal("stale revoke accepted", e)
	}
	r, e = b.store.ResolveContactCode(b.ctx, peer, *next.CodeID)
	if e != nil {
		t.Fatal(e)
	}
	b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, peer.Actor.ID, a.Actor.ID)
	if e = b.store.ValidateContactResolution(b.ctx, peer, *next.CodeID, r); e == nil {
		t.Fatal("block leaked late selector")
	}
	b.exec(`DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, peer.Actor.ID, a.Actor.ID)
	b.exec(`UPDATE person_contact_codes SET expires_at=clock_timestamp()-interval '1 second' WHERE owner_account_id=$1`, a.Actor.ID)
	if _, e = b.store.ResolveContactCode(b.ctx, peer, *next.CodeID); !errors.Is(e, cc.ErrNotFound) {
		t.Fatal("expired code valid", e)
	}
	revoked, e := b.store.ChangeOwnContactCode(b.ctx, a, cc.Input{OperationID: agentMemoryID(t, f.f.private), ExpectedVersion: next.Version}, true)
	if e != nil || revoked.CodeID != nil || revoked.Version != 3 {
		t.Fatal("revoke", e)
	}
}
