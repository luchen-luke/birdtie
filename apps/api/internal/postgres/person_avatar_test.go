package postgres

import (
	"bytes"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	av "github.com/birdtie/birdtie/apps/api/internal/profileavatar"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestPersonAvatarCurrentProfileProofRetiresChangedAvatar(t *testing.T) {
	a := identity.Profile{AccountID: "01234567-89ab-4cde-8fab-0123456789ab", DisplayName: "成员", Visibility: "public"}
	b := a
	b.Avatar = &av.Metadata{Version: 1}
	if sameCurrentProfile(a, b) {
		t.Fatal("new avatar failed to retire old profile")
	}
	a.Avatar = &av.Metadata{Version: 2}
	if sameCurrentProfile(a, b) {
		t.Fatal("replacement failed to retire old profile")
	}
	for _, sql := range []string{currentProfileSQL, contactTiesBaseSQL, contactFollowsBaseSQL, contactRequestsBaseSQL, chatPageVisibleSQL} {
		if !strings.Contains(sql, "birdtie_person_avatar_metadata") {
			t.Fatal("avatar missing from current authorized source")
		}
	}
	if !strings.Contains(currentProfileSQL, "person_avatars a WHERE a.account_id=$1") {
		t.Fatal("avatar epoch missing")
	}
}

func TestPersonAvatarNativeCASReceiptVisibilityReplacementAndDelete(t *testing.T) {
	ownedMigrationDatabase(t)
	f := visibilityMatrixNative(t)
	t.Cleanup(func() {
		ownedSocialFixtureCleanup(t, f.ctx, f.pool, `DELETE FROM person_avatar_operations WHERE account_id=ANY($1::uuid[])`, f.ids)
		ownedSocialFixtureCleanup(t, f.ctx, f.pool, `DELETE FROM person_avatars WHERE account_id=ANY($1::uuid[])`, f.ids)
	})
	access := currentProfileNativeAccess(t, f, 0)
	a := av.Access{AccountID: access.Actor.ID, AccountType: "person", SessionDigest: access.SessionDigest}
	first, second := image.NewRGBA(image.Rect(0, 0, 20, 20)), image.NewRGBA(image.Rect(0, 0, 20, 20))
	second.Set(10, 10, color.RGBA{R: 255, A: 255})
	var one, two bytes.Buffer
	png.Encode(&one, first)
	png.Encode(&two, second)
	in := av.Input{OperationID: "12200000-0000-4000-8000-000000000001", Content: one.Bytes(), ContentType: "image/png"}
	result, e := f.store.ChangeOwnAvatar(f.ctx, a, in)
	if e != nil || result.AppliedVersion != 1 || result.Current.Avatar == nil || !result.Current.Avatar.Valid() {
		t.Fatal("save", result, e)
	}
	duplicate, e := f.store.ChangeOwnAvatar(f.ctx, a, in)
	if e != nil || duplicate.AppliedVersion != 1 || duplicate.Current.Version != 1 {
		t.Fatal("idempotent save", duplicate, e)
	}
	var count int
	f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND purpose='owner_avatar_edit'`, a.AccountID).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate audit", count)
	}
	stale := in
	stale.OperationID = "12200000-0000-4000-8000-000000000002"
	if _, e = f.store.ChangeOwnAvatar(f.ctx, a, stale); !errors.Is(e, av.ErrConflict) {
		t.Fatal("stale CAS", e)
	}
	f.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, a.AccountID)
	metadata, raw, e := f.store.ReadAvatarContent(f.ctx, av.Access{}, a.AccountID, 1)
	if e != nil || len(raw) != metadata.ByteCount {
		t.Fatal("public content", e)
	}
	profile, e := f.store.ReadProfileCurrent(f.ctx, identity.ProfileAccess{}, a.AccountID)
	if e != nil || profile.Avatar == nil {
		t.Fatal("public Profile DTO", e)
	}
	secondInput := av.Input{OperationID: stale.OperationID, ExpectedVersion: 1, Content: two.Bytes(), ContentType: "image/png"}
	result, e = f.store.ChangeOwnAvatar(f.ctx, a, secondInput)
	if e != nil || result.Current.Version != 2 {
		t.Fatal("replace", e)
	}
	if e = f.store.ValidateProfileCurrent(f.ctx, identity.ProfileAccess{}, a.AccountID, profile); !errors.Is(e, identity.ErrConflict) {
		t.Fatal("old Profile proof survived replacement", e)
	}
	if _, _, e = f.store.ReadAvatarContent(f.ctx, av.Access{}, a.AccountID, 1); !errors.Is(e, av.ErrNotFound) {
		t.Fatal("old content survived replacement", e)
	}
	peer := currentProfileNativeAccess(t, f, 6)
	f.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, peer.Actor.ID, a.AccountID)
	if _, _, e = f.store.ReadAvatarContent(f.ctx, av.Access{AccountID: peer.Actor.ID, AccountType: "person", SessionDigest: peer.SessionDigest}, a.AccountID, 2); !errors.Is(e, av.ErrNotFound) {
		t.Fatal("blocked avatar leaked", e)
	}
	result, e = f.store.ChangeOwnAvatar(f.ctx, a, av.Input{OperationID: "12200000-0000-4000-8000-000000000003", ExpectedVersion: 2, Delete: true})
	if e != nil || result.Current.Version != 3 || result.Current.Avatar != nil {
		t.Fatal("delete", result, e)
	}
	if _, _, e = f.store.ReadAvatarContent(f.ctx, av.Access{}, a.AccountID, 2); !errors.Is(e, av.ErrNotFound) {
		t.Fatal("deleted content survived", e)
	}
	receipt, e := f.store.ReadOwnAvatarOperation(f.ctx, a, in.OperationID)
	if e != nil || receipt.AppliedVersion != 1 || receipt.Current.Version != 3 || receipt.Current.Avatar != nil {
		t.Fatal("receipt retained deleted content", receipt, e)
	}
	f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:])
	if _, e = f.store.ChangeOwnAvatar(f.ctx, a, av.Input{OperationID: "12200000-0000-4000-8000-000000000004", ExpectedVersion: 3, Delete: true}); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("revoked session wrote", e)
	}
}
