package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/relationshipcontext"
	"github.com/jackc/pgx/v5/pgxpool"
)

type contextPurposeFixture struct {
	f         *contextBuilderNativeFixture
	selection acb.PurposeSelection
	memory    string
	tie       string
}

func contextPurposeNative(t *testing.T) *contextPurposeFixture {
	t.Helper()
	f := contextBuilderNative(t)
	b := f.place.private.base
	var installed bool
	if e := b.pool.QueryRow(b.ctx, `SELECT to_regclass('public.agent_context_purpose_bindings') IS NOT NULL`).Scan(&installed); e != nil || !installed {
		t.Fatal("requires native migration076", e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{
			`DELETE FROM agent_context_purpose_bindings WHERE grant_id IN(SELECT id FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]))`,
			`DELETE FROM agent_context_purpose_previews WHERE owner_id=ANY($1::uuid[])`,
			`DELETE FROM consent_grants WHERE owner_account_id=ANY($1::uuid[])`,
			`DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`,
			`DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM person_agent_relationship_consent WHERE account_id=ANY($1::uuid[])`,
		} {
			if _, e := b.pool.Exec(ctx, q, b.accounts); e != nil {
				t.Error("owned grant cleanup", e)
			}
		}
	})
	savePrivateCanaries(t, f.place.private)
	memory := agentMemoryID(t, f.place.private)
	if _, e := b.store.PutOwnMemory(b.ctx, f.place.private.owner, memory, agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "context.explicit.native", Summary: "不复制此私密正文_CONTEXT_PURPOSE_CANARY", StructuredValue: json.RawMessage(`{"declared":true}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: f.now.Add(time.Hour)}); e != nil {
		t.Fatal(e)
	}
	if _, e := b.store.PutOwnPolicy(b.ctx, f.place.private.owner, agentpolicysettings.Autonomy, policyNativeInput(agentpolicysettings.Autonomy, 0, f.now.Add(time.Hour))); e != nil {
		t.Fatal(e)
	}
	var request, tie string
	if e := b.pool.QueryRow(b.ctx, `INSERT INTO connection_requests(sender_account_id,recipient_account_id,note,scope,state,expires_at) VALUES($1,$2,'本地合成明确好友申请','friend','accepted',now()+interval '1 day') RETURNING id`, b.person.ID, b.other.ID).Scan(&request); e != nil {
		t.Fatal(e)
	}
	if e := b.pool.QueryRow(b.ctx, `INSERT INTO person_ties(person_a_account_id,person_b_account_id,request_id) VALUES(LEAST($1::uuid,$2::uuid),GREATEST($1::uuid,$2::uuid),$3) RETURNING id`, b.person.ID, b.other.ID, request).Scan(&tie); e != nil {
		t.Fatal(e)
	}
	if _, e := b.store.SetRelationshipConsent(b.ctx, b.person.ID, relationshipcontext.Consent{Enabled: true}); e != nil {
		t.Fatal(e)
	}
	r := f.request(t, acb.ActivitySearch)
	s := acb.PurposeSelection{AgentID: r.Agent.AgentID, TaskID: r.TaskID, CityID: r.CityID, TaskUpdatedAt: r.TaskUpdatedAt, CurrentQuery: r.CurrentQuery, ProfileFields: []string{"availability"}, MemoryIDs: []string{memory}, PlaceIDs: []string{f.place.place}, ActivityIDs: []string{f.public}, RelationshipTieIDs: []string{tie}, PolicyFamilies: []agentpolicysettings.Family{agentpolicysettings.Autonomy}, DeadlineAt: r.DeadlineAt}
	return &contextPurposeFixture{f: f, selection: s, memory: memory, tie: tie}
}
func (f *contextPurposeFixture) approve(t *testing.T) (acb.PurposePreview, acb.PurposeGrant) {
	t.Helper()
	b := f.f.place.private.base
	p, e := b.store.PreviewOwnContextPurpose(b.ctx, f.f.place.private.owner, f.selection)
	if e != nil {
		t.Fatal("native preview", e)
	}
	g, e := b.store.ApproveOwnContextPurpose(b.ctx, f.f.place.private.owner, p.ID)
	if e != nil {
		t.Fatal("native approval", e)
	}
	return p, g
}
func requirePurposeDenied(t *testing.T, r acb.PurposeResolution, e error) {
	t.Helper()
	if e == nil || !reflect.DeepEqual(r, acb.PurposeResolution{}) {
		t.Fatal("failed permission returned authority", e)
	}
	if strings.Contains(e.Error(), "SELECT") || strings.Contains(e.Error(), "CANARY") {
		t.Fatal("source leaked through error")
	}
}

func TestContextPurposeNativeSixSourcesAndIdempotentLifecycle(t *testing.T) {
	f := contextPurposeNative(t)
	b := f.f.place.private.base
	p, g := f.approve(t)
	if p.Review.City == nil || p.Review.Task == nil || len(p.Review.Profile) != 1 || len(p.Review.Memories) != 1 || len(p.Review.Policies) != 1 || len(p.Review.Places) != 1 || len(p.Review.Activities) != 1 || len(p.Review.Relationships) != 1 || !strings.Contains(p.Review.Memories[0].Summary, "CONTEXT_PURPOSE_CANARY") {
		t.Fatal("human did not receive the exact selected native values")
	}
	if _, ok := p.Review.Profile["agentNotes"]; ok {
		t.Fatal("unselected private field entered preview")
	}
	if len(g.Sources) != 8 || g.Revision != 1 || g.Purpose != acb.TaskContextRead {
		t.Fatal("six sources + two anchors not native", len(g.Sources))
	}
	for _, src := range g.Sources {
		if src.RowToken == "" || src.NativeTime.IsZero() {
			t.Fatal("invented source", src.Kind)
		}
	}
	var stored string
	if e := b.pool.QueryRow(b.ctx, `SELECT selection::text||sources::text FROM agent_context_purpose_previews WHERE id=$1`, p.ID).Scan(&stored); e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{f.selection.CurrentQuery, "CANARY", "currentQuery", "合成私密"} {
		if strings.Contains(stored, secret) {
			t.Fatal("second raw source snapshot", secret)
		}
	}
	if _, e := b.store.Authenticate(b.ctx, f.f.place.private.owner.SessionDigest); e != nil {
		t.Fatal(e)
	}
	again, e := b.store.ApproveOwnContextPurpose(b.ctx, f.f.place.private.owner, p.ID)
	if e != nil || again.ID != g.ID || !again.ExpiresAt.Equal(g.ExpiresAt) || !again.CreatedAt.Equal(g.CreatedAt) {
		t.Fatal("same key retry renewed/multiplied grant", e)
	}
	r, e := b.store.ResolveOwnContextPurpose(b.ctx, f.f.place.private.owner, g.ID, f.selection)
	if e != nil || len(r.Sources) != 8 || r.ExpiresAt.After(g.ExpiresAt) || r.BoundSelection.CurrentQuery != f.selection.CurrentQuery {
		t.Fatal("native machine read", e)
	}
	if _, e := b.store.ReadOwnContextPurpose(b.ctx, f.f.place.private.owner, g.ID); e != nil {
		t.Fatal(e)
	}
	rev, e := b.store.RevokeOwnContextPurpose(b.ctx, f.f.place.private.owner, g.ID, g.Revision)
	if e != nil || rev.Revision != 2 || rev.RevokedAt == nil {
		t.Fatal("native revoke", e)
	}
	retry, e := b.store.RevokeOwnContextPurpose(b.ctx, f.f.place.private.owner, g.ID, g.Revision)
	if e != nil || retry.Revision != 2 || !retry.RevokedAt.Equal(*rev.RevokedAt) {
		t.Fatal("revoke retry not original", e)
	}
	r, e = b.store.ResolveOwnContextPurpose(b.ctx, f.f.place.private.owner, g.ID, f.selection)
	requirePurposeDenied(t, r, e)
	if _, e = b.store.ApproveOwnContextPurpose(b.ctx, f.f.place.private.owner, p.ID); e == nil {
		t.Fatal("revoked preview manufactured fresh grant")
	}
	var count int
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM consent_grants WHERE owner_account_id=$1 AND purpose='TASK_CONTEXT_READ'`, b.person.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal("duplicate grant", count, e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND purpose='TASK_CONTEXT_READ'`, b.person.ID).Scan(&count); e != nil || count != 2 {
		t.Fatal("grant/revoke audit duplicated", count, e)
	}
}

func TestContextPurposeNativeRejectsDifferentIdentitySelectionAndOldGrant(t *testing.T) {
	f := contextPurposeNative(t)
	b := f.f.place.private.base
	_, g := f.approve(t)
	for name, access := range map[string]agentprofile.PrivateAccess{"peer": f.f.place.private.peer, "organization": f.f.place.private.org, "business": f.f.place.private.biz} {
		t.Run(name, func(t *testing.T) {
			r, e := b.store.ResolveOwnContextPurpose(b.ctx, access, g.ID, f.selection)
			requirePurposeDenied(t, r, e)
		})
	}
	for name, mutate := range map[string]func(*acb.PurposeSelection){"query": func(s *acb.PurposeSelection) { s.CurrentQuery = "更改后的请求"; s.QueryDigest = "" }, "field": func(s *acb.PurposeSelection) { s.ProfileFields = []string{"agentNotes"} }, "task": func(s *acb.PurposeSelection) { s.TaskID = b.other.ID }, "agent": func(s *acb.PurposeSelection) { s.AgentID = b.otherID }, "deadline": func(s *acb.PurposeSelection) { s.DeadlineAt = s.DeadlineAt.Add(time.Minute) }, "memory": func(s *acb.PurposeSelection) { s.MemoryIDs = nil }} {
		t.Run(name, func(t *testing.T) {
			s := f.selection
			mutate(&s)
			r, e := b.store.ResolveOwnContextPurpose(b.ctx, f.f.place.private.owner, g.ID, s)
			requirePurposeDenied(t, r, e)
		})
	}
	old, e := b.store.GrantProfileRead(b.ctx, b.person.ID, b.other.ID, f.f.now.Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	r, e := b.store.ResolveOwnContextPurpose(b.ctx, f.f.place.private.owner, old.ID, f.selection)
	requirePurposeDenied(t, r, e)
}

func TestContextPurposeNativeSourceChangesAndABANeverRestore(t *testing.T) {
	for _, name := range []string{"AccountABA", "AgentABA", "MetadataABA", "TaskABA", "PlaceABA", "ActivityCancel", "MemoryDelete", "ProfileEdit", "PolicyEdit", "TieABA", "RelationshipConsentOff", "Block"} {
		t.Run(name, func(t *testing.T) {
			f := contextPurposeNative(t)
			b := f.f.place.private.base
			_, g := f.approve(t)
			switch name {
			case "AccountABA":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
				b.exec(`UPDATE accounts SET status='active' WHERE id=$1`, b.person.ID)
			case "AgentABA":
				b.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, f.selection.AgentID)
				b.exec(`UPDATE agents SET status='active' WHERE id=$1`, f.selection.AgentID)
			case "MetadataABA":
				b.exec(`UPDATE agent_profiles SET profile_version=profile_version+1,updated_at=clock_timestamp() WHERE agent_id=$1`, f.selection.AgentID)
				b.exec(`UPDATE agent_profiles SET profile_version=profile_version+1,updated_at=clock_timestamp() WHERE agent_id=$1`, f.selection.AgentID)
			case "TaskABA":
				b.exec(`UPDATE agent_tasks SET query=query WHERE id=$1`, f.selection.TaskID)
			case "PlaceABA":
				b.exec(`UPDATE places SET name=name WHERE id=$1`, f.f.place.place)
			case "ActivityCancel":
				b.exec(`UPDATE activities SET cancelled_at=clock_timestamp() WHERE id=$1`, f.f.public)
			case "MemoryDelete":
				var version int64
				e := b.pool.QueryRow(b.ctx, `SELECT version FROM agent_memories WHERE id=$1`, f.memory).Scan(&version)
				if e != nil {
					t.Fatal(e)
				}
				if _, e = b.store.DeleteOwnMemory(b.ctx, f.f.place.private.owner, f.memory, version); e != nil {
					t.Fatal(e)
				}
			case "ProfileEdit":
				savePrivateCanaries(t, f.f.place.private)
			case "PolicyEdit":
				pol, e := b.store.GetOwnPolicies(b.ctx, f.f.place.private.owner)
				if e != nil {
					t.Fatal(e)
				}
				if _, e = b.store.PutOwnPolicy(b.ctx, f.f.place.private.owner, agentpolicysettings.Autonomy, policyNativeInput(agentpolicysettings.Autonomy, pol.Autonomy.NativeRevision, f.f.now.Add(time.Hour))); e != nil {
					t.Fatal(e)
				}
			case "TieABA":
				b.exec(`UPDATE person_ties SET status='removed' WHERE id=$1`, f.tie)
				b.exec(`UPDATE person_ties SET status='active' WHERE id=$1`, f.tie)
			case "RelationshipConsentOff":
				if _, e := b.store.SetRelationshipConsent(b.ctx, b.person.ID, relationshipcontext.Consent{Enabled: false}); e != nil {
					t.Fatal(e)
				}
			case "Block":
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, b.person.ID, b.other.ID)
			}
			r, e := b.store.ResolveOwnContextPurpose(b.ctx, f.f.place.private.owner, g.ID, f.selection)
			requirePurposeDenied(t, r, e)
		})
	}
}

func TestContextPurposeNativeGrantAndPreviewImmutability(t *testing.T) {
	f := contextPurposeNative(t)
	b := f.f.place.private.base
	p, g := f.approve(t)
	for name, q := range map[string]string{"renew": `UPDATE consent_grants SET expires_at=expires_at+interval '1 minute' WHERE id=$1`, "recipient": `UPDATE consent_grants SET recipient_account_id=NULL WHERE id=$1`, "actions": `UPDATE consent_grants SET actions=ARRAY['read','write'] WHERE id=$1`, "purpose": `UPDATE consent_grants SET purpose='MODEL_CONTEXT_EGRESS' WHERE id=$1`, "revision": `UPDATE consent_grants SET revision=revision+1 WHERE id=$1`, "created": `UPDATE consent_grants SET created_at=created_at-interval '1 second' WHERE id=$1`} {
		t.Run(name, func(t *testing.T) {
			if _, e := b.pool.Exec(b.ctx, q, g.ID); e == nil {
				t.Fatal("grant guard missing")
			}
		})
	}
	if _, e := b.pool.Exec(b.ctx, `UPDATE agent_context_purpose_previews SET selection=selection WHERE id=$1`, p.ID); e == nil {
		t.Fatal("preview mutable")
	}
	if _, e := b.pool.Exec(b.ctx, `UPDATE agent_context_purpose_bindings SET preview_id=preview_id WHERE grant_id=$1`, g.ID); e == nil {
		t.Fatal("binding mutable")
	}
	if _, e := b.store.RevokeOwnContextPurpose(b.ctx, f.f.place.private.owner, g.ID, 1); e != nil {
		t.Fatal(e)
	}
	if _, e := b.pool.Exec(b.ctx, `UPDATE consent_grants SET revoked_at=NULL,revision=revision+1 WHERE id=$1`, g.ID); e == nil {
		t.Fatal("revocation ABA restored authorization")
	}
	r, e := b.store.ResolveOwnContextPurpose(b.ctx, f.f.place.private.owner, g.ID, f.selection)
	requirePurposeDenied(t, r, e)
}

func TestContextPurposeNativePreviewMustStillMatchSourcesAtApproval(t *testing.T) {
	for _, name := range []string{"SourceChanged", "Expired", "MissingConfiguredPolicy", "DisabledRelationship"} {
		t.Run(name, func(t *testing.T) {
			f := contextPurposeNative(t)
			b := f.f.place.private.base
			if name == "MissingConfiguredPolicy" {
				b.exec(`DELETE FROM agent_policy_settings WHERE agent_id=$1`, f.selection.AgentID)
			}
			if name == "DisabledRelationship" {
				if _, e := b.store.SetRelationshipConsent(b.ctx, b.person.ID, relationshipcontext.Consent{Enabled: false}); e != nil {
					t.Fatal(e)
				}
			}
			if name == "MissingConfiguredPolicy" || name == "DisabledRelationship" {
				if _, e := b.store.PreviewOwnContextPurpose(b.ctx, f.f.place.private.owner, f.selection); e == nil {
					t.Fatal("unconfigured source made real purpose")
				}
				return
			}
			if name == "Expired" {
				var now time.Time
				b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&now)
				f.selection.DeadlineAt = now.Add(200 * time.Millisecond).Truncate(time.Microsecond)
			}
			p, e := b.store.PreviewOwnContextPurpose(b.ctx, f.f.place.private.owner, f.selection)
			if e != nil {
				t.Fatal(e)
			}
			if name == "SourceChanged" {
				b.exec(`UPDATE places SET name=name WHERE id=$1`, f.f.place.place)
			} else {
				time.Sleep(250 * time.Millisecond)
			}
			if g, e := b.store.ApproveOwnContextPurpose(b.ctx, f.f.place.private.owner, p.ID); e == nil || g.ID != "" {
				t.Fatal("stale preview created grant", e)
			}
			var count int
			if e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM consent_grants WHERE owner_account_id=$1 AND purpose='TASK_CONTEXT_READ'`, b.person.ID).Scan(&count); e != nil || count != 0 {
				t.Fatal("denied approval side effect", e)
			}
		})
	}
}

func TestContextPurposeNativeRealSourceWaitFinalACLAndExpiry(t *testing.T) {
	for _, name := range []string{"PhantomBlock", "NaturalExpiry", "Cancelled"} {
		t.Run(name, func(t *testing.T) {
			f := contextPurposeNative(t)
			b := f.f.place.private.base
			if name == "NaturalExpiry" {
				var now time.Time
				b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&now)
				f.selection.DeadlineAt = now.Add(800 * time.Millisecond).Truncate(time.Microsecond)
			}
			_, g := f.approve(t)
			lock, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer lock.Rollback(context.Background())
			if _, e = lock.Exec(b.ctx, `SELECT id FROM person_ties WHERE id=$1 FOR UPDATE`, f.tie); e != nil {
				t.Fatal(e)
			}
			cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
			if e != nil {
				t.Fatal(e)
			}
			cfg.ConnConfig.RuntimeParams["application_name"] = "purpose033-wait-" + b.person.ID
			pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			store := New(pool, false)
			ctx, cancel := context.WithCancel(b.ctx)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				r, e := store.ResolveOwnContextPurpose(ctx, f.f.place.private.owner, g.ID, f.selection)
				if e == nil || !reflect.DeepEqual(r, acb.PurposeResolution{}) {
					done <- fmt.Errorf("returned stale authority: %v", e)
					return
				}
				done <- nil
			}()
			waited := false
			until := time.Now().Add(3 * time.Second)
			for time.Now().Before(until) {
				if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock')`, cfg.ConnConfig.RuntimeParams["application_name"]).Scan(&waited); e != nil {
					t.Fatal(e)
				}
				if waited {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if !waited {
				t.Fatal("actual native Tie lock wait missing")
			}
			if name == "PhantomBlock" {
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, b.person.ID, b.other.ID)
			} else if name == "NaturalExpiry" {
				time.Sleep(900 * time.Millisecond)
			} else {
				cancel()
			}
			if e = lock.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e := <-done:
				if e != nil {
					t.Fatal(e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("native wait did not finish")
			}
		})
	}
}

func TestContextPurposeNativeSessionIdentityAndAbsoluteExpiryBinding(t *testing.T) {
	for _, name := range []string{"absolute_expiry_changed", "method_changed", "creation_changed", "revoked", "idle_naturally_expired"} {
		t.Run(name, func(t *testing.T) {
			f := contextPurposeNative(t)
			b := f.f.place.private.base
			_, g := f.approve(t)
			switch name {
			case "absolute_expiry_changed":
				b.exec(`UPDATE sessions SET expires_at=expires_at+interval '1 minute' WHERE id=$1`, f.f.place.private.ownerSession)
			case "method_changed":
				b.exec(`UPDATE sessions SET authentication_method='changed' WHERE id=$1`, f.f.place.private.ownerSession)
			case "creation_changed":
				b.exec(`UPDATE sessions SET created_at=created_at-interval '1 second' WHERE id=$1`, f.f.place.private.ownerSession)
			case "revoked":
				if e := b.store.RevokeSession(b.ctx, f.f.place.private.owner.SessionDigest); e != nil {
					t.Fatal(e)
				}
			case "idle_naturally_expired":
				b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '100 milliseconds' WHERE id=$1`, f.f.place.private.ownerSession)
				time.Sleep(120 * time.Millisecond)
			}
			r, e := b.store.ResolveOwnContextPurpose(b.ctx, f.f.place.private.owner, g.ID, f.selection)
			requirePurposeDenied(t, r, e)
		})
	}
}

func TestContextPurposeNativeDownRefusesApprovalHistoryAtomically(t *testing.T) {
	f := contextPurposeNative(t)
	b := f.f.place.private.base
	_, g := f.approve(t)
	down, e := os.ReadFile("../../migrations/076_agent_context_purpose_bindings.down.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, revoked := range []bool{false, true} {
		if revoked {
			if _, e = b.store.RevokeOwnContextPurpose(b.ctx, f.f.place.private.owner, g.ID, g.Revision); e != nil {
				t.Fatal(e)
			}
		}
		conn, e := b.pool.Acquire(b.ctx)
		if e != nil {
			t.Fatal(e)
		}
		_, err := conn.Exec(b.ctx, string(down))
		if err == nil {
			conn.Release()
			t.Fatal("downgrade erased approval history")
		}
		if _, e = conn.Exec(b.ctx, "ROLLBACK"); e != nil {
			conn.Release()
			t.Fatal(e)
		}
		var remains bool
		if e = conn.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM agent_context_purpose_bindings WHERE grant_id=$1)`, g.ID).Scan(&remains); e != nil || !remains {
			conn.Release()
			t.Fatal("failed down was not atomic", e)
		}
		conn.Release()
	}
}

func TestContextPurposeNativePreviewBoundedNoHiddenAutoApproval(t *testing.T) {
	f := contextPurposeNative(t)
	b := f.f.place.private.base
	p, e := b.store.PreviewOwnContextPurpose(b.ctx, f.f.place.private.owner, f.selection)
	if e != nil {
		t.Fatal(e)
	}
	// Explicit synthetic pending rows exercise the native count gate; they are
	// not 128 real user approvals, and none creates a consent_grant.
	b.exec(`INSERT INTO agent_context_purpose_previews(id,owner_id,agent_id,session_id,task_id,selection,sources,authority,observed_at,expires_at)
 SELECT gen_random_uuid(),owner_id,agent_id,session_id,task_id,selection,sources,authority,observed_at,expires_at FROM agent_context_purpose_previews CROSS JOIN generate_series(1,127) WHERE id=$1`, p.ID)
	if out, e := b.store.PreviewOwnContextPurpose(b.ctx, f.f.place.private.owner, f.selection); e == nil || out.ID != "" {
		t.Fatal("unbounded pending previews")
	}
	var count int
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM consent_grants WHERE owner_account_id=$1 AND purpose='TASK_CONTEXT_READ'`, b.person.ID).Scan(&count); e != nil || count != 0 {
		t.Fatal("pending previews became permission", e)
	}
}

func TestContextPurposeNativeApprovedDurationCannotExceedConcretePreview(t *testing.T) {
	for _, name := range []string{"chosen_ten_minutes_but_preview_five", "short_original_idle_then_authenticate_extends"} {
		t.Run(name, func(t *testing.T) {
			f := contextPurposeNative(t)
			b := f.f.place.private.base
			if name == "short_original_idle_then_authenticate_extends" {
				b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '2 minutes' WHERE id=$1`, f.f.place.private.ownerSession)
				var now time.Time
				if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
					t.Fatal(e)
				}
				f.selection.DeadlineAt = now.Add(4 * time.Minute).Truncate(time.Microsecond)
			}
			preview, e := b.store.PreviewOwnContextPurpose(b.ctx, f.f.place.private.owner, f.selection)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = b.store.Authenticate(b.ctx, f.f.place.private.owner.SessionDigest); e != nil {
				t.Fatal(e)
			}
			grant, e := b.store.ApproveOwnContextPurpose(b.ctx, f.f.place.private.owner, preview.ID)
			if e != nil {
				t.Fatal(e)
			}
			if grant.ExpiresAt.After(preview.ExpiresAt) {
				t.Fatal("actual grant extends the concrete preview's displayed original limit")
			}
			resolution, e := b.store.ResolveOwnContextPurpose(b.ctx, f.f.place.private.owner, grant.ID, f.selection)
			if e != nil || resolution.ExpiresAt.After(preview.ExpiresAt) {
				t.Fatal("machine lease renews concrete preview limit", e)
			}
			if _, e = b.store.Authenticate(b.ctx, f.f.place.private.owner.SessionDigest); e != nil {
				t.Fatal(e)
			}
			retry, e := b.store.ApproveOwnContextPurpose(b.ctx, f.f.place.private.owner, preview.ID)
			if e != nil || !retry.ExpiresAt.Equal(grant.ExpiresAt) {
				t.Fatal("same preview retry renewed grant", e)
			}
		})
	}
}
