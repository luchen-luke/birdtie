package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func businessMemberProjectionNativeFixture(t *testing.T) *businessConsoleFixture {
	t.Helper()
	ownedMigrationDatabase(t)
	f := newBusinessConsoleFixture(t)
	// Registered after the original fixture: these exclusively owned sources
	// retire before its Business/Person account cleanup. No seed is modified.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, query := range []string{
			`DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`,
			`UPDATE agent_profiles SET profile_version=profile_version+1 WHERE owner_id=ANY($1::uuid[]) AND owner_type='PERSON'`,
			`DELETE FROM agent_profile_field_visibility WHERE owner_id=ANY($1::uuid[]) AND owner_type='PERSON'`,
			`DELETE FROM agent_profiles WHERE owner_id=ANY($1::uuid[]) AND owner_type='PERSON'`,
			`DELETE FROM agents WHERE principal_account_id=ANY($1::uuid[])`,
			`DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`,
		} {
			if _, err := f.pool.Exec(ctx, query, f.people); err != nil {
				t.Error("owned member projection source cleanup", err)
			}
		}
	})
	return f
}

func businessMemberProjectionNativeProfile(t *testing.T, f *businessConsoleFixture, index int, name string) {
	t.Helper()
	f.exec(t, `INSERT INTO user_profiles(account_id,display_name,bio,visibility)
	 VALUES($1,$2,'PRIVATE_MEMBER_BIO_NOT_A_ROSTER_LABEL','public')
	 ON CONFLICT(account_id) DO UPDATE SET display_name=EXCLUDED.display_name,bio=EXCLUDED.bio,visibility=EXCLUDED.visibility`, f.people[index], name)
}

func businessMemberProjectionNativeAgent(t *testing.T, f *businessConsoleFixture, index int) string {
	t.Helper()
	var id string
	if err := f.pool.QueryRow(f.ctx, `INSERT INTO agents(agent_type,principal_account_id)
	 VALUES('personal',$1) RETURNING id`, f.people[index]).Scan(&id); err != nil {
		t.Fatal("owned personal Agent source", err)
	}
	var version int64
	if err := f.pool.QueryRow(f.ctx, `SELECT profile_version FROM agent_profiles
	 WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON'`, id, f.people[index]).Scan(&version); err != nil || version != 1 {
		t.Fatal("actual AgentProfile bootstrap", err)
	}
	return id
}

func businessMemberProjectionNativeRule(t *testing.T, f *businessConsoleFixture, agentID string, audience agentprofile.FieldVisibility) {
	t.Helper()
	rules := agentprofile.DefaultFieldRules()
	rules[agentprofile.FieldDisplayName] = agentprofile.FieldRule{Visibility: audience, CommunityIDs: []string{}}
	raw, err := json.Marshal(rules)
	if err != nil || len(rules) != 11 {
		t.Fatal("canonical eleven-field policy", err)
	}
	// The original policy trigger requires an exact +1 authoritative revision.
	// Keep that contract rather than bypassing guards or reusing a generation.
	f.exec(t, `WITH revision AS (
	 UPDATE agent_profiles SET profile_version=profile_version+1
	 WHERE agent_id=$1 RETURNING agent_id,owner_id,profile_version)
	 INSERT INTO agent_profile_field_visibility(agent_id,owner_id,owner_type,rules,written_profile_version)
	 SELECT agent_id,owner_id,'PERSON',$2::jsonb,profile_version FROM revision
	 ON CONFLICT(agent_id) DO UPDATE SET rules=EXCLUDED.rules,written_profile_version=EXCLUDED.written_profile_version`, agentID, raw)
}

func businessMemberProjectionNativeSnapshot(t *testing.T, f *businessConsoleFixture, businessID string) string {
	t.Helper()
	var state string
	err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
	 'members',(SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY m.user_account_id),'[]'::jsonb) FROM business_memberships m WHERE m.business_id=$1),
	 'membershipControl',(SELECT to_jsonb(c) FROM business_console_membership_controls c WHERE c.business_id=$1),
	 'businessAudit',(SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY e.id),'[]'::jsonb) FROM business_console_audit_events e WHERE e.business_id=$1),
	 'personAudit',(SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY e.id),'[]'::jsonb) FROM audit_events e WHERE e.actor_account_id=ANY($2::uuid[])),
	 'profiles',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.account_id),'[]'::jsonb) FROM user_profiles p WHERE p.account_id=ANY($2::uuid[])),
	 'agentProfiles',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.agent_id),'[]'::jsonb) FROM agent_profiles p WHERE p.owner_id=ANY($2::uuid[])),
	 'fieldPolicies',(SELECT coalesce(jsonb_agg(to_jsonb(v) ORDER BY v.agent_id),'[]'::jsonb) FROM agent_profile_field_visibility v WHERE v.owner_id=ANY($2::uuid[])),
	 'memories',(SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY m.id),'[]'::jsonb) FROM agent_memories m WHERE m.owner_id=ANY($2::uuid[])))::text`, businessID, f.people).Scan(&state)
	if err != nil {
		t.Fatal("owned roster/source snapshot", err)
	}
	return state
}

func businessMemberProjectionNativeRead(t *testing.T, f *businessConsoleFixture, index int, businessID string, version int64, names map[string]string) businessconsole.Console {
	t.Helper()
	before := businessMemberProjectionNativeSnapshot(t, f, businessID)
	out, err := f.store.ReadBusinessConsole(f.ctx, f.who(index, businessID))
	if err != nil {
		t.Fatal("current authorized roster read", err)
	}
	if out.Business.ID != businessID || out.MembershipVersion != version || len(out.Members) != len(names) {
		t.Fatalf("original roster/version changed: version=%d members=%d", out.MembershipVersion, len(out.Members))
	}
	seen := map[string]bool{}
	for _, member := range out.Members {
		want, ok := names[member.PersonID]
		if !ok || seen[member.PersonID] || member.DisplayName != want || member.Role == "" || member.Status == "" {
			t.Fatal("label replaced, duplicated or widened the authoritative roster")
		}
		seen[member.PersonID] = true
	}
	raw, err := json.Marshal(out)
	if err != nil || strings.Contains(string(raw), "PRIVATE_MEMBER_BIO_NOT_A_ROSTER_LABEL") {
		t.Fatal("ordinary Profile content leaked through roster label", err)
	}
	if after := businessMemberProjectionNativeSnapshot(t, f, businessID); after != before {
		t.Fatal("roster projection wrote membership, audit, Profile, policy or Memory")
	}
	return out
}

func TestBusinessMemberProjectionNativeAuthorizedMetadataAndIsolation(t *testing.T) {
	f := businessMemberProjectionNativeFixture(t)
	id := f.newBusinessID(t)
	f.claim(t, id)
	f.grant(t, id)
	if _, err := f.store.ChangeBusinessMember(f.ctx, f.who(0, id), businessconsole.MemberInput{Action: "grant", Role: "admin", TargetPersonID: f.people[2]}); err != nil {
		t.Fatal("owner can manage before any Agent or label exists", err)
	}
	if _, err := f.store.ChangeBusinessMember(f.ctx, f.who(0, id), businessconsole.MemberInput{ExpectedVersion: 1, Action: "grant", Role: "member", TargetPersonID: f.people[3]}); err != nil {
		t.Fatal("unlabelled ordinary Person remains manageable", err)
	}
	names := map[string]string{f.people[0]: "", f.people[2]: "", f.people[3]: ""}
	owner := businessMemberProjectionNativeRead(t, f, 0, id, 2, names)
	admin := businessMemberProjectionNativeRead(t, f, 2, id, 2, names)
	if !owner.CanManage || !owner.CanManageMembers || !admin.CanManage || admin.CanManageMembers {
		t.Fatal("optional media changed owner/admin management permissions")
	}
	var agentCount int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agents WHERE principal_account_id=ANY($1::uuid[])`, f.people).Scan(&agentCount); err != nil || agentCount != 0 {
		t.Fatal("ordinary management required or created an Agent", err)
	}
	for index, name := range []string{"  合成所有者  ", "OTHER_BUSINESS_REVIEWER_NAME", "合成管理员", "合成名单成员"} {
		businessMemberProjectionNativeProfile(t, f, index, name)
		businessMemberProjectionNativeAgent(t, f, index)
	}
	names[f.people[0]], names[f.people[2]], names[f.people[3]] = "合成所有者", "合成管理员", "合成名单成员"
	owner = businessMemberProjectionNativeRead(t, f, 0, id, 2, names)
	admin = businessMemberProjectionNativeRead(t, f, 2, id, 2, names)
	if owner.Business.Role != "owner" || admin.Business.Role != "admin" {
		t.Fatal("label altered membership identity or role")
	}
	reviewer := businessMemberProjectionNativeRead(t, f, 1, id, 0, map[string]string{})
	if !reviewer.CanReview || reviewer.CanManage || reviewer.CanManageMembers {
		t.Fatal("review permission became roster management")
	}
	if _, err := f.store.ReadBusinessConsole(f.ctx, f.who(3, id)); !errors.Is(err, businessconsole.ErrForbidden) {
		t.Fatal("named member inherited owner/admin roster read", err)
	}
	if _, err := f.store.ChangeBusinessMember(f.ctx, f.who(2, id), businessconsole.MemberInput{ExpectedVersion: 2, Action: "grant", Role: "admin", TargetPersonID: f.people[3]}); !errors.Is(err, businessconsole.ErrForbidden) {
		t.Fatal("named administrator inherited owner-only mutation", err)
	}

	// The target blocks only the actual admin reader. The same authorized
	// roster remains readable by its unblocked owner; no Business viewer is used.
	f.exec(t, `INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.people[3], f.people[2])
	adminNames := map[string]string{f.people[0]: "合成所有者", f.people[2]: "合成管理员", f.people[3]: ""}
	businessMemberProjectionNativeRead(t, f, 2, id, 2, adminNames)
	businessMemberProjectionNativeRead(t, f, 0, id, 2, names)
	f.exec(t, `DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, f.people[3], f.people[2])

	second := f.newBusinessID(t)
	f.claim(t, second)
	if _, err := f.store.ChangeBusinessMember(f.ctx, f.who(0, second), businessconsole.MemberInput{Action: "grant", Role: "member", TargetPersonID: f.people[3]}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ChangeBusinessMember(f.ctx, f.who(0, second), businessconsole.MemberInput{ExpectedVersion: 1, Action: "transfer_owner", Role: "owner", TargetPersonID: f.people[3]}); err != nil {
		t.Fatal(err)
	}
	businessMemberProjectionNativeRead(t, f, 3, second, 2, map[string]string{f.people[0]: "合成所有者", f.people[3]: "合成名单成员"})
	for _, index := range []int{1, 2} {
		if _, err := f.store.ReadBusinessConsole(f.ctx, f.who(index, second)); !errors.Is(err, businessconsole.ErrForbidden) {
			t.Fatal("another Business role widened roster access", err)
		}
	}
	listed, err := f.store.ListBusinessConsoles(f.ctx, f.access[2])
	if err != nil || len(listed) != 1 || listed[0].ID != id {
		t.Fatal("member labels exposed a cross-Business directory", err)
	}
	businessMemberProjectionNativeRead(t, f, 0, id, 2, names)
}

func TestBusinessMemberProjectionNativeSourceAndSessionRetirement(t *testing.T) {
	f := businessMemberProjectionNativeFixture(t)
	id := f.newBusinessID(t)
	f.claim(t, id)
	if _, err := f.store.ChangeBusinessMember(f.ctx, f.who(0, id), businessconsole.MemberInput{Action: "grant", Role: "admin", TargetPersonID: f.people[2]}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ChangeBusinessMember(f.ctx, f.who(0, id), businessconsole.MemberInput{ExpectedVersion: 1, Action: "grant", Role: "member", TargetPersonID: f.people[3]}); err != nil {
		t.Fatal(err)
	}
	businessMemberProjectionNativeProfile(t, f, 0, "合成管理人")
	businessMemberProjectionNativeAgent(t, f, 0)
	businessMemberProjectionNativeProfile(t, f, 2, "当前公开名")
	agentID := businessMemberProjectionNativeAgent(t, f, 2)
	// The third Person deliberately has no UserProfile and no Agent. Its
	// original membership survives throughout every optional-source transition.
	names := map[string]string{f.people[0]: "合成管理人", f.people[2]: "当前公开名", f.people[3]: ""}
	read := func(want string) {
		t.Helper()
		names[f.people[2]] = want
		out := businessMemberProjectionNativeRead(t, f, 0, id, 2, names)
		if !out.CanManageMembers {
			t.Fatal("missing or retired label removed owner management")
		}
		for _, member := range out.Members {
			if member.PersonID == f.people[2] && (member.Role != "admin" || member.Status != "active") {
				t.Fatal("source transition changed authoritative membership")
			}
		}
	}
	read("当前公开名")
	unchangedMembership := func() string {
		t.Helper()
		var state string
		if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_array(
		 (SELECT to_jsonb(c) FROM business_console_membership_controls c WHERE c.business_id=$1),
		 (SELECT jsonb_agg(to_jsonb(m) ORDER BY m.user_account_id) FROM business_memberships m WHERE m.business_id=$1),
		 (SELECT jsonb_agg(to_jsonb(e) ORDER BY e.id) FROM business_console_audit_events e WHERE e.business_id=$1))::text`, id).Scan(&state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	originalMembership := unchangedMembership()
	f.exec(t, `UPDATE user_profiles SET display_name='  更新后的公开名  ' WHERE account_id=$1`, f.people[2])
	read("更新后的公开名")
	for _, invalid := range []string{"A", "姓名\n控制字符", strings.Repeat("界", 27)} {
		f.exec(t, `UPDATE user_profiles SET display_name=$2 WHERE account_id=$1`, f.people[2], invalid)
		read("")
	}
	f.exec(t, `UPDATE user_profiles SET display_name='恢复公开名' WHERE account_id=$1`, f.people[2])
	read("恢复公开名")
	businessMemberProjectionNativeRule(t, f, agentID, agentprofile.VisibilityPrivate)
	read("")
	businessMemberProjectionNativeRule(t, f, agentID, agentprofile.VisibilityPublic)
	read("恢复公开名")
	f.exec(t, `UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, f.people[2])
	read("")
	f.exec(t, `UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, f.people[2])
	read("恢复公开名")
	f.exec(t, `INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.people[0], f.people[2])
	read("")
	f.exec(t, `DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, f.people[0], f.people[2])
	read("恢复公开名")
	f.exec(t, `DELETE FROM user_profiles WHERE account_id=$1`, f.people[2])
	read("")
	businessMemberProjectionNativeProfile(t, f, 2, "新当前名")
	read("新当前名")
	f.exec(t, `UPDATE agents SET status='retired' WHERE id=$1`, agentID)
	read("")
	f.exec(t, `UPDATE agents SET status='active' WHERE id=$1`, agentID)
	read("新当前名")
	f.exec(t, `UPDATE accounts SET status='suspended' WHERE id=$1`, f.people[2])
	read("")
	if state := unchangedMembership(); state != originalMembership {
		t.Fatal("display source edits or retirement changed membership version or audit")
	}
	if _, err := f.store.ReadBusinessConsole(f.ctx, f.who(2, id)); !errors.Is(err, businessconsole.ErrForbidden) {
		t.Fatal("suspended administrator retained console access", err)
	}
	removed, err := f.store.ChangeBusinessMember(f.ctx, f.who(0, id), businessconsole.MemberInput{ExpectedVersion: 2, Action: "remove", TargetPersonID: f.people[2]})
	if err != nil || removed.MembershipVersion != 3 || !removed.CanManageMembers {
		t.Fatal("unlabelled suspended Person cannot be revoked by owner", err)
	}
	businessMemberProjectionNativeRead(t, f, 0, id, 3, names)
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, f.access[0].SessionDigest[:])
	if _, err = f.store.ReadBusinessConsole(f.ctx, f.who(0, id)); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("revoked Session returned member projection", err)
	}
	if _, err = f.store.ListBusinessConsoles(f.ctx, f.access[0]); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("revoked Session listed projected Business identities", err)
	}
}
