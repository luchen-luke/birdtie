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
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

type agentPrivateFixture struct {
	base                  *agentProfileFixture
	owner, peer, org, biz agentprofile.PrivateAccess
	ownerSession          string
}

func agentPrivateTestFixture(t *testing.T) *agentPrivateFixture {
	t.Helper()
	b := agentProfileTestFixture(t)
	var installed bool
	if err := b.pool.QueryRow(b.ctx, `SELECT to_regclass('public.agent_private_profiles') IS NOT NULL`).Scan(&installed); err != nil || !installed {
		t.Fatal("private Agent Profile integration requires migration 054")
	}
	f := &agentPrivateFixture{base: b}
	// This cleanup runs before the inherited foundation cleanup. Sessions,
	// grants and blocks belong only to this fixture's random accounts. Private
	// contents are then removed by the metadata FK cascade, not an unsafe delete.
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, statement := range []string{
			`DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`,
			`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
		} {
			if _, err := b.pool.Exec(cleanupCtx, statement, b.accounts); err != nil {
				t.Errorf("owned private-profile cleanup failed: %v", err)
			}
		}
	})
	for _, item := range []struct {
		principal actorref.PrincipalRef
		access    *agentprofile.PrivateAccess
	}{
		{b.person, &f.owner}, {b.other, &f.peer}, {b.org, &f.org}, {b.business, &f.biz},
	} {
		var sessionID string
		_, digest, err := identity.NewToken()
		if err != nil {
			t.Fatal("cannot create a disposable session digest")
		}
		if err = b.pool.QueryRow(b.ctx, `INSERT INTO sessions
			(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)
			VALUES($1,$2,'test',now()+interval '2 hours',now()+interval '1 hour') RETURNING id`,
			item.principal.ID, digest[:]).Scan(&sessionID); err != nil {
			t.Fatal("cannot insert a disposable private-profile session")
		}
		*item.access = agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: item.principal}
		if item.principal.Equal(b.person) {
			f.ownerSession = sessionID
		}
	}
	b.exec(`INSERT INTO user_profiles(account_id,display_name,bio,visibility)
		VALUES($1,'合成公开资料','旧公开字段保持原值','public')`, b.person.ID)
	b.exec(`INSERT INTO user_profiles(account_id,display_name,bio,visibility)
		VALUES($1,'合成其他用户','其他用户公开字段','public')`, b.other.ID)
	return f
}

func privateProfileCanaries() agentprofile.PrivateFields {
	return agentprofile.PrivateFields{
		PersonalPreferences:    []string{"合成私密个人偏好-AGE002"},
		SocialPreferences:      []string{"合成私密社交偏好-AGE002"},
		Availability:           "合成私密时间声明-AGE002",
		PreferredActivityTypes: []string{"合成私密活动偏好-AGE002"},
		TravelPreferences:      []string{"合成私密出行偏好-AGE002"},
		InteractionPreferences: []string{"合成私密交互偏好-AGE002"},
		PrivateCityHistory:     "合成私密历史城市声明-AGE002",
		LanguagePreferences:    []string{"合成私密语言偏好-AGE002"},
		AgentNotes:             "合成私密备注-AGE002",
	}
}

func savePrivateCanaries(t *testing.T, f *agentPrivateFixture) agentprofile.PrivateRecord {
	t.Helper()
	current, err := f.base.store.ReadOwnAgentPrivateProfile(f.base.ctx, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := f.base.store.ReplaceOwnAgentPrivateProfile(f.base.ctx, f.owner,
		agentprofile.ReplacePrivateInput{ExpectedVersion: current.Profile.ProfileVersion, Fields: privateProfileCanaries()})
	if err != nil || !saved.Configured || saved.Profile.ProfileVersion != current.Profile.ProfileVersion+1 {
		t.Fatalf("explicit owner private replacement failed: %v", err)
	}
	return saved
}

func requirePrivateProfileError(t *testing.T, record agentprofile.PrivateRecord, err, expected error) {
	t.Helper()
	if !errors.Is(err, expected) || !reflect.DeepEqual(record, agentprofile.PrivateRecord{}) {
		t.Fatalf("private operation did not reject without a payload: error=%v expected=%v", err, expected)
	}
}

func assertNoPrivateCanaries(t *testing.T, value any) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "合成私密") || strings.Contains(string(body), "agentNotes") || strings.Contains(string(body), "privateCityHistory") {
		t.Fatal("an existing ordinary/public projection leaked Private Agent Profile contents")
	}
}

func TestAgentPrivateProfileOwnerLifecycleIntegration(t *testing.T) {
	f := agentPrivateTestFixture(t)
	b := f.base
	publicBefore, err := b.store.ReadProfile(b.ctx, b.other.ID, b.person.ID)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := b.store.GrantProfileRead(b.ctx, b.person.ID, b.other.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	initial, err := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.owner)
	if err != nil || initial.Configured || !agentprofile.PrivateFieldsEmpty(initial.Fields) || initial.Profile.ProfileVersion != 1 {
		t.Fatal("missing private row did not remain unconfigured/empty at the current base revision")
	}
	var count int
	if err = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_private_profiles WHERE agent_id=$1`, b.personID).Scan(&count); err != nil || count != 0 {
		t.Fatal("private GET wrote a missing row")
	}
	saved := savePrivateCanaries(t, f)
	if saved.Profile.AgentID != b.personID || saved.Profile.OwnerID != b.person.ID || agentprofile.ValidatePrivateRecord(saved) != nil {
		t.Fatal("private save changed stable Agent/owner/schema metadata")
	}
	var written int64
	if err = b.pool.QueryRow(b.ctx, `SELECT written_profile_version FROM agent_private_profiles WHERE agent_id=$1`, b.personID).Scan(&written); err != nil || written != saved.Profile.ProfileVersion {
		t.Fatal("private contents and the current metadata revision were not committed together")
	}
	connected, err := pgxpool.New(b.ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer connected.Close()
	reloaded, err := New(connected, false).ReadOwnAgentPrivateProfile(b.ctx, f.owner)
	if err != nil || !reflect.DeepEqual(reloaded.Fields, saved.Fields) || !sameAgentProfileMetadata(reloaded.Profile, saved.Profile) {
		t.Fatal("private fields/revision did not persist across a new database connection")
	}
	cleared, err := b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.owner,
		agentprofile.ReplacePrivateInput{ExpectedVersion: saved.Profile.ProfileVersion, Fields: agentprofile.PrivateFields{}})
	if err != nil || cleared.Configured || !agentprofile.PrivateFieldsEmpty(cleared.Fields) || cleared.Profile.ProfileVersion != saved.Profile.ProfileVersion+1 {
		t.Fatal("explicit clear did not atomically remove contents and advance the version")
	}
	if err = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_private_profiles WHERE agent_id=$1`, b.personID).Scan(&count); err != nil || count != 0 {
		t.Fatal("explicit clear retained a private row")
	}
	current, err := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.owner)
	if err != nil || current.Configured || !sameAgentProfileMetadata(current.Profile, cleared.Profile) {
		t.Fatal("clear was not retained on the next owner read")
	}
	stale, err := b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.owner,
		agentprofile.ReplacePrivateInput{ExpectedVersion: saved.Profile.ProfileVersion, Fields: privateProfileCanaries()})
	requirePrivateProfileError(t, stale, err, agentprofile.ErrConflict)
	publicAfter, err := b.store.ReadProfile(b.ctx, b.other.ID, b.person.ID)
	if err != nil || publicAfter != publicBefore {
		t.Fatal("private save/clear changed the existing UserProfile/Public Profile")
	}
	var grantRevision int64
	var grantActive bool
	if err = b.pool.QueryRow(b.ctx, `SELECT revision,revoked_at IS NULL FROM consent_grants WHERE id=$1`, grant.ID).Scan(&grantRevision, &grantActive); err != nil || grantRevision != grant.Revision || !grantActive {
		t.Fatal("private save/clear changed a public-profile grant")
	}
	if err = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM consent_grants WHERE owner_account_id=$1 AND resource_type<>'profile'`, b.person.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("private edit granted Memory/analysis/model permissions")
	}
}

func TestAgentPrivateProfileSessionAndWorkspaceBoundariesIntegration(t *testing.T) {
	f := agentPrivateTestFixture(t)
	saved := savePrivateCanaries(t, f)
	owner := f.owner.WorkspacePrincipal
	for _, item := range []struct {
		name   string
		access agentprofile.PrivateAccess
	}{
		{"anonymous", agentprofile.PrivateAccess{WorkspacePrincipal: owner}},
		{"unknown_session", agentprofile.PrivateAccess{SessionDigest: [32]byte{42}, WorkspacePrincipal: owner}},
		{"peer_claiming_owner", agentprofile.PrivateAccess{SessionDigest: f.peer.SessionDigest, WorkspacePrincipal: owner}},
		{"owner_claiming_peer", agentprofile.PrivateAccess{SessionDigest: f.owner.SessionDigest, WorkspacePrincipal: f.peer.WorkspacePrincipal}},
		{"organization_session_claiming_person", agentprofile.PrivateAccess{SessionDigest: f.org.SessionDigest, WorkspacePrincipal: owner}},
		{"business_session_claiming_person", agentprofile.PrivateAccess{SessionDigest: f.biz.SessionDigest, WorkspacePrincipal: owner}},
		{"organization_workspace", agentprofile.PrivateAccess{SessionDigest: f.owner.SessionDigest, WorkspacePrincipal: f.org.WorkspacePrincipal}},
		{"business_workspace", agentprofile.PrivateAccess{SessionDigest: f.owner.SessionDigest, WorkspacePrincipal: f.biz.WorkspacePrincipal}},
		{"community_workspace", agentprofile.PrivateAccess{SessionDigest: f.owner.SessionDigest, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Community, ID: owner.ID}}},
		{"zero_owner", agentprofile.PrivateAccess{SessionDigest: f.owner.SessionDigest, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: "00000000-0000-0000-0000-000000000000"}}},
		{"unknown_owner_type", agentprofile.PrivateAccess{SessionDigest: f.owner.SessionDigest, WorkspacePrincipal: actorref.PrincipalRef{Type: "UNKNOWN", ID: owner.ID}}},
	} {
		t.Run(item.name, func(t *testing.T) {
			record, err := f.base.store.ReadOwnAgentPrivateProfile(f.base.ctx, item.access)
			requirePrivateProfileError(t, record, err, agentprofile.ErrForbidden)
			record, err = f.base.store.ReplaceOwnAgentPrivateProfile(f.base.ctx, item.access,
				agentprofile.ReplacePrivateInput{ExpectedVersion: saved.Profile.ProfileVersion, Fields: agentprofile.PrivateFields{}})
			requirePrivateProfileError(t, record, err, agentprofile.ErrForbidden)
		})
	}
	current, err := f.base.store.ReadOwnAgentPrivateProfile(f.base.ctx, f.owner)
	if err != nil || !sameAgentProfileMetadata(current.Profile, saved.Profile) || !reflect.DeepEqual(current.Fields, saved.Fields) {
		t.Fatal("rejected cross-principal operations changed owner's private contents")
	}
}

func TestAgentPrivateProfileCurrentSessionAndIdentityIntegration(t *testing.T) {
	f := agentPrivateTestFixture(t)
	saved := savePrivateCanaries(t, f)
	b := f.base
	for _, item := range []struct {
		name, statement, restore string
		args                     []any
	}{
		{"revoked_session", `UPDATE sessions SET revoked_at=now() WHERE id=$1`, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, []any{f.ownerSession}},
		{"absolute_expired", `UPDATE sessions SET created_at=now()-interval '2 hours',expires_at=now()-interval '1 hour',idle_expires_at=now()-interval '1 hour' WHERE id=$1`, `UPDATE sessions SET expires_at=now()+interval '2 hours',idle_expires_at=now()+interval '1 hour' WHERE id=$1`, []any{f.ownerSession}},
		{"idle_expired", `UPDATE sessions SET created_at=now()-interval '2 hours',idle_expires_at=now()-interval '1 hour' WHERE id=$1`, `UPDATE sessions SET idle_expires_at=now()+interval '1 hour' WHERE id=$1`, []any{f.ownerSession}},
		{"development_auth_disabled", `UPDATE sessions SET authentication_method='dev_phone' WHERE id=$1`, `UPDATE sessions SET authentication_method='test' WHERE id=$1`, []any{f.ownerSession}},
		{"account_suspended", `UPDATE accounts SET status='suspended' WHERE id=$1`, `UPDATE accounts SET status='active' WHERE id=$1`, []any{b.person.ID}},
		{"account_deleted", `UPDATE accounts SET status='deleted' WHERE id=$1`, `UPDATE accounts SET status='active' WHERE id=$1`, []any{b.person.ID}},
		{"agent_suspended", `UPDATE agents SET status='suspended' WHERE id=$1`, `UPDATE agents SET status='active' WHERE id=$1`, []any{b.personID}},
		{"agent_retired", `UPDATE agents SET status='retired' WHERE id=$1`, `UPDATE agents SET status='active' WHERE id=$1`, []any{b.personID}},
	} {
		t.Run(item.name, func(t *testing.T) {
			b.exec(item.statement, item.args...)
			defer b.exec(item.restore, item.args...)
			record, err := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.owner)
			requirePrivateProfileError(t, record, err, agentprofile.ErrForbidden)
			record, err = b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.owner,
				agentprofile.ReplacePrivateInput{ExpectedVersion: saved.Profile.ProfileVersion, Fields: agentprofile.PrivateFields{}})
			requirePrivateProfileError(t, record, err, agentprofile.ErrForbidden)
		})
	}
	current, err := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.owner)
	if err != nil || !reflect.DeepEqual(current.Fields, saved.Fields) || !sameAgentProfileMetadata(current.Profile, saved.Profile) {
		t.Fatal("invalid session/identity changed retained private fields or stable Agent")
	}
}

func TestAgentPrivateProfilePublicGrantsRolesAndBlocksAreNotPrivatePermissionIntegration(t *testing.T) {
	f := agentPrivateTestFixture(t)
	b := f.base
	saved := savePrivateCanaries(t, f)
	for _, viewer := range []string{"", b.other.ID} {
		public, err := b.store.ReadProfile(b.ctx, viewer, b.person.ID)
		if err != nil {
			t.Fatal(err)
		}
		assertNoPrivateCanaries(t, public)
	}
	b.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, b.person.ID)
	if _, err := b.store.GrantProfileRead(b.ctx, b.person.ID, b.other.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	public, err := b.store.ReadProfile(b.ctx, b.other.ID, b.person.ID)
	if err != nil {
		t.Fatal("existing profile_view grant did not retain its original public-field behavior")
	}
	assertNoPrivateCanaries(t, public)
	b.exec(`INSERT INTO organization_memberships(organization_id,user_account_id,role)
		VALUES($1,$2,'owner'),($1,$3,'member')`, b.orgID, b.other.ID, b.person.ID)
	for _, access := range []agentprofile.PrivateAccess{
		{SessionDigest: f.peer.SessionDigest, WorkspacePrincipal: f.owner.WorkspacePrincipal},
		{SessionDigest: f.peer.SessionDigest, WorkspacePrincipal: f.org.WorkspacePrincipal},
	} {
		private, readErr := b.store.ReadOwnAgentPrivateProfile(b.ctx, access)
		requirePrivateProfileError(t, private, readErr, agentprofile.ErrForbidden)
	}
	adapter, err := agentcognitive.NewCurrentDomainAdapter(b.store)
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := adapter.ReadOwnProfile(b.ctx, agentcognitive.SessionAccess{
		Digest: f.owner.SessionDigest, Workspace: f.owner.WorkspacePrincipal})
	if err != nil {
		t.Fatal(err)
	}
	assertNoPrivateCanaries(t, ordinary)
	if err = b.store.BlockAccount(b.ctx, b.person.ID, b.other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = b.store.ReadProfile(b.ctx, b.other.ID, b.person.ID); !errors.Is(err, identity.ErrNotFound) {
		t.Fatal("Block did not retain precedence over the old UserProfile/public grant")
	}
	stolen, err := b.store.ReadOwnAgentPrivateProfile(b.ctx,
		agentprofile.PrivateAccess{SessionDigest: f.peer.SessionDigest, WorkspacePrincipal: f.owner.WorkspacePrincipal})
	requirePrivateProfileError(t, stolen, err, agentprofile.ErrForbidden)
	self, err := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.owner)
	if err != nil || !reflect.DeepEqual(self.Fields, saved.Fields) {
		t.Fatal("blocking a third party altered owner's private content")
	}
}

func TestAgentPrivateProfileConcurrentVersionCASIntegration(t *testing.T) {
	f := agentPrivateTestFixture(t)
	b := f.base
	initial, err := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		record agentprofile.PrivateRecord
		err    error
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	var attempts sync.WaitGroup
	for _, notes := range []string{"合成私密并发写-A", "合成私密并发写-B"} {
		attempts.Add(1)
		go func(notes string) {
			defer attempts.Done()
			<-start
			record, replaceErr := b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.owner,
				agentprofile.ReplacePrivateInput{ExpectedVersion: initial.Profile.ProfileVersion, Fields: agentprofile.PrivateFields{AgentNotes: notes}})
			results <- outcome{record, replaceErr}
		}(notes)
	}
	close(start)
	attempts.Wait()
	close(results)
	var success, conflict int
	var winner agentprofile.PrivateRecord
	for result := range results {
		if result.err == nil {
			success++
			winner = result.record
		} else {
			requirePrivateProfileError(t, result.record, result.err, agentprofile.ErrConflict)
			conflict++
		}
	}
	if success != 1 || conflict != 1 || winner.Profile.ProfileVersion != initial.Profile.ProfileVersion+1 {
		t.Fatalf("concurrent private CAS outcomes: success=%d conflict=%d", success, conflict)
	}
	current, err := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.owner)
	if err != nil || !reflect.DeepEqual(current.Fields, winner.Fields) || !sameAgentProfileMetadata(current.Profile, winner.Profile) {
		t.Fatal("concurrent private CAS produced lost or inconsistent fields/revision")
	}
	var count int
	if err = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_private_profiles WHERE agent_id=$1`, b.personID).Scan(&count); err != nil || count != 1 {
		t.Fatal("concurrent private CAS duplicated a private resource")
	}
}

func TestAgentPrivateProfileWriteFailureRollsBackAndRedactsIntegration(t *testing.T) {
	f := agentPrivateTestFixture(t)
	b := f.base
	before := savePrivateCanaries(t, f)
	suffix := strings.ReplaceAll(b.personID, "-", "")
	functionName, triggerName := "private_failure_"+suffix, "private_fail_guard_"+suffix
	// The fault is unique and scoped to this fixture's Agent. Its PostgreSQL
	// DETAIL intentionally contains private JSON: the store must redact it and
	// roll back the already-advanced foundation version in the same transaction.
	b.exec(`CREATE FUNCTION ` + functionName + `() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'owned synthetic private write failure' USING DETAIL=NEW.fields::text; END $$`)
	t.Cleanup(func() {
		if _, err := b.pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS `+triggerName+` ON agent_private_profiles`); err != nil {
			t.Errorf("owned private fault trigger cleanup failed: %v", err)
		}
		if _, err := b.pool.Exec(context.Background(), `DROP FUNCTION `+functionName+`() `); err != nil {
			t.Errorf("owned private fault function cleanup failed: %v", err)
		}
	})
	b.exec(`CREATE TRIGGER ` + triggerName + ` BEFORE INSERT OR UPDATE ON agent_private_profiles
		FOR EACH ROW WHEN (NEW.agent_id='` + b.personID + `'::uuid) EXECUTE FUNCTION ` + functionName + `() `)
	failed, err := b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.owner,
		agentprofile.ReplacePrivateInput{ExpectedVersion: before.Profile.ProfileVersion, Fields: agentprofile.PrivateFields{AgentNotes: "合成私密回滚错误载荷-AGE002"}})
	requirePrivateProfileError(t, failed, err, agentprofile.ErrUnavailable)
	if strings.Contains(err.Error(), "合成私密") || strings.Contains(err.Error(), "agentNotes") || strings.Contains(err.Error(), "DETAIL") {
		t.Fatal("a private database failure exposed its underlying failed-row detail")
	}
	after, err := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.owner)
	if err != nil || !sameAgentProfileMetadata(before.Profile, after.Profile) || !reflect.DeepEqual(before.Fields, after.Fields) {
		t.Fatal("failed private write did not roll back both metadata and fields")
	}
}

func TestAgentPrivateProfileInvalidInputHasNoEffectsIntegration(t *testing.T) {
	f := agentPrivateTestFixture(t)
	before := savePrivateCanaries(t, f)
	for _, item := range []struct {
		name  string
		input agentprofile.ReplacePrivateInput
	}{
		{"missing_version", agentprofile.ReplacePrivateInput{Fields: privateProfileCanaries()}},
		{"negative_version", agentprofile.ReplacePrivateInput{ExpectedVersion: -1, Fields: privateProfileCanaries()}},
		{"oversized_text", agentprofile.ReplacePrivateInput{ExpectedVersion: before.Profile.ProfileVersion, Fields: agentprofile.PrivateFields{AgentNotes: strings.Repeat("字", agentprofile.MaxPrivateTextRunes+1)}}},
		{"oversized_list", agentprofile.ReplacePrivateInput{ExpectedVersion: before.Profile.ProfileVersion, Fields: agentprofile.PrivateFields{LanguagePreferences: make([]string, agentprofile.MaxPrivateListItems+1)}}},
		{"invalid_control", agentprofile.ReplacePrivateInput{ExpectedVersion: before.Profile.ProfileVersion, Fields: agentprofile.PrivateFields{AgentNotes: "invalid\x00private"}}},
	} {
		t.Run(item.name, func(t *testing.T) {
			result, err := f.base.store.ReplaceOwnAgentPrivateProfile(f.base.ctx, f.owner, item.input)
			requirePrivateProfileError(t, result, err, agentprofile.ErrInvalid)
		})
	}
	after, err := f.base.store.ReadOwnAgentPrivateProfile(f.base.ctx, f.owner)
	if err != nil || !sameAgentProfileMetadata(before.Profile, after.Profile) || !reflect.DeepEqual(before.Fields, after.Fields) {
		t.Fatal("invalid replacement changed private fields or advanced metadata")
	}
}

func TestAgentPrivateProfileExpiryDuringMetadataWaitIntegration(t *testing.T) {
	for _, operation := range []string{"read", "replace"} {
		t.Run(operation, func(t *testing.T) {
			f := agentPrivateTestFixture(t)
			b := f.base
			before := savePrivateCanaries(t, f)
			blocker, err := b.pool.Begin(b.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback(context.Background())
			if _, err = blocker.Exec(b.ctx, `SELECT agent_id FROM agent_profiles WHERE agent_id=$1 FOR UPDATE`, b.personID); err != nil {
				t.Fatal(err)
			}
			b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '800 milliseconds' WHERE id=$1`, f.ownerSession)
			type outcome struct {
				record agentprofile.PrivateRecord
				err    error
			}
			completed := make(chan outcome, 1)
			go func() {
				var record agentprofile.PrivateRecord
				var operationErr error
				if operation == "read" {
					record, operationErr = b.store.ReadOwnAgentPrivateProfile(b.ctx, f.owner)
				} else {
					record, operationErr = b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.owner,
						agentprofile.ReplacePrivateInput{ExpectedVersion: before.Profile.ProfileVersion, Fields: agentprofile.PrivateFields{AgentNotes: "合成私密过期操作不得保存"}})
				}
				completed <- outcome{record, operationErr}
			}()
			blocked := false
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				if err = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
					WHERE $1::integer=ANY(pg_blocking_pids(pid)))`, int(blocker.Conn().PgConn().PID())).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !blocked {
				t.Fatal("operation did not reach the actual metadata lock before session expiry")
			}
			time.Sleep(900 * time.Millisecond)
			if err = blocker.Commit(b.ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-completed:
				requirePrivateProfileError(t, result.record, result.err, agentprofile.ErrForbidden)
			case <-time.After(3 * time.Second):
				t.Fatal("expired private operation did not finish after its metadata lock was released")
			}
			b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '1 hour' WHERE id=$1`, f.ownerSession)
			after, readErr := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.owner)
			if readErr != nil || !sameAgentProfileMetadata(before.Profile, after.Profile) || !reflect.DeepEqual(before.Fields, after.Fields) {
				t.Fatal("operation expired during metadata wait released fields or committed a partial write")
			}
		})
	}
}
