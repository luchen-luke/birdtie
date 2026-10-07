package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5/pgxpool"
)

// All identities and contents are local synthetic fixtures. Real sessions and
// native Agent bindings are exercised; no seed identity or inference consent
// is borrowed. Memory removal uses its real parent FK cascade, not a waiver.
func agentMemoryTestFixture(t *testing.T) *agentPrivateFixture {
	t.Helper()
	f := agentPrivateTestFixture(t)
	b := f.base
	var installed bool
	if err := b.pool.QueryRow(b.ctx, `SELECT to_regclass('public.agent_memories') IS NOT NULL`).Scan(&installed); err != nil || !installed {
		t.Fatal("Agent Memory integration requires migration 056")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := b.pool.Exec(ctx, `DELETE FROM agent_profiles WHERE owner_id=ANY($1::uuid[])`, b.accounts); err != nil {
			t.Errorf("owned Memory parent cascade cleanup failed: %v", err)
		}
		var count int
		if err := b.pool.QueryRow(ctx, `SELECT count(*) FROM agent_memories WHERE owner_id=ANY($1::uuid[])`, b.accounts).Scan(&count); err != nil || count != 0 {
			t.Errorf("owned Memory cascade residue: count=%d error=%v", count, err)
		}
	})
	return f
}

func agentMemoryID(t *testing.T, f *agentPrivateFixture) string {
	t.Helper()
	var id string
	if err := f.base.pool.QueryRow(f.base.ctx, `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		t.Fatal("cannot allocate an owned Memory test ID")
	}
	return id
}

func agentMemoryInput(key string) agentmemory.PutInput {
	return agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: key,
		Summary: "合成本人直接声明-AGE004", StructuredValue: json.RawMessage(`{"declared":true,"items":["合成偏好"]}`),
		Visibility: agentmemory.VisibilityPrivate, ValidUntil: time.Now().UTC().Add(24 * time.Hour).Truncate(time.Microsecond)}
}

func requireAgentMemoryError(t *testing.T, record agentmemory.Record, err, expected error) {
	t.Helper()
	if !errors.Is(err, expected) || !reflect.DeepEqual(record, agentmemory.Record{}) {
		t.Fatalf("Memory operation did not reject without a payload: error=%v expected=%v", err, expected)
	}
	if strings.Contains(err.Error(), "合成") || strings.Contains(err.Error(), "structured") || strings.Contains(err.Error(), "DETAIL") {
		t.Fatal("Memory error exposed synthetic private contents or database detail")
	}
}

func mustPutAgentMemory(t *testing.T, f *agentPrivateFixture, id string, input agentmemory.PutInput) agentmemory.Record {
	t.Helper()
	record, err := f.base.store.PutOwnMemory(f.base.ctx, f.owner, id, input)
	if err != nil || agentmemory.ValidateRecord(record) != nil || record.ID != id || record.AgentID != f.base.personID ||
		record.OwnerID != f.base.person.ID || record.OwnerType != actorref.Person || record.SourceType != agentmemory.SourceExplicit ||
		record.Confidence != 1 || record.LastReinforcedAt != nil || record.Status != agentmemory.StatusActive || record.Version != input.ExpectedVersion+1 {
		t.Fatalf("native explicit Memory save failed or changed its binding: error=%v", err)
	}
	return record
}

func agentMemoryOwnedSourceSnapshot(t *testing.T, f *agentPrivateFixture) string {
	t.Helper()
	var value string
	err := f.base.pool.QueryRow(f.base.ctx, `SELECT jsonb_build_object(
		'accounts',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM accounts x WHERE id=ANY($1::uuid[])),
		'agents',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agents x WHERE principal_account_id=ANY($1::uuid[])),
		'metadata',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY agent_id),'[]') FROM agent_profiles x WHERE owner_id=ANY($1::uuid[])),
		'ordinaryProfiles',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY account_id),'[]') FROM user_profiles x WHERE account_id=ANY($1::uuid[])),
		'privateProfiles',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY agent_id),'[]') FROM agent_private_profiles x WHERE owner_id=ANY($1::uuid[])),
		'visibility',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY agent_id),'[]') FROM agent_profile_field_visibility x WHERE owner_id=ANY($1::uuid[])),
		'grants',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM consent_grants x WHERE owner_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])),
		'tasks',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agent_tasks x WHERE owner_account_id=ANY($1::uuid[]))
		)::text`, f.base.accounts).Scan(&value)
	if err != nil {
		t.Fatal("cannot snapshot owned native source rows")
	}
	return value
}

func TestAgentMemoryLifecycleAndSourceSeparationIntegration(t *testing.T) {
	f := agentMemoryTestFixture(t)
	b := f.base
	private := savePrivateCanaries(t, f)
	if _, err := b.store.GrantProfileRead(b.ctx, b.person.ID, b.other.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	sourceBefore := agentMemoryOwnedSourceSnapshot(t, f)
	initial, err := b.store.ReadOwnMemories(b.ctx, f.owner)
	if err != nil || initial == nil || len(initial) != 0 {
		t.Fatal("initial owner Memory read did not return a non-nil empty collection")
	}
	id := agentMemoryID(t, f)
	input := agentMemoryInput("lifecycle.preference")
	created := mustPutAgentMemory(t, f, id, input)
	retry, err := b.store.PutOwnMemory(b.ctx, f.owner, id, input)
	if err != nil || !reflect.DeepEqual(retry, created) {
		t.Fatal("identical create retry changed contents/revision or failed")
	}
	pool, err := pgxpool.New(b.ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	read, err := New(pool, false).ReadOwnMemories(b.ctx, f.owner)
	if err != nil || len(read) != 1 || !reflect.DeepEqual(read[0], created) {
		t.Fatal("explicit Memory did not persist across an independent database connection")
	}
	input.ExpectedVersion = 1
	input.Summary = "合成直接修改后的声明-AGE004"
	input.Visibility = agentmemory.VisibilityAgentOnly
	updated := mustPutAgentMemory(t, f, id, input)
	if !updated.CreatedAt.Equal(created.CreatedAt) || updated.UpdatedAt.Before(created.UpdatedAt) {
		t.Fatal("Memory update changed original creation time")
	}
	for _, expected := range []int64{1, 2} {
		t.Run("identical_update_retry_expected_"+string(rune('0'+expected)), func(t *testing.T) {
			repeat := input
			repeat.ExpectedVersion = expected
			got, repeatErr := b.store.PutOwnMemory(b.ctx, f.owner, id, repeat)
			if repeatErr != nil || !reflect.DeepEqual(got, updated) {
				t.Fatal("identical update retry unexpectedly advanced its revision")
			}
		})
	}
	stale := input
	stale.Summary = "合成旧版本不准覆盖"
	stale.ExpectedVersion = 1
	got, err := b.store.PutOwnMemory(b.ctx, f.owner, id, stale)
	requireAgentMemoryError(t, got, err, agentmemory.ErrConflict)
	got, err = b.store.PutOwnMemory(b.ctx, f.owner, agentMemoryID(t, f), stale)
	requireAgentMemoryError(t, got, err, agentmemory.ErrNotFound)
	if agentMemoryOwnedSourceSnapshot(t, f) != sourceBefore {
		t.Fatal("Memory editing changed native UserProfile, private Profile, policy, source versions, tasks or grants")
	}
	currentPrivate, err := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.owner)
	if err != nil || !sameAgentProfileMetadata(currentPrivate.Profile, private.Profile) || !reflect.DeepEqual(currentPrivate.Fields, private.Fields) {
		t.Fatal("Memory revision was incorrectly merged with AgentProfile revision")
	}
	public, err := b.store.ReadProfile(b.ctx, b.other.ID, b.person.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(public)
	if err != nil || strings.Contains(string(encoded), "AGE004") || strings.Contains(string(encoded), "memoryKey") {
		t.Fatal("ordinary public Profile leaked new private Memory contents")
	}
	ports := agentcognitive.UnavailableCognitivePorts{}
	if view, readErr := ports.ReadMemory(b.ctx, agentcognitive.ReadRequest{}); !errors.Is(readErr, agentcognitive.ErrUnavailable) || !reflect.ValueOf(view).IsZero() {
		t.Fatal("human Memory storage enabled the unavailable cognition reader")
	}
}

func TestAgentMemoryBootstrapAndMissingMetadataNeverRecreatedIntegration(t *testing.T) {
	f := agentMemoryTestFixture(t)
	b := f.base
	initialSource := agentMemoryOwnedSourceSnapshot(t, f)
	metadata, err := b.store.GetAgentProfile(b.ctx, b.personID, b.person)
	if err != nil || metadata.ProfileVersion != 1 || metadata.AgentID != b.personID {
		t.Fatal("native Agent INSERT bootstrap did not create its original metadata version 1")
	}
	rows, err := b.store.ReadOwnMemories(b.ctx, f.owner)
	if err != nil || rows == nil || len(rows) != 0 || agentMemoryOwnedSourceSnapshot(t, f) != initialSource {
		t.Fatal("ordinary bootstrapped owner GET did not return empty without writes")
	}
	b.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, b.personID)
	sourceBefore := agentMemoryOwnedSourceSnapshot(t, f)
	rows, err = b.store.ReadOwnMemories(b.ctx, f.owner)
	if !errors.Is(err, agentmemory.ErrNotFound) || rows != nil {
		t.Fatal("missing metadata GET revived an owner read or returned an empty success")
	}
	id := agentMemoryID(t, f)
	got, err := b.store.PutOwnMemory(b.ctx, f.owner, id, agentMemoryInput("metadata.absent"))
	requireAgentMemoryError(t, got, err, agentmemory.ErrNotFound)
	got, err = b.store.DeleteOwnMemory(b.ctx, f.owner, id, 1)
	requireAgentMemoryError(t, got, err, agentmemory.ErrNotFound)
	if agentMemoryOwnedSourceSnapshot(t, f) != sourceBefore {
		t.Fatal("a Memory endpoint recreated deleted metadata or default public policy")
	}
}

func TestAgentMemorySessionAndWorkspaceBoundariesIntegration(t *testing.T) {
	f := agentMemoryTestFixture(t)
	b := f.base
	id := agentMemoryID(t, f)
	input := agentMemoryInput("boundaries.explicit")
	created := mustPutAgentMemory(t, f, id, input)
	input.ExpectedVersion = created.Version
	input.Summary = "合成跨主体不能写入"
	for _, item := range []struct {
		name   string
		access agentprofile.PrivateAccess
	}{
		{"anonymous", agentprofile.PrivateAccess{WorkspacePrincipal: b.person}},
		{"unknown_session", agentprofile.PrivateAccess{SessionDigest: [32]byte{42}, WorkspacePrincipal: b.person}},
		{"peer_claims_owner", agentprofile.PrivateAccess{SessionDigest: f.peer.SessionDigest, WorkspacePrincipal: b.person}},
		{"owner_claims_peer", agentprofile.PrivateAccess{SessionDigest: f.owner.SessionDigest, WorkspacePrincipal: b.other}},
		{"organization_session", f.org}, {"business_session", f.biz},
		{"organization_claims_person", agentprofile.PrivateAccess{SessionDigest: f.org.SessionDigest, WorkspacePrincipal: b.person}},
		{"business_claims_person", agentprofile.PrivateAccess{SessionDigest: f.biz.SessionDigest, WorkspacePrincipal: b.person}},
		{"organization_workspace", agentprofile.PrivateAccess{SessionDigest: f.owner.SessionDigest, WorkspacePrincipal: b.org}},
		{"business_workspace", agentprofile.PrivateAccess{SessionDigest: f.owner.SessionDigest, WorkspacePrincipal: b.business}},
		{"community_workspace", agentprofile.PrivateAccess{SessionDigest: f.owner.SessionDigest, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Community, ID: b.person.ID}}},
		{"zero_owner", agentprofile.PrivateAccess{SessionDigest: f.owner.SessionDigest, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: "00000000-0000-0000-0000-000000000000"}}},
	} {
		t.Run(item.name, func(t *testing.T) {
			read, err := b.store.ReadOwnMemories(b.ctx, item.access)
			if !errors.Is(err, agentmemory.ErrForbidden) || read != nil {
				t.Fatal("invalid session/workspace released any Memory records")
			}
			got, err := b.store.PutOwnMemory(b.ctx, item.access, id, input)
			requireAgentMemoryError(t, got, err, agentmemory.ErrForbidden)
			got, err = b.store.DeleteOwnMemory(b.ctx, item.access, id, 1)
			requireAgentMemoryError(t, got, err, agentmemory.ErrForbidden)
		})
	}
	peerRead, err := b.store.ReadOwnMemories(b.ctx, f.peer)
	if err != nil || len(peerRead) != 0 {
		t.Fatal("actual peer session read another owner's Memory")
	}
	// A valid peer cannot mutate the same global address: collision is a generic
	// conflict, never a payload revealing the other owner or source contents.
	peerCreate := input
	peerCreate.ExpectedVersion = 0
	got, err := b.store.PutOwnMemory(b.ctx, f.peer, id, peerCreate)
	requireAgentMemoryError(t, got, err, agentmemory.ErrConflict)
	got, err = b.store.PutOwnMemory(b.ctx, f.peer, id, input)
	requireAgentMemoryError(t, got, err, agentmemory.ErrNotFound)
	got, err = b.store.DeleteOwnMemory(b.ctx, f.peer, id, 1)
	requireAgentMemoryError(t, got, err, agentmemory.ErrNotFound)
	ownerRead, err := b.store.ReadOwnMemories(b.ctx, f.owner)
	if err != nil || len(ownerRead) != 1 || !reflect.DeepEqual(ownerRead[0], created) {
		t.Fatal("rejected role/cross-owner operation touched the owner's current Memory")
	}
}

func TestAgentMemoryCurrentSessionAndAgentIntegration(t *testing.T) {
	f := agentMemoryTestFixture(t)
	b := f.base
	id := agentMemoryID(t, f)
	input := agentMemoryInput("current.identity")
	created := mustPutAgentMemory(t, f, id, input)
	input.ExpectedVersion = 1
	input.Summary = "合成无效会话不得改稿"
	for _, item := range []struct {
		name, statement, restore string
		args                     []any
	}{
		{"revoked", `UPDATE sessions SET revoked_at=now() WHERE id=$1`, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, []any{f.ownerSession}},
		{"absolute_expiry", `UPDATE sessions SET created_at=now()-interval '2 hours',expires_at=now()-interval '1 hour',idle_expires_at=now()-interval '1 hour' WHERE id=$1`, `UPDATE sessions SET expires_at=now()+interval '2 hours',idle_expires_at=now()+interval '1 hour' WHERE id=$1`, []any{f.ownerSession}},
		{"idle_expiry", `UPDATE sessions SET created_at=now()-interval '2 hours',idle_expires_at=now()-interval '1 hour' WHERE id=$1`, `UPDATE sessions SET idle_expires_at=now()+interval '1 hour' WHERE id=$1`, []any{f.ownerSession}},
		{"dev_auth_off", `UPDATE sessions SET authentication_method='dev_phone' WHERE id=$1`, `UPDATE sessions SET authentication_method='test' WHERE id=$1`, []any{f.ownerSession}},
		{"account_suspended", `UPDATE accounts SET status='suspended' WHERE id=$1`, `UPDATE accounts SET status='active' WHERE id=$1`, []any{b.person.ID}},
		{"account_deleted", `UPDATE accounts SET status='deleted' WHERE id=$1`, `UPDATE accounts SET status='active' WHERE id=$1`, []any{b.person.ID}},
		{"agent_suspended", `UPDATE agents SET status='suspended' WHERE id=$1`, `UPDATE agents SET status='active' WHERE id=$1`, []any{b.personID}},
		{"agent_retired", `UPDATE agents SET status='retired' WHERE id=$1`, `UPDATE agents SET status='active' WHERE id=$1`, []any{b.personID}},
	} {
		t.Run(item.name, func(t *testing.T) {
			b.exec(item.statement, item.args...)
			defer b.exec(item.restore, item.args...)
			read, err := b.store.ReadOwnMemories(b.ctx, f.owner)
			if !errors.Is(err, agentmemory.ErrForbidden) || read != nil {
				t.Fatal("revoked/expired/inactive identity read Memory")
			}
			got, err := b.store.PutOwnMemory(b.ctx, f.owner, id, input)
			requireAgentMemoryError(t, got, err, agentmemory.ErrForbidden)
			got, err = b.store.DeleteOwnMemory(b.ctx, f.owner, id, 1)
			requireAgentMemoryError(t, got, err, agentmemory.ErrForbidden)
		})
	}
	rows, err := b.store.ReadOwnMemories(b.ctx, f.owner)
	if err != nil || len(rows) != 1 || !reflect.DeepEqual(rows[0], created) {
		t.Fatal("failed current identity operations changed contents/revision")
	}
	b.exec(`DELETE FROM agents WHERE id=$1`, b.personID)
	rows, err = b.store.ReadOwnMemories(b.ctx, f.owner)
	if !errors.Is(err, agentmemory.ErrForbidden) || rows != nil {
		t.Fatal("deleted native Agent revived a stale owner read")
	}
	got, err := b.store.PutOwnMemory(b.ctx, f.owner, id, input)
	requireAgentMemoryError(t, got, err, agentmemory.ErrForbidden)
	var count int
	if err = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memories WHERE owner_id=$1`, b.person.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("actual Agent parent deletion failed to remove its Memory contents")
	}
}

func TestAgentMemoryConcurrentCASIntegration(t *testing.T) {
	f := agentMemoryTestFixture(t)
	id := agentMemoryID(t, f)
	input := agentMemoryInput("concurrent.replace")
	mustPutAgentMemory(t, f, id, input)
	type outcome struct {
		record agentmemory.Record
		err    error
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	var attempts sync.WaitGroup
	for _, summary := range []string{"合成并发声明-A", "合成并发声明-B"} {
		attempts.Add(1)
		go func(summary string) {
			defer attempts.Done()
			<-start
			candidate := input
			candidate.ExpectedVersion = 1
			candidate.Summary = summary
			record, err := f.base.store.PutOwnMemory(f.base.ctx, f.owner, id, candidate)
			results <- outcome{record, err}
		}(summary)
	}
	close(start)
	attempts.Wait()
	close(results)
	var success, conflict int
	var winner agentmemory.Record
	for result := range results {
		if result.err == nil {
			success++
			winner = result.record
		} else {
			requireAgentMemoryError(t, result.record, result.err, agentmemory.ErrConflict)
			conflict++
		}
	}
	if success != 1 || conflict != 1 || winner.Version != 2 {
		t.Fatalf("real Memory CAS outcomes: success=%d conflict=%d", success, conflict)
	}
	read, err := f.base.store.ReadOwnMemories(f.base.ctx, f.owner)
	if err != nil || len(read) != 1 || !reflect.DeepEqual(read[0], winner) {
		t.Fatal("concurrent Memory writes lost the winning revision/content")
	}
}

func TestAgentMemoryDeletionScrubsAndNeverRevivesIntegration(t *testing.T) {
	f := agentMemoryTestFixture(t)
	b := f.base
	id := agentMemoryID(t, f)
	input := agentMemoryInput("delete.original")
	created := mustPutAgentMemory(t, f, id, input)
	deleted, err := b.store.DeleteOwnMemory(b.ctx, f.owner, id, 1)
	if err != nil || deleted.ID != id || deleted.Status != agentmemory.StatusDeleted || deleted.Version != 2 ||
		deleted.Summary != "" || string(deleted.StructuredValue) != "{}" || deleted.LastReinforcedAt != nil ||
		!deleted.CreatedAt.Equal(created.CreatedAt) || !deleted.ValidFrom.Equal(created.ValidFrom) || !deleted.ValidUntil.Equal(created.ValidUntil) {
		t.Fatal("owner CAS delete failed to scrub contents while retaining stable control metadata")
	}
	retry, err := b.store.DeleteOwnMemory(b.ctx, f.owner, id, 1)
	if err != nil || !reflect.DeepEqual(retry, deleted) {
		t.Fatal("identical delete retry advanced or failed its tombstone")
	}
	got, err := b.store.DeleteOwnMemory(b.ctx, f.owner, id, 2)
	requireAgentMemoryError(t, got, err, agentmemory.ErrConflict)
	for _, expected := range []int64{0, 1, 2} {
		t.Run("no_revive_"+string(rune('0'+expected)), func(t *testing.T) {
			candidate := input
			candidate.ExpectedVersion = expected
			got, err := b.store.PutOwnMemory(b.ctx, f.owner, id, candidate)
			requireAgentMemoryError(t, got, err, agentmemory.ErrConflict)
		})
	}
	rows, err := b.store.ReadOwnMemories(b.ctx, f.owner)
	if err != nil || rows == nil || len(rows) != 0 {
		t.Fatal("list released deleted contents or an address tombstone")
	}
	var status, summary, value string
	var version int64
	if err = b.pool.QueryRow(b.ctx, `SELECT status,summary,structured_value::text,version FROM agent_memories WHERE id=$1`, id).Scan(&status, &summary, &value, &version); err != nil ||
		status != "DELETED" || summary != "" || value != "{}" || version != 2 {
		t.Fatal("actual deleted row retained private payload or a wrong revision")
	}
	if _, err = b.pool.Exec(b.ctx, `DELETE FROM agent_memories WHERE id=$1`, id); err == nil {
		t.Fatal("ordinary physical deletion bypassed the control tombstone guard")
	}
	newID := agentMemoryID(t, f)
	mustPutAgentMemory(t, f, newID, input)
	rows, err = b.store.ReadOwnMemories(b.ctx, f.owner)
	if err != nil || len(rows) != 1 || rows[0].ID != newID {
		t.Fatal("new explicit declaration reused a tombstone or could not reuse its released key")
	}
}

func TestAgentMemoryExpiryAndInferredReservationIntegration(t *testing.T) {
	f := agentMemoryTestFixture(t)
	b := f.base
	explicitID, expiredID, inferredID := agentMemoryID(t, f), agentMemoryID(t, f), agentMemoryID(t, f)
	explicit := mustPutAgentMemory(t, f, explicitID, agentMemoryInput("sorting.explicit"))
	b.exec(`INSERT INTO agent_memories(id,agent_id,owner_id,memory_type,memory_key,summary,structured_value,valid_from,valid_until)
		VALUES($1,$2,$3,'CITY','expiry.logical','合成已过期直接声明','{}',clock_timestamp()-interval '2 days',clock_timestamp()-interval '1 day')`, expiredID, b.personID, b.person.ID)
	// SQL shape only: not verified provenance, accepted inference or a service
	// writer. Owner human review can see its reserved, non-active status.
	b.exec(`INSERT INTO agent_memories(id,agent_id,owner_id,memory_type,memory_key,summary,structured_value,confidence,source_type,status,visibility,valid_until)
		VALUES($1,$2,$3,'PREFERENCE','sorting.inferred','合成待审形状；不是推断接纳','{"shapeOnly":true}',0.9,'INFERRED','PENDING_REVIEW','AGENT_ONLY',clock_timestamp()+interval '1 day')`, inferredID, b.personID, b.person.ID)
	var expiryBefore string
	if err := b.pool.QueryRow(b.ctx, `SELECT to_jsonb(m)::text FROM agent_memories m WHERE id=$1`, expiredID).Scan(&expiryBefore); err != nil {
		t.Fatal(err)
	}
	rows, err := b.store.ReadOwnMemories(b.ctx, f.owner)
	if err != nil || len(rows) != 3 || rows[2].ID != inferredID || rows[2].Status != agentmemory.StatusPendingReview || rows[2].SourceType != agentmemory.SourceInferred {
		t.Fatal("EXPLICIT did not take priority over the newer INFERRED reserved shape")
	}
	foundExpired := false
	for _, row := range rows {
		if row.ID == expiredID {
			foundExpired = row.Status == agentmemory.StatusExpired && row.Version == 1
		}
	}
	var expiryAfter string
	if err = b.pool.QueryRow(b.ctx, `SELECT to_jsonb(m)::text FROM agent_memories m WHERE id=$1`, expiredID).Scan(&expiryAfter); err != nil || !foundExpired || expiryBefore != expiryAfter {
		t.Fatal("logical expiry did not project EXPIRED, or invented a stored status/revision/update")
	}
	input := agentMemoryInput("sorting.inferred")
	input.ExpectedVersion = 1
	got, err := b.store.PutOwnMemory(b.ctx, f.owner, inferredID, input)
	requireAgentMemoryError(t, got, err, agentmemory.ErrUnavailable)
	if _, err = b.pool.Exec(b.ctx, `UPDATE agent_memories SET version=2,status='ACTIVE' WHERE id=$1`, inferredID); err == nil {
		t.Fatal("raw reserved INFERRED shape could be made active")
	}
	// Logical expiry alone has not released a physical ACTIVE uniqueness slot.
	newExpiredKey := agentMemoryInput("expiry.logical")
	newExpiredKey.MemoryType = agentmemory.TypeCity
	got, err = b.store.PutOwnMemory(b.ctx, f.owner, agentMemoryID(t, f), newExpiredKey)
	requireAgentMemoryError(t, got, err, agentmemory.ErrConflict)
	newExpiredKey.ExpectedVersion = 1
	renewed := mustPutAgentMemory(t, f, expiredID, newExpiredKey)
	if renewed.Status != agentmemory.StatusActive || !renewed.ValidUntil.After(time.Now()) {
		t.Fatal("owner could not explicitly renew an expired same-ID declaration")
	}
	if discarded, err := b.store.DeleteOwnMemory(b.ctx, f.owner, inferredID, 1); err != nil || discarded.SourceType != agentmemory.SourceInferred || discarded.Status != agentmemory.StatusDeleted || discarded.Summary != "" {
		t.Fatal("owner could not discard a pending inferred shape without activating it")
	}
	rows, err = b.store.ReadOwnMemories(b.ctx, f.owner)
	if err != nil || len(rows) != 2 {
		t.Fatal("discarded inferred reservation remained in owner list")
	}
	for _, row := range rows {
		if row.ID == explicitID && !reflect.DeepEqual(row, explicit) {
			t.Fatal("inference/expiry handling touched independent explicit declaration")
		}
	}
}

func TestAgentMemoryInvalidInputHasNoEffectsIntegration(t *testing.T) {
	f := agentMemoryTestFixture(t)
	b := f.base
	id := agentMemoryID(t, f)
	input := agentMemoryInput("invalid.current")
	created := mustPutAgentMemory(t, f, id, input)
	for _, item := range []struct {
		name     string
		mutate   func(*agentmemory.PutInput)
		expected error
	}{
		{"negative_version", func(i *agentmemory.PutInput) { i.ExpectedVersion = -1 }, agentmemory.ErrInvalid},
		{"version_overflow", func(i *agentmemory.PutInput) { i.ExpectedVersion = math.MaxInt64 }, agentmemory.ErrConflict},
		{"unsupported_type", func(i *agentmemory.PutInput) { i.MemoryType = "SECRET_FACT" }, agentmemory.ErrInvalid},
		{"private_key_padding", func(i *agentmemory.PutInput) { i.MemoryKey = " invalid" }, agentmemory.ErrInvalid},
		{"empty_summary", func(i *agentmemory.PutInput) { i.Summary = " " }, agentmemory.ErrInvalid},
		{"summary_limit", func(i *agentmemory.PutInput) { i.Summary = strings.Repeat("x", agentmemory.MaxSummaryBytes+1) }, agentmemory.ErrInvalid},
		{"summary_control", func(i *agentmemory.PutInput) { i.Summary = "private\x00declaration" }, agentmemory.ErrInvalid},
		{"value_array", func(i *agentmemory.PutInput) { i.StructuredValue = json.RawMessage(`[]`) }, agentmemory.ErrInvalid},
		{"value_duplicate", func(i *agentmemory.PutInput) { i.StructuredValue = json.RawMessage(`{"private":1,"private":2}`) }, agentmemory.ErrInvalid},
		{"public_visibility", func(i *agentmemory.PutInput) { i.Visibility = "PUBLIC" }, agentmemory.ErrInvalid},
		{"expired_deadline", func(i *agentmemory.PutInput) { i.ValidUntil = time.Now().Add(-time.Hour) }, agentmemory.ErrInvalid},
		{"unbounded_retention", func(i *agentmemory.PutInput) { i.ValidUntil = time.Now().Add(agentmemory.MaxValidity + time.Hour) }, agentmemory.ErrInvalid},
	} {
		t.Run(item.name, func(t *testing.T) {
			candidate := input
			candidate.ExpectedVersion = 1
			item.mutate(&candidate)
			got, err := b.store.PutOwnMemory(b.ctx, f.owner, id, candidate)
			requireAgentMemoryError(t, got, err, item.expected)
		})
	}
	for _, badID := range []string{"", "not-a-uuid", "00000000-0000-0000-0000-000000000000", " " + id} {
		got, err := b.store.PutOwnMemory(b.ctx, f.owner, badID, input)
		requireAgentMemoryError(t, got, err, agentmemory.ErrInvalid)
	}
	for _, version := range []int64{0, -1} {
		got, err := b.store.DeleteOwnMemory(b.ctx, f.owner, id, version)
		requireAgentMemoryError(t, got, err, agentmemory.ErrInvalid)
	}
	rows, err := b.store.ReadOwnMemories(b.ctx, f.owner)
	if err != nil || len(rows) != 1 || !reflect.DeepEqual(rows[0], created) {
		t.Fatal("rejected input changed persisted Memory")
	}
}

func TestAgentMemoryWriteFailureRollsBackAndRedactsIntegration(t *testing.T) {
	f := agentMemoryTestFixture(t)
	b := f.base
	id := agentMemoryID(t, f)
	input := agentMemoryInput("failure.existing")
	before := mustPutAgentMemory(t, f, id, input)
	sourceBefore := agentMemoryOwnedSourceSnapshot(t, f)
	suffix := strings.ReplaceAll(b.personID, "-", "")
	functionName, triggerName := "memory_failure_"+suffix, "memory_fail_guard_"+suffix
	b.exec(`CREATE FUNCTION ` + functionName + `() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'owned synthetic Memory failure' USING DETAIL=NEW.summary||NEW.structured_value::text; END $$`)
	t.Cleanup(func() {
		if _, err := b.pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS `+triggerName+` ON agent_memories`); err != nil {
			t.Errorf("owned Memory fault trigger cleanup failed: %v", err)
		}
		if _, err := b.pool.Exec(context.Background(), `DROP FUNCTION `+functionName+`() `); err != nil {
			t.Errorf("owned Memory fault function cleanup failed: %v", err)
		}
	})
	b.exec(`CREATE TRIGGER ` + triggerName + ` BEFORE INSERT OR UPDATE ON agent_memories
		FOR EACH ROW WHEN (NEW.agent_id='` + b.personID + `'::uuid) EXECUTE FUNCTION ` + functionName + `() `)
	for _, operation := range []string{"create", "replace", "delete"} {
		t.Run(operation, func(t *testing.T) {
			var record agentmemory.Record
			var err error
			candidate := input
			candidate.Summary = "合成失败正文绝不进入错误"
			candidate.StructuredValue = json.RawMessage(`{"secret":"合成错误细节绝不泄漏"}`)
			switch operation {
			case "create":
				candidate.MemoryKey = "failure.new"
				record, err = b.store.PutOwnMemory(b.ctx, f.owner, agentMemoryID(t, f), candidate)
			case "replace":
				candidate.ExpectedVersion = 1
				record, err = b.store.PutOwnMemory(b.ctx, f.owner, id, candidate)
			case "delete":
				record, err = b.store.DeleteOwnMemory(b.ctx, f.owner, id, 1)
			}
			requireAgentMemoryError(t, record, err, agentmemory.ErrUnavailable)
			read, readErr := b.store.ReadOwnMemories(b.ctx, f.owner)
			if readErr != nil || len(read) != 1 || !reflect.DeepEqual(read[0], before) || agentMemoryOwnedSourceSnapshot(t, f) != sourceBefore {
				t.Fatal("failed Memory write retained contents/partial version or changed another native source")
			}
		})
	}
}

func TestAgentMemoryExactRetryAfterPostgresRoundTripIntegration(t *testing.T) {
	for _, item := range []struct {
		name   string
		mutate func(*agentmemory.PutInput)
	}{
		{"nanosecond_deadline", func(i *agentmemory.PutInput) { i.ValidUntil = i.ValidUntil.Add(123 * time.Nanosecond) }},
		{"json_exponent_number", func(i *agentmemory.PutInput) { i.StructuredValue = json.RawMessage(`{"quantity":1e2}`) }},
		{"json_negative_zero", func(i *agentmemory.PutInput) { i.StructuredValue = json.RawMessage(`{"quantity":-0}`) }},
	} {
		t.Run(item.name, func(t *testing.T) {
			f := agentMemoryTestFixture(t)
			id := agentMemoryID(t, f)
			input := agentMemoryInput("retry.roundtrip")
			item.mutate(&input)
			created := mustPutAgentMemory(t, f, id, input)
			retry, err := f.base.store.PutOwnMemory(f.base.ctx, f.owner, id, input)
			if err != nil || !reflect.DeepEqual(retry, created) {
				t.Fatalf("identical accepted create request was not idempotent after database normalization: error=%v", err)
			}
		})
	}
}

func TestAgentMemoryJsonbIdempotenceKeepsNumericPrecisionIntegration(t *testing.T) {
	for _, item := range []struct{ name, first, second string }{
		{"large_integer_neighbors", "9007199254740992", "9007199254740993"},
		{"high_precision_decimal_neighbors", "0.12345678901234567890123456", "0.12345678901234567890123457"},
	} {
		t.Run(item.name, func(t *testing.T) {
			f := agentMemoryTestFixture(t)
			id := agentMemoryID(t, f)
			input := agentMemoryInput("retry.numeric.precision")
			input.StructuredValue = json.RawMessage(`{"quantity":` + item.first + `}`)
			created := mustPutAgentMemory(t, f, id, input)
			retry, err := f.base.store.PutOwnMemory(f.base.ctx, f.owner, id, input)
			if err != nil || !reflect.DeepEqual(retry, created) {
				t.Fatal("exact high-precision declaration retry was not idempotent")
			}
			different := input
			different.StructuredValue = json.RawMessage(`{"quantity":` + item.second + `}`)
			got, err := f.base.store.PutOwnMemory(f.base.ctx, f.owner, id, different)
			requireAgentMemoryError(t, got, err, agentmemory.ErrConflict)
			different.ExpectedVersion = 1
			updated := mustPutAgentMemory(t, f, id, different)
			var matchesSecond, matchesFirst bool
			if err = f.base.pool.QueryRow(f.base.ctx, `SELECT structured_value=$2::jsonb,structured_value=$3::jsonb
				FROM agent_memories WHERE id=$1`, id, different.StructuredValue, input.StructuredValue).Scan(&matchesSecond, &matchesFirst); err != nil ||
				!matchesSecond || matchesFirst || reflect.DeepEqual(created.StructuredValue, updated.StructuredValue) {
				t.Fatal("a real numeric change was collapsed by floating-point idempotence comparison")
			}
			rows, err := f.base.store.ReadOwnMemories(f.base.ctx, f.owner)
			if err != nil || len(rows) != 1 || !reflect.DeepEqual(rows[0], updated) || updated.Version != 2 {
				t.Fatal("precise replacement did not persist as its own Memory revision")
			}
		})
	}
}

func TestAgentMemoryStoredNumericBoundsRejectBeforeWriteIntegration(t *testing.T) {
	f := agentMemoryTestFixture(t)
	b := f.base
	sourceBefore := agentMemoryOwnedSourceSnapshot(t, f)
	for _, literal := range []string{"1e63", "1e64", "1e100", "1e308", "1e-100"} {
		t.Run(literal, func(t *testing.T) {
			input := agentMemoryInput("stored.numeric.bound")
			input.StructuredValue = json.RawMessage(`{"quantity":` + literal + `}`)
			if _, err := agentmemory.NormalizePutInput(input, time.Now()); err != nil {
				t.Fatal("raw numeric lexical bound unexpectedly changed; storage has a separate jsonb bound")
			}
			var before, after string
			if err := b.pool.QueryRow(b.ctx, `SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY id),'[]')::text
				FROM agent_memories m WHERE owner_id=$1`, b.person.ID).Scan(&before); err != nil {
				t.Fatal("cannot snapshot owned Memory numeric boundary")
			}
			id := agentMemoryID(t, f)
			if literal == "1e63" {
				created := mustPutAgentMemory(t, f, id, input)
				retry, err := b.store.PutOwnMemory(b.ctx, f.owner, id, input)
				if err != nil || !reflect.DeepEqual(retry, created) {
					t.Fatal("supported edge-of-storage numeric representation lost idempotence")
				}
			} else {
				got, err := b.store.PutOwnMemory(b.ctx, f.owner, id, input)
				requireAgentMemoryError(t, got, err, agentmemory.ErrInvalid)
				if err = b.pool.QueryRow(b.ctx, `SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY id),'[]')::text
					FROM agent_memories m WHERE owner_id=$1`, b.person.ID).Scan(&after); err != nil || before != after {
					t.Fatal("stored-number rejection changed Memory payload/version or left an incomplete record")
				}
			}
			if agentMemoryOwnedSourceSnapshot(t, f) != sourceBefore {
				t.Fatal("numeric storage validation changed native Profile, privacy rules, grants or identity")
			}
		})
	}
}

func TestAgentMemorySessionExpiryDuringMetadataWaitIntegration(t *testing.T) {
	for _, operation := range []string{"read", "put", "delete"} {
		t.Run(operation, func(t *testing.T) {
			f := agentMemoryTestFixture(t)
			b := f.base
			id := agentMemoryID(t, f)
			input := agentMemoryInput("expiry.session.wait")
			before := mustPutAgentMemory(t, f, id, input)
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
				record agentmemory.Record
				rows   []agentmemory.Record
				err    error
			}
			completed := make(chan outcome, 1)
			go func() {
				result := outcome{}
				switch operation {
				case "read":
					result.rows, result.err = b.store.ReadOwnMemories(b.ctx, f.owner)
				case "put":
					candidate := input
					candidate.ExpectedVersion = 1
					candidate.Summary = "合成等待过期不得提交"
					result.record, result.err = b.store.PutOwnMemory(b.ctx, f.owner, id, candidate)
				case "delete":
					result.record, result.err = b.store.DeleteOwnMemory(b.ctx, f.owner, id, 1)
				}
				completed <- result
			}()
			blocked := false
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				if err = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_pids(pid)))`, int(blocker.Conn().PgConn().PID())).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !blocked {
				t.Fatal("Memory operation did not reach actual metadata wait before session expiry")
			}
			time.Sleep(900 * time.Millisecond)
			if err = blocker.Commit(b.ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-completed:
				requireAgentMemoryError(t, result.record, result.err, agentmemory.ErrForbidden)
				if result.rows != nil {
					t.Fatal("session expired during wait released Memory list")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("expired Memory operation failed to finish after lock release")
			}
			b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '1 hour' WHERE id=$1`, f.ownerSession)
			rows, err := b.store.ReadOwnMemories(b.ctx, f.owner)
			if err != nil || len(rows) != 1 || !reflect.DeepEqual(rows[0], before) {
				t.Fatal("session expired while waiting committed content/deletion or a partial revision")
			}
		})
	}
}

func agentMemoryOwnedRowsSnapshot(t *testing.T, f *agentPrivateFixture) string {
	t.Helper()
	var memories string
	if err := f.base.pool.QueryRow(f.base.ctx, `SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY id),'[]')::text
		FROM agent_memories m WHERE owner_id=ANY($1::uuid[])`, f.base.accounts).Scan(&memories); err != nil {
		t.Fatal("cannot snapshot complete owned Memory rows")
	}
	// Concurrent packages own different random fixtures. Their transient rows
	// must not participate in this operation's rollback assertion. The isolated
	// runner separately compares every public table's complete baseline rows.
	return agentMemoryOwnedSourceSnapshot(t, f) + "|" + memories
}

func TestAgentMemoryLateDeadlineWhileWaitingForMemoryRowIntegration(t *testing.T) {
	f := agentMemoryTestFixture(t)
	b := f.base
	id := agentMemoryID(t, f)
	input := agentMemoryInput("deadline.wait.actual-row")
	before := mustPutAgentMemory(t, f, id, input)
	ownedBefore := agentMemoryOwnedRowsSnapshot(t, f)
	blocker, err := b.pool.Begin(b.ctx)
	if err != nil {
		t.Fatal("cannot open real Memory-row lock blocker")
	}
	defer blocker.Rollback(context.Background())
	if _, err = blocker.Exec(b.ctx, `SELECT id FROM agent_memories WHERE id=$1 FOR UPDATE`, id); err != nil {
		t.Fatal("cannot obtain actual owned Memory row lock")
	}
	if err = b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()+interval '2 seconds'`).Scan(&input.ValidUntil); err != nil {
		t.Fatal("cannot derive actual PG deadline")
	}
	input.ExpectedVersion = 1
	input.Summary = "合成到期后不得提交的修改"
	type outcome struct {
		record agentmemory.Record
		err    error
	}
	completed := make(chan outcome, 1)
	go func() {
		record, writeErr := b.store.PutOwnMemory(b.ctx, f.owner, id, input)
		completed <- outcome{record, writeErr}
	}()
	blocked := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
			WHERE $1::integer=ANY(pg_blocking_pids(pid)) AND query LIKE '%FROM agent_memories%' AND query LIKE '%FOR UPDATE%')`, int(blocker.Conn().PgConn().PID())).Scan(&blocked); err != nil {
			t.Fatal("cannot inspect actual Memory row wait")
		}
		if blocked {
			break
		}
		select {
		case <-completed:
			t.Fatal("Memory write completed before reaching the real row wait")
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("regression failed to prove the precise Memory FOR UPDATE wait")
	}
	var pgNow time.Time
	for {
		if err = b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&pgNow); err != nil {
			t.Fatal("cannot inspect real PG clock during row wait")
		}
		if !pgNow.Before(input.ValidUntil) {
			break
		}
		if time.Now().After(deadline.Add(2 * time.Second)) {
			t.Fatal("actual PG clock did not cross the regression deadline")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err = blocker.Commit(b.ctx); err != nil {
		t.Fatal("cannot release proven Memory row wait")
	}
	var result outcome
	select {
	case result = <-completed:
	case <-time.After(3 * time.Second):
		t.Fatal("Memory operation failed to finish after actual deadline/lock release")
	}
	requireAgentMemoryError(t, result.record, result.err, agentmemory.ErrInvalid)
	if agentMemoryOwnedRowsSnapshot(t, f) != ownedBefore {
		t.Fatal("late deadline rejection changed complete owned Memory or native source rows")
	}
	rows, err := b.store.ReadOwnMemories(b.ctx, f.owner)
	if err != nil || len(rows) != 1 || !reflect.DeepEqual(rows[0], before) {
		t.Fatal("row wait crossing deadline lost the original stored declaration")
	}
	t.Log("ACTUAL_ROW_WAIT reached=true deadlineCrossed=true result=INVALID fullOwnedRowsUnchanged=true")
}

func TestAgentMemoryLateDeadlineDuringUpdateRollsBackAtFinalBoundaryIntegration(t *testing.T) {
	f := agentMemoryTestFixture(t)
	b := f.base
	id := agentMemoryID(t, f)
	input := agentMemoryInput("deadline.final-boundary")
	before := mustPutAgentMemory(t, f, id, input)
	ownedBefore := agentMemoryOwnedRowsSnapshot(t, f)
	var marker int64
	if err := b.pool.QueryRow(b.ctx, `SELECT hashtextextended($1,0)`, "owned Memory final boundary "+id).Scan(&marker); err != nil {
		t.Fatal("cannot allocate private transaction-stage marker")
	}
	suffix := strings.ReplaceAll(id, "-", "")
	functionName, triggerName := "memory_deadline_"+suffix, "zz_memory_deadline_"+suffix
	b.exec(`CREATE FUNCTION ` + functionName + `() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			PERFORM pg_advisory_xact_lock(` + strconv.FormatInt(marker, 10) + `::bigint);
			WHILE clock_timestamp()<NEW.valid_until LOOP
				PERFORM pg_sleep(0.005);
			END LOOP;
			RETURN NEW;
		END $$`)
	t.Cleanup(func() {
		if _, err := b.pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS `+triggerName+` ON agent_memories`); err != nil {
			t.Errorf("owned deadline trigger cleanup failed: %v", err)
		}
		if _, err := b.pool.Exec(context.Background(), `DROP FUNCTION `+functionName+`() `); err != nil {
			t.Errorf("owned deadline function cleanup failed: %v", err)
		}
	})
	b.exec(`CREATE TRIGGER ` + triggerName + ` BEFORE UPDATE ON agent_memories
		FOR EACH ROW WHEN (NEW.id='` + id + `'::uuid) EXECUTE FUNCTION ` + functionName + `() `)
	if err := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()+interval '2 seconds'`).Scan(&input.ValidUntil); err != nil {
		t.Fatal("cannot derive final-boundary PG deadline")
	}
	input.ExpectedVersion = 1
	input.Summary = "合成最终提交边界过期不得保存"
	type outcome struct {
		record agentmemory.Record
		err    error
	}
	completed := make(chan outcome, 1)
	go func() {
		record, err := b.store.PutOwnMemory(b.ctx, f.owner, id, input)
		completed <- outcome{record, err}
	}()
	entered := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND granted
			AND classid=((($1::bigint>>32)&4294967295)::oid) AND objid=(($1::bigint&4294967295)::oid)
			AND objsubid=1 AND database=(SELECT oid FROM pg_database WHERE datname=current_database()))`, marker).Scan(&entered); err != nil {
			t.Fatal("cannot observe actual owned update trigger stage")
		}
		if entered {
			break
		}
		select {
		case <-completed:
			t.Fatal("Memory operation completed before entering the actual update trigger")
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !entered {
		t.Fatal("regression did not prove entry into the actual deadline-drifting update")
	}
	var result outcome
	select {
	case result = <-completed:
	case <-time.After(4 * time.Second):
		t.Fatal("deadline-drifting update did not finish")
	}
	var pgNow time.Time
	if err := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&pgNow); err != nil || pgNow.Before(input.ValidUntil) {
		t.Fatal("test did not actually cross its native database deadline")
	}
	requireAgentMemoryError(t, result.record, result.err, agentmemory.ErrInvalid)
	if agentMemoryOwnedRowsSnapshot(t, f) != ownedBefore {
		t.Fatal("final deadline failure committed private content/revision or changed complete owned native source rows")
	}
	rows, err := b.store.ReadOwnMemories(b.ctx, f.owner)
	if err != nil || len(rows) != 1 || !reflect.DeepEqual(rows[0], before) {
		t.Fatal("final deadline failure did not roll back to the original declaration")
	}
	t.Log("ACTUAL_UPDATE_TRIGGER reached=true deadlineCrossed=true result=INVALID fullOwnedRowsUnchanged=true")
}
