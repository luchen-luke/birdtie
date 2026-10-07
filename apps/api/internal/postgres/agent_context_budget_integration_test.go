package postgres

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	aca "github.com/birdtie/birdtie/apps/api/internal/agentcontextadapter"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	acr "github.com/birdtie/birdtie/apps/api/internal/agentcontextrelevance"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
)

func TestContextBudgetNativePreservesRelevanceBeforeIDForLimitAndThreshold(t *testing.T) {
	f := contextPurposeNative(t)
	b := f.f.place.private.base
	ids := []string{agentMemoryID(t, f.f.place.private), agentMemoryID(t, f.f.place.private)}
	sort.Strings(ids)
	// Create higher-relevance first and lower-relevance last using native human
	// writes, so explicit recency has an actual independent ordering to consume.
	for _, i := range []int{1, 0} {
		id := ids[i]
		summary := "羽毛球"
		if i == 1 {
			summary = f.selection.CurrentQuery
		}
		if _, e := b.store.PutOwnMemory(b.ctx, f.f.place.private.owner, id, agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "budget.relevance." + id, Summary: summary, StructuredValue: json.RawMessage(`{}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: f.f.now.Add(time.Hour)}); e != nil {
			t.Fatal(e)
		}
	}
	f.selection.MemoryIDs = ids
	f.selection.ProfileFields = nil
	_, g := f.approve(t)
	r := f.f.request(t, acb.ActivitySearch)
	r.Mode = acb.MachineTaskContext
	r.Selection = acb.ExactTaskContext
	r.MemoryIDs = ids
	r.PlaceIDs = f.selection.PlaceIDs
	r.ActivityIDs = f.selection.ActivityIDs
	r.RelationshipTieIDs = f.selection.RelationshipTieIDs
	r.PolicyFamilies = f.selection.PolicyFamilies
	r.PurposeGrantID = g.ID
	r.PurposeDeadlineAt = f.selection.DeadlineAt
	svc, _ := acr.NewService(b.store)
	out, e := svc.Retrieve(b.ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	view := out.View()
	scores := map[string]int{}
	for _, m := range view.Matches {
		if m.Kind == "PURPOSE_EXPLICIT_MEMORY" {
			scores[m.SourceID] = m.Score
		}
	}
	if len(view.Context.Memories) != 2 || view.Context.Memories[0].ID != ids[1] || scores[ids[1]] <= scores[ids[0]] {
		t.Fatal("native two unequal relevance scores and inverse IDs absent", scores)
	}
	one, threshold := 1, 1.0
	for name, budget := range map[string]aca.Budget{"limitOnly": {MaxEncodedBytes: 32768, ItemLimit: &one}, "thresholdOnly": {MaxEncodedBytes: 32768, ConfidenceThreshold: &threshold}} {
		t.Run(name, func(t *testing.T) {
			v, e := aca.ProjectRelated(out.ProjectionBundle(), budget, out.ExcludedCounts())
			if e != nil {
				t.Fatal(e)
			}
			found := false
			for _, fact := range v.Facts {
				if fact.Kind == "EXPLICIT_MEMORY" {
					found = true
					if fact.SourceID != ids[1] {
						t.Fatal("lower relevance ID displaced original higher-ranked source", scores)
					}
					break
				}
			}
			if !found {
				t.Fatal("native higher-relevance memory omitted unexpectedly")
			}
		})
	}
	times := map[string]time.Time{}
	for _, source := range view.Context.Sources {
		if source.Kind == "PURPOSE_EXPLICIT_MEMORY" {
			times[source.ID] = source.NativeTime
		}
	}
	if !times[ids[0]].After(times[ids[1]]) {
		t.Fatal("actual source update order absent", times)
	}
	weighted, e := aca.ProjectRelated(out.ProjectionBundle(), aca.Budget{MaxEncodedBytes: 32768, ItemLimit: &one, RecencyWeight: 1}, out.ExcludedCounts())
	if e != nil || len(weighted.Facts) != 2 || weighted.Facts[1].SourceID != ids[0] {
		t.Fatal("explicit actual native recency did not favor newer selected row", e, weighted)
	}
	if e = svc.Revalidate(b.ctx, r.Access, out); e != nil {
		t.Fatal(e)
	}
	t.Logf("LOCAL_SYNTHETIC native high score=%d > low=%d; high ID sorts later; original AGE034 rank must survive limit/threshold only", scores[ids[1]], scores[ids[0]])
}

func TestContextBudgetNativeActualConfidenceControlsAndReadOnly(t *testing.T) {
	f, r, id := relevanceNative(t)
	b := f.f.place.private.base
	var schema88 bool
	if e := b.pool.QueryRow(b.ctx, `SELECT to_regclass('public.model_request_runs') IS NOT NULL`).Scan(&schema88); e != nil || !schema88 {
		t.Fatal("requires actual schema088", e)
	}
	before := policyNativeSnapshot(t, f.f.place.private, true)
	svc, e := acr.NewService(b.store)
	if e != nil {
		t.Fatal(e)
	}
	out, e := svc.Retrieve(b.ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	bundle := out.ProjectionBundle()
	if len(bundle.Memories) != 1 || bundle.Memories[0].ID != id {
		t.Fatal("actual relevance source")
	}
	var actual float64
	if e = b.pool.QueryRow(b.ctx, `SELECT confidence FROM agent_memories WHERE id=$1`, id).Scan(&actual); e != nil {
		t.Fatal(e)
	}
	a := bundle.Memories[0].Confidence
	if a == nil || a.Value == nil || a.Semantics != agentconfidence.DirectDeclaration || *a.Value != actual {
		t.Fatal("native confidence not copied exactly", a, actual)
	}
	one, threshold := 1, 1.0
	budget := aca.Budget{MaxEncodedBytes: 32768, ItemLimit: &one, Priority: []string{"memories", "activities", "profile", "places", "relationships"}, ConfidenceThreshold: &threshold, RecencyWeight: 0.1}
	v, e := aca.ProjectRelated(bundle, budget, out.ExcludedCounts())
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(v)
	if len(v.Facts) != 2 || v.Facts[1].SourceID != id || v.Facts[1].Confidence == nil || v.Budget.Used != len(raw) || v.Budget.Used > v.Budget.Limit || v.ModelAccess != "UNAVAILABLE" || v.MemoryPromotionAllowed {
		t.Fatal("native controls", v)
	}
	if v.Budget.OmissionReasons["profile"][aca.OmittedLimit] != 1 || v.RelevanceExcluded["profile"] != 1 {
		t.Fatal("limit/relevance conflated", v.Budget, v.RelevanceExcluded)
	}
	for _, canary := range []string{"SELECTED_IRRELEVANT", "UNSELECTED_HISTORY_CANARY", "PRIVATE_STRUCTURED_NOT_OUTPUT", "rowToken", "authority"} {
		if strings.Contains(string(raw), canary) {
			t.Fatal("native omitted data leaked", canary)
		}
	}
	if e = svc.Revalidate(b.ctx, r.Access, out); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, policyNativeSnapshot(t, f.f.place.private, true)) {
		t.Fatal("projection changed native domain rows")
	}
	// Mutating or removing supplied confidence changes the original Bundle seal.
	builder, _ := acb.NewService(b.store)
	built, e := builder.Build(b.ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	built.Bundle.Memories[0].Confidence = nil
	if _, e = builder.RevalidateOwn(b.ctx, r.Access, built); e == nil {
		t.Fatal("removed native confidence escaped complete seal")
	}
	t.Log("LOCAL_SYNTHETIC actual schema088; original direct confidence equals row; four bounded controls; no new Memory/Model authorization")
}

func TestContextBudgetNativeOmittedSourcesAndAuthorityStillInvalidate(t *testing.T) {
	for _, change := range []string{"omittedMemoryABA", "omittedProfileEdit", "omittedTieABA", "grantRevoke", "accountABA", "crossAccount"} {
		t.Run(change, func(t *testing.T) {
			f, r, id := relevanceNative(t)
			b := f.f.place.private.base
			svc, _ := acr.NewService(b.store)
			out, e := svc.Retrieve(b.ctx, r)
			if e != nil {
				t.Fatal(e)
			}
			zero := 0
			v, e := aca.ProjectRelated(out.ProjectionBundle(), aca.Budget{MaxEncodedBytes: 32768, ItemLimit: &zero}, out.ExcludedCounts())
			if e != nil {
				t.Fatal(e)
			}
			if len(v.Facts) != 1 || len(v.Sources) != 3 || v.Sections["memories"] != aca.OmittedLimit {
				t.Fatal("optional omission removed required policy/task/city")
			}
			switch change {
			case "omittedMemoryABA":
				var version int64
				if e = b.pool.QueryRow(b.ctx, `SELECT version FROM agent_memories WHERE id=$1`, id).Scan(&version); e != nil {
					t.Fatal(e)
				}
				for _, summary := range []string{"本人临时更正羽毛球内容", "本人明确记录的羽毛球偏好_SELECTED_RELEVANT"} {
					saved, err := b.store.PutOwnMemory(b.ctx, r.Access, id, agentmemory.PutInput{ExpectedVersion: version, MemoryType: agentmemory.TypePreference, MemoryKey: "relevance.selected", Summary: summary, StructuredValue: json.RawMessage(`{"opaque":"PRIVATE_STRUCTURED_NOT_OUTPUT"}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: f.f.now.Add(time.Hour)})
					if err != nil || saved.Version != version+1 {
						t.Fatal("actual source ABA", err)
					}
					version = saved.Version
				}
			case "omittedProfileEdit":
				savePrivateCanaries(t, f.f.place.private)
			case "omittedTieABA":
				b.exec(`UPDATE person_ties SET status='removed' WHERE id=$1`, f.tie)
				b.exec(`UPDATE person_ties SET status='active' WHERE id=$1`, f.tie)
			case "grantRevoke":
				if _, e = b.store.RevokeOwnContextPurpose(b.ctx, r.Access, r.PurposeGrantID, 1); e != nil {
					t.Fatal(e)
				}
			case "accountABA":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
				b.exec(`UPDATE accounts SET status='active' WHERE id=$1`, b.person.ID)
			case "crossAccount":
				r.Access = f.f.place.private.peer
			}
			if e = svc.Revalidate(b.ctx, r.Access, out); e == nil {
				t.Fatal("omission bypassed original authority/source")
			}
			if _, e = svc.Retrieve(b.ctx, r); e == nil {
				t.Fatal("old grant automatically narrowed or renewed")
			}
		})
	}
}

func TestContextBudgetNativeOmittedProjectionAfterActualLockWait(t *testing.T) {
	f, r, _ := relevanceNative(t)
	b := f.f.place.private.base
	svc, _ := acr.NewService(b.store)
	out, e := svc.Retrieve(b.ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	zero := 0
	if _, e = aca.ProjectRelated(out.ProjectionBundle(), aca.Budget{MaxEncodedBytes: 32768, ItemLimit: &zero}, out.ExcludedCounts()); e != nil {
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
	result := make(chan error, 1)
	go func() { result <- svc.Revalidate(b.ctx, r.Access, out) }()
	placeMemoryWaitForBlock(t, f.f.place, lock.Conn().PgConn().PID())
	var expiry time.Time
	if e = b.pool.QueryRow(b.ctx, `WITH expiry AS MATERIALIZED(SELECT clock_timestamp()+interval '120ms' AS at) UPDATE sessions SET expires_at=expiry.at,idle_expires_at=expiry.at FROM expiry WHERE token_sha256=$1 RETURNING expiry.at`, r.Access.SessionDigest[:]).Scan(&expiry); e != nil {
		t.Fatal(e)
	}
	for {
		var now time.Time
		if e = b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			t.Fatal(e)
		}
		if !now.Before(expiry) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if e = lock.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	if e = <-result; e == nil {
		t.Fatal("expiry after actual wait exposed omitted projection")
	}
}
