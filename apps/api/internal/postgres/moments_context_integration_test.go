package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/jackc/pgx/v5/pgxpool"
)

func momentString(value string) *string { return &value }

func TestMomentContextLinksIntegration(t *testing.T) {
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("requires disposable migration database")
	}
	ownedMigrationDatabase(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	s := New(pool, false)
	const owner = "b1700000-0000-4000-8000-000000000010"
	const other = "b1700000-0000-4000-8000-000000000011"
	const place = "b1700000-0000-4000-8000-000000000015"
	const activity = "b1700000-0000-4000-8000-000000000016"
	const community = "b1700000-0000-4000-8000-000000000030"
	const organization = "b1700000-0000-4000-8000-000000000013"
	in := content.MomentInput{CityID: "aberdeen-gb", PlaceID: place,
		Title: "Private linked record", Body: "local test", TimePrecision: "unknown",
		LocationPrecision: "place", ActivityID: momentString(activity),
		CommunityID: momentString(community), OrganizationID: momentString(organization)}
	m, err := s.CreateMomentDraft(ctx, owner, in)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, q := range []string{
			`DELETE FROM agent_domain_outbox WHERE source_type='MOMENT' AND source_id=$1`,
			`DELETE FROM audit_events WHERE resource_type='moment' AND resource_id=$1`,
			`DELETE FROM moments WHERE id=$1`,
		} {
			if _, err := pool.Exec(ctx, q, m.ID); err != nil {
				t.Errorf("owned Moment context cleanup: %v", err)
			}
		}
	}()
	if m.Visibility != "private" || m.Status != "draft" || m.PlaceID != place ||
		len(m.ActivityIDs) != 1 || m.ActivityIDs[0] != activity ||
		m.CommunityID != community || m.OrganizationID != organization {
		t.Fatalf("context create: %+v", m)
	}
	if _, err := s.GetOwnMoment(ctx, other, m.ID); !errors.Is(err, content.ErrNotFound) {
		t.Fatalf("other person read: %v", err)
	}
	oldClient := content.MomentInput{CityID: in.CityID, PlaceID: place,
		Title: "Edited privately", TimePrecision: "unknown", LocationPrecision: "place"}
	m, err = s.UpdateMomentDraft(ctx, owner, m.ID, m.Revision, oldClient)
	if err != nil || len(m.ActivityIDs) != 1 || m.CommunityID != community || m.OrganizationID != organization {
		t.Fatalf("old client preserves links: %+v %v", m, err)
	}
	oldClient.ActivityID = momentString("")
	m, err = s.UpdateMomentDraft(ctx, owner, m.ID, m.Revision, oldClient)
	if err != nil || len(m.ActivityIDs) != 0 || m.CommunityID != community {
		t.Fatalf("explicit clear: %+v %v", m, err)
	}
	oldClient.CommunityID = momentString("b1700000-0000-4000-8000-000000000031")
	if _, err := s.UpdateMomentDraft(ctx, other, m.ID, m.Revision, oldClient); !errors.Is(err, content.ErrConflict) {
		t.Fatalf("other person edit: %v", err)
	}
	if _, err := s.CreateMomentDraft(ctx, other, oldClient); !errors.Is(err, content.ErrConflict) {
		t.Fatalf("pending Community member link: %v", err)
	}
	if _, err := s.UpdateMomentDraft(ctx, owner, m.ID, m.Revision, content.MomentInput{
		CityID: in.CityID, PlaceID: place, Title: "Invalid activity", TimePrecision: "unknown",
		LocationPrecision: "place", ActivityID: momentString("11111111-1111-4111-8111-111111111111"),
	}); !errors.Is(err, content.ErrConflict) {
		t.Fatalf("invalid Activity link: %v", err)
	}
	still, err := s.GetOwnMoment(ctx, owner, m.ID)
	if err != nil || still.Revision != m.Revision || still.Title != m.Title {
		t.Fatalf("invalid update committed: %+v %v", still, err)
	}
	if err := s.WithdrawMoment(ctx, owner, m.ID, m.Revision); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListOwnMoments(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list {
		if item.ID == m.ID {
			t.Fatal("withdrawn context appeared in own list")
		}
	}
}
