package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/jackc/pgx/v5/pgxpool"
)

// One person may belong to several principals; a role on one must not grant
// management of another principal's activity or expose its draft.
func TestActorAuthorizationAcrossPrincipalsIntegration(t *testing.T) {
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("requires disposable database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	people := make([]string, 4) // Organization, Community, Business owners; outsider
	for i := range people {
		if err = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type)
			VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&people[i]); err != nil {
			t.Fatal(err)
		}
	}
	var orgPrincipal, businessPrincipal, orgID, communityID, businessID string
	var activityIDs []string
	defer func() {
		for _, id := range activityIDs {
			_, _ = pool.Exec(ctx, `DELETE FROM admin_audit_events WHERE resource_type='activity' AND resource_id=$1`, id)
			_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE resource_type='activity' AND resource_id=$1`, id)
			_, _ = pool.Exec(ctx, `DELETE FROM activities WHERE id=$1`, id)
		}
		if communityID != "" {
			_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE resource_type='community' AND resource_id=$1`, communityID)
			_, _ = pool.Exec(ctx, `DELETE FROM community_memberships WHERE community_id=$1`, communityID)
			_, _ = pool.Exec(ctx, `DELETE FROM communities WHERE id=$1`, communityID)
		}
		if orgID != "" {
			_, _ = pool.Exec(ctx, `DELETE FROM organization_memberships WHERE organization_id=$1`, orgID)
			_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, orgID)
		}
		if businessID != "" {
			_, _ = pool.Exec(ctx, `DELETE FROM business_memberships WHERE business_id=$1`, businessID)
			_, _ = pool.Exec(ctx, `DELETE FROM businesses WHERE id=$1`, businessID)
		}
		_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, people)
		ids := append(append([]string{}, people...), orgPrincipal, businessPrincipal)
		_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE id=ANY($1::uuid[])`, ids)
	}()
	if err = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'organization') RETURNING id`).Scan(&orgPrincipal); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO organizations(account_id,organization_type,name)
		VALUES($1,'club','Synthetic matrix Organization') RETURNING id`, orgPrincipal).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO organization_memberships(organization_id,user_account_id,role)
		VALUES($1,$2,'owner'),($1,$3,'member'),($1,$4,'member')`, orgID, people[0], people[1], people[2]); err != nil {
		t.Fatal(err)
	}
	store := New(pool, false)
	comm, err := store.CreateSocialCommunity(ctx, people[1], community.SocialInput{
		Name: "Synthetic matrix Community", Visibility: "public", JoinPolicy: "open", CityID: "aberdeen-gb",
	})
	if err != nil {
		t.Fatal(err)
	}
	communityID = comm.ID
	if _, err = pool.Exec(ctx, `INSERT INTO community_memberships(community_id,user_account_id,role,status)
		VALUES($1,$2,'member','active'),($1,$3,'member','active')`, communityID, people[0], people[2]); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'business') RETURNING id`).Scan(&businessPrincipal); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO businesses(account_id,name,claim_status,claim_source_url,claim_reviewed_by,claim_reviewed_at)
		VALUES($1,'Synthetic matrix Business','verified','https://example.org/matrix',$2,now()) RETURNING id`,
		businessPrincipal, people[3]).Scan(&businessID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO business_memberships(business_id,user_account_id,role)
		VALUES($1,$2,'owner'),($1,$3,'member'),($1,$4,'member')`, businessID, people[2], people[0], people[1]); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(72 * time.Hour).UTC().Truncate(time.Second)
	actors := []struct {
		kind  string
		id    string
		owner int
	}{
		{"ORGANIZATION", orgID, 0},
		{"COMMUNITY", communityID, 1},
		{"BUSINESS", businessID, 2},
	}
	for _, principal := range actors {
		input := activitypublish.Input{
			Organizer: activitypublish.Organizer{Type: principal.kind, ID: principal.id},
			CityID:    "aberdeen-gb", Title: "Synthetic actor matrix", Summary: "授权隔离测试",
			StartsAt: start, EndsAt: start.Add(time.Hour), TimeZone: "Europe/London",
			Modality: "online", PhysicalPlaceStatus: "not_applicable", Visibility: "public",
		}
		for i, person := range people {
			if i == principal.owner {
				continue
			}
			if _, e := store.CreateSocialDraft(ctx, person, input); !errors.Is(e, activitypublish.ErrForbidden) {
				t.Fatalf("%s non-owner %d created draft: %v", principal.kind, i, e)
			}
		}
		draft, e := store.CreateSocialDraft(ctx, people[principal.owner], input)
		if e != nil {
			t.Fatalf("%s owner create: %v", principal.kind, e)
		}
		activityIDs = append(activityIDs, draft.ID)
		for i, person := range people {
			managed, e := store.ListSocialActivities(ctx, person)
			if e != nil {
				t.Fatal(e)
			}
			found := false
			for _, item := range managed {
				if item.ID == draft.ID {
					found = true
				}
			}
			if found != (i == principal.owner) {
				t.Fatalf("%s draft leaked in managed list to actor %d", principal.kind, i)
			}
			if i == principal.owner {
				continue
			}
			if _, e = store.UpdateSocialActivity(ctx, person, draft.ID, input); !errors.Is(e, activitypublish.ErrForbidden) {
				t.Fatalf("%s non-owner %d edited: %v", principal.kind, i, e)
			}
			if _, e = store.PublishSocialActivity(ctx, person, draft.ID); !errors.Is(e, activitypublish.ErrForbidden) {
				t.Fatalf("%s non-owner %d published: %v", principal.kind, i, e)
			}
		}
		if _, e = store.PublishSocialActivity(ctx, people[principal.owner], draft.ID); e != nil {
			t.Fatalf("%s owner publish: %v", principal.kind, e)
		}
		public, e := store.GetActivity(ctx, draft.ID, people[3])
		if e != nil || public.Organizer.Type != principal.kind || public.Organizer.ID != principal.id {
			t.Fatalf("%s public organizer projection: %+v %v", principal.kind, public.Organizer, e)
		}
		owner := people[principal.owner]
		if _, e = pool.Exec(ctx, `UPDATE accounts SET status='suspended' WHERE id=$1`, owner); e != nil {
			t.Fatal(e)
		}
		managed, e := store.ListSocialActivities(ctx, owner)
		if e != nil {
			t.Fatal(e)
		}
		for _, item := range managed {
			if item.ID == draft.ID {
				t.Fatalf("%s suspended owner retained managed draft", principal.kind)
			}
		}
		if _, e = store.CancelSocialActivity(ctx, owner, draft.ID); !errors.Is(e, activitypublish.ErrForbidden) {
			t.Fatalf("%s suspended owner cancelled activity: %v", principal.kind, e)
		}
		if _, e = pool.Exec(ctx, `UPDATE accounts SET status='active' WHERE id=$1`, owner); e != nil {
			t.Fatal(e)
		}
	}
}
