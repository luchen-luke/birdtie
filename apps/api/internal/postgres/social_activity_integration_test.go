package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activityparticipation"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSocialActivityVisibilityIntegration(t *testing.T) {
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
	people, cleanupPeople := ownedSocialPeopleFixture(t, ctx, pool, 3)
	defer cleanupPeople()
	store := New(pool, false)
	comm, err := store.CreateSocialCommunity(ctx, people[0], community.SocialInput{Name: "Synthetic activity test", Visibility: "private", JoinPolicy: "request", CityID: "aberdeen-gb"})
	if err != nil {
		t.Fatal(err)
	}
	var activityIDs []string
	defer func() {
		for _, id := range activityIDs {
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM audit_events WHERE resource_type='activity' AND resource_id=$1`, id)
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM activities WHERE id=$1`, id)
		}
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM audit_events WHERE resource_type='community' AND resource_id=$1`, comm.ID)
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM communities WHERE id=$1`, comm.ID)
	}()
	m, err := store.JoinSocialCommunity(ctx, people[1], comm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.DecideSocialRequest(ctx, people[0], comm.ID, m.ID, true); err != nil {
		t.Fatal(err)
	}
	base := activitypublish.Input{CityID: "aberdeen-gb", Title: "合成羽毛球活动", Summary: "测试数据", StartsAt: time.Now().Add(48 * time.Hour), EndsAt: time.Now().Add(50 * time.Hour), TimeZone: "Europe/London",
		Organizer: activitypublish.Organizer{Type: "COMMUNITY", ID: comm.ID}}
	public := base
	public.Visibility = "public"
	if _, err = store.CreateSocialDraft(ctx, people[1], public); !errors.Is(err, activitypublish.ErrForbidden) {
		t.Fatalf("ordinary Community member created activity: %v", err)
	}
	a, err := store.CreateSocialDraft(ctx, people[0], public)
	if err != nil {
		t.Fatal(err)
	}
	activityIDs = append(activityIDs, a.ID)
	if a.Organizer.Type != "COMMUNITY" || a.Organizer.ID != comm.ID {
		t.Fatalf("organizer response: %+v", a.Organizer)
	}
	if _, err = store.PublishSocialActivity(ctx, people[0], a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.UpdateSocialActivity(ctx, people[1], a.ID, public); !errors.Is(err, activitypublish.ErrForbidden) {
		t.Fatalf("ordinary Community member edited activity: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE community_memberships SET role='admin'
		WHERE community_id=$1 AND user_account_id=$2`, comm.ID, people[1]); err != nil {
		t.Fatal(err)
	}
	if _, err = store.UpdateSocialActivity(ctx, people[1], a.ID, public); err != nil {
		t.Fatalf("Community admin edit: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE community_memberships SET role='member'
		WHERE community_id=$1 AND user_account_id=$2`, comm.ID, people[1]); err != nil {
		t.Fatal(err)
	}
	if _, err = store.CancelSocialActivity(ctx, people[1], a.ID); !errors.Is(err, activitypublish.ErrForbidden) {
		t.Fatalf("ordinary Community member cancelled activity: %v", err)
	}
	seen, err := store.GetActivity(ctx, a.ID, people[2])
	if err != nil {
		t.Fatal(err)
	}
	if seen.Organizer.Type != "COMMUNITY" || seen.Organizer.Name != comm.Name {
		t.Fatalf("public organizer: %+v", seen.Organizer)
	}
	if _, changed, err := store.JoinActivity(ctx, people[2], a.ID); err != nil || !changed {
		t.Fatalf("outsider public RSVP: %v %v", changed, err)
	}
	var membershipCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM community_memberships WHERE community_id=$1 AND user_account_id=$2`, comm.ID, people[2]).Scan(&membershipCount); err != nil {
		t.Fatal(err)
	}
	if membershipCount != 0 {
		t.Fatal("RSVP silently joined Community")
	}
	members := base
	members.Visibility = "organizer_members"
	members.Title = "合成成员活动"
	b, err := store.CreateSocialDraft(ctx, people[0], members)
	if err != nil {
		t.Fatal(err)
	}
	activityIDs = append(activityIDs, b.ID)
	if _, err = store.PublishSocialActivity(ctx, people[1], b.ID); !errors.Is(err, activitypublish.ErrForbidden) {
		t.Fatalf("ordinary Community member published activity: %v", err)
	}
	if _, err = store.PublishSocialActivity(ctx, people[0], b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetActivity(ctx, b.ID, people[1]); err != nil {
		t.Fatalf("member detail: %v", err)
	}
	if _, err = store.GetActivity(ctx, b.ID, people[2]); !errors.Is(err, foundation.ErrNotFound) {
		t.Fatalf("outsider detail: %v", err)
	}
	if _, _, err = store.JoinActivity(ctx, people[2], b.ID); !errors.Is(err, activityparticipation.ErrNotFound) {
		t.Fatalf("outsider RSVP: %v", err)
	}
	if _, _, err = store.JoinActivity(ctx, people[1], b.ID); err != nil {
		t.Fatalf("member RSVP: %v", err)
	}
	person := base
	person.Organizer = activitypublish.Organizer{Type: "PERSON", ID: people[0]}
	person.Visibility = "invite_only"
	person.Title = "合成邀请活动"
	if _, err = store.CreateSocialDraft(ctx, people[1], person); !errors.Is(err, activitypublish.ErrForbidden) {
		t.Fatalf("another Person created owner activity: %v", err)
	}
	c, err := store.CreateSocialDraft(ctx, people[0], person)
	if err != nil {
		t.Fatal(err)
	}
	activityIDs = append(activityIDs, c.ID)
	if _, err = store.PublishSocialActivity(ctx, people[1], c.ID); !errors.Is(err, activitypublish.ErrForbidden) {
		t.Fatalf("another Person published activity: %v", err)
	}
	if _, err = store.PublishSocialActivity(ctx, people[0], c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.UpdateSocialActivity(ctx, people[1], c.ID, person); !errors.Is(err, activitypublish.ErrForbidden) {
		t.Fatalf("another Person edited activity: %v", err)
	}
	if _, err = store.CancelSocialActivity(ctx, people[1], c.ID); !errors.Is(err, activitypublish.ErrForbidden) {
		t.Fatalf("another Person cancelled activity: %v", err)
	}
	if _, err = store.GetActivity(ctx, c.ID, people[2]); !errors.Is(err, foundation.ErrNotFound) {
		t.Fatalf("uninvited detail: %v", err)
	}
	if err = store.InviteActivityPerson(ctx, people[1], c.ID, people[2]); !errors.Is(err, activitypublish.ErrForbidden) {
		t.Fatalf("non-organizer invited a person: %v", err)
	}
	if err = store.InviteActivityPerson(ctx, people[0], c.ID, people[2]); err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetActivity(ctx, c.ID, people[2]); err != nil {
		t.Fatalf("invited detail: %v", err)
	}
	hybrid := base
	hybrid.Title = "合成混合活动"
	hybrid.Visibility = "public"
	hybrid.Modality = "hybrid"
	hybrid.PhysicalPlaceStatus = "tbd"
	d, err := store.CreateSocialDraft(ctx, people[0], hybrid)
	if err != nil {
		t.Fatalf("hybrid draft: %v", err)
	}
	activityIDs = append(activityIDs, d.ID)
	if d.Modality != "hybrid" || d.PhysicalPlaceStatus != "tbd" || d.PlaceID != nil {
		t.Fatalf("hybrid draft location: %+v", d)
	}
	if _, err = store.PublishSocialActivity(ctx, people[0], d.ID); err != nil {
		t.Fatalf("hybrid publish: %v", err)
	}
	hybridSeen, err := store.GetActivity(ctx, d.ID, people[2])
	if err != nil || hybridSeen.Modality != "hybrid" || hybridSeen.PhysicalPlaceStatus != "tbd" || hybridSeen.PlaceID != "" {
		t.Fatalf("hybrid public detail: %+v, %v", hybridSeen, err)
	}
	managed, err := store.ListSocialActivities(ctx, people[0])
	if err != nil {
		t.Fatalf("list social activities: %v", err)
	}
	found := map[string]bool{}
	for _, item := range managed {
		found[item.ID] = true
	}
	for _, id := range activityIDs {
		if !found[id] {
			t.Fatalf("managed list omitted %s", id)
		}
	}
}
