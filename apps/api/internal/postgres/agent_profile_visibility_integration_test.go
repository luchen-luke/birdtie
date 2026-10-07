package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type agentVisibilityFixture struct {
	private     *agentPrivateFixture
	communityID string
	thirdID     string
}

func agentVisibilityTestFixture(t *testing.T) *agentVisibilityFixture {
	t.Helper()
	f := &agentVisibilityFixture{private: agentPrivateTestFixture(t)}
	b := f.private.base
	var installed bool
	if err := b.pool.QueryRow(b.ctx, `SELECT to_regclass('public.agent_profile_field_visibility') IS NOT NULL
		AND to_regprocedure('birdtie_agent_profile_field_allowed(uuid,uuid,text)') IS NOT NULL`).Scan(&installed); err != nil || !installed {
		t.Fatal("field visibility integration requires migration 055")
	}
	// A third exclusively owned Person keeps the community owner active while
	// both target/viewer membership transitions can be tested independently.
	if err := b.pool.QueryRow(b.ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&f.thirdID); err != nil {
		t.Fatal(err)
	}
	b.accounts = append(b.accounts, f.thirdID)
	b.exec(`INSERT INTO agents(agent_type,principal_account_id) VALUES('personal',$1)`, f.thirdID)
	if err := b.pool.QueryRow(b.ctx, `INSERT INTO communities(owner_account_id,name,summary,
		visibility,publication_status,owner_confirmed_at,source_label,source_ref,maintainer_label)
		VALUES($1,'合成字段权限社区','仅本地一次性库','hidden','published',now(),
			'合成测试','disposable://field-visibility','合成验证') RETURNING id`, f.thirdID).Scan(&f.communityID); err != nil {
		t.Fatal(err)
	}
	b.exec(`INSERT INTO community_memberships(community_id,user_account_id,role,status)
		VALUES($1,$2,'member','active'),($1,$3,'member','active')`, f.communityID, b.person.ID, b.other.ID)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, item := range []struct {
			query string
			args  []any
		}{
			{`DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`, []any{b.accounts}},
			{`DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`, []any{b.accounts}},
			{`DELETE FROM communities WHERE id=$1`, []any{f.communityID}},
		} {
			if _, err := b.pool.Exec(ctx, item.query, item.args...); err != nil {
				t.Errorf("owned field-visibility fixture cleanup failed: %v", err)
			}
		}
	})
	return f
}

func replaceVisibilityRules(t *testing.T, f *agentVisibilityFixture, rules agentprofile.FieldRules) agentprofile.VisibilityRecord {
	t.Helper()
	b := f.private.base
	current, err := b.store.ReadOwnAgentProfileVisibility(b.ctx, f.private.owner)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := b.store.ReplaceOwnAgentProfileVisibility(b.ctx, f.private.owner,
		agentprofile.ReplaceVisibilityInput{ExpectedVersion: current.Profile.ProfileVersion, Rules: rules})
	if err != nil || saved.Profile.ProfileVersion != current.Profile.ProfileVersion+1 {
		t.Fatalf("explicit field rule replacement failed: %v", err)
	}
	return saved
}

func requireVisibilityError(t *testing.T, actual agentprofile.VisibilityRecord, err, expected error) {
	t.Helper()
	if !errors.Is(err, expected) || !reflect.DeepEqual(actual, agentprofile.VisibilityRecord{}) {
		t.Fatalf("field policy error did not reject without payload: %v; expected %v", err, expected)
	}
}

func requireProjectionError(t *testing.T, actual agentprofile.ProjectedRecord, err, expected error) {
	t.Helper()
	if !errors.Is(err, expected) || !reflect.DeepEqual(actual, agentprofile.ProjectedRecord{}) {
		t.Fatalf("field projection error did not reject without payload: %v; expected %v", err, expected)
	}
}

func requireProjectionKeys(t *testing.T, actual agentprofile.ProjectedRecord, expected ...agentprofile.FieldKey) {
	t.Helper()
	if agentprofile.ValidateProjectedRecord(actual) != nil || len(actual.Fields) != len(expected) {
		t.Fatalf("unexpected projected field count: actual=%d expected=%d", len(actual.Fields), len(expected))
	}
	for _, key := range expected {
		if _, ok := actual.Fields[key]; !ok {
			t.Fatalf("authorized projection omitted field %s", key)
		}
	}
	body, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	for _, prohibited := range []string{"rules", "communityIds", "profileVersion", "agentId", "ownerType", "schemaVersion", "configured"} {
		if strings.Contains(string(body), `"`+prohibited+`"`) {
			t.Fatalf("projection leaked internal field %s", prohibited)
		}
	}
}

func (f *agentVisibilityFixture) acceptedTie(t *testing.T) (requestID, tieID string) {
	t.Helper()
	b := f.private.base
	if err := b.pool.QueryRow(b.ctx, `INSERT INTO connection_requests(sender_account_id,recipient_account_id,
		note,scope,state,expires_at) VALUES($1,$2,'合成已接受好友申请','friend','accepted',now()+interval '1 day') RETURNING id`,
		b.person.ID, b.other.ID).Scan(&requestID); err != nil {
		t.Fatal(err)
	}
	if err := b.pool.QueryRow(b.ctx, `INSERT INTO person_ties(person_a_account_id,person_b_account_id,request_id)
		VALUES(LEAST($1::uuid,$2::uuid),GREATEST($1::uuid,$2::uuid),$3) RETURNING id`,
		b.person.ID, b.other.ID, requestID).Scan(&tieID); err != nil {
		t.Fatal(err)
	}
	return requestID, tieID
}

func TestAgentProfileVisibilityOwnerLifecycleIntegration(t *testing.T) {
	f := agentVisibilityTestFixture(t)
	b := f.private.base
	var publicBefore string
	if err := b.pool.QueryRow(b.ctx, `SELECT to_jsonb(p)::text FROM user_profiles p WHERE account_id=$1`, b.person.ID).Scan(&publicBefore); err != nil {
		t.Fatal(err)
	}
	initial, err := b.store.ReadOwnAgentProfileVisibility(b.ctx, f.private.owner)
	if err != nil || initial.Configured || !agentprofile.FieldRulesDefault(initial.Rules) || initial.Profile.ProfileVersion != 1 {
		t.Fatal("missing policy was not a no-write canonical default")
	}
	var count int
	if err = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_profile_field_visibility WHERE agent_id=$1`, b.personID).Scan(&count); err != nil || count != 0 {
		t.Fatal("owner field-policy GET created a missing row")
	}
	private := savePrivateCanaries(t, f.private)
	rules := agentprofile.DefaultFieldRules()
	rules[agentprofile.FieldDisplayName] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPrivate}
	rules[agentprofile.FieldAgentNotes] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPublic}
	saved := replaceVisibilityRules(t, f, rules)
	if !saved.Configured || saved.Profile.AgentID != b.personID || saved.Profile.OwnerID != b.person.ID || saved.Profile.ProfileVersion != private.Profile.ProfileVersion+1 {
		t.Fatal("policy save changed stable binding or did not share aggregate version")
	}
	var writtenPolicy, writtenPrivate int64
	if err = b.pool.QueryRow(b.ctx, `SELECT v.written_profile_version,p.written_profile_version
		FROM agent_profile_field_visibility v JOIN agent_private_profiles p USING(agent_id) WHERE v.agent_id=$1`, b.personID).Scan(&writtenPolicy, &writtenPrivate); err != nil || writtenPolicy != saved.Profile.ProfileVersion || writtenPrivate != private.Profile.ProfileVersion {
		t.Fatal("policy CAS rewrote private content revision or failed to persist its own revision")
	}
	connected, err := pgxpool.New(b.ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer connected.Close()
	reloaded, err := New(connected, false).ReadOwnAgentProfileVisibility(b.ctx, f.private.owner)
	if err != nil || !reflect.DeepEqual(reloaded.Rules, saved.Rules) || !sameAgentProfileMetadata(reloaded.Profile, saved.Profile) {
		t.Fatal("rules/version did not persist across a new database connection")
	}
	cleared := replaceVisibilityRules(t, f, agentprofile.DefaultFieldRules())
	if cleared.Configured || !agentprofile.FieldRulesDefault(cleared.Rules) || cleared.Profile.ProfileVersion != saved.Profile.ProfileVersion+1 {
		t.Fatal("explicit all-default policy clear did not advance version")
	}
	if err = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_profile_field_visibility WHERE agent_id=$1`, b.personID).Scan(&count); err != nil || count != 0 {
		t.Fatal("explicit all-default clear retained policy row")
	}
	retainedPrivate, err := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.private.owner)
	if err != nil || !retainedPrivate.Configured || !reflect.DeepEqual(retainedPrivate.Fields, private.Fields) || retainedPrivate.Profile.ProfileVersion != cleared.Profile.ProfileVersion {
		t.Fatal("policy edit/clear altered private field values or aggregate revision")
	}
	stale, err := b.store.ReplaceOwnAgentProfileVisibility(b.ctx, f.private.owner,
		agentprofile.ReplaceVisibilityInput{ExpectedVersion: saved.Profile.ProfileVersion, Rules: rules})
	requireVisibilityError(t, stale, err, agentprofile.ErrConflict)
	var publicAfter string
	if err = b.pool.QueryRow(b.ctx, `SELECT to_jsonb(p)::text FROM user_profiles p WHERE account_id=$1`, b.person.ID).Scan(&publicAfter); err != nil || publicBefore != publicAfter {
		t.Fatal("policy mutation changed ordinary Public/UserProfile values")
	}
	if err = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM consent_grants WHERE owner_account_id=$1`, b.person.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("policy mutation created consent/model/analysis grant")
	}
}

func TestAgentProfileVisibilityOwnerSessionBoundariesIntegration(t *testing.T) {
	f := agentVisibilityTestFixture(t)
	b := f.private.base
	owner := f.private.owner.WorkspacePrincipal
	for _, item := range []struct {
		name   string
		access agentprofile.PrivateAccess
	}{
		{"anonymous", agentprofile.PrivateAccess{WorkspacePrincipal: owner}},
		{"unknown_session", agentprofile.PrivateAccess{SessionDigest: [32]byte{42}, WorkspacePrincipal: owner}},
		{"peer_claiming_owner", agentprofile.PrivateAccess{SessionDigest: f.private.peer.SessionDigest, WorkspacePrincipal: owner}},
		{"owner_claiming_peer", agentprofile.PrivateAccess{SessionDigest: f.private.owner.SessionDigest, WorkspacePrincipal: f.private.peer.WorkspacePrincipal}},
		{"organization_session", agentprofile.PrivateAccess{SessionDigest: f.private.org.SessionDigest, WorkspacePrincipal: owner}},
		{"business_session", agentprofile.PrivateAccess{SessionDigest: f.private.biz.SessionDigest, WorkspacePrincipal: owner}},
		{"organization_workspace", agentprofile.PrivateAccess{SessionDigest: f.private.owner.SessionDigest, WorkspacePrincipal: f.private.org.WorkspacePrincipal}},
		{"community_workspace", agentprofile.PrivateAccess{SessionDigest: f.private.owner.SessionDigest, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Community, ID: owner.ID}}},
	} {
		t.Run(item.name, func(t *testing.T) {
			actual, err := b.store.ReadOwnAgentProfileVisibility(b.ctx, item.access)
			requireVisibilityError(t, actual, err, agentprofile.ErrForbidden)
			actual, err = b.store.ReplaceOwnAgentProfileVisibility(b.ctx, item.access,
				agentprofile.ReplaceVisibilityInput{ExpectedVersion: 1, Rules: agentprofile.DefaultFieldRules()})
			requireVisibilityError(t, actual, err, agentprofile.ErrForbidden)
		})
	}
	for _, item := range []struct {
		name, change, restore string
		id                    string
	}{
		{"revoked", `UPDATE sessions SET revoked_at=now() WHERE id=$1`, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.private.ownerSession},
		{"expired", `UPDATE sessions SET created_at=now()-interval '2 hours',idle_expires_at=now()-interval '1 hour' WHERE id=$1`, `UPDATE sessions SET idle_expires_at=now()+interval '1 hour' WHERE id=$1`, f.private.ownerSession},
		{"suspended_account", `UPDATE accounts SET status='suspended' WHERE id=$1`, `UPDATE accounts SET status='active' WHERE id=$1`, b.person.ID},
		{"suspended_agent", `UPDATE agents SET status='suspended' WHERE id=$1`, `UPDATE agents SET status='active' WHERE id=$1`, b.personID},
	} {
		t.Run(item.name, func(t *testing.T) {
			b.exec(item.change, item.id)
			defer b.exec(item.restore, item.id)
			actual, err := b.store.ReadOwnAgentProfileVisibility(b.ctx, f.private.owner)
			requireVisibilityError(t, actual, err, agentprofile.ErrForbidden)
			actual, err = b.store.ReplaceOwnAgentProfileVisibility(b.ctx, f.private.owner,
				agentprofile.ReplaceVisibilityInput{ExpectedVersion: 1, Rules: agentprofile.DefaultFieldRules()})
			requireVisibilityError(t, actual, err, agentprofile.ErrForbidden)
		})
	}
}

func TestAgentProfileVisibilityFiveAudiencesProjectionIntegration(t *testing.T) {
	f := agentVisibilityTestFixture(t)
	b := f.private.base
	savePrivateCanaries(t, f.private)
	anonymous, err := b.store.ReadAgentProfileFields(b.ctx, [32]byte{}, b.person.ID)
	if err != nil {
		t.Fatal(err)
	}
	requireProjectionKeys(t, anonymous, agentprofile.FieldDisplayName, agentprofile.FieldBio)
	rules := agentprofile.DefaultFieldRules()
	rules[agentprofile.FieldAvailability] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPublic}
	rules[agentprofile.FieldPersonalPreferences] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityConnections}
	rules[agentprofile.FieldSocialPreferences] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityCommunity, CommunityIDs: []string{f.communityID}}
	rules[agentprofile.FieldAgentNotes] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityAgentOnly}
	replaceVisibilityRules(t, f, rules)
	f.acceptedTie(t)
	for _, item := range []struct {
		name   string
		digest [32]byte
		keys   []agentprofile.FieldKey
	}{
		{"anonymous_public_only", [32]byte{}, []agentprofile.FieldKey{agentprofile.FieldDisplayName, agentprofile.FieldBio, agentprofile.FieldAvailability}},
		{"friend_and_member", f.private.peer.SessionDigest, []agentprofile.FieldKey{agentprofile.FieldDisplayName, agentprofile.FieldBio, agentprofile.FieldAvailability, agentprofile.FieldPersonalPreferences, agentprofile.FieldSocialPreferences}},
		{"organization_public_only", f.private.org.SessionDigest, []agentprofile.FieldKey{agentprofile.FieldDisplayName, agentprofile.FieldBio, agentprofile.FieldAvailability}},
		{"business_public_only", f.private.biz.SessionDigest, []agentprofile.FieldKey{agentprofile.FieldDisplayName, agentprofile.FieldBio, agentprofile.FieldAvailability}},
		{"owner_human_review_including_agent_only", f.private.owner.SessionDigest, agentprofile.ConfigurableFieldKeys()},
	} {
		t.Run(item.name, func(t *testing.T) {
			actual, err := b.store.ReadAgentProfileFields(b.ctx, item.digest, b.person.ID)
			if err != nil {
				t.Fatal(err)
			}
			requireProjectionKeys(t, actual, item.keys...)
		})
	}
	allPrivate := agentprofile.DefaultFieldRules()
	for key := range allPrivate {
		allPrivate[key] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPrivate}
	}
	replaceVisibilityRules(t, f, allPrivate)
	none, err := b.store.ReadAgentProfileFields(b.ctx, f.private.peer.SessionDigest, b.person.ID)
	requireProjectionError(t, none, err, agentprofile.ErrNotFound)
}

func TestAgentProfileVisibilitySourceGrantAndSessionRevocationIntegration(t *testing.T) {
	f := agentVisibilityTestFixture(t)
	b := f.private.base
	savePrivateCanaries(t, f.private)
	b.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, b.person.ID)
	for _, digest := range [][32]byte{{}, f.private.peer.SessionDigest} {
		actual, err := b.store.ReadAgentProfileFields(b.ctx, digest, b.person.ID)
		requireProjectionError(t, actual, err, agentprofile.ErrNotFound)
	}
	grant, err := b.store.GrantProfileRead(b.ctx, b.person.ID, b.other.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	granted, err := b.store.ReadAgentProfileFields(b.ctx, f.private.peer.SessionDigest, b.person.ID)
	if err != nil {
		t.Fatal(err)
	}
	requireProjectionKeys(t, granted, agentprofile.FieldDisplayName, agentprofile.FieldBio)
	// A PUBLIC field still cannot bypass the existing whole-profile source ACL.
	rules := agentprofile.DefaultFieldRules()
	rules[agentprofile.FieldAvailability] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPublic}
	replaceVisibilityRules(t, f, rules)
	anonymous, err := b.store.ReadAgentProfileFields(b.ctx, [32]byte{}, b.person.ID)
	requireProjectionError(t, anonymous, err, agentprofile.ErrNotFound)
	granted, err = b.store.ReadAgentProfileFields(b.ctx, f.private.peer.SessionDigest, b.person.ID)
	if err != nil {
		t.Fatal(err)
	}
	requireProjectionKeys(t, granted, agentprofile.FieldDisplayName, agentprofile.FieldBio, agentprofile.FieldAvailability)
	if err = b.store.RevokeProfileGrant(b.ctx, b.person.ID, grant.ID); err != nil {
		t.Fatal(err)
	}
	revoked, err := b.store.ReadAgentProfileFields(b.ctx, f.private.peer.SessionDigest, b.person.ID)
	requireProjectionError(t, revoked, err, agentprofile.ErrNotFound)
	b.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, b.person.ID)
	for _, item := range []struct {
		name, change, restore string
		args                  []any
	}{
		{"unknown_digest", "", "", nil},
		{"revoked_session", `UPDATE sessions SET revoked_at=now() WHERE token_sha256=$1`, `UPDATE sessions SET revoked_at=NULL WHERE token_sha256=$1`, []any{f.private.peer.SessionDigest[:]}},
		{"idle_expired", `UPDATE sessions SET created_at=now()-interval '2 hours',idle_expires_at=now()-interval '1 hour' WHERE token_sha256=$1`, `UPDATE sessions SET idle_expires_at=now()+interval '1 hour' WHERE token_sha256=$1`, []any{f.private.peer.SessionDigest[:]}},
		{"absolute_expired", `UPDATE sessions SET created_at=now()-interval '2 hours',expires_at=now()-interval '1 hour',idle_expires_at=now()-interval '1 hour' WHERE token_sha256=$1`, `UPDATE sessions SET expires_at=now()+interval '2 hours',idle_expires_at=now()+interval '1 hour' WHERE token_sha256=$1`, []any{f.private.peer.SessionDigest[:]}},
		{"viewer_suspended", `UPDATE accounts SET status='suspended' WHERE id=$1`, `UPDATE accounts SET status='active' WHERE id=$1`, []any{b.other.ID}},
		{"development_auth_disabled", `UPDATE sessions SET authentication_method='dev_phone' WHERE token_sha256=$1`, `UPDATE sessions SET authentication_method='test' WHERE token_sha256=$1`, []any{f.private.peer.SessionDigest[:]}},
	} {
		t.Run(item.name, func(t *testing.T) {
			digest := f.private.peer.SessionDigest
			if item.change == "" {
				digest = [32]byte{123}
			} else {
				b.exec(item.change, item.args...)
				defer b.exec(item.restore, item.args...)
			}
			actual, err := b.store.ReadAgentProfileFields(b.ctx, digest, b.person.ID)
			requireProjectionError(t, actual, err, agentprofile.ErrForbidden)
		})
	}
}

func TestAgentProfileVisibilityCurrentFriendProofAndBlockIntegration(t *testing.T) {
	f := agentVisibilityTestFixture(t)
	b := f.private.base
	savePrivateCanaries(t, f.private)
	rules := agentprofile.DefaultFieldRules()
	rules[agentprofile.FieldPersonalPreferences] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityConnections}
	replaceVisibilityRules(t, f, rules)
	assertWithoutFriendField := func(t *testing.T) {
		t.Helper()
		actual, err := b.store.ReadAgentProfileFields(b.ctx, f.private.peer.SessionDigest, b.person.ID)
		if err != nil {
			t.Fatal(err)
		}
		requireProjectionKeys(t, actual, agentprofile.FieldDisplayName, agentprofile.FieldBio)
	}
	assertWithoutFriendField(t)
	// A pending request and an accepted conversation are not durable friend proof.
	var requestID string
	if err := b.pool.QueryRow(b.ctx, `INSERT INTO connection_requests(sender_account_id,recipient_account_id,note,
		scope,state,expires_at) VALUES($1,$2,'合成待接受申请','friend','pending',now()+interval '1 day') RETURNING id`, b.person.ID, b.other.ID).Scan(&requestID); err != nil {
		t.Fatal(err)
	}
	t.Run("pending_friend_request", assertWithoutFriendField)
	b.exec(`UPDATE connection_requests SET state='accepted',scope='conversation',city_id='aberdeen-gb' WHERE id=$1`, requestID)
	t.Run("accepted_conversation_not_friend", assertWithoutFriendField)
	b.exec(`DELETE FROM connection_requests WHERE id=$1`, requestID)
	requestID, tieID := f.acceptedTie(t)
	active, err := b.store.ReadAgentProfileFields(b.ctx, f.private.peer.SessionDigest, b.person.ID)
	if err != nil {
		t.Fatal(err)
	}
	requireProjectionKeys(t, active, agentprofile.FieldDisplayName, agentprofile.FieldBio, agentprofile.FieldPersonalPreferences)
	t.Run("accepted_request_revoked", func(t *testing.T) {
		b.exec(`UPDATE connection_requests SET state='withdrawn' WHERE id=$1`, requestID)
		defer b.exec(`UPDATE connection_requests SET state='accepted' WHERE id=$1`, requestID)
		assertWithoutFriendField(t)
	})
	t.Run("request_pair_changed", func(t *testing.T) {
		b.exec(`UPDATE connection_requests SET recipient_account_id=$2 WHERE id=$1`, requestID, f.thirdID)
		defer b.exec(`UPDATE connection_requests SET recipient_account_id=$2 WHERE id=$1`, requestID, b.other.ID)
		assertWithoutFriendField(t)
	})
	t.Run("tie_removed", func(t *testing.T) {
		b.exec(`UPDATE person_ties SET status='removed' WHERE id=$1`, tieID)
		defer b.exec(`UPDATE person_ties SET status='active' WHERE id=$1`, tieID)
		assertWithoutFriendField(t)
	})
	for _, direction := range []string{"owner_blocks_viewer", "viewer_blocks_owner"} {
		t.Run(direction, func(t *testing.T) {
			actor, target := b.person.ID, b.other.ID
			if direction == "viewer_blocks_owner" {
				actor, target = target, actor
			}
			b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, actor, target)
			defer b.exec(`DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, actor, target)
			actual, err := b.store.ReadAgentProfileFields(b.ctx, f.private.peer.SessionDigest, b.person.ID)
			requireProjectionError(t, actual, err, agentprofile.ErrNotFound)
		})
	}
}

func TestAgentProfileVisibilityCurrentCommunityAndOwnerMembershipIntegration(t *testing.T) {
	f := agentVisibilityTestFixture(t)
	b := f.private.base
	savePrivateCanaries(t, f.private)
	rules := agentprofile.DefaultFieldRules()
	rules[agentprofile.FieldSocialPreferences] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityCommunity, CommunityIDs: []string{f.communityID}}
	replaceVisibilityRules(t, f, rules)
	active, err := b.store.ReadAgentProfileFields(b.ctx, f.private.peer.SessionDigest, b.person.ID)
	if err != nil {
		t.Fatal(err)
	}
	requireProjectionKeys(t, active, agentprofile.FieldDisplayName, agentprofile.FieldBio, agentprofile.FieldSocialPreferences)
	for _, item := range []struct {
		name, change, restore string
		args                  []any
	}{
		{"viewer_pending", `UPDATE community_memberships SET status='pending' WHERE community_id=$1 AND user_account_id=$2`, `UPDATE community_memberships SET status='active' WHERE community_id=$1 AND user_account_id=$2`, []any{f.communityID, b.other.ID}},
		{"viewer_invited", `UPDATE community_memberships SET status='invited' WHERE community_id=$1 AND user_account_id=$2`, `UPDATE community_memberships SET status='active' WHERE community_id=$1 AND user_account_id=$2`, []any{f.communityID, b.other.ID}},
		{"viewer_left", `UPDATE community_memberships SET status='left' WHERE community_id=$1 AND user_account_id=$2`, `UPDATE community_memberships SET status='active' WHERE community_id=$1 AND user_account_id=$2`, []any{f.communityID, b.other.ID}},
		{"owner_left", `UPDATE community_memberships SET status='left' WHERE community_id=$1 AND user_account_id=$2`, `UPDATE community_memberships SET status='active' WHERE community_id=$1 AND user_account_id=$2`, []any{f.communityID, b.person.ID}},
		{"archived_community", `UPDATE communities SET lifecycle_status='archived' WHERE id=$1`, `UPDATE communities SET lifecycle_status='active' WHERE id=$1`, []any{f.communityID}},
		{"unpublished_community", `UPDATE communities SET publication_status='hidden' WHERE id=$1`, `UPDATE communities SET publication_status='published' WHERE id=$1`, []any{f.communityID}},
		{"expired_community", `UPDATE communities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, `UPDATE communities SET expires_at=NULL WHERE id=$1`, []any{f.communityID}},
	} {
		t.Run(item.name, func(t *testing.T) {
			b.exec(item.change, item.args...)
			defer b.exec(item.restore, item.args...)
			actual, err := b.store.ReadAgentProfileFields(b.ctx, f.private.peer.SessionDigest, b.person.ID)
			if err != nil {
				t.Fatal(err)
			}
			requireProjectionKeys(t, actual, agentprofile.FieldDisplayName, agentprofile.FieldBio)
		})
	}
}

func TestAgentProfileVisibilityCommunityWriteEligibilityAndInvalidInputIntegration(t *testing.T) {
	f := agentVisibilityTestFixture(t)
	b := f.private.base
	rules := agentprofile.DefaultFieldRules()
	rules[agentprofile.FieldSocialPreferences] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityCommunity, CommunityIDs: []string{f.communityID}}
	for _, item := range []struct {
		name, change, restore string
		args                  []any
	}{
		{"owner_pending", `UPDATE community_memberships SET status='pending' WHERE community_id=$1 AND user_account_id=$2`, `UPDATE community_memberships SET status='active' WHERE community_id=$1 AND user_account_id=$2`, []any{f.communityID, b.person.ID}},
		{"owner_invited", `UPDATE community_memberships SET status='invited' WHERE community_id=$1 AND user_account_id=$2`, `UPDATE community_memberships SET status='active' WHERE community_id=$1 AND user_account_id=$2`, []any{f.communityID, b.person.ID}},
		{"owner_left", `UPDATE community_memberships SET status='left' WHERE community_id=$1 AND user_account_id=$2`, `UPDATE community_memberships SET status='active' WHERE community_id=$1 AND user_account_id=$2`, []any{f.communityID, b.person.ID}},
		{"archived", `UPDATE communities SET lifecycle_status='archived' WHERE id=$1`, `UPDATE communities SET lifecycle_status='active' WHERE id=$1`, []any{f.communityID}},
		{"unpublished", `UPDATE communities SET publication_status='hidden' WHERE id=$1`, `UPDATE communities SET publication_status='published' WHERE id=$1`, []any{f.communityID}},
		{"expired", `UPDATE communities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, `UPDATE communities SET expires_at=NULL WHERE id=$1`, []any{f.communityID}},
	} {
		t.Run(item.name, func(t *testing.T) {
			b.exec(item.change, item.args...)
			defer b.exec(item.restore, item.args...)
			actual, err := b.store.ReplaceOwnAgentProfileVisibility(b.ctx, f.private.owner,
				agentprofile.ReplaceVisibilityInput{ExpectedVersion: 1, Rules: rules})
			requireVisibilityError(t, actual, err, agentprofile.ErrForbidden)
		})
	}
	missingRules := agentprofile.DefaultFieldRules()
	delete(missingRules, agentprofile.FieldBio)
	unknownRules := agentprofile.DefaultFieldRules()
	unknownRules[agentprofile.FieldKey("inferenceConsent")] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPublic}
	for _, item := range []struct {
		name  string
		input agentprofile.ReplaceVisibilityInput
	}{
		{"missing_version", agentprofile.ReplaceVisibilityInput{Rules: agentprofile.DefaultFieldRules()}},
		{"missing_field", agentprofile.ReplaceVisibilityInput{ExpectedVersion: 1, Rules: missingRules}},
		{"unknown_field", agentprofile.ReplaceVisibilityInput{ExpectedVersion: 1, Rules: unknownRules}},
	} {
		t.Run(item.name, func(t *testing.T) {
			actual, err := b.store.ReplaceOwnAgentProfileVisibility(b.ctx, f.private.owner, item.input)
			requireVisibilityError(t, actual, err, agentprofile.ErrInvalid)
		})
	}
	current, err := b.store.ReadOwnAgentProfileVisibility(b.ctx, f.private.owner)
	if err != nil || current.Profile.ProfileVersion != 1 || current.Configured {
		t.Fatal("denied or malformed community settings changed policy/version")
	}
	// Viewer need not be a current member when the owner explicitly configures
	// a group; eligibility is required when that viewer reads later.
	b.exec(`UPDATE community_memberships SET status='pending' WHERE community_id=$1 AND user_account_id=$2`, f.communityID, b.other.ID)
	replaceVisibilityRules(t, f, rules)
}

func TestAgentProfileVisibilitySharedAggregateConcurrentCASIntegration(t *testing.T) {
	for _, secondOperation := range []string{"policy", "private"} {
		t.Run("policy_vs_"+secondOperation, func(t *testing.T) {
			f := agentVisibilityTestFixture(t)
			b := f.private.base
			start := make(chan struct{})
			results := make(chan error, 2)
			var attempts sync.WaitGroup
			for index := 0; index < 2; index++ {
				attempts.Add(1)
				go func(index int) {
					defer attempts.Done()
					<-start
					var err error
					if index == 1 && secondOperation == "private" {
						_, err = b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.private.owner,
							agentprofile.ReplacePrivateInput{ExpectedVersion: 1, Fields: privateProfileCanaries()})
					} else {
						rules := agentprofile.DefaultFieldRules()
						key := agentprofile.FieldDisplayName
						if index == 1 {
							key = agentprofile.FieldBio
						}
						rules[key] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPrivate}
						_, err = b.store.ReplaceOwnAgentProfileVisibility(b.ctx, f.private.owner,
							agentprofile.ReplaceVisibilityInput{ExpectedVersion: 1, Rules: rules})
					}
					results <- err
				}(index)
			}
			close(start)
			attempts.Wait()
			close(results)
			var successes, conflicts int
			for err := range results {
				if err == nil {
					successes++
				} else if errors.Is(err, agentprofile.ErrConflict) {
					conflicts++
				} else {
					t.Fatalf("concurrent aggregate CAS unexpected error: %v", err)
				}
			}
			if successes != 1 || conflicts != 1 {
				t.Fatalf("shared aggregate CAS success=%d conflict=%d", successes, conflicts)
			}
			current, err := b.store.ReadOwnAgentProfileVisibility(b.ctx, f.private.owner)
			if err != nil || current.Profile.ProfileVersion != 2 {
				t.Fatal("concurrent policy/content CAS did not produce one revision")
			}
		})
	}
}

func TestAgentProfileVisibilityWriteFailureAtomicRedactionIntegration(t *testing.T) {
	f := agentVisibilityTestFixture(t)
	b := f.private.base
	rules := agentprofile.DefaultFieldRules()
	rules[agentprofile.FieldDisplayName] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPrivate}
	before := replaceVisibilityRules(t, f, rules)
	suffix := strings.ReplaceAll(b.personID, "-", "")
	functionName, triggerName := "visibility_failure_"+suffix, "visibility_fail_"+suffix
	b.exec(`CREATE FUNCTION ` + functionName + `() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'owned synthetic policy failure' USING DETAIL=NEW.rules::text; END $$`)
	t.Cleanup(func() {
		if _, err := b.pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS `+triggerName+` ON agent_profile_field_visibility`); err != nil {
			t.Errorf("owned visibility fault trigger cleanup failed: %v", err)
		}
		if _, err := b.pool.Exec(context.Background(), `DROP FUNCTION `+functionName+`() `); err != nil {
			t.Errorf("owned visibility fault function cleanup failed: %v", err)
		}
	})
	b.exec(`CREATE TRIGGER ` + triggerName + ` BEFORE INSERT OR UPDATE ON agent_profile_field_visibility
		FOR EACH ROW WHEN (NEW.agent_id='` + b.personID + `'::uuid) EXECUTE FUNCTION ` + functionName + `()`)
	rules[agentprofile.FieldBio] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityAgentOnly}
	failed, err := b.store.ReplaceOwnAgentProfileVisibility(b.ctx, f.private.owner,
		agentprofile.ReplaceVisibilityInput{ExpectedVersion: before.Profile.ProfileVersion, Rules: rules})
	requireVisibilityError(t, failed, err, agentprofile.ErrUnavailable)
	if strings.Contains(err.Error(), "communityIds") || strings.Contains(err.Error(), "visibility") || strings.Contains(err.Error(), "DETAIL") {
		t.Fatal("policy database fault leaked underlying rule detail")
	}
	after, err := b.store.ReadOwnAgentProfileVisibility(b.ctx, f.private.owner)
	if err != nil || !reflect.DeepEqual(after.Rules, before.Rules) || !sameAgentProfileMetadata(after.Profile, before.Profile) {
		t.Fatal("failed policy write did not atomically roll back rules and metadata")
	}
}

func TestAgentProfileVisibilityEmptyAndRevokedSourcesIntegration(t *testing.T) {
	f := agentVisibilityTestFixture(t)
	b := f.private.base
	b.exec(`UPDATE user_profiles SET display_name='',bio='' WHERE account_id=$1`, b.person.ID)
	empty, err := b.store.ReadAgentProfileFields(b.ctx, [32]byte{}, b.person.ID)
	requireProjectionError(t, empty, err, agentprofile.ErrNotFound)
	b.exec(`UPDATE user_profiles SET display_name='合成公开名字',bio='合成公开介绍' WHERE account_id=$1`, b.person.ID)
	for _, item := range []struct {
		name, change, restore string
		id                    string
	}{
		{"owner_account_suspended", `UPDATE accounts SET status='suspended' WHERE id=$1`, `UPDATE accounts SET status='active' WHERE id=$1`, b.person.ID},
		{"personal_agent_suspended", `UPDATE agents SET status='suspended' WHERE id=$1`, `UPDATE agents SET status='active' WHERE id=$1`, b.personID},
	} {
		t.Run(item.name, func(t *testing.T) {
			b.exec(item.change, item.id)
			defer b.exec(item.restore, item.id)
			actual, err := b.store.ReadAgentProfileFields(b.ctx, [32]byte{}, b.person.ID)
			requireProjectionError(t, actual, err, agentprofile.ErrNotFound)
		})
	}
	for _, target := range []string{b.org.ID, b.business.ID, f.communityID} {
		actual, err := b.store.ReadAgentProfileFields(b.ctx, [32]byte{}, target)
		requireProjectionError(t, actual, err, agentprofile.ErrNotFound)
	}
}

func blockVisibilityMetadata(t *testing.T, f *agentVisibilityFixture) pgx.Tx {
	t.Helper()
	b := f.private.base
	blocker, err := b.pool.Begin(b.ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = blocker.Rollback(context.Background()) })
	if _, err = blocker.Exec(b.ctx, `SELECT agent_id FROM agent_profiles WHERE agent_id=$1 FOR UPDATE`, b.personID); err != nil {
		t.Fatal(err)
	}
	return blocker
}

func waitForVisibilityLock(t *testing.T, f *agentVisibilityFixture, blockerPID int) int {
	t.Helper()
	b := f.private.base
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var waitingPID int
		err := b.pool.QueryRow(b.ctx, `SELECT pid FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_pids(pid))
			ORDER BY pid LIMIT 1`, blockerPID).Scan(&waitingPID)
		if err == nil {
			return waitingPID
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("visibility operation did not reach its actual PostgreSQL lock")
	return 0
}

func TestAgentProfileVisibilityExpiryDuringMetadataWaitIntegration(t *testing.T) {
	for _, scenario := range []string{"owner_read_session", "owner_write_session", "viewer_session", "coarse_grant", "community_scope", "community_write"} {
		t.Run(scenario, func(t *testing.T) {
			f := agentVisibilityTestFixture(t)
			b := f.private.base
			savePrivateCanaries(t, f.private)
			rules := agentprofile.DefaultFieldRules()
			if strings.HasPrefix(scenario, "community_") {
				rules[agentprofile.FieldSocialPreferences] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityCommunity, CommunityIDs: []string{f.communityID}}
			}
			before := replaceVisibilityRules(t, f, rules)
			if scenario == "coarse_grant" {
				b.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, b.person.ID)
				if _, err := b.store.GrantProfileRead(b.ctx, b.person.ID, b.other.ID, time.Now().Add(time.Hour)); err != nil {
					t.Fatal(err)
				}
				b.exec(`UPDATE consent_grants SET expires_at=clock_timestamp()+interval '800 milliseconds'
					WHERE owner_account_id=$1 AND recipient_account_id=$2`, b.person.ID, b.other.ID)
			} else if strings.HasPrefix(scenario, "community_") {
				b.exec(`UPDATE communities SET expires_at=clock_timestamp()+interval '800 milliseconds' WHERE id=$1`, f.communityID)
			} else {
				digest := f.private.owner.SessionDigest
				if scenario == "viewer_session" {
					digest = f.private.peer.SessionDigest
				}
				b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '800 milliseconds' WHERE token_sha256=$1`, digest[:])
			}
			blocker := blockVisibilityMetadata(t, f)
			type outcome struct {
				policy     agentprofile.VisibilityRecord
				projection agentprofile.ProjectedRecord
				err        error
			}
			completed := make(chan outcome, 1)
			go func() {
				var result outcome
				switch scenario {
				case "owner_read_session":
					result.policy, result.err = b.store.ReadOwnAgentProfileVisibility(b.ctx, f.private.owner)
				case "owner_write_session", "community_write":
					result.policy, result.err = b.store.ReplaceOwnAgentProfileVisibility(b.ctx, f.private.owner,
						agentprofile.ReplaceVisibilityInput{ExpectedVersion: before.Profile.ProfileVersion, Rules: rules})
				default:
					result.projection, result.err = b.store.ReadAgentProfileFields(b.ctx, f.private.peer.SessionDigest, b.person.ID)
				}
				completed <- result
			}()
			waitForVisibilityLock(t, f, int(blocker.Conn().PgConn().PID()))
			time.Sleep(900 * time.Millisecond)
			if err := blocker.Commit(b.ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-completed:
				switch scenario {
				case "owner_read_session", "owner_write_session", "community_write":
					requireVisibilityError(t, result.policy, result.err, agentprofile.ErrForbidden)
				case "viewer_session":
					requireProjectionError(t, result.projection, result.err, agentprofile.ErrForbidden)
				case "coarse_grant":
					requireProjectionError(t, result.projection, result.err, agentprofile.ErrNotFound)
				case "community_scope":
					if result.err != nil {
						t.Fatal(result.err)
					}
					requireProjectionKeys(t, result.projection, agentprofile.FieldDisplayName, agentprofile.FieldBio)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("visibility operation did not finish after metadata release")
			}
			b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '1 hour' WHERE account_id=ANY($1::uuid[])`, b.accounts)
			after, err := b.store.ReadOwnAgentProfileVisibility(b.ctx, f.private.owner)
			if err != nil || !sameAgentProfileMetadata(before.Profile, after.Profile) || !reflect.DeepEqual(before.Rules, after.Rules) {
				t.Fatal("expiry while waiting committed an unauthorized field policy/revision")
			}
		})
	}
}

func TestAgentProfileVisibilityConcurrentRevocationAndBlockSerializationIntegration(t *testing.T) {
	for _, scenario := range []string{"grant_revoke", "tie_remove", "community_member_remove", "block_serialization"} {
		t.Run(scenario, func(t *testing.T) {
			f := agentVisibilityTestFixture(t)
			b := f.private.base
			savePrivateCanaries(t, f.private)
			rules := agentprofile.DefaultFieldRules()
			var grantID, tieID, memberID string
			switch scenario {
			case "grant_revoke":
				b.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, b.person.ID)
				grant, err := b.store.GrantProfileRead(b.ctx, b.person.ID, b.other.ID, time.Now().Add(time.Hour))
				if err != nil {
					t.Fatal(err)
				}
				grantID = grant.ID
			case "tie_remove":
				rules[agentprofile.FieldPersonalPreferences] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityConnections}
				_, tieID = f.acceptedTie(t)
			case "community_member_remove":
				rules[agentprofile.FieldSocialPreferences] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityCommunity, CommunityIDs: []string{f.communityID}}
				if err := b.pool.QueryRow(b.ctx, `SELECT id FROM community_memberships WHERE community_id=$1 AND user_account_id=$2`, f.communityID, b.other.ID).Scan(&memberID); err != nil {
					t.Fatal(err)
				}
			}
			replaceVisibilityRules(t, f, rules)
			blocker := blockVisibilityMetadata(t, f)
			type outcome struct {
				record agentprofile.ProjectedRecord
				err    error
			}
			completed := make(chan outcome, 1)
			go func() {
				actual, err := b.store.ReadAgentProfileFields(b.ctx, f.private.peer.SessionDigest, b.person.ID)
				completed <- outcome{actual, err}
			}()
			readerPID := waitForVisibilityLock(t, f, int(blocker.Conn().PgConn().PID()))
			var blockedEffect chan error
			switch scenario {
			case "grant_revoke":
				if err := b.store.RevokeProfileGrant(b.ctx, b.person.ID, grantID); err != nil {
					t.Fatal(err)
				}
			case "tie_remove":
				if err := b.store.RemoveTie(b.ctx, b.person.ID, tieID); err != nil {
					t.Fatal(err)
				}
			case "community_member_remove":
				if err := b.store.RemoveSocialMember(b.ctx, f.thirdID, f.communityID, memberID); err != nil {
					t.Fatal(err)
				}
			case "block_serialization":
				blockedEffect = make(chan error, 1)
				go func() { blockedEffect <- b.store.BlockAccount(b.ctx, b.person.ID, b.other.ID) }()
				waitForVisibilityLock(t, f, readerPID)
				select {
				case err := <-blockedEffect:
					t.Fatalf("Block crossed the projection's current account lock: %v", err)
				default:
				}
			}
			if err := blocker.Commit(b.ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-completed:
				if scenario == "grant_revoke" {
					requireProjectionError(t, result.record, result.err, agentprofile.ErrNotFound)
				} else {
					if result.err != nil {
						t.Fatal(result.err)
					}
					requireProjectionKeys(t, result.record, agentprofile.FieldDisplayName, agentprofile.FieldBio)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("revocation/projection locks did not resolve")
			}
			if blockedEffect != nil {
				select {
				case err := <-blockedEffect:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("serialized Block did not finish")
				}
				actual, err := b.store.ReadAgentProfileFields(b.ctx, f.private.peer.SessionDigest, b.person.ID)
				requireProjectionError(t, actual, err, agentprofile.ErrNotFound)
			}
		})
	}
}
