package postgres

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/follow"
	"github.com/birdtie/birdtie/apps/api/internal/opportunity"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
)

// Uses only this test's random accounts, Cities, Places and Activities. Real
// authorization and lifecycle methods establish relationships and publication;
// synthetic records are never treated as real pilot or verified identity data.
func TestOpportunityExplanationLiveSourceIntegration(t *testing.T) {
	pool := newPeopleTestPool(t)
	f := newPeoplePair(t, pool)
	f.scopes()
	activities := []string{}
	organizationID := ""
	t.Cleanup(func() {
		for _, statement := range []string{
			`DELETE FROM admin_audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM follows WHERE follower_account_id=ANY($1::uuid[])`,
		} {
			if _, err := pool.Exec(f.ctx, statement, f.ids); err != nil {
				t.Errorf("opportunity cleanup: %v", err)
			}
		}
		if _, err := pool.Exec(f.ctx, `DELETE FROM activities WHERE id=ANY($1::uuid[])`, activities); err != nil {
			t.Errorf("Activity cleanup: %v", err)
		}
		if organizationID != "" {
			if _, err := pool.Exec(f.ctx, `DELETE FROM organizations WHERE id=$1`, organizationID); err != nil {
				t.Errorf("Organization cleanup: %v", err)
			}
		}
	})
	constraints, _ := json.Marshal(socialintent.Constraints{Category: "badminton", PlaceID: f.places[0]})
	source, err := f.store.CreateSocialIntentDraft(f.ctx, f.ids[0], socialintent.DraftInput{
		Type: "FIND_ACTIVITY", Title: "PRIVATE_OPPORTUNITY_SOURCE_SENTINEL", Constraints: constraints,
		Audience: "PRIVATE", Modality: "IN_PERSON", ContextID: f.cityContexts[0], ExpiresAt: time.Now().Add(48 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ActivateSocialIntent(f.ctx, f.ids[0], source.ID); err != nil {
		t.Fatal(err)
	}
	createActivity := func(actor, kind, id, title string) string {
		t.Helper()
		input := activitypublish.Input{Organizer: activitypublish.Organizer{Type: kind, ID: id}, CityID: f.cities[0],
			PlaceID: f.places[0], Title: title, Summary: "RAW_ACTIVITY_SUMMARY_SENTINEL", Description: "RAW_ACTIVITY_DESCRIPTION_SENTINEL",
			StartsAt: time.Now().Add(24 * time.Hour), EndsAt: time.Now().Add(26 * time.Hour), TimeZone: "Europe/London",
			CategoryCode: "badminton", Visibility: "public", Modality: "in_person", PhysicalPlaceStatus: "confirmed"}
		draft, e := f.store.CreateSocialDraft(f.ctx, actor, input)
		if e != nil {
			t.Fatal(e)
		}
		activities = append(activities, draft.ID)
		if _, e = f.store.PublishSocialActivity(f.ctx, actor, draft.ID); e != nil {
			t.Fatal(e)
		}
		return draft.ID
	}
	friendActivity := createActivity(f.ids[1], "PERSON", f.ids[1], "好友主办的合成活动")
	request, err := f.store.CreateFriendRequest(f.ctx, f.ids[0], f.ids[1], "Explicit synthetic friendship")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.DecideRequest(f.ctx, f.ids[1], request.ID, "accept"); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(f.ctx, `INSERT INTO organizations(account_id,organization_type,name,profile)
 VALUES($1,'club','RAW_ORGANIZATION_NAME_SENTINEL','{"privateNote":"PRIVATE_ORGANIZATION_PROFILE_SENTINEL"}') RETURNING id`, f.ids[3]).Scan(&organizationID); err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO organization_memberships(organization_id,user_account_id,role,status) VALUES($1,$2,'owner','active')`, organizationID, f.ids[2])
	organizationActivity := createActivity(f.ids[2], "ORGANIZATION", organizationID, "组织主办的合成活动")
	if _, err = f.store.Follow(f.ctx, f.ids[0], follow.Target{Type: "ORGANIZATION", ID: organizationID}); err != nil {
		t.Fatal(err)
	}
	const communityID = "b1700000-0000-4000-8000-000000000030"
	f.exec(`INSERT INTO community_memberships(community_id,user_account_id,role,status) VALUES($1,$2,'admin','active')`, communityID, f.ids[2])
	if _, err = f.store.JoinSocialCommunity(f.ctx, f.ids[0], communityID); err != nil {
		t.Fatal(err)
	}
	communityActivity := createActivity(f.ids[2], "COMMUNITY", communityID, "社群主办的合成活动")
	load := func() (opportunity.Inputs, []opportunity.Candidate) {
		t.Helper()
		inputs, e := f.store.LoadOpportunityInputs(f.ctx, f.ids[0])
		if e != nil {
			t.Fatal(e)
		}
		return inputs, opportunity.Generate(time.Now(), inputs)
	}
	find := func(candidates []opportunity.Candidate, id string) opportunity.Candidate {
		t.Helper()
		for _, candidate := range candidates {
			if candidate.Entity.ID == id {
				return candidate
			}
		}
		t.Fatalf("authorized candidate missing %s: %+v", id, candidates)
		return opportunity.Candidate{}
	}
	_, candidates := load()
	if len(candidates) != 3 {
		t.Fatalf("matching fabricated or mixed outside scope: %+v", candidates)
	}
	friend := find(candidates, friendActivity)
	organization := find(candidates, organizationActivity)
	community := find(candidates, communityActivity)
	if friend.Title != "好友主办的合成活动" || friend.PlaceName != "Synthetic new people Place" || friend.RouteTier != opportunity.TierExistingTie || !opportunityReasonContains(friend, "TIE_ORGANIZER") {
		t.Fatalf("real Tie reason/name: %+v", friend)
	}
	if organization.Title != "组织主办的合成活动" || !opportunityReasonContains(organization, "ORGANIZATION_ACTIVITY") || !opportunityReasonContains(organization, "FOLLOWED_ORGANIZER") || !strings.Contains(organization.Reason, "这是一条组织主办的活动") {
		t.Fatalf("typed organization reason: %+v", organization)
	}
	if !opportunityReasonContains(community, "JOINED_COMMUNITY") || opportunityReasonContains(community, "ORGANIZATION_ACTIVITY") {
		t.Fatalf("Community/Organization meanings mixed: %+v", community)
	}
	payload, e := json.Marshal(candidates)
	if e != nil {
		t.Fatal(e)
	}
	for _, value := range []string{"PRIVATE_OPPORTUNITY_SOURCE_SENTINEL", "RAW_ORGANIZATION_NAME_SENTINEL", "PRIVATE_ORGANIZATION_PROFILE_SENTINEL", "RAW_ACTIVITY_SUMMARY_SENTINEL", "RAW_ACTIVITY_DESCRIPTION_SENTINEL", "latitude", "longitude", "memberAccountIds", "body", "好友也喜欢", "会来", "已核验"} {
		if strings.Contains(string(payload), value) {
			t.Fatalf("explanation exposes/invents %q: %s", value, payload)
		}
	}
	// A private friend's public Activity is still an authorized public source;
	// the explanation reveals only the owner's existing relationship, no Profile.
	f.exec(`UPDATE user_profiles SET visibility='private',bio='PRIVATE_FRIEND_BIO_SENTINEL' WHERE account_id=$1`, f.ids[1])
	_, candidates = load()
	if !opportunityReasonContains(find(candidates, friendActivity), "TIE_ORGANIZER") {
		t.Fatal("public Activity lost lawful owner relationship explanation")
	}
	// Suspending the Person must remove the friendship reason without claiming
	// that their already public Activity has become private.
	f.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.ids[1])
	inputs, candidates := load()
	friend = find(candidates, friendActivity)
	if inputs.TiedPeople[f.ids[1]] || opportunityReasonContains(friend, "TIE_ORGANIZER") || friend.RouteTier == opportunity.TierExistingTie {
		t.Fatalf("suspended friend still influenced explanation: %+v", friend)
	}
	if _, err := f.store.GetActivity(f.ctx, friendActivity, f.ids[0]); err != nil {
		t.Fatalf("public Activity unexpectedly unreadable: %v", err)
	}
	f.exec(`UPDATE accounts SET status='active' WHERE id=$1`, f.ids[1])
	// Defend against a stale active Tie whose accepted request has been revoked.
	f.exec(`UPDATE connection_requests SET state='declined' WHERE id=$1`, request.ID)
	inputs, candidates = load()
	if inputs.TiedPeople[f.ids[1]] || opportunityReasonContains(find(candidates, friendActivity), "TIE_ORGANIZER") {
		t.Fatal("nonaccepted request supplied friendship explanation")
	}
	f.exec(`UPDATE connection_requests SET state='accepted' WHERE id=$1`, request.ID)
	_, candidates = load()
	if !opportunityReasonContains(find(candidates, friendActivity), "TIE_ORGANIZER") {
		t.Fatal("restored valid friend source missing")
	}
	if err = f.store.Unfollow(f.ctx, f.ids[0], follow.Target{Type: "ORGANIZATION", ID: organizationID}); err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE community_memberships SET status='left' WHERE community_id=$1 AND user_account_id=$2`, communityID, f.ids[0])
	_, candidates = load()
	organization = find(candidates, organizationActivity)
	community = find(candidates, communityActivity)
	if opportunityReasonContains(organization, "FOLLOWED_ORGANIZER") || !opportunityReasonContains(organization, "ORGANIZATION_ACTIVITY") {
		t.Fatal("unfollow erased typed organizer or retained follow")
	}
	if opportunityReasonContains(community, "JOINED_COMMUNITY") {
		t.Fatal("left membership retained relationship explanation")
	}
	ties, err := f.store.ListTies(f.ctx, f.ids[0])
	if err != nil || len(ties) != 1 {
		t.Fatalf("fixture Tie: %+v %v", ties, err)
	}
	if err = f.store.RemoveTie(f.ctx, f.ids[0], ties[0].ID); err != nil {
		t.Fatal(err)
	}
	_, candidates = load()
	if opportunityReasonContains(find(candidates, friendActivity), "TIE_ORGANIZER") {
		t.Fatal("removed Tie retained friendship explanation")
	}
	if err = f.store.BlockAccount(f.ctx, f.ids[0], f.ids[1]); err != nil {
		t.Fatal(err)
	}
	_, candidates = load()
	for _, candidate := range candidates {
		if candidate.Entity.ID == friendActivity {
			t.Fatal("Block retained organizer Activity or reason")
		}
	}
	f.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.places[0])
	_, candidates = load()
	if len(candidates) != 0 {
		t.Fatal("hidden Place retained title/reason candidates")
	}
}

func opportunityReasonContains(candidate opportunity.Candidate, wanted string) bool {
	for _, code := range candidate.ReasonCodes {
		if code == wanted {
			return true
		}
	}
	return false
}
