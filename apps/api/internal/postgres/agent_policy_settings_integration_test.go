package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func policyNativeFixture(t *testing.T) *agentPrivateFixture {
	t.Helper()
	f := agentPrivateTestFixture(t)
	var installed bool
	if f.base.pool.QueryRow(f.base.ctx, `SELECT to_regclass('public.agent_policy_settings') IS NOT NULL`).Scan(&installed) != nil || !installed {
		t.Fatal("requires actual migration065")
	}
	return f
}
func policyNativeInput(f agentpolicysettings.Family, version int64, expiry time.Time) agentpolicysettings.PutInput {
	raw := `{"level":"LEVEL_2_PREPARE"}`
	if f == agentpolicysettings.Attention {
		raw = `{"defaultRoute":"NORMAL","rules":[{"eventType":"UserQuery","route":"IMMEDIATE"}]}`
	} else if f == agentpolicysettings.Social {
		raw = `{"rules":[{"category":"UNKNOWN_PERSON","preference":"REVIEW_REQUIRED"}]}`
	}
	return agentpolicysettings.PutInput{ExpectedVersion: version, Settings: json.RawMessage(raw), ExpiresAt: expiry.UTC().Truncate(time.Microsecond)}
}
func policyNativeSnapshot(t *testing.T, f *agentPrivateFixture, includeSettings bool) string {
	t.Helper()
	query := `SELECT jsonb_build_object('profile',(SELECT jsonb_agg(to_jsonb(p) ORDER BY account_id) FROM user_profiles p WHERE account_id=ANY($1::uuid[])),
 'metadata',(SELECT jsonb_agg(to_jsonb(p) ORDER BY agent_id) FROM agent_profiles p WHERE owner_id=ANY($1::uuid[])),
 'private',(SELECT jsonb_agg(to_jsonb(p) ORDER BY agent_id) FROM agent_private_profiles p WHERE owner_id=ANY($1::uuid[])),
 'visibility',(SELECT jsonb_agg(to_jsonb(p) ORDER BY agent_id) FROM agent_profile_field_visibility p WHERE owner_id=ANY($1::uuid[])),
 'memory',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM agent_memories p WHERE owner_id=ANY($1::uuid[])),
 'grants',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM consent_grants p WHERE owner_account_id=ANY($1::uuid[])),
 'notification',(SELECT jsonb_agg(to_jsonb(p) ORDER BY owner_id) FROM native_notification_policies p WHERE owner_id=ANY($1::uuid[])),
 'settings',CASE WHEN $2::boolean THEN (SELECT jsonb_agg(to_jsonb(p) ORDER BY agent_id,family) FROM agent_policy_settings p WHERE owner_id=ANY($1::uuid[])) END)::text`
	var raw string
	if err := f.base.pool.QueryRow(f.base.ctx, query, f.base.accounts, includeSettings).Scan(&raw); err != nil {
		t.Fatal("cannot snapshot actual nonempty native sources")
	}
	return raw
}
func policyNativeRecord(b agentpolicysettings.Bundle, f agentpolicysettings.Family) agentpolicysettings.Record {
	switch f {
	case agentpolicysettings.Attention:
		return b.Attention
	case agentpolicysettings.Social:
		return b.Social
	}
	return b.Autonomy
}
func policyNativeRequireReject(t *testing.T, b agentpolicysettings.Bundle, e, want error) {
	t.Helper()
	if !errors.Is(e, want) || !reflect.DeepEqual(b, agentpolicysettings.Bundle{}) {
		t.Fatalf("rejection=%v want=%v empty=%t", e, want, reflect.DeepEqual(b, agentpolicysettings.Bundle{}))
	}
}
func TestPolicySettingsNativePersistenceIndependentCAS(t *testing.T) {
	f := policyNativeFixture(t)
	b := f.base
	savePrivateCanaries(t, f)
	if _, err := b.store.GrantProfileRead(b.ctx, b.person.ID, b.other.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var memoryID string
	if b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid()`).Scan(&memoryID) != nil {
		t.Fatal("memory ID unavailable")
	}
	if _, err := b.store.PutOwnMemory(b.ctx, f.owner, memoryID, agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "policy-api.retained", Summary: "合成旧Memory保留", StructuredValue: json.RawMessage(`{"synthetic":true}`), Visibility: agentmemory.VisibilityAgentOnly, ValidUntil: time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.store.PutOwnNotificationPolicy(b.ctx, f.owner, agentnotification.PutInput{Enabled: true, DefaultRoute: agentnotification.Digest, Rules: []agentnotification.Rule{}, ExpiresAt: time.Now().UTC().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	// Nonempty visibility is a different native source and independent version.
	visibility, err := b.store.ReadOwnAgentProfileVisibility(b.ctx, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	rules := agentprofile.DefaultFieldRules()
	rules[agentprofile.FieldBio] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPrivate, CommunityIDs: []string{}}
	if _, err = b.store.ReplaceOwnAgentProfileVisibility(b.ctx, f.owner, agentprofile.ReplaceVisibilityInput{ExpectedVersion: visibility.Profile.ProfileVersion, Rules: rules}); err != nil {
		t.Fatal(err)
	}
	before := policyNativeSnapshot(t, f, false)
	t.Run("GETMissingNeverBackfills", func(t *testing.T) {
		current, e := b.store.GetOwnPolicies(b.ctx, f.owner)
		if e != nil || agentpolicysettings.ValidateBundle(current) != nil || current.AgentID != b.personID || current.OwnerID != b.person.ID {
			t.Fatal("missing settings read invalid", e)
		}
		var n int
		if b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_policy_settings WHERE agent_id=$1`, b.personID).Scan(&n) != nil || n != 0 {
			t.Fatal("GET persisted a default")
		}
	})
	for _, family := range agentpolicysettings.Families() {
		t.Run(string(family), func(t *testing.T) {
			current, e := b.store.PutOwnPolicy(b.ctx, f.owner, family, policyNativeInput(family, 0, time.Now().Add(time.Hour)))
			if e != nil || policyNativeRecord(current, family).NativeRevision != 1 {
				t.Fatal("create failed", e)
			}
			if before != policyNativeSnapshot(t, f, false) {
				t.Fatal("policy changed another native source")
			}
			got, e := New(b.pool, false).GetOwnPolicies(b.ctx, f.owner)
			if e != nil || !reflect.DeepEqual(policyNativeRecord(got, family), policyNativeRecord(current, family)) {
				t.Fatal("new Store lost persistent settings", e)
			}
		})
	}
	t.Run("IndependentNativeRevision", func(t *testing.T) {
		current, e := b.store.PutOwnPolicy(b.ctx, f.owner, agentpolicysettings.Social, policyNativeInput(agentpolicysettings.Social, 1, time.Now().Add(time.Hour)))
		if e != nil || current.Social.NativeRevision != 2 || current.Attention.NativeRevision != 1 || current.Autonomy.NativeRevision != 1 {
			t.Fatal("cross-family version consumed", e)
		}
	})
	t.Run("StaleCASNoWrite", func(t *testing.T) {
		snapshot := policyNativeSnapshot(t, f, true)
		got, e := b.store.PutOwnPolicy(b.ctx, f.owner, agentpolicysettings.Social, policyNativeInput(agentpolicysettings.Social, 1, time.Now().Add(time.Hour)))
		policyNativeRequireReject(t, got, e, agentpolicysettings.ErrConflict)
		if snapshot != policyNativeSnapshot(t, f, true) {
			t.Fatal("stale write changed rows")
		}
	})
	t.Run("PeerOwnDefaultAndForeignDenied", func(t *testing.T) {
		got, e := b.store.GetOwnPolicies(b.ctx, f.peer)
		if e != nil || got.OwnerID != b.other.ID || got.AgentID != b.otherID || got.Attention.Configured || got.Social.Configured || got.Autonomy.Configured {
			t.Fatal("peer inherited owner preferences")
		}
		wrong := f.peer
		wrong.WorkspacePrincipal = b.person
		got, e = b.store.GetOwnPolicies(b.ctx, wrong)
		policyNativeRequireReject(t, got, e, agentpolicysettings.ErrForbidden)
	})
	for name, access := range map[string]agentprofile.PrivateAccess{"Org": f.org, "DormantBusiness": f.biz} {
		t.Run(name, func(t *testing.T) {
			got, e := b.store.GetOwnPolicies(b.ctx, access)
			policyNativeRequireReject(t, got, e, agentpolicysettings.ErrForbidden)
		})
	}
	t.Run("NilContextValidPool", func(t *testing.T) {
		got, e := b.store.GetOwnPolicies(nil, f.owner)
		policyNativeRequireReject(t, got, e, agentpolicysettings.ErrUnavailable)
		got, e = b.store.PutOwnPolicy(nil, f.owner, agentpolicysettings.Attention, policyNativeInput(agentpolicysettings.Attention, 1, time.Now().Add(time.Hour)))
		policyNativeRequireReject(t, got, e, agentpolicysettings.ErrUnavailable)
	})
	if before != policyNativeSnapshot(t, f, false) {
		t.Fatal("native sources not independent")
	}
	t.Log("nonempty ordinary/private/visibility/Memory/grants/notification rows preserved; no runtime consent granted")
}
func TestPolicySettingsNativeConcurrentFamilies(t *testing.T) {
	f := policyNativeFixture(t)
	b := f.base
	var wg sync.WaitGroup
	results := make(chan error, 3)
	for _, family := range agentpolicysettings.Families() {
		wg.Add(1)
		go func(family agentpolicysettings.Family) {
			defer wg.Done()
			_, e := b.store.PutOwnPolicy(b.ctx, f.owner, family, policyNativeInput(family, 0, time.Now().Add(time.Hour)))
			results <- e
		}(family)
	}
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil {
			t.Fatal("different-family concurrent write failed", e)
		}
	}
	got, e := b.store.GetOwnPolicies(b.ctx, f.owner)
	if e != nil || got.Attention.NativeRevision != 1 || got.Social.NativeRevision != 1 || got.Autonomy.NativeRevision != 1 {
		t.Fatal("family versions not independent")
	}
	results = make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := b.store.PutOwnPolicy(b.ctx, f.owner, agentpolicysettings.Autonomy, policyNativeInput(agentpolicysettings.Autonomy, 1, time.Now().Add(time.Hour)))
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for e := range results {
		if e == nil {
			wins++
		} else if errors.Is(e, agentpolicysettings.ErrConflict) {
			conflicts++
		} else {
			t.Fatal("unexpected concurrent error", e)
		}
	}
	if wins != 1 || conflicts != 11 {
		t.Fatal("CAS admitted multiple winners", wins, conflicts)
	}
	t.Log("3 independent-family concurrent writes succeed; 12 same-family CAS gives 1 winner/11 conflicts")
}
func TestPolicySettingsNativeRetainedExpired(t *testing.T) {
	f := policyNativeFixture(t)
	b := f.base
	got, e := b.store.PutOwnPolicy(b.ctx, f.owner, agentpolicysettings.Autonomy, policyNativeInput(agentpolicysettings.Autonomy, 0, time.Now().Add(250*time.Millisecond)))
	if e != nil {
		t.Fatal(e)
	}
	before := policyNativeSnapshot(t, f, true)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(*got.Autonomy.ExpiresAt) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	expired, e := New(b.pool, false).GetOwnPolicies(b.ctx, f.owner)
	if e != nil || expired.Autonomy.NativeRevision != 1 || expired.Autonomy.Status != "EXPIRED" || !expired.Autonomy.Configured || before != policyNativeSnapshot(t, f, true) {
		t.Fatal("expired GET restored or changed row", e)
	}
	got, e = b.store.PutOwnPolicy(b.ctx, f.owner, agentpolicysettings.Autonomy, policyNativeInput(agentpolicysettings.Autonomy, 1, time.Now().Add(time.Hour)))
	if e != nil || got.Autonomy.NativeRevision != 2 || got.Autonomy.Status != "ACTIVE" {
		t.Fatal("explicit versioned replacement failed", e)
	}
}
func TestPolicySettingsNativeCurrentIdentity(t *testing.T) {
	for _, mode := range []string{"revoked", "expired", "idle_expired", "account_suspended", "agent_retired", "metadata_missing", "dev_phone", "wrong_digest", "wrong_owner"} {
		t.Run(mode, func(t *testing.T) {
			f := policyNativeFixture(t)
			b := f.base
			access := f.owner
			switch mode {
			case "revoked":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.ownerSession)
			case "expired":
				b.exec(`UPDATE sessions SET created_at=clock_timestamp()-interval '4 hours',expires_at=clock_timestamp()-interval '1 hour',idle_expires_at=clock_timestamp()-interval '2 hours' WHERE id=$1`, f.ownerSession)
			case "idle_expired":
				b.exec(`UPDATE sessions SET created_at=clock_timestamp()-interval '4 hours',idle_expires_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, f.ownerSession)
			case "account_suspended":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
			case "agent_retired":
				b.exec(`UPDATE agents SET status='retired' WHERE id=$1`, b.personID)
			case "metadata_missing":
				b.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, b.personID)
			case "dev_phone":
				b.exec(`UPDATE sessions SET authentication_method='dev_phone' WHERE id=$1`, f.ownerSession)
			case "wrong_digest":
				access.SessionDigest[0] ^= 0xff
			case "wrong_owner":
				access.WorkspacePrincipal = b.other
			}
			before := policyNativeSnapshot(t, f, true)
			want := agentpolicysettings.ErrForbidden
			if mode == "metadata_missing" {
				want = agentpolicysettings.ErrNotFound
			}
			got, e := b.store.GetOwnPolicies(b.ctx, access)
			policyNativeRequireReject(t, got, e, want)
			got, e = b.store.PutOwnPolicy(b.ctx, access, agentpolicysettings.Autonomy, policyNativeInput(agentpolicysettings.Autonomy, 0, time.Now().Add(time.Hour)))
			policyNativeRequireReject(t, got, e, want)
			if before != policyNativeSnapshot(t, f, true) {
				t.Fatal("denied operation modified native rows")
			}
		})
	}
}
func TestPolicySettingsNativeSQLShapes(t *testing.T) {
	f := policyNativeFixture(t)
	b := f.base
	bad := []struct{ name, family, raw string }{
		{"AttentionScalar", "ATTENTION", `1`}, {"AttentionArray", "ATTENTION", `[]`}, {"AttentionMissing", "ATTENTION", `{}`}, {"AttentionNull", "ATTENTION", `{"defaultRoute":"BLOCK","rules":null}`}, {"AttentionWrongRoute", "ATTENTION", `{"defaultRoute":"ALLOW","rules":[]}`}, {"AttentionNestedUnknown", "ATTENTION", `{"defaultRoute":"BLOCK","rules":[{"eventType":"UserQuery","route":"BLOCK","allow":true}]}`}, {"AttentionNestedMissing", "ATTENTION", `{"defaultRoute":"BLOCK","rules":[{"route":"BLOCK"}]}`}, {"AttentionNestedScalar", "ATTENTION", `{"defaultRoute":"BLOCK","rules":[1]}`}, {"AttentionDupEvent", "ATTENTION", `{"defaultRoute":"BLOCK","rules":[{"eventType":"UserQuery","route":"BLOCK"},{"eventType":"UserQuery","route":"BLOCK"}]}`}, {"AttentionUnknownEvent", "ATTENTION", `{"defaultRoute":"BLOCK","rules":[{"eventType":"Fake","route":"BLOCK"}]}`}, {"AttentionNullPause", "ATTENTION", `{"defaultRoute":"BLOCK","rules":[],"pauseUntil":null}`}, {"AttentionOffsetPause", "ATTENTION", `{"defaultRoute":"BLOCK","rules":[],"pauseUntil":"2026-10-03T10:00:00+08:00"}`}, {"AttentionNanosecondPause", "ATTENTION", `{"defaultRoute":"BLOCK","rules":[],"pauseUntil":"2026-10-03T10:00:00.123456789Z"}`},
		{"SocialMissing", "SOCIAL", `{}`}, {"SocialScalar", "SOCIAL", `1`}, {"SocialWrongArray", "SOCIAL", `{"rules":{}}`}, {"SocialSparse", "SOCIAL", `{"rules":[]}`}, {"AutonomyMissing", "AUTONOMY", `{}`}, {"AutonomyNumeric", "AUTONOMY", `{"level":0}`}, {"AutonomyNull", "AUTONOMY", `{"level":null}`}, {"AutonomyUnknown", "AUTONOMY", `{"level":"OTHER"}`}, {"Autonomy3", "AUTONOMY", `{"level":"LEVEL_3_DELEGATE"}`}, {"AutonomyAuthority", "AUTONOMY", `{"level":"LEVEL_0_OBSERVE","consent":true}`}, {"UnknownFamily", "FAKE", `{"level":"LEVEL_0_OBSERVE"}`},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			var valid bool
			e := b.pool.QueryRow(b.ctx, `SELECT birdtie_agent_policy_settings_valid($1,$2::jsonb)`, c.family, c.raw).Scan(&valid)
			if e != nil || valid {
				t.Fatal("SQL shape function failed safely", e)
			}
			_, e = b.pool.Exec(b.ctx, `INSERT INTO agent_policy_settings(agent_id,owner_id,owner_type,family,schema_version,native_revision,settings,valid_from,expires_at,updated_at) VALUES($1,$2,'PERSON',$3,'agent-policy-settings-v1',1,$4::jsonb,statement_timestamp(),statement_timestamp()+interval '1 hour',statement_timestamp())`, b.personID, b.person.ID, c.family, c.raw)
			var p *pgconn.PgError
			if !errors.As(e, &p) || p.Code != "23514" {
				t.Fatalf("actual SQL constraint expected23514 got %v", e)
			}
		})
	}
	for _, c := range []struct{ name, family, raw string }{{"Attention", "ATTENTION", `{"defaultRoute":"BLOCK","rules":[]}`}, {"Autonomy", "AUTONOMY", `{"level":"LEVEL_2_PREPARE"}`}} {
		t.Run("Valid"+c.name, func(t *testing.T) {
			var ok bool
			if b.pool.QueryRow(b.ctx, `SELECT birdtie_agent_policy_settings_valid($1,$2::jsonb)`, c.family, c.raw).Scan(&ok) != nil || !ok {
				t.Fatal("valid SQL shape rejected")
			}
		})
	}
	if _, e := b.store.PutOwnPolicy(b.ctx, f.owner, agentpolicysettings.Autonomy, policyNativeInput(agentpolicysettings.Autonomy, 0, time.Now().Add(time.Hour))); e != nil {
		t.Fatal(e)
	}
	for _, c := range []struct{ name, sql string }{{"NoVersionIncrement", `UPDATE agent_policy_settings SET settings='{"level":"LEVEL_1_ASSIST"}' WHERE agent_id=$1`}, {"SkipVersion", `UPDATE agent_policy_settings SET native_revision=native_revision+2 WHERE agent_id=$1`}, {"ChangeFamily", `UPDATE agent_policy_settings SET native_revision=native_revision+1,family='ATTENTION' WHERE agent_id=$1`}, {"ChangeOwner", `UPDATE agent_policy_settings SET native_revision=native_revision+1,owner_id=$2 WHERE agent_id=$1`}, {"ChangeSchema", `UPDATE agent_policy_settings SET native_revision=native_revision+1,schema_version='v2' WHERE agent_id=$1`}, {"Expire", `UPDATE agent_policy_settings SET native_revision=native_revision+1,expires_at=clock_timestamp()-interval '1 second' WHERE agent_id=$1`}, {"FutureFrom", `UPDATE agent_policy_settings SET native_revision=native_revision+1,valid_from=clock_timestamp()+interval '5 minutes',updated_at=clock_timestamp()+interval '5 minutes' WHERE agent_id=$1`}, {"LongTTL", `UPDATE agent_policy_settings SET native_revision=native_revision+1,expires_at=valid_from+interval '721 hours' WHERE agent_id=$1`}, {"BadUpdated", `UPDATE agent_policy_settings SET native_revision=native_revision+1,updated_at=updated_at+interval '1 second' WHERE agent_id=$1`}, {"Infinity", `UPDATE agent_policy_settings SET native_revision=native_revision+1,expires_at='infinity' WHERE agent_id=$1`}} {
		t.Run(c.name, func(t *testing.T) {
			before := policyNativeSnapshot(t, f, true)
			args := []any{b.personID}
			if c.name == "ChangeOwner" {
				args = append(args, b.other.ID)
			}
			_, e := b.pool.Exec(b.ctx, c.sql, args...)
			var p *pgconn.PgError
			if !errors.As(e, &p) || p.Code != "23514" || before != policyNativeSnapshot(t, f, true) {
				t.Fatal("actual SQL mutation not atomically rejected", e)
			}
		})
	}
}
func TestPolicySettingsNativeExplicitRC(t *testing.T) {
	f := policyNativeFixture(t)
	b := f.base
	cfg := b.pool.Config().Copy()
	cfg.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"
	cfg.ConnConfig.RuntimeParams["TimeZone"] = "Asia/Shanghai"
	pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	var isolation string
	if pool.QueryRow(b.ctx, `SHOW default_transaction_isolation`).Scan(&isolation) != nil || isolation != "repeatable read" {
		t.Fatal("real RR pool absent")
	}
	store := New(pool, false)
	tx, _, e := store.beginPolicySettings(b.ctx, f.owner)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	var current, zone string
	if tx.QueryRow(b.ctx, `SELECT current_setting('transaction_isolation'),current_setting('TimeZone')`).Scan(&current, &zone) != nil || current != "read committed" || zone != "UTC" {
		t.Fatal("native transaction did not override RR/timezone")
	}
	if tx.Commit(b.ctx) != nil {
		t.Fatal("cannot close current tx")
	}
	got, e := store.PutOwnPolicy(b.ctx, f.owner, agentpolicysettings.Autonomy, policyNativeInput(agentpolicysettings.Autonomy, 0, time.Now().Add(time.Hour)))
	if e != nil || got.Autonomy.ValidFrom.Location() != time.UTC || got.ObservedAt.Location() != time.UTC {
		t.Fatal("actual RC normalized persistence failed", e)
	}
}
func TestPolicySettingsNativeMissingAndInvalid(t *testing.T) {
	f := policyNativeFixture(t)
	b := f.base
	for name, a := range map[string]agentprofile.PrivateAccess{"Zero": {}, "ForgedOrg": {SessionDigest: f.owner.SessionDigest, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Organization, ID: b.person.ID}}} {
		t.Run(name, func(t *testing.T) {
			got, e := b.store.GetOwnPolicies(b.ctx, a)
			policyNativeRequireReject(t, got, e, agentpolicysettings.ErrForbidden)
		})
	}
	for name, input := range map[string]agentpolicysettings.PutInput{"Level3": {Settings: json.RawMessage(`{"level":"LEVEL_3_DELEGATE"}`), ExpiresAt: time.Now().Add(time.Hour)}, "Expired": policyNativeInput(agentpolicysettings.Autonomy, 0, time.Now().Add(-time.Hour)), "Long": policyNativeInput(agentpolicysettings.Autonomy, 0, time.Now().Add(31*24*time.Hour)), "ClaimPermission": {Settings: json.RawMessage(`{"level":"LEVEL_0_OBSERVE","approved":true}`), ExpiresAt: time.Now().Add(time.Hour)}} {
		t.Run(name, func(t *testing.T) {
			before := policyNativeSnapshot(t, f, true)
			got, e := b.store.PutOwnPolicy(b.ctx, f.owner, agentpolicysettings.Autonomy, input)
			policyNativeRequireReject(t, got, e, agentpolicysettings.ErrInvalid)
			if before != policyNativeSnapshot(t, f, true) {
				t.Fatal("invalid settings persisted")
			}
		})
	}
	t.Run("UnknownFamily", func(t *testing.T) {
		got, e := b.store.PutOwnPolicy(b.ctx, f.owner, "OTHER", policyNativeInput(agentpolicysettings.Autonomy, 0, time.Now().Add(time.Hour)))
		policyNativeRequireReject(t, got, e, agentpolicysettings.ErrInvalid)
	})
}

// The server has no runtime source-purpose resolver. This actual human API
// stores preferences only; legacy process-local policy revisions are not used.
