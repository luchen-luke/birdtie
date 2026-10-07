package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/community"
)

type automaticMaintainerFixture struct {
	f                                       *agentVisibilityFixture
	ownerCanary, reviewerCanary             string
	communityIDs, placeIDs, activityIDs     []string
	placeCandidateIDs, activityCandidateIDs []string
}

func newAutomaticMaintainerFixture(t *testing.T) *automaticMaintainerFixture {
	t.Helper()
	r := &automaticMaintainerFixture{f: agentVisibilityTestFixture(t)}
	b := r.f.private.base
	r.ownerCanary = "synthetic-private-maintainer-owner-" + b.person.ID
	r.reviewerCanary = "synthetic-private-maintainer-reviewer-" + b.other.ID
	for _, item := range []struct {
		id, canary string
		access     agentprofile.PrivateAccess
	}{
		{b.person.ID, r.ownerCanary, r.f.private.owner}, {b.other.ID, r.reviewerCanary, r.f.private.peer},
	} {
		b.exec(`UPDATE accounts SET handle=$2 WHERE id=$1`, item.id, item.canary)
		b.exec(`UPDATE user_profiles SET display_name=$2,visibility='private' WHERE account_id=$1`, item.id, item.canary)
		current, err := b.store.ReadOwnAgentProfileVisibility(b.ctx, item.access)
		if err != nil {
			t.Fatal(err)
		}
		rules := agentprofile.DefaultFieldRules()
		rules[agentprofile.FieldDisplayName] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPrivate}
		if _, err = b.store.ReplaceOwnAgentProfileVisibility(b.ctx, item.access, agentprofile.ReplaceVisibilityInput{ExpectedVersion: current.Profile.ProfileVersion, Rules: rules}); err != nil {
			t.Fatal(err)
		}
	}
	b.exec(`INSERT INTO city_editor_memberships(city_id,account_id,role) VALUES
		('aberdeen-gb',$1,'contributor'),('aberdeen-gb',$2,'reviewer')`, b.person.ID, b.other.ID)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, item := range []struct {
			query string
			ids   []string
		}{
			{`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`, b.accounts},
			{`DELETE FROM audit_events WHERE resource_type='community' AND resource_id=ANY($1::text[])`, r.communityIDs},
			{`DELETE FROM admin_audit_events WHERE actor_account_id=ANY($1::uuid[])`, b.accounts},
			{`DELETE FROM activity_sources WHERE candidate_id=ANY($1::uuid[])`, r.activityCandidateIDs},
			{`DELETE FROM activity_candidates WHERE id=ANY($1::uuid[])`, r.activityCandidateIDs},
			{`DELETE FROM city_seed_activities WHERE activity_id=ANY($1::uuid[])`, r.activityIDs},
			{`DELETE FROM activities WHERE id=ANY($1::uuid[])`, r.activityIDs},
			{`DELETE FROM place_sources WHERE candidate_id=ANY($1::uuid[])`, r.placeCandidateIDs},
			{`DELETE FROM place_candidates WHERE id=ANY($1::uuid[])`, r.placeCandidateIDs},
			{`DELETE FROM city_seed_items WHERE place_id=ANY($1::uuid[])`, r.placeIDs},
			{`DELETE FROM place_aliases WHERE place_id=ANY($1::uuid[])`, r.placeIDs},
			{`DELETE FROM places WHERE id=ANY($1::uuid[])`, r.placeIDs},
			{`DELETE FROM communities WHERE id=ANY($1::uuid[])`, r.communityIDs},
			{`DELETE FROM city_editor_memberships WHERE account_id=ANY($1::uuid[])`, b.accounts},
		} {
			if _, err := b.pool.Exec(ctx, item.query, item.ids); err != nil {
				t.Errorf("owned automatic-maintainer fixture cleanup failed: %v", err)
			}
		}
		var remaining int
		if err := b.pool.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM communities WHERE id=ANY($1::uuid[]))+
			(SELECT count(*) FROM places WHERE id=ANY($2::uuid[]))+
			(SELECT count(*) FROM activities WHERE id=ANY($3::uuid[]))+
			(SELECT count(*) FROM place_candidates WHERE id=ANY($4::uuid[]))+
			(SELECT count(*) FROM activity_candidates WHERE id=ANY($5::uuid[]))`, r.communityIDs,
			r.placeIDs, r.activityIDs, r.placeCandidateIDs, r.activityCandidateIDs).Scan(&remaining); err != nil || remaining != 0 {
			t.Errorf("owned automatic-maintainer fixture residue: %d, %v", remaining, err)
		}
	})
	return r
}

func (r *automaticMaintainerFixture) sourceRow(t *testing.T, kind, id string) string {
	t.Helper()
	b := r.f.private.base
	query := map[string]string{
		"community": `SELECT to_jsonb(c)::text FROM communities c WHERE id=$1`,
		"place":     `SELECT to_jsonb(p)::text FROM places p WHERE id=$1`,
		"activity":  `SELECT to_jsonb(a)::text FROM activities a WHERE id=$1`,
	}[kind]
	if query == "" {
		t.Fatal("invalid owned fixture source kind")
	}
	var row string
	if err := b.pool.QueryRow(b.ctx, query, id).Scan(&row); err != nil {
		t.Fatal(err)
	}
	return row
}

func (r *automaticMaintainerFixture) assertGroup(t *testing.T, id, expected string) {
	t.Helper()
	b := r.f.private.base
	before := r.sourceRow(t, "community", id)
	results, err := b.store.Search(b.ctx, "aberdeen-gb", "", []string{b.person.ID})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, group := range results.Groups {
		if group.ID == id {
			found = true
			if group.Source.Maintainer != expected || !strings.Contains(group.Name, b.person.ID) {
				t.Fatal("Group source maintainer projection changed independent Community facts or disclosed an automatic private handle")
			}
		}
	}
	if !found || before != r.sourceRow(t, "community", id) {
		t.Fatal("Group source projection lost its eligible resource or rewrote a persisted source row")
	}
}

func TestAgentProfileVisibilityAutomaticMaintainerCommunitiesIntegration(t *testing.T) {
	r := newAutomaticMaintainerFixture(t)
	b := r.f.private.base
	for _, api := range []string{"social", "legacy"} {
		t.Run(api, func(t *testing.T) {
			var id string
			if api == "social" {
				record, err := b.store.CreateSocialCommunity(b.ctx, b.person.ID, community.SocialInput{
					CityID: "aberdeen-gb", Name: "independent-social-group-" + b.person.ID,
					Summary: "合成独立公开社区", Visibility: "public", JoinPolicy: "open",
				})
				if err != nil {
					t.Fatal(err)
				}
				id = record.ID
			} else {
				record, err := b.store.SubmitCommunity(b.ctx, b.person.ID, "aberdeen-gb", community.Input{
					Name: "independent-legacy-group-" + b.person.ID, Summary: "合成独立公开社区",
					SourceLabel: "Independent website", SourceURL: "https://example.invalid/legacy-group",
					RightsNote: "Synthetic only", ExpiresAt: time.Now().Add(24 * time.Hour),
				})
				if err != nil {
					t.Fatal(err)
				}
				id = record.ID
			}
			r.communityIDs = append(r.communityIDs, id)
			var raw string
			if err := b.pool.QueryRow(b.ctx, `SELECT maintainer_label FROM communities WHERE id=$1`, id).Scan(&raw); err != nil || raw != "社区维护者" {
				t.Fatal("new native Community source copied an account handle instead of a fixed role")
			}
			r.assertGroup(t, id, "社区维护者")
			// Simulate a historical auto-copy only in this exclusively owned
			// synthetic row. Read projection must not backfill the raw source.
			b.exec(`UPDATE communities SET maintainer_label=$2 WHERE id=$1`, id, r.ownerCanary)
			r.assertGroup(t, id, "社区维护者")
			if api == "social" {
				if _, err := b.store.JoinSocialCommunity(b.ctx, b.other.ID, id); err != nil {
					t.Fatal(err)
				}
				if err := b.store.TransferSocialOwner(b.ctx, b.person.ID, id, b.other.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := b.store.UpdateSocialCommunity(b.ctx, b.other.ID, id, community.SocialInput{
					CityID: "aberdeen-gb", Name: "independent-edited-group-" + b.person.ID, Summary: "显式公开的新社区说明",
					Visibility: "public", JoinPolicy: "open",
				}); err != nil {
					t.Fatal(err)
				}
				b.exec(`UPDATE communities SET source_label='Changed independent source',source_ref='https://example.invalid/new-community-source' WHERE id=$1`, id)
				r.assertGroup(t, id, "社区维护者")
			}
		})
	}
	for _, origin := range []string{"no_audit_similar_prefix", "wrong_purpose", "wrong_action", "null_actor"} {
		t.Run(origin, func(t *testing.T) {
			var id string
			label := "independent-manual-maintainer"
			if err := b.pool.QueryRow(b.ctx, `INSERT INTO communities(city_id,owner_account_id,name,summary,
				visibility,publication_status,owner_confirmed_at,source_label,source_ref,maintainer_label)
				VALUES('aberdeen-gb',$1::uuid,'independent-manual-group-'||$1::uuid::text,'Independent manual source','public',
				'published',now(),'Birdtie Community','birdtie:community:'||gen_random_uuid()::text,$2) RETURNING id`, b.person.ID, label).Scan(&id); err != nil {
				t.Fatal(err)
			}
			r.communityIDs = append(r.communityIDs, id)
			if origin != "no_audit_similar_prefix" {
				action, purpose := "publish", "owner_confirmed"
				var actor any = b.person.ID
				if origin == "wrong_purpose" {
					purpose = "unrelated-purpose"
				} else if origin == "wrong_action" {
					action = "unrelated-action"
				} else {
					actor = nil
				}
				b.exec(`INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)
					VALUES($1,$2,'community',$3,'allowed',$4)`, actor, action, id, purpose)
			}
			r.assertGroup(t, id, label)
		})
	}
}

func (r *automaticMaintainerFixture) assertPlace(t *testing.T, id, expected string) {
	t.Helper()
	b := r.f.private.base
	before := r.sourceRow(t, "place", id)
	place, err := b.store.GetPlace(b.ctx, id)
	if err != nil || place.Source.Maintainer != expected {
		t.Fatal("Place source projection disclosed an automatic private reviewer handle or hid independent metadata")
	}
	list, err := b.store.ListPlaces(b.ctx, "aberdeen-gb", b.person.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range list {
		if p.ID == id {
			found = p.Source.Maintainer == expected
		}
	}
	results, err := b.store.Search(b.ctx, "aberdeen-gb", "", []string{b.person.ID})
	if err != nil || !found {
		t.Fatal("Place list source projection failed")
	}
	found = false
	for _, p := range results.Places {
		if p.ID == id {
			found = p.Source.Maintainer == expected
		}
	}
	if !found || before != r.sourceRow(t, "place", id) {
		t.Fatal("Agent Place source projection lost the eligible place or rewrote its full source row")
	}
}

func TestAgentProfileVisibilityAutomaticMaintainerPlacesIntegration(t *testing.T) {
	r := newAutomaticMaintainerFixture(t)
	b := r.f.private.base
	var placeID string
	for _, decision := range []string{"publish", "link_existing"} {
		t.Run(decision, func(t *testing.T) {
			candidate, err := b.store.Submit(b.ctx, b.person.ID, "aberdeen-gb", cityseed.SubmitInput{
				Name: "independent-place-" + decision + "-" + b.person.ID, CategoryCode: "synthetic", LocationPrecision: "none",
				SourceLabel: "Independent website " + decision, SourceURL: "https://example.invalid/" + decision,
				RightsNote: "Synthetic only", ExpiresAt: time.Now().Add(48 * time.Hour),
			})
			if err != nil {
				t.Fatal(err)
			}
			r.placeCandidateIDs = append(r.placeCandidateIDs, candidate.ID)
			input := cityseed.ReviewInput{Decision: decision}
			if decision == "link_existing" {
				input.TargetPlaceID = placeID
				// Force this source update through the actual reviewer API.
				b.exec(`UPDATE places SET expires_at=now()+interval '1 day' WHERE id=$1`, placeID)
			}
			reviewed, err := b.store.Review(b.ctx, b.other.ID, candidate.ID, input)
			if err != nil || reviewed.ResolvedPlaceID == nil {
				t.Fatalf("actual Place reviewer workflow failed: %v", err)
			}
			if decision == "publish" {
				placeID = *reviewed.ResolvedPlaceID
				r.placeIDs = append(r.placeIDs, placeID)
			}
			var raw string
			if err := b.pool.QueryRow(b.ctx, `SELECT maintainer_label FROM places WHERE id=$1`, placeID).Scan(&raw); err != nil || raw != "城市维护者" {
				t.Fatal("new Place reviewer path copied a private account handle")
			}
			r.assertPlace(t, placeID, "城市维护者")
			b.exec(`UPDATE places SET maintainer_label=$2 WHERE id=$1`, placeID, r.reviewerCanary)
			r.assertPlace(t, placeID, "城市维护者")
			// Persisted source proof is independent of the current reviewer's
			// permission and candidate state. They must not revive a raw copy.
			b.exec(`UPDATE place_candidates SET reviewed_by=$2 WHERE id=$1`, candidate.ID, r.f.thirdID)
			b.exec(`UPDATE city_editor_memberships SET state='revoked' WHERE account_id=$1`, b.other.ID)
			r.assertPlace(t, placeID, "城市维护者")
			b.exec(`UPDATE city_editor_memberships SET state='active' WHERE account_id=$1`, b.other.ID)
		})
	}
	t.Run("unlinked_manual_maintainer_account", func(t *testing.T) {
		label := "independent-explicit-place-maintainer"
		var id string
		if err := b.pool.QueryRow(b.ctx, `INSERT INTO places(id,city_id,name,category_code,publication_status,
			source_label,source_ref,maintainer_label,maintainer_account_id)
			VALUES(gen_random_uuid(),'aberdeen-gb','independent-manual-place-'||$1::text,'synthetic','published',
			'Independent manual website','https://example.invalid/manual-place',$2,$3) RETURNING id`, b.person.ID, label, b.other.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		r.placeIDs = append(r.placeIDs, id)
		r.assertPlace(t, id, label)
	})
	t.Run("wrong_source_link_stays_independent", func(t *testing.T) {
		label := "independent-changed-place-source"
		b.exec(`UPDATE places SET source_ref='https://example.invalid/unlinked-source',maintainer_label=$2 WHERE id=$1`, placeID, label)
		r.assertPlace(t, placeID, label)
	})
}

func TestAgentProfileVisibilityAutomaticMaintainerHistoricalActivityReaderIntegration(t *testing.T) {
	r := newAutomaticMaintainerFixture(t)
	b := r.f.private.base
	organizationName := "independent-organization-host-" + b.orgID
	b.exec(`UPDATE organizations SET name=$2,visibility='public' WHERE id=$1`, b.orgID, organizationName)
	b.exec(`INSERT INTO organization_memberships(organization_id,user_account_id,role,status)
		VALUES($1,$2,'owner','active')`, b.orgID, b.person.ID)
	draft, err := b.store.CreateDraft(b.ctx, b.person.ID, b.orgID, activitypublish.Input{
		CityID: "aberdeen-gb", Title: "Independent historical source fixture-" + b.person.ID,
		StartsAt: time.Now().Add(time.Hour), EndsAt: time.Now().Add(2 * time.Hour), TimeZone: "Europe/London", Visibility: "public",
	})
	if err != nil {
		t.Fatal(err)
	}
	r.activityIDs = append(r.activityIDs, draft.ID)
	if _, err := b.store.PublishActivity(b.ctx, b.person.ID, b.orgID, draft.ID); err != nil {
		t.Fatal(err)
	}
	// ReviewActivity currently fails its organizer constraint (separate retained
	// AUDIT_KNOWN_FAILURE evidence). This is a synthetic historical-source reader
	// fixture on a valid organizer, not proof that that publication API works.
	candidate, err := b.store.SubmitActivity(b.ctx, b.person.ID, "aberdeen-gb", cityseed.ActivityInput{
		Title: "Independent candidate fixture", HostLabel: organizationName,
		StartsAt: time.Now().Add(time.Hour), EndsAt: time.Now().Add(2 * time.Hour), TimeZone: "Europe/London",
		SourceLabel: "Synthetic historical review origin", SourceURL: "https://example.invalid/historical-reviewed-event",
		RightsNote: "Synthetic historical reader only", ExpiresAt: time.Now().Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	r.activityCandidateIDs = append(r.activityCandidateIDs, candidate.ID)
	b.exec(`UPDATE activity_candidates SET status='published',reviewed_by=$2,reviewed_at=now(),resolved_activity_id=$3 WHERE id=$1`, candidate.ID, b.other.ID, draft.ID)
	b.exec(`INSERT INTO activity_sources(activity_id,candidate_id,source_label,source_url,rights_note,reviewer_account_id,verified_at,expires_at)
		VALUES($1,$2,$3,$4,'Synthetic historical reader only',$5,now(),now()+interval '1 day')`, draft.ID, candidate.ID, candidate.SourceLabel, candidate.SourceURL, b.other.ID)
	b.exec(`UPDATE activities SET source_label=$2,source_ref=$3,maintainer_label=$4 WHERE id=$1`, draft.ID, candidate.SourceLabel, candidate.SourceURL, r.reviewerCanary)
	assert := func(t *testing.T, expected string) {
		t.Helper()
		before := r.sourceRow(t, "activity", draft.ID)
		activity, err := b.store.GetActivity(b.ctx, draft.ID, "")
		if err != nil || activity.Source.Maintainer != expected || activity.HostLabel != organizationName || activity.Organizer.Name != organizationName {
			t.Fatal("historical review maintenance projection leaked a handle or changed the independent organizer/host")
		}
		results, err := b.store.Search(b.ctx, "aberdeen-gb", "", []string{b.person.ID})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range results.Activities {
			if item.ID == draft.ID {
				found = item.Source.Maintainer == expected && item.HostLabel == organizationName
			}
		}
		if !found || before != r.sourceRow(t, "activity", draft.ID) {
			t.Fatal("Agent Activity source projection lost its permitted resource or rewrote the source row")
		}
	}
	t.Run("precise_persisted_review_origin", func(t *testing.T) { assert(t, "城市维护者") })
	t.Run("current_reviewer_revocation_does_not_restore_raw_handle", func(t *testing.T) {
		b.exec(`UPDATE activity_candidates SET reviewed_by=$2 WHERE id=$1`, candidate.ID, r.f.thirdID)
		b.exec(`UPDATE city_editor_memberships SET state='revoked' WHERE account_id=$1`, b.other.ID)
		assert(t, "城市维护者")
	})
	t.Run("independent_unlinked_source_preserved", func(t *testing.T) {
		label := "independent-manual-activity-maintainer"
		b.exec(`UPDATE activities SET source_ref='https://example.invalid/unlinked-event',maintainer_label=$2 WHERE id=$1`, draft.ID, label)
		assert(t, label)
	})
}
