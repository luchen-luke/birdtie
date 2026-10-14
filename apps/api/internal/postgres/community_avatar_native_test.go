package postgres

import (
	"bytes"
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	ca "github.com/birdtie/birdtie/apps/api/internal/communityavatar"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"image"
	"image/png"
	"testing"
	"time"
)

func communityAvatarNativeInput(t *testing.T, f *commNativeStoreFixture, version int64) ca.Input {
	t.Helper()
	var raw bytes.Buffer
	if e := png.Encode(&raw, image.NewGray(image.Rect(0, 0, 130, 128))); e != nil {
		t.Fatal(e)
	}
	var op string
	if e := f.base.base.pool.QueryRow(f.base.base.ctx, `SELECT gen_random_uuid()`).Scan(&op); e != nil {
		t.Fatal(e)
	}
	return ca.Input{OperationID: op, ExpectedVersion: version, ContentType: "image/png", Content: raw.Bytes()}
}
func communityAvatarNativeAccess(f *commNativeStoreFixture) ca.Access {
	return ca.Access{AccountID: f.actor.ID, AccountType: "person", SessionDigest: f.base.owner.SessionDigest}
}

func TestCommunityAvatarNativeApprovalCASRecoveryAndPrivateIsolation(t *testing.T) {
	ownedMigrationDatabase(t)
	f := commNativeStore(t)
	b := f.base.base
	a := communityAvatarNativeAccess(f)
	private := agentPrivateFullSnapshot(t, b.pool, b.accounts)
	in := communityAvatarNativeInput(t, f, 0)
	state, e := b.store.ReadCommunityAvatar(b.ctx, a, f.id)
	if e != nil || state.Version != 0 || state.Avatar != nil {
		t.Fatal("empty version0 editable", state, e)
	}
	if _, e = b.store.ChangeCommunityAvatar(b.ctx, a, f.id, in); !errors.Is(e, ca.ErrChanged) {
		t.Fatal("unapproved write", e)
	}
	generic, e := b.store.MutateHumanCommunity(b.ctx, a.SessionDigest, f.actor, community.HumanCommand{Operation: "archive", CommunityID: f.id, Preview: true})
	if e != nil {
		t.Fatal(e)
	}
	approval, e := b.store.PreviewCommunityAvatar(b.ctx, a, f.id, in)
	if e != nil {
		t.Fatal(e)
	}
	in.Approval = approval.Token
	out, e := b.store.ChangeCommunityAvatar(b.ctx, a, f.id, in)
	if e != nil || out.AppliedVersion != 1 || out.Current.Avatar == nil {
		t.Fatal("approved result", out, e)
	}
	meta, pixels, e := b.store.ReadCommunityAvatarContent(b.ctx, a, f.id, 1)
	if e != nil || meta.SHA256 != approval.InputSHA256 || len(pixels) != meta.ByteCount {
		t.Fatal("normalized content", e)
	}
	if _, e = b.store.MutateHumanCommunity(b.ctx, a.SessionDigest, f.actor, community.HumanCommand{Operation: "archive", CommunityID: f.id, Snapshot: generic.Approval.Snapshot}); !errors.Is(e, community.ErrApprovalStale) {
		t.Fatal("avatar changed generic preview material", e)
	}
	replay, e := b.store.ChangeCommunityAvatar(b.ctx, a, f.id, in)
	if e != nil || replay.AppliedVersion != 1 || replay.Current.Version != 1 {
		t.Fatal("single effect replay", e)
	}
	conflict := in
	conflict.Delete = true
	conflict.Content = nil
	conflict.ContentType = ""
	if _, e = b.store.ChangeCommunityAvatar(b.ctx, a, f.id, conflict); !errors.Is(e, ca.ErrChanged) {
		t.Fatal("op body mismatch", e)
	}
	clear := communityAvatarNativeInput(t, f, 1)
	clear.Delete = true
	clear.Content = nil
	clear.ContentType = ""
	p, e := b.store.PreviewCommunityAvatar(b.ctx, a, f.id, clear)
	if e != nil {
		t.Fatal(e)
	}
	clear.Approval = p.Token
	if _, e = b.store.ChangeCommunityAvatar(b.ctx, a, f.id, clear); e != nil {
		t.Fatal(e)
	}
	receipt, e := b.store.ReadCommunityAvatarOperation(b.ctx, a, f.id, in.OperationID)
	if e != nil || receipt.AppliedVersion != 1 || receipt.Current.Version != 2 || receipt.Current.Avatar != nil {
		t.Fatal("original success distinct from later clear", receipt, e)
	}
	if _, _, e = b.store.ReadCommunityAvatarContent(b.ctx, a, f.id, 1); !errors.Is(e, ca.ErrNotFound) {
		t.Fatal("retired content", e)
	}
	peer := ca.Access{AccountID: b.other.ID, AccountType: "person", SessionDigest: f.base.peer.SessionDigest}
	if _, e = b.store.PreviewCommunityAvatar(b.ctx, peer, f.id, communityAvatarNativeInput(t, f, 2)); !errors.Is(e, ca.ErrForbidden) {
		t.Fatal("viewer writes", e)
	}
	if private != agentPrivateFullSnapshot(t, b.pool, b.accounts) {
		t.Fatal("thumbnail changed private profile/Memory/grants")
	}
}

func TestCommunityAvatarNativeViewerBlocksAndMembershipABARetireApproval(t *testing.T) {
	ownedMigrationDatabase(t)
	f := commNativeStore(t)
	b := f.base.base
	a := communityAvatarNativeAccess(f)
	in := communityAvatarNativeInput(t, f, 0)
	p, e := b.store.PreviewCommunityAvatar(b.ctx, a, f.id, in)
	if e != nil {
		t.Fatal(e)
	}
	in.Approval = p.Token
	aba, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	for _, role := range []string{"admin", "owner"} {
		if _, e = aba.Exec(b.ctx, `UPDATE community_memberships SET role=$3 WHERE community_id=$1 AND user_account_id=$2`, f.id, a.AccountID, role); e != nil {
			aba.Rollback(context.Background())
			t.Fatal(e)
		}
	}
	if e = aba.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.ChangeCommunityAvatar(b.ctx, a, f.id, in); !errors.Is(e, ca.ErrChanged) {
		t.Fatal("membership ABA accepted old preview", e)
	}
	p, e = b.store.PreviewCommunityAvatar(b.ctx, a, f.id, in)
	if e != nil {
		t.Fatal(e)
	}
	in.Approval = p.Token
	if _, e = b.store.ChangeCommunityAvatar(b.ctx, a, f.id, in); e != nil {
		t.Fatal(e)
	}
	peer := ca.Access{AccountID: b.other.ID, AccountType: "person", SessionDigest: f.base.peer.SessionDigest}
	if _, _, e = b.store.ReadCommunityAvatarContent(b.ctx, peer, f.id, 1); e != nil {
		t.Fatal("public viewer", e)
	}
	b.exec(`UPDATE communities SET visibility='hidden' WHERE id=$1`, f.id)
	if _, e = b.store.ReadCommunityAvatar(b.ctx, peer, f.id); !errors.Is(e, ca.ErrNotFound) {
		t.Fatal("hidden outsider", e)
	}
	b.exec(`INSERT INTO community_memberships(community_id,user_account_id,role,status) VALUES($1,$2,'member','invited')`, f.id, peer.AccountID)
	if _, _, e = b.store.ReadCommunityAvatarContent(b.ctx, peer, f.id, 1); e != nil {
		t.Fatal("current hidden invitation", e)
	}
	b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, peer.AccountID, a.AccountID)
	t.Cleanup(func() {
		b.exec(`DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, peer.AccountID, a.AccountID)
	})
	if _, _, e = b.store.ReadCommunityAvatarContent(b.ctx, peer, f.id, 1); !errors.Is(e, ca.ErrNotFound) {
		t.Fatal("block must retire thumbnail", e)
	}
}

func TestCommunityAvatarNativeResourceWaitSessionRevokeRollsBack(t *testing.T) {
	ownedMigrationDatabase(t)
	f := commNativeStore(t)
	b := f.base.base
	a := communityAvatarNativeAccess(f)
	in := communityAvatarNativeInput(t, f, 0)
	p, e := b.store.PreviewCommunityAvatar(b.ctx, a, f.id, in)
	if e != nil {
		t.Fatal(e)
	}
	in.Approval = p.Token
	held, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer held.Rollback(context.Background())
	var pid int
	if e = held.QueryRow(b.ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	if _, e = held.Exec(b.ctx, `SELECT id FROM communities WHERE id=$1 FOR UPDATE`, f.id); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := b.store.ChangeCommunityAvatar(b.ctx, a, f.id, in); done <- e }()
	waiting := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND query LIKE 'SELECT xmin::text FROM communities%' AND query LIKE '%FOR UPDATE%')`, pid).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		select {
		case e := <-done:
			t.Fatal("did not wait on current community", e)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("resource wait not observed")
	}
	b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:])
	if e = held.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatal("late Session revoke", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("write did not finish")
	}
	var count int
	if e = b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM community_avatars WHERE community_id=$1)+(SELECT count(*) FROM community_avatar_operations WHERE community_id=$1)`, f.id).Scan(&count); e != nil || count != 0 {
		t.Fatal("revoked effect retained", count, e)
	}
}
