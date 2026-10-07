package postgres

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSocialIntentAudienceAuthorizationIntegration(t *testing.T) {
	dsn := os.Getenv("BIRDTIE_DATABASE_URL")
	if dsn == "" {
		t.Skip("set BIRDTIE_DATABASE_URL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var installed bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.social_intent_audience_targets') IS NOT NULL`).Scan(&installed); err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Skip("037_social_intent_audiences not applied")
	}
	var communityID, cityContextID string
	if err := pool.QueryRow(ctx, `SELECT id FROM communities
		WHERE id='b1700000-0000-4000-8000-000000000030' AND lifecycle_status='active'`).Scan(&communityID); err != nil {
		t.Fatalf("required 003_community_social seed Community missing: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM contexts WHERE context_type='CITY' AND city_id='aberdeen-gb'`).Scan(&cityContextID); err != nil {
		t.Fatal(err)
	}
	// Creator, friend, Community invitee, local reader are independently owned.
	ids, cleanupPeople := ownedSocialPeopleFixture(t, ctx, pool, 4)
	defer cleanupPeople()
	if _, err := pool.Exec(ctx, `UPDATE user_profiles SET display_name='Synthetic Intent audience',visibility='public'
		WHERE account_id=ANY($1::uuid[])`, ids); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`, ids)
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM person_contexts WHERE person_account_id=ANY($1::uuid[])`, ids)
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM community_memberships WHERE user_account_id=ANY($1::uuid[])`, ids)
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`, ids)
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`, ids)
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, ids)
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`, ids)
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`, ids)
	}()
	store := New(pool, false)
	request, err := store.CreateFriendRequest(ctx, ids[0], ids[1], "Synthetic friend scope")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DecideRequest(ctx, ids[1], request.ID, "accept"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{ids[0], ids[2]} {
		if _, err := pool.Exec(ctx, `INSERT INTO community_memberships
			(community_id,user_account_id,role,status) VALUES($1,$2,'member','active')`, communityID, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.CreateSocialIntentDraft(ctx, ids[3], socialintent.DraftInput{
		Type: "FIND_COMPANION", Title: "Unauthorized Community draft",
		Constraints: json.RawMessage(`{}`), Audience: "COMMUNITY", Modality: "ONLINE",
		CommunityID: communityID, ExpiresAt: time.Now().Add(24 * time.Hour),
	}); err != socialintent.ErrInvalidAudience {
		t.Fatalf("nonmember created Community Intent: %v", err)
	}
	if _, err := store.CreateSocialIntentDraft(ctx, ids[0], socialintent.DraftInput{
		Type: "FIND_COMPANION", Title: "Unknown City draft",
		Constraints: json.RawMessage(`{}`), Audience: "LOCAL", Modality: "ONLINE",
		CityID: "unknown-city", ExpiresAt: time.Now().Add(24 * time.Hour),
	}); err != socialintent.ErrInvalidAudience {
		t.Fatalf("unknown City accepted: %v", err)
	}
	create := func(audience string, input socialintent.DraftInput) string {
		t.Helper()
		input.Type = "FIND_COMPANION"
		input.Title = "Synthetic " + audience
		input.Constraints = json.RawMessage(`{}`)
		input.Modality = "ONLINE"
		input.Audience = audience
		input.ExpiresAt = time.Now().Add(24 * time.Hour)
		item, err := store.CreateSocialIntentDraft(ctx, ids[0], input)
		if err != nil || item.Audience != audience {
			t.Fatalf("create %s: %+v %v", audience, item, err)
		}
		return item.ID
	}
	privateID := create("PRIVATE", socialintent.DraftInput{})
	friendID := create("FRIENDS", socialintent.DraftInput{})
	communityIntentID := create("COMMUNITY", socialintent.DraftInput{CommunityID: communityID})
	localID := create("LOCAL", socialintent.DraftInput{CityID: "aberdeen-gb"})
	publicID := create("PUBLIC", socialintent.DraftInput{})
	inviteID := create("INVITE_ONLY", socialintent.DraftInput{InviteeIDs: []string{ids[2]}})
	if own, err := store.GetOwnSocialIntent(ctx, ids[0], communityIntentID); err != nil || own.CommunityID != communityID {
		t.Fatalf("owner Community target not persisted: %+v %v", own, err)
	}
	if own, err := store.GetOwnSocialIntent(ctx, ids[0], inviteID); err != nil || len(own.InviteeIDs) != 1 || own.InviteeIDs[0] != ids[2] {
		t.Fatalf("owner invitees not persisted: %+v %v", own, err)
	}
	all := []string{privateID, friendID, communityIntentID, localID, publicID, inviteID}
	for _, id := range all {
		if _, err := store.GetVisibleSocialIntent(ctx, ids[1], id); err == nil {
			t.Fatal("draft leaked to another person")
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE social_intents SET status='ACTIVE' WHERE id=ANY($1::uuid[])`, all); err != nil {
		t.Fatal(err)
	}
	has := func(viewer, id string) bool {
		t.Helper()
		_, err := store.GetVisibleSocialIntent(ctx, viewer, id)
		if err != nil && err != socialintent.ErrNotFound {
			t.Fatal(err)
		}
		return err == nil
	}
	if has("", privateID) || has(ids[1], privateID) || !has("", publicID) ||
		has("", friendID) || has("", communityIntentID) || has("", localID) || has("", inviteID) {
		t.Fatal("anonymous audience policy leaked private scope")
	}
	if !has(ids[1], friendID) || has(ids[2], friendID) ||
		!has(ids[2], communityIntentID) || has(ids[1], communityIntentID) ||
		!has(ids[2], inviteID) || has(ids[1], inviteID) {
		t.Fatal("friend/community/invite audience boundary failed")
	}
	if visible, err := store.GetVisibleSocialIntent(ctx, ids[2], inviteID); err != nil || len(visible.InviteeIDs) != 0 {
		t.Fatalf("invitee list leaked to reader: %+v %v", visible, err)
	}
	if has(ids[3], localID) {
		t.Fatal("local Intent visible without explicit current City Context")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO person_contexts(person_account_id,context_id,relation)
		VALUES($1,$2,'current')`, ids[3], cityContextID); err != nil {
		t.Fatal(err)
	}
	if !has(ids[3], localID) || has(ids[1], localID) {
		t.Fatal("local City Context boundary failed")
	}
	if _, err := pool.Exec(ctx, `UPDATE social_intent_invitations SET status='revoked'
		WHERE intent_id=$1 AND invitee_account_id=$2`, inviteID, ids[2]); err != nil {
		t.Fatal(err)
	}
	if has(ids[2], inviteID) {
		t.Fatal("revoked invite still visible")
	}
	if _, err := pool.Exec(ctx, `UPDATE community_memberships SET status='left'
		WHERE community_id=$1 AND user_account_id=$2`, communityID, ids[2]); err != nil {
		t.Fatal(err)
	}
	if has(ids[2], communityIntentID) {
		t.Fatal("left member still sees Community Intent")
	}
	if err := store.BlockAccount(ctx, ids[0], ids[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSocialIntentDraft(ctx, ids[0], socialintent.DraftInput{
		Type: "FIND_COMPANION", Title: "Blocked invitation draft",
		Constraints: json.RawMessage(`{}`), Audience: "INVITE_ONLY", Modality: "ONLINE",
		InviteeIDs: []string{ids[1]}, ExpiresAt: time.Now().Add(24 * time.Hour),
	}); err != socialintent.ErrInvalidAudience {
		t.Fatalf("blocked invitee accepted: %v", err)
	}
	if has(ids[1], publicID) || has(ids[1], friendID) {
		t.Fatal("blocked person sees social Intent")
	}
	if _, err := pool.Exec(ctx, `UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	if has("", publicID) || has(ids[3], localID) {
		t.Fatal("private profile leaked public/local Intent")
	}
	if _, err := pool.Exec(ctx, `UPDATE social_intents SET status='CANCELLED' WHERE id=$1`, publicID); err != nil {
		t.Fatal(err)
	}
	if visible, err := store.ListVisibleSocialIntents(ctx, ""); err != nil {
		t.Fatal(err)
	} else {
		for _, item := range visible {
			if item.CreatorID == ids[0] {
				t.Fatal("anonymous list leaked hidden Intent")
			}
		}
	}
}
