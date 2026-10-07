package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// The activity labels below are synthetic inputs in an explicitly disposable
// database. They are not real organizers, verified events or deployment proof.
type activityLabelFixture struct {
	f       *agentVisibilityFixture
	ids     []string
	placeID string
	name    string
	handle  string
}

func newActivityLabelFixture(t *testing.T) *activityLabelFixture {
	t.Helper()
	r := &activityLabelFixture{f: agentVisibilityTestFixture(t)}
	b := r.f.private.base
	r.name = "synthetic-derived-name-" + b.person.ID
	r.handle = "independent-label-handle-" + b.person.ID
	b.exec(`UPDATE user_profiles SET display_name=$2 WHERE account_id=$1`, b.person.ID, r.name)
	b.exec(`UPDATE accounts SET handle=$2 WHERE id=$1`, b.person.ID, "Birdtie 成员")
	if err := b.pool.QueryRow(b.ctx, `INSERT INTO places(id,city_id,name,category_code,
		publication_status,source_label,source_ref,maintainer_label)
		VALUES(gen_random_uuid(),'aberdeen-gb','合成独立活动地点','synthetic-label',
			'published','合成验证','disposable://activity-label','合成验证') RETURNING id`).Scan(&r.placeID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, item := range []struct {
			query string
			args  []any
		}{
			{`DELETE FROM admin_audit_events WHERE actor_account_id=ANY($1::uuid[])`, []any{b.accounts}},
			{`DELETE FROM activities WHERE id=ANY($1::uuid[])`, []any{r.ids}},
			{`DELETE FROM places WHERE id=$1`, []any{r.placeID}},
		} {
			if _, err := b.pool.Exec(ctx, item.query, item.args...); err != nil {
				t.Errorf("owned activity-label cleanup failed: %v", err)
			}
		}
		var remaining int
		if err := b.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM activities WHERE id=ANY($1::uuid[]))+
			(SELECT count(*) FROM places WHERE id=$2)`, r.ids, r.placeID).Scan(&remaining); err != nil || remaining != 0 {
			t.Errorf("owned activity-label fixture residue: %d, %v", remaining, err)
		}
	})
	return r
}

func (r *activityLabelFixture) create(t *testing.T, organizer activitypublish.Organizer) string {
	t.Helper()
	b := r.f.private.base
	draft, err := b.store.CreateSocialDraft(b.ctx, b.person.ID, activitypublish.Input{
		CityID: "aberdeen-gb", PlaceID: r.placeID, Title: "合成公开活动-" + b.person.ID,
		Summary: "明确公开的活动事实", StartsAt: time.Now().Add(time.Hour), EndsAt: time.Now().Add(2 * time.Hour),
		TimeZone: "Europe/London", CategoryCode: "synthetic-label", Visibility: "public", Organizer: organizer,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.ids = append(r.ids, draft.ID)
	if _, err := b.store.PublishSocialActivity(b.ctx, b.person.ID, draft.ID); err != nil {
		t.Fatal(err)
	}
	return draft.ID
}

func (r *activityLabelFixture) setAudience(t *testing.T, audience agentprofile.FieldVisibility) {
	t.Helper()
	rules := agentprofile.DefaultFieldRules()
	ids := []string{}
	if audience == agentprofile.VisibilityCommunity {
		ids = append(ids, r.f.communityID)
	}
	rules[agentprofile.FieldDisplayName] = agentprofile.FieldRule{Visibility: audience, CommunityIDs: ids}
	replaceVisibilityRules(t, r.f, rules)
}

func activityLabelByID(t *testing.T, activities []foundation.Activity, id string) foundation.Activity {
	t.Helper()
	for _, activity := range activities {
		if activity.ID == id {
			return activity
		}
	}
	t.Fatal("independent public activity disappeared from its eligible resource")
	return foundation.Activity{}
}

func (r *activityLabelFixture) assertPublicPaths(t *testing.T, id, viewer, expected string) {
	t.Helper()
	b := r.f.private.base
	assert := func(t *testing.T, activity foundation.Activity) {
		t.Helper()
		if activity.HostLabel != expected || activity.Organizer.Name != expected || activity.Source.Maintainer != expected {
			t.Fatal("activity host/organizer/maintainer did not project the current viewer's permitted label")
		}
		encoded, err := json.Marshal(activity)
		if err != nil || (expected != r.name && strings.Contains(string(encoded), r.name)) {
			t.Fatal("activity payload retained an automatically copied, currently forbidden name")
		}
	}
	t.Run("detail", func(t *testing.T) {
		activity, err := b.store.GetActivity(b.ctx, id, viewer)
		if err != nil {
			t.Fatal(err)
		}
		assert(t, activity)
	})
	for _, path := range []string{"city_list", "place_list", "discovery", "agent_activity_search", "agent_generic_search"} {
		t.Run(path, func(t *testing.T) {
			var items []foundation.Activity
			var err error
			switch path {
			case "city_list":
				items, err = b.store.ListActivities(b.ctx, "aberdeen-gb", viewer)
			case "place_list":
				items, err = b.store.ListPlaceActivities(b.ctx, r.placeID, viewer)
			case "discovery":
				items, err = b.store.FindActivities(b.ctx, "aberdeen-gb", viewer, foundation.ActivitySearchFilter{Category: "synthetic-label"})
			case "agent_activity_search":
				items, err = b.store.SearchActivities(b.ctx, "aberdeen-gb", viewer, "synthetic-label", "any", false, nil)
			case "agent_generic_search":
				result, searchErr := b.store.Search(b.ctx, "aberdeen-gb", viewer, []string{b.person.ID})
				items, err = result.Activities, searchErr
			}
			if err != nil {
				t.Fatal(err)
			}
			assert(t, activityLabelByID(t, items, id))
		})
	}
	t.Run("hidden_name_is_not_a_search_term", func(t *testing.T) {
		result, err := b.store.Search(b.ctx, "aberdeen-gb", viewer, []string{strings.ToLower(r.name)})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, activity := range result.Activities {
			found = found || activity.ID == id
		}
		if found != (expected == r.name) {
			t.Fatal("Agent search matched a forbidden copied name or missed a current visible name")
		}
	})
}

func TestAgentProfileVisibilityActivityDerivedLabelIntegration(t *testing.T) {
	r := newActivityLabelFixture(t)
	b := r.f.private.base
	id := r.create(t, activitypublish.Organizer{Type: "PERSON", ID: b.person.ID, Name: "ignored client name"})
	var persistedBefore string
	if err := b.pool.QueryRow(b.ctx, `SELECT to_jsonb(a)::text FROM activities a WHERE id=$1`, id).Scan(&persistedBefore); err != nil {
		t.Fatal(err)
	}
	requestID, tieID := r.f.acceptedTie(t)
	for _, audience := range []agentprofile.FieldVisibility{agentprofile.VisibilityPublic, agentprofile.VisibilityPrivate,
		agentprofile.VisibilityConnections, agentprofile.VisibilityCommunity, agentprofile.VisibilityAgentOnly} {
		t.Run(string(audience), func(t *testing.T) {
			r.setAudience(t, audience)
			anonymousLabel, peerLabel := "Birdtie 成员", "Birdtie 成员"
			if audience == agentprofile.VisibilityPublic {
				anonymousLabel = r.name
			}
			if audience == agentprofile.VisibilityPublic || audience == agentprofile.VisibilityConnections || audience == agentprofile.VisibilityCommunity {
				peerLabel = r.name
			}
			t.Run("anonymous", func(t *testing.T) { r.assertPublicPaths(t, id, "", anonymousLabel) })
			t.Run("peer", func(t *testing.T) { r.assertPublicPaths(t, id, b.other.ID, peerLabel) })
			t.Run("owner", func(t *testing.T) { r.assertPublicPaths(t, id, b.person.ID, r.name) })
			managed, err := b.store.ListSocialActivities(b.ctx, b.person.ID)
			if err != nil || len(managed) != 1 || managed[0].Organizer.Name != r.name {
				t.Fatal("owner managed projection used an anonymous/organization viewer or cached old label")
			}
		})
	}
	t.Run("connection_revoked_after_publication", func(t *testing.T) {
		r.setAudience(t, agentprofile.VisibilityConnections)
		b.exec(`UPDATE connection_requests SET state='withdrawn' WHERE id=$1`, requestID)
		defer b.exec(`UPDATE connection_requests SET state='accepted' WHERE id=$1`, requestID)
		r.assertPublicPaths(t, id, b.other.ID, "Birdtie 成员")
	})
	t.Run("tie_removed_after_publication", func(t *testing.T) {
		b.exec(`UPDATE person_ties SET status='removed' WHERE id=$1`, tieID)
		defer b.exec(`UPDATE person_ties SET status='active' WHERE id=$1`, tieID)
		r.assertPublicPaths(t, id, b.other.ID, "Birdtie 成员")
	})
	t.Run("community_viewer_left_after_publication", func(t *testing.T) {
		r.setAudience(t, agentprofile.VisibilityCommunity)
		b.exec(`UPDATE community_memberships SET status='left' WHERE community_id=$1 AND user_account_id=$2`, r.f.communityID, b.other.ID)
		defer b.exec(`UPDATE community_memberships SET status='active' WHERE community_id=$1 AND user_account_id=$2`, r.f.communityID, b.other.ID)
		r.assertPublicPaths(t, id, b.other.ID, "Birdtie 成员")
	})
	t.Run("community_owner_left_after_publication", func(t *testing.T) {
		b.exec(`UPDATE community_memberships SET status='left' WHERE community_id=$1 AND user_account_id=$2`, r.f.communityID, b.person.ID)
		defer b.exec(`UPDATE community_memberships SET status='active' WHERE community_id=$1 AND user_account_id=$2`, r.f.communityID, b.person.ID)
		r.assertPublicPaths(t, id, b.other.ID, "Birdtie 成员")
	})
	t.Run("block_preserves_activity_resource_acl", func(t *testing.T) {
		r.setAudience(t, agentprofile.VisibilityPublic)
		b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, b.person.ID, b.other.ID)
		defer b.exec(`DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, b.person.ID, b.other.ID)
		if _, err := b.store.GetActivity(b.ctx, id, b.other.ID); err == nil {
			t.Fatal("field projection enlarged the original blocked Activity ACL")
		}
		result, err := b.store.Search(b.ctx, "aberdeen-gb", b.other.ID, []string{b.person.ID})
		if err != nil || len(result.Activities) != 0 {
			t.Fatal("blocked activity leaked into Agent search")
		}
	})
	t.Run("suspended_personal_agent", func(t *testing.T) {
		b.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, b.personID)
		defer b.exec(`UPDATE agents SET status='active' WHERE id=$1`, b.personID)
		r.assertPublicPaths(t, id, "", "Birdtie 成员")
		r.assertPublicPaths(t, id, b.other.ID, "Birdtie 成员")
	})
	t.Run("live_name_edit_does_not_use_stored_copy", func(t *testing.T) {
		old := r.name
		r.name = "synthetic-edited-current-name-" + b.person.ID
		b.exec(`UPDATE user_profiles SET display_name=$2 WHERE account_id=$1`, b.person.ID, r.name)
		r.assertPublicPaths(t, id, "", r.name)
		managed, err := b.store.getSocialManagedActivity(b.ctx, b.person.ID, id)
		if err != nil || managed.Organizer.Name != r.name {
			t.Fatal("owner managed detail kept the old automatically derived label")
		}
		result, err := b.store.Search(b.ctx, "aberdeen-gb", "", []string{strings.ToLower(old)})
		if err != nil || len(result.Activities) != 0 {
			t.Fatal("Agent search retained the stale historical profile name")
		}
	})
	t.Run("deleted_personal_agent_does_not_restore_stored_name", func(t *testing.T) {
		// The metadata and overlay cascade away. Their absence must not restore
		// the old copied label through the default PUBLIC field policy.
		b.exec(`DELETE FROM agents WHERE id=$1`, b.personID)
		r.assertPublicPaths(t, id, "", "Birdtie 成员")
		r.assertPublicPaths(t, id, b.other.ID, "Birdtie 成员")
		managed, err := b.store.getSocialManagedActivity(b.ctx, b.person.ID, id)
		if err != nil || managed.Organizer.Name != "Birdtie 成员" {
			t.Fatal("deleted Agent revived a copied label in owner activity management")
		}
	})
	t.Run("no_independent_handle_uses_generic_label", func(t *testing.T) {
		b.exec(`UPDATE accounts SET handle=NULL WHERE id=$1`, b.person.ID)
		r.assertPublicPaths(t, id, "", "Birdtie 成员")
		r.assertPublicPaths(t, id, b.other.ID, "Birdtie 成员")
	})
	var persistedAfter string
	if err := b.pool.QueryRow(b.ctx, `SELECT to_jsonb(a)::text FROM activities a WHERE id=$1`, id).Scan(&persistedAfter); err != nil || persistedBefore != persistedAfter {
		t.Fatal("viewer projection rewrote persisted activity data instead of reading current field policy")
	}
}

func TestAgentProfileVisibilityActivityIndependentLabelsIntegration(t *testing.T) {
	r := newActivityLabelFixture(t)
	b := r.f.private.base
	r.setAudience(t, agentprofile.VisibilityPrivate)
	for _, item := range []struct {
		name, change string
		args         []any
	}{
		{"external_source", `UPDATE activities SET source_label='Independent authorized event source',source_ref='https://example.invalid/event' WHERE id=$1`, nil},
		{"unknown_legacy_source", `UPDATE activities SET source_label='Legacy independent source' WHERE id=$1`, nil},
		{"incomplete_server_ref", `UPDATE activities SET source_ref='birdtie:activity:not-a-uuid' WHERE id=$1`, nil},
		{"creator_not_person_organizer", `UPDATE activities SET created_by_account_id=$2 WHERE id=$1`, []any{b.other.ID}},
		{"host_not_person_organizer", `UPDATE activities SET host_account_id=$2 WHERE id=$1`, []any{b.other.ID}},
	} {
		t.Run(item.name, func(t *testing.T) {
			id := r.create(t, activitypublish.Organizer{Type: "PERSON", ID: b.person.ID})
			args := append([]any{id}, item.args...)
			b.exec(item.change, args...)
			label := "independent-event-label-" + id
			b.exec(`UPDATE activities SET host_label=$2,maintainer_label=$2 WHERE id=$1`, id, label)
			activity, err := b.store.GetActivity(b.ctx, id, "")
			if err != nil || activity.HostLabel != label || activity.Organizer.Name != label || activity.Source.Maintainer != label {
				t.Fatal("unknown/independently supplied activity label was mistaken for a native copied Profile field")
			}
			result, err := b.store.Search(b.ctx, "aberdeen-gb", "", []string{label})
			if err != nil || len(result.Activities) != 1 || result.Activities[0].ID != id {
				t.Fatal("independent event label lost its legitimate search behavior")
			}
		})
	}
	t.Run("organization_label", func(t *testing.T) {
		label := "independent-organization-label-" + b.orgID
		b.exec(`UPDATE organizations SET name=$2,visibility='public' WHERE id=$1`, b.orgID, label)
		b.exec(`INSERT INTO organization_memberships(organization_id,user_account_id,role,status) VALUES($1,$2,'owner','active')`, b.orgID, b.person.ID)
		id := r.create(t, activitypublish.Organizer{Type: "ORGANIZATION", ID: b.orgID})
		activity, err := b.store.GetActivity(b.ctx, id, "")
		if err != nil || activity.HostLabel != label || activity.Organizer.Name != label || activity.Source.Maintainer != label {
			t.Fatal("Person field policy hid independent Organization labels")
		}
		managed, err := b.store.ListManagedActivities(b.ctx, b.person.ID, b.orgID)
		if err != nil || len(managed) != 1 || managed[0].Organizer.Name != label {
			t.Fatal("Organization management label regressed")
		}
	})
	t.Run("community_label", func(t *testing.T) {
		label := "independent-community-label-" + r.f.communityID
		b.exec(`UPDATE communities SET name=$2 WHERE id=$1`, r.f.communityID, label)
		b.exec(`UPDATE community_memberships SET role='admin' WHERE community_id=$1 AND user_account_id=$2`, r.f.communityID, b.person.ID)
		id := r.create(t, activitypublish.Organizer{Type: "COMMUNITY", ID: r.f.communityID})
		activity, err := b.store.GetActivity(b.ctx, id, "")
		if err != nil || activity.HostLabel != label || activity.Organizer.Name != label || activity.Source.Maintainer != label {
			t.Fatal("Person field policy hid independent Community labels")
		}
	})
}

func TestAgentProfileVisibilityActivitySourceLabelACLIntegration(t *testing.T) {
	r := newActivityLabelFixture(t)
	b := r.f.private.base
	id := r.create(t, activitypublish.Organizer{Type: "PERSON", ID: b.person.ID})
	var persistedBefore string
	if err := b.pool.QueryRow(b.ctx, `SELECT to_jsonb(a)::text FROM activities a WHERE id=$1`, id).Scan(&persistedBefore); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var raw string
		if err := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object(
			'profile',(SELECT to_jsonb(p) FROM user_profiles p WHERE account_id=$1),
			'metadata',(SELECT to_jsonb(m) FROM agent_profiles m WHERE owner_id=$1),
			'policy',(SELECT to_jsonb(v) FROM agent_profile_field_visibility v WHERE owner_id=$1),
			'grants',(SELECT jsonb_agg(to_jsonb(g) ORDER BY g.id) FROM consent_grants g WHERE owner_account_id=$1)
		)::text`, b.person.ID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	assert := func(t *testing.T, viewer, label string) {
		t.Helper()
		before := snapshot()
		r.assertPublicPaths(t, id, viewer, label)
		if before != snapshot() {
			t.Fatal("activity label projection mutated ordinary source, metadata, policy or consent grants")
		}
	}
	setProfile := func(t *testing.T, visibility string) {
		t.Helper()
		if _, err := b.store.UpdateOwnProfile(b.ctx, b.person.ID, identity.ProfileInput{
			DisplayName: r.name, Bio: "独立合成普通资料", Visibility: visibility,
		}); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("public_source_default_public_field", func(t *testing.T) {
		assert(t, "", r.name)
		assert(t, b.other.ID, r.name)
	})
	t.Run("source_made_private_after_publication", func(t *testing.T) {
		setProfile(t, "private")
		// PUBLIC field alone does not republish its ordinary private source.
		assert(t, "", "Birdtie 成员")
		assert(t, b.other.ID, "Birdtie 成员")
		assert(t, b.person.ID, r.name)
		managed, err := b.store.getSocialManagedActivity(b.ctx, b.person.ID, id)
		if err != nil || managed.Organizer.Name != r.name {
			t.Fatal("owner management lost self access to a private ordinary source")
		}
	})
	grant, err := b.store.GrantProfileRead(b.ctx, b.person.ID, b.other.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("live_exact_profile_view_grant", func(t *testing.T) {
		ordinary, err := b.store.ReadProfile(b.ctx, b.other.ID, b.person.ID)
		if err != nil || ordinary.DisplayName != r.name {
			t.Fatal("fixture's real profile_view grant is not active")
		}
		assert(t, b.other.ID, r.name)
		assert(t, "", "Birdtie 成员")
		assert(t, r.f.thirdID, "Birdtie 成员")
	})
	for _, item := range []struct {
		name, change, restore string
	}{
		{"wrong_resource", `UPDATE consent_grants SET resource_id='unrelated-source' WHERE id=$1`, `UPDATE consent_grants SET resource_id=owner_account_id::text WHERE id=$1`},
		{"wrong_resource_type", `UPDATE consent_grants SET resource_type='memory' WHERE id=$1`, `UPDATE consent_grants SET resource_type='profile' WHERE id=$1`},
		{"wrong_purpose", `UPDATE consent_grants SET purpose='unrelated-purpose' WHERE id=$1`, `UPDATE consent_grants SET purpose='profile_view' WHERE id=$1`},
		{"missing_read_action", `UPDATE consent_grants SET actions=ARRAY['write']::text[] WHERE id=$1`, `UPDATE consent_grants SET actions=ARRAY['read']::text[] WHERE id=$1`},
		{"expired_profile_grant", `UPDATE consent_grants SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, `UPDATE consent_grants SET expires_at=clock_timestamp()+interval '1 hour' WHERE id=$1`},
	} {
		t.Run(item.name, func(t *testing.T) {
			b.exec(item.change, grant.ID)
			defer b.exec(item.restore, grant.ID)
			assert(t, b.other.ID, "Birdtie 成员")
		})
	}
	for _, audience := range []agentprofile.FieldVisibility{agentprofile.VisibilityPrivate, agentprofile.VisibilityAgentOnly} {
		t.Run("grant_cannot_bypass_"+string(audience), func(t *testing.T) {
			r.setAudience(t, audience)
			assert(t, b.other.ID, "Birdtie 成员")
			assert(t, b.person.ID, r.name)
		})
	}
	r.setAudience(t, agentprofile.VisibilityPublic)
	t.Run("real_grant_revoke_rejects_source_access", func(t *testing.T) {
		if err := b.store.RevokeProfileGrant(b.ctx, b.person.ID, grant.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := b.store.ReadProfile(b.ctx, b.other.ID, b.person.ID); err == nil {
			t.Fatal("fixture's real profile_view revocation did not remove ordinary source access")
		}
		assert(t, b.other.ID, "Birdtie 成员")
		assert(t, "", "Birdtie 成员")
		assert(t, b.person.ID, r.name)
	})
	t.Run("source_republished_uses_current_policy", func(t *testing.T) {
		setProfile(t, "public")
		assert(t, "", r.name)
		assert(t, b.other.ID, r.name)
	})
	var persistedAfter string
	if err := b.pool.QueryRow(b.ctx, `SELECT to_jsonb(a)::text FROM activities a WHERE id=$1`, id).Scan(&persistedAfter); err != nil || persistedBefore != persistedAfter {
		t.Fatal("source ACL changes rewrote the independent Activity row")
	}
}

func TestAgentProfileVisibilityActivityInitialPrivateLabelIntegration(t *testing.T) {
	r := newActivityLabelFixture(t)
	b := r.f.private.base
	if _, err := b.store.UpdateOwnProfile(b.ctx, b.person.ID, identity.ProfileInput{
		DisplayName: r.name, Bio: "合成私密普通资料", Visibility: "private",
	}); err != nil {
		t.Fatal(err)
	}
	// The field defaults to PUBLIC, but activity publication is not an extra
	// grant to copy a private ordinary Profile into its persisted public source.
	policy, err := b.store.ReadOwnAgentProfileVisibility(b.ctx, r.f.private.owner)
	if err != nil || policy.Configured || !agentprofile.FieldRulesDefault(policy.Rules) {
		t.Fatal("fixture did not retain its no-write default PUBLIC name field")
	}
	id := r.create(t, activitypublish.Organizer{Type: "PERSON", ID: b.person.ID})
	t.Run("raw_public_source_never_copies_private_name", func(t *testing.T) {
		var host, maintainer string
		if err := b.pool.QueryRow(b.ctx, `SELECT host_label,maintainer_label FROM activities WHERE id=$1`, id).Scan(&host, &maintainer); err != nil {
			t.Fatal(err)
		}
		if host != "Birdtie 成员" || maintainer != "Birdtie 成员" || strings.Contains(host+maintainer, r.name) {
			t.Fatal("initial Person activity auto-label copied a private ordinary Profile name into raw public source")
		}
	})
	t.Run("owner_dynamic_projection_remains_permitted", func(t *testing.T) {
		r.assertPublicPaths(t, id, b.person.ID, r.name)
		managed, err := b.store.getSocialManagedActivity(b.ctx, b.person.ID, id)
		if err != nil || managed.Organizer.Name != r.name {
			t.Fatal("owner's dynamic private-source name access was confused with a public persistent copy")
		}
	})
	t.Run("anonymous_and_peer_do_not_inherit_activity_authorization", func(t *testing.T) {
		r.assertPublicPaths(t, id, "", "Birdtie 成员")
		r.assertPublicPaths(t, id, b.other.ID, "Birdtie 成员")
	})
	current, err := b.store.ReadOwnAgentProfileVisibility(b.ctx, r.f.private.owner)
	if err != nil || current.Profile.ProfileVersion != policy.Profile.ProfileVersion || current.Configured {
		t.Fatal("activity projection/publishing mutated source Agent metadata or field policy")
	}
}

func TestAgentProfileVisibilityActivityNoHandleBypassLabelIntegration(t *testing.T) {
	r := newActivityLabelFixture(t)
	b := r.f.private.base
	// The private account handle can be the exact hidden Profile text. The
	// projection must not replace a rejected field with that other private source.
	r.handle = r.name
	b.exec(`UPDATE accounts SET handle=$2 WHERE id=$1`, b.person.ID, r.handle)
	id := r.create(t, activitypublish.Organizer{Type: "PERSON", ID: b.person.ID})
	for _, audience := range []agentprofile.FieldVisibility{agentprofile.VisibilityPrivate, agentprofile.VisibilityAgentOnly} {
		t.Run("same_handle_does_not_bypass_"+string(audience), func(t *testing.T) {
			r.setAudience(t, audience)
			r.assertPublicPaths(t, id, "", "Birdtie 成员")
			r.assertPublicPaths(t, id, b.other.ID, "Birdtie 成员")
			r.assertPublicPaths(t, id, b.person.ID, r.name)
		})
	}
	r.setAudience(t, agentprofile.VisibilityPublic)
	t.Run("same_handle_does_not_bypass_private_ordinary_source", func(t *testing.T) {
		if _, err := b.store.UpdateOwnProfile(b.ctx, b.person.ID, identity.ProfileInput{
			DisplayName: r.name, Bio: "合成私密普通资料", Visibility: "private",
		}); err != nil {
			t.Fatal(err)
		}
		r.assertPublicPaths(t, id, "", "Birdtie 成员")
		r.assertPublicPaths(t, id, b.other.ID, "Birdtie 成员")
		r.assertPublicPaths(t, id, b.person.ID, r.name)
	})
	assertRawGeneric := func(t *testing.T, id string) {
		t.Helper()
		var host, maintainer string
		if err := b.pool.QueryRow(b.ctx, `SELECT host_label,maintainer_label FROM activities WHERE id=$1`, id).Scan(&host, &maintainer); err != nil || host != "Birdtie 成员" || maintainer != "Birdtie 成员" {
			t.Fatal("initial denied auto-label copied a hidden same-name account handle")
		}
	}
	t.Run("initial_private_source_denies_same_handle_copy", func(t *testing.T) {
		privateID := r.create(t, activitypublish.Organizer{Type: "PERSON", ID: b.person.ID})
		assertRawGeneric(t, privateID)
		r.assertPublicPaths(t, privateID, "", "Birdtie 成员")
	})
	t.Run("initial_private_field_denies_same_handle_copy", func(t *testing.T) {
		if _, err := b.store.UpdateOwnProfile(b.ctx, b.person.ID, identity.ProfileInput{
			DisplayName: r.name, Bio: "合成公开普通资料", Visibility: "public",
		}); err != nil {
			t.Fatal(err)
		}
		r.setAudience(t, agentprofile.VisibilityPrivate)
		privateID := r.create(t, activitypublish.Organizer{Type: "PERSON", ID: b.person.ID})
		assertRawGeneric(t, privateID)
		r.assertPublicPaths(t, privateID, "", "Birdtie 成员")
	})
	t.Run("allowed_blank_name_preserves_existing_handle_fallback", func(t *testing.T) {
		r.setAudience(t, agentprofile.VisibilityPublic)
		// The ordinary legacy table permits a blank name. This positive control
		// is only for the already-authorized name source, not a new handle grant.
		b.exec(`UPDATE user_profiles SET display_name='' WHERE account_id=$1`, b.person.ID)
		r.assertPublicPaths(t, id, "", r.handle)
		r.assertPublicPaths(t, id, b.other.ID, r.handle)
		created := r.create(t, activitypublish.Organizer{Type: "PERSON", ID: b.person.ID})
		var host, maintainer string
		if err := b.pool.QueryRow(b.ctx, `SELECT host_label,maintainer_label FROM activities WHERE id=$1`, created).Scan(&host, &maintainer); err != nil || host != r.handle || maintainer != r.handle {
			t.Fatal("permitted initial name source lost its existing handle fallback")
		}
	})
}
