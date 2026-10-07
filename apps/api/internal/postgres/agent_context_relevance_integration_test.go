package postgres

import (
	"context"
	"encoding/json"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	acr "github.com/birdtie/birdtie/apps/api/internal/agentcontextrelevance"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"reflect"
	"strings"
	"testing"
	"time"
)

func relevanceNative(t *testing.T) (*contextPurposeFixture, acb.Request, string) {
	t.Helper()
	f := contextPurposeNative(t)
	b := f.f.place.private.base
	current, e := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.f.place.private.owner)
	if e != nil {
		t.Fatal(e)
	}
	fields := current.Fields
	fields.Availability = "周末羽毛球"
	fields.AgentNotes = "意大利面_SELECTED_IRRELEVANT"
	if _, e = b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.f.place.private.owner, agentprofile.ReplacePrivateInput{ExpectedVersion: current.Profile.ProfileVersion, Fields: fields}); e != nil {
		t.Fatal(e)
	}
	id := agentMemoryID(t, f.f.place.private)
	if _, e = b.store.PutOwnMemory(b.ctx, f.f.place.private.owner, id, agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "relevance.selected", Summary: "本人明确记录的羽毛球偏好_SELECTED_RELEVANT", StructuredValue: json.RawMessage(`{"opaque":"PRIVATE_STRUCTURED_NOT_OUTPUT"}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: f.f.now.Add(time.Hour)}); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		hidden := agentMemoryID(t, f.f.place.private)
		if _, e = b.store.PutOwnMemory(b.ctx, f.f.place.private.owner, hidden, agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "relevance.unselected." + hidden, Summary: "羽毛球_UNSELECTED_HISTORY_CANARY", StructuredValue: json.RawMessage(`{}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: f.f.now.Add(time.Hour)}); e != nil {
			t.Fatal(e)
		}
	}
	f.selection.ProfileFields = []string{"availability", "agentNotes"}
	f.selection.MemoryIDs = []string{f.memory, id}
	_, g := f.approve(t)
	r := f.f.request(t, acb.ActivitySearch)
	r.Mode = acb.MachineTaskContext
	r.Selection = acb.ExactTaskContext
	r.ProfileFields = f.selection.ProfileFields
	r.MemoryIDs = f.selection.MemoryIDs
	r.PlaceIDs = f.selection.PlaceIDs
	r.ActivityIDs = f.selection.ActivityIDs
	r.RelationshipTieIDs = f.selection.RelationshipTieIDs
	r.PolicyFamilies = f.selection.PolicyFamilies
	r.PurposeGrantID = g.ID
	r.PurposeDeadlineAt = f.selection.DeadlineAt
	return f, r, id
}
func TestRelevanceNativeApprovedSubsetReadOnlyAndExactSourceVersions(t *testing.T) {
	f, r, id := relevanceNative(t)
	b := f.f.place.private.base
	before := policyNativeSnapshot(t, f.f.place.private, true)
	svc, e := acr.NewService(b.store)
	if e != nil {
		t.Fatal(e)
	}
	out, e := svc.Retrieve(b.ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	v := out.View()
	if len(v.Context.Profile) != 1 || len(v.Context.Memories) != 1 || v.Context.Memories[0].ID != id || len(v.Context.Relationships) != 0 || len(v.Context.Policies) != 1 || len(v.Context.Activities) != 1 {
		t.Fatal("native selected relevance", v)
	}
	raw, _ := json.Marshal(v)
	for _, secret := range []string{"SELECTED_IRRELEVANT", "CONTEXT_PURPOSE_CANARY", "UNSELECTED_HISTORY_CANARY", "rowToken"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("excluded/historical body leaked", secret)
		}
	}
	grant, e := b.store.ReadOwnContextPurpose(b.ctx, r.Access, r.PurposeGrantID)
	if e != nil {
		t.Fatal(e)
	}
	for _, src := range v.Context.Sources {
		found := false
		for _, native := range grant.Sources {
			native.RowToken = ""
			if reflect.DeepEqual(src, native) {
				found = true
			}
		}
		if !found || src.NativeTime.IsZero() {
			t.Fatal("not exact native source version", src)
		}
	}
	if e = svc.Revalidate(b.ctx, r.Access, out); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, policyNativeSnapshot(t, f.f.place.private, true)) {
		t.Fatal("retrieval wrote native Profile/Memory/Policy")
	}
	copy := out.ProjectionBundle()
	copy.Profile["availability"] = json.RawMessage(`"changed"`)
	if strings.Contains(string(out.ProjectionBundle().Profile["availability"]), "changed") {
		t.Fatal("consumer modified original control")
	}
	for name, change := range map[string]func(*acb.Request){"subset": func(r *acb.Request) { r.MemoryIDs = []string{id} }, "query": func(r *acb.Request) { r.CurrentQuery = "另一轮查询" }, "human": func(r *acb.Request) { r.Mode = acb.HumanSelfReview }, "differentGrant": func(r *acb.Request) { r.PurposeGrantID = f.memory }, "otherAccount": func(r *acb.Request) { r.Access = f.f.place.private.peer }} {
		t.Run(name, func(t *testing.T) {
			candidate := r
			change(&candidate)
			if _, e := svc.Retrieve(b.ctx, candidate); e == nil {
				t.Fatal("relevance inherited different permission")
			}
		})
	}
}
func TestRelevanceNativeFilteredOutSourceAndLifecycleStillInvalidate(t *testing.T) {
	for _, change := range []string{"filteredMemoryABA", "filteredProfileEdit", "filteredTieABA", "grantRevoke", "accountABA"} {
		t.Run(change, func(t *testing.T) {
			f, r, _ := relevanceNative(t)
			b := f.f.place.private.base
			svc, _ := acr.NewService(b.store)
			out, e := svc.Retrieve(b.ctx, r)
			if e != nil {
				t.Fatal(e)
			}
			switch change {
			case "filteredMemoryABA":
				var version int64
				if e = b.pool.QueryRow(b.ctx, `SELECT version FROM agent_memories WHERE id=$1`, f.memory).Scan(&version); e != nil {
					t.Fatal(e)
				}
				for _, summary := range []string{"临时更改后的非相关内容", "不复制此私密正文_CONTEXT_PURPOSE_CANARY"} {
					saved, err := b.store.PutOwnMemory(b.ctx, r.Access, f.memory, agentmemory.PutInput{ExpectedVersion: version, MemoryType: agentmemory.TypePreference, MemoryKey: "context.explicit.native", Summary: summary, StructuredValue: json.RawMessage(`{"declared":true}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: f.f.now.Add(time.Hour)})
					if err != nil || saved.Version != version+1 {
						t.Fatal("actual native ABA", err)
					}
					version = saved.Version
				}
			case "filteredProfileEdit":
				savePrivateCanaries(t, f.f.place.private)
			case "filteredTieABA":
				b.exec(`UPDATE person_ties SET status='removed' WHERE id=$1`, f.tie)
				b.exec(`UPDATE person_ties SET status='active' WHERE id=$1`, f.tie)
			case "grantRevoke":
				if _, e = b.store.RevokeOwnContextPurpose(b.ctx, r.Access, r.PurposeGrantID, 1); e != nil {
					t.Fatal(e)
				}
			case "accountABA":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
				b.exec(`UPDATE accounts SET status='active' WHERE id=$1`, b.person.ID)
			}
			if e = svc.Revalidate(b.ctx, r.Access, out); e == nil {
				t.Fatal("filtered-out/current authority change ignored")
			}
			if _, e = svc.Retrieve(b.ctx, r); e == nil {
				t.Fatal("old approval silently narrowed or renewed")
			}
		})
	}
}
func TestRelevanceNativeFinalRealWaitRejectsSessionExpiry(t *testing.T) {
	f, r, _ := relevanceNative(t)
	b := f.f.place.private.base
	svc, _ := acr.NewService(b.store)
	out, e := svc.Retrieve(b.ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	lock, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback(context.Background())
	if _, e = lock.Exec(b.ctx, `SELECT id FROM accounts WHERE id=$1 FOR NO KEY UPDATE`, b.person.ID); e != nil {
		t.Fatal(e)
	}
	b.exec(`WITH expiry AS MATERIALIZED(SELECT clock_timestamp()+interval '400ms' AS at) UPDATE sessions SET expires_at=expiry.at,idle_expires_at=expiry.at FROM expiry WHERE token_sha256=$1`, r.Access.SessionDigest[:])
	result := make(chan error, 1)
	go func() { result <- svc.Revalidate(b.ctx, r.Access, out) }()
	placeMemoryWaitForBlock(t, f.f.place, lock.Conn().PgConn().PID())
	time.Sleep(550 * time.Millisecond)
	if e = lock.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	if e = <-result; e == nil {
		t.Fatal("final real-wait expiry exposed old projection")
	}
}
