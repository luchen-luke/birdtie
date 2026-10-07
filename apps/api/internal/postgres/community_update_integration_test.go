package postgres

import (
	"errors"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/community"
)

// Own random fixture accounts and Community, never seed-selected users. This
// exercises the actual successful UPDATE SQL, including its parameter typing.
func TestCommunitySocialUpdateParameterAndPermissionIntegration(t *testing.T) {
	pool := newPeopleTestPool(t)
	f := newPeoplePair(t, pool)
	owner, admin, member := f.ids[0], f.ids[1], f.ids[2]
	initial := community.SocialInput{Name: "Synthetic update regression", Summary: "initial description", Visibility: "public", JoinPolicy: "request"}
	created, err := f.store.CreateSocialCommunity(f.ctx, owner, initial)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		f.exec(`DELETE FROM audit_events WHERE resource_type='community' AND resource_id=$1`, created.ID)
		f.exec(`DELETE FROM communities WHERE id=$1`, created.ID)
	})
	countUpdates := func() int {
		return f.count(`SELECT count(*) FROM audit_events WHERE resource_type='community' AND resource_id=$1 AND action='community_update'`, created.ID)
	}
	assertDenied := func(actor string) {
		t.Helper()
		before, err := f.store.GetSocialCommunity(f.ctx, owner, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		auditCount := countUpdates()
		_, err = f.store.UpdateSocialCommunity(f.ctx, actor, created.ID, community.SocialInput{Name: "forbidden change", Summary: "forbidden", Visibility: "public", JoinPolicy: "open"})
		if !errors.Is(err, community.ErrForbidden) {
			t.Fatalf("unauthorized update must be forbidden, got %v", err)
		}
		after, err := f.store.GetSocialCommunity(f.ctx, owner, created.ID)
		if err != nil || after.Name != before.Name || after.Description != before.Description || after.Visibility != before.Visibility || !after.UpdatedAt.Equal(before.UpdatedAt) || countUpdates() != auditCount {
			t.Fatalf("unauthorized actor modified row/audit: before=%+v after=%+v err=%v", before, after, err)
		}
	}
	assertDenied(member) // Nonmember cannot reach the successful SQL path.
	join := func(actor string) community.Membership {
		t.Helper()
		pending, err := f.store.JoinSocialCommunity(f.ctx, actor, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		active, err := f.store.DecideSocialRequest(f.ctx, owner, created.ID, pending.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		return active
	}
	adminMembership := join(admin)
	if _, err = f.store.ChangeSocialMemberRole(f.ctx, owner, created.ID, adminMembership.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	join(member)
	assertDenied(member) // Ordinary member still cannot update.
	ownerInput := community.SocialInput{Name: "Synthetic owner hidden update", Summary: "owner description", Visibility: "hidden", JoinPolicy: "request"}
	updated, err := f.store.UpdateSocialCommunity(f.ctx, owner, created.ID, ownerInput)
	if err != nil {
		t.Fatalf("owner UPDATE must prepare and persist without untyped unused parameter: %v", err)
	}
	if updated.Name != ownerInput.Name || updated.Description != ownerInput.Summary || updated.Visibility != "hidden" || updated.MyRole == nil || *updated.MyRole != "owner" || countUpdates() != 1 {
		t.Fatalf("owner update fields/audit: %+v", updated)
	}
	adminInput := community.SocialInput{Name: "Synthetic admin private update", Summary: "admin description", Visibility: "private", JoinPolicy: "invite_only"}
	updated, err = f.store.UpdateSocialCommunity(f.ctx, admin, created.ID, adminInput)
	if err != nil || updated.Name != adminInput.Name || updated.Description != adminInput.Summary || updated.Visibility != "private" || updated.JoinPolicy != "invite_only" || updated.MyRole == nil || *updated.MyRole != "admin" || countUpdates() != 2 {
		t.Fatalf("admin UPDATE fields/audit: %+v %v", updated, err)
	}
	if err = f.store.LeaveSocialCommunity(f.ctx, member, created.ID); err != nil {
		t.Fatal(err)
	}
	assertDenied(member) // Revoked member cannot update after successful edits.
}
