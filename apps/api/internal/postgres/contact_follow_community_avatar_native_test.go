package postgres

import (
	"context"
	"errors"
	ca "github.com/birdtie/birdtie/apps/api/internal/communityavatar"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/follow"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"testing"
)

func TestBatchAFollowCommunityApprovedAvatarSourceAndSessionABA(t *testing.T) {
	ownedMigrationDatabase(t)
	f := commNativeStore(t)
	b := f.base.base
	owner := communityAvatarNativeAccess(f)
	in := communityAvatarNativeInput(t, f, 0)
	approval, e := b.store.PreviewCommunityAvatar(b.ctx, owner, f.id, in)
	if e != nil {
		t.Fatal(e)
	}
	in.Approval = approval.Token
	if _, e = b.store.ChangeCommunityAvatar(b.ctx, owner, f.id, in); e != nil {
		t.Fatal(e)
	}
	viewer := identity.Actor{ID: b.other.ID, AccountType: "person"}
	access := ea.Access{Actor: viewer, SessionDigest: f.base.peer.SessionDigest}
	if _, e = b.store.Follow(b.ctx, viewer.ID, follow.Target{Type: follow.Community, ID: f.id}); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := b.pool.Exec(context.Background(), `DELETE FROM follows WHERE follower_account_id=$1 AND community_id=$2`, viewer.ID, f.id); e != nil {
			t.Error(e)
		}
	})
	request := connection.PageRequest{Limit: 20}
	page, e := b.store.ListContactFollowsPage(b.ctx, access, request)
	if e != nil || len(page.Items) != 1 || page.Items[0].CommunityAvatar == nil || page.Items[0].CommunityAvatar.CommunityID != f.id {
		t.Fatal("approved current Follow projection", e)
	}
	if e = b.store.ValidateContactPageResponse(b.ctx, access, connection.ContactFollows, request, page); e != nil {
		t.Fatal("current proof", e)
	}
	meta, pixels, e := b.store.ReadCommunityAvatarContent(b.ctx, ca.Access{AccountID: viewer.ID, AccountType: "person", SessionDigest: access.SessionDigest}, f.id, 1)
	if e != nil || len(pixels) != meta.ByteCount || meta.SHA256 != page.Items[0].CommunityAvatar.SHA256 {
		t.Fatal("same approved content bytes", e)
	}
	b.exec(`UPDATE communities SET visibility='hidden' WHERE id=$1`, f.id)
	if e = b.store.ValidateContactPageResponse(b.ctx, access, connection.ContactFollows, request, page); !errors.Is(e, connection.ErrConflict) {
		t.Fatal("hidden source released", e)
	}
	b.exec(`UPDATE communities SET visibility='public' WHERE id=$1`, f.id)
	if e = b.store.ValidateContactPageResponse(b.ctx, access, connection.ContactFollows, request, page); !errors.Is(e, connection.ErrConflict) {
		t.Fatal("community ABA revived old proof", e)
	}
	page, e = b.store.ListContactFollowsPage(b.ctx, access, request)
	if e != nil {
		t.Fatal(e)
	}
	if e = b.store.BlockAccount(b.ctx, viewer.ID, f.actor.ID); e != nil {
		t.Fatal(e)
	}
	if e = b.store.UnblockAccount(b.ctx, viewer.ID, f.actor.ID); e != nil {
		t.Fatal(e)
	}
	if e = b.store.ValidateContactPageResponse(b.ctx, access, connection.ContactFollows, request, page); !errors.Is(e, connection.ErrConflict) {
		t.Fatal("original block/unblock safety generation lost", e)
	}
	if _, e = b.store.Follow(b.ctx, viewer.ID, follow.Target{Type: follow.Community, ID: f.id}); e != nil {
		t.Fatal("fresh current follow after original safety change", e)
	}
	page, e = b.store.ListContactFollowsPage(b.ctx, access, request)
	if e != nil {
		t.Fatal(e)
	}
	b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, access.SessionDigest[:])
	if e = b.store.ValidateContactPageResponse(b.ctx, access, connection.ContactFollows, request, page); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("revoked Session released metadata", e)
	}
	b.exec(`UPDATE sessions SET revoked_at=NULL WHERE token_sha256=$1`, access.SessionDigest[:])
	if e = b.store.ValidateContactPageResponse(b.ctx, access, connection.ContactFollows, request, page); !errors.Is(e, connection.ErrConflict) {
		t.Fatal("Session ABA revived metadata", e)
	}
	current, e := b.store.ListContactFollowsPage(b.ctx, access, request)
	if e != nil {
		t.Fatal(e)
	}
	clear := communityAvatarNativeInput(t, f, 1)
	clear.Delete = true
	clear.Content = nil
	clear.ContentType = ""
	approval, e = b.store.PreviewCommunityAvatar(b.ctx, owner, f.id, clear)
	if e != nil {
		t.Fatal(e)
	}
	clear.Approval = approval.Token
	if _, e = b.store.ChangeCommunityAvatar(b.ctx, owner, f.id, clear); e != nil {
		t.Fatal(e)
	}
	if e = b.store.ValidateContactPageResponse(b.ctx, access, connection.ContactFollows, request, current); !errors.Is(e, connection.ErrConflict) {
		t.Fatal("old avatar proof survived clear", e)
	}
}
