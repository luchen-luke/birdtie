package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCommunitySocialPermissionsIntegration(t *testing.T) {
	dsn := os.Getenv("BIRDTIE_DATABASE_URL")
	if dsn == "" {
		t.Skip("set BIRDTIE_DATABASE_URL for PostgreSQL integration")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	people, cleanupPeople := ownedSocialPeopleFixture(t, ctx, pool, 3)
	defer cleanupPeople()
	s := New(pool, false)
	created, err := s.CreateSocialCommunity(ctx, people[0], community.SocialInput{
		Name: "Synthetic permission test", Summary: "rollback cleanup", Visibility: "private", JoinPolicy: "request"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM audit_events WHERE resource_type='community' AND resource_id=$1`, created.ID)
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM communities WHERE id=$1`, created.ID)
	}()
	if created.MemberCount != 1 || created.MyRole == nil || *created.MyRole != "owner" {
		t.Fatalf("creator owner: %+v", created)
	}
	member, err := s.JoinSocialCommunity(ctx, people[1], created.ID)
	if err != nil || member.Status != "pending" {
		t.Fatalf("request: %+v %v", member, err)
	}
	_, err = s.DecideSocialRequest(ctx, people[2], created.ID, member.ID, true)
	if !errors.Is(err, community.ErrForbidden) {
		t.Fatalf("outsider approval: %v", err)
	}
	_, err = s.DecideSocialRequest(ctx, people[1], created.ID, member.ID, true)
	if !errors.Is(err, community.ErrForbidden) {
		t.Fatalf("self approval: %v", err)
	}
	_, err = s.ListSocialMembers(ctx, people[2], created.ID, false)
	if !errors.Is(err, community.ErrNotFound) {
		t.Fatalf("private member leak: %v", err)
	}
	member, err = s.DecideSocialRequest(ctx, people[0], created.ID, member.ID, true)
	if err != nil || member.Status != "active" {
		t.Fatalf("owner approval: %+v %v", member, err)
	}
	_, err = s.JoinSocialCommunity(ctx, people[1], created.ID)
	if !errors.Is(err, community.ErrConflict) {
		t.Fatalf("duplicate join: %v", err)
	}
	_, err = s.ChangeSocialMemberRole(ctx, people[1], created.ID, member.ID, "admin")
	if !errors.Is(err, community.ErrForbidden) {
		t.Fatalf("member self promotion: %v", err)
	}
	member, err = s.ChangeSocialMemberRole(ctx, people[0], created.ID, member.ID, "admin")
	if err != nil || member.Role != "admin" {
		t.Fatalf("owner role update: %+v %v", member, err)
	}
	if err = s.ArchiveSocialCommunity(ctx, people[1], created.ID); !errors.Is(err, community.ErrForbidden) {
		t.Fatalf("admin archive: %v", err)
	}
	if err = s.TransferSocialOwner(ctx, people[1], created.ID, people[2]); !errors.Is(err, community.ErrForbidden) {
		t.Fatalf("admin transfer: %v", err)
	}
	if err = s.TransferSocialOwner(ctx, people[0], created.ID, people[1]); err != nil {
		t.Fatal(err)
	}
	if err = s.ArchiveSocialCommunity(ctx, people[0], created.ID); !errors.Is(err, community.ErrForbidden) {
		t.Fatalf("old owner archive: %v", err)
	}
	if err = s.ArchiveSocialCommunity(ctx, people[1], created.ID); err != nil {
		t.Fatal(err)
	}
	_, err = s.JoinSocialCommunity(ctx, people[2], created.ID)
	if !errors.Is(err, community.ErrNotFound) {
		t.Fatalf("archive join: %v", err)
	}
	hidden, err := s.CreateSocialCommunity(ctx, people[0], community.SocialInput{
		Name: "Synthetic hidden group", Summary: "private summary", Visibility: "hidden", JoinPolicy: "invite_only"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM audit_events WHERE resource_type='community' AND resource_id=$1`, hidden.ID)
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM communities WHERE id=$1`, hidden.ID)
	}()
	if _, err = s.GetSocialCommunity(ctx, people[2], hidden.ID); !errors.Is(err, community.ErrNotFound) {
		t.Fatalf("hidden outsider detail: %v", err)
	}
	if _, err = s.ListSocialCommunities(ctx, people[2], "", false); err != nil {
		t.Fatal(err)
	}
	invitation, err := s.InviteSocialMember(ctx, people[0], hidden.ID, people[1])
	if err != nil || invitation.Status != "invited" {
		t.Fatalf("hidden invitation: %+v %v", invitation, err)
	}
	visible, err := s.GetSocialCommunity(ctx, people[1], hidden.ID)
	if err != nil || visible.ID != hidden.ID || visible.Description != "" {
		t.Fatalf("hidden invitee limited detail: %+v %v", visible, err)
	}
	mine, err := s.ListSocialCommunities(ctx, people[1], "", true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range mine {
		if item.ID == hidden.ID {
			found = true
			if item.Description != "" {
				t.Fatalf("hidden invitation leaked description: %+v", item)
			}
		}
	}
	if !found {
		t.Fatal("invited hidden community missing from mine")
	}
}
