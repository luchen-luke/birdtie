package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/jackc/pgx/v5/pgxpool"
	"reflect"
	"strings"
	"testing"
	"time"
)

type contextBuilderNativeFixture struct {
	place           *placeMemoryFixture
	task            agentworkspace.Task
	public, private string
	now             time.Time
}

func contextBuilderPurposeRequest(t *testing.T, f *contextBuilderNativeFixture) acb.Request {
	t.Helper()
	b := f.place.private.base
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM agent_context_purpose_bindings WHERE preview_id IN(SELECT id FROM agent_context_purpose_previews WHERE owner_id=ANY($1::uuid[]))`,
			`DELETE FROM agent_context_purpose_previews WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]) AND purpose='TASK_CONTEXT_READ'`} {
			if _, e := b.pool.Exec(context.Background(), q, b.accounts); e != nil {
				t.Error("owned purpose builder cleanup", e)
			}
		}
	})
	savePrivateCanaries(t, f.place.private)
	id := agentMemoryID(t, f.place.private)
	if _, e := b.store.PutOwnMemory(b.ctx, f.place.private.owner, id, agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "builder.purpose.selected", Summary: "本人明确记录的周末羽毛球偏好", StructuredValue: json.RawMessage(`{"declared":true}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: f.now.Add(time.Hour)}); e != nil {
		t.Fatal(e)
	}
	if _, e := b.store.PutOwnPolicy(b.ctx, f.place.private.owner, agentpolicysettings.Autonomy, policyNativeInput(agentpolicysettings.Autonomy, 0, f.now.Add(time.Hour))); e != nil {
		t.Fatal(e)
	}
	r := f.request(t, acb.ActivitySearch)
	r.Mode = acb.MachineTaskContext
	r.Selection = acb.ExactTaskContext
	r.ProfileFields = []string{"availability"}
	r.MemoryIDs = []string{id}
	r.PolicyFamilies = []agentpolicysettings.Family{agentpolicysettings.Autonomy}
	r.PlaceIDs = []string{f.place.place}
	r.PurposeDeadlineAt = r.DeadlineAt
	p, e := b.store.PreviewOwnContextPurpose(b.ctx, r.Access, acb.PurposeSelectionFromRequest(r))
	if e != nil {
		t.Fatal("actual native human preview", e)
	}
	g, e := b.store.ApproveOwnContextPurpose(b.ctx, r.Access, p.ID)
	if e != nil {
		t.Fatal("actual native concrete approval", e)
	}
	r.PurposeGrantID = g.ID
	return r
}
func TestContextBuilderMachineNativeGrantActualAssemblyAndRevalidation(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	r := contextBuilderPurposeRequest(t, f)
	before := policyNativeSnapshot(t, f.place.private, true)
	svc, e := acb.NewService(b.store)
	if e != nil {
		t.Fatal(e)
	}
	built, e := svc.Build(b.ctx, r)
	if e != nil {
		t.Fatal("native machine context after actual approval", e)
	}
	if acb.ValidateBuilt(r, built) != nil || len(built.Bundle.Profile) != 1 || len(built.Bundle.Memories) != 1 || len(built.Bundle.Policies) != 1 || len(built.Bundle.Places) != 1 || len(built.Bundle.Activities) != 1 || built.Bundle.City == nil || built.Bundle.Task == nil || built.Bundle.ModelAccess != "UNAVAILABLE" || built.Bundle.MemoryPromotionAllowed {
		t.Fatal("native selected groups not assembled")
	}
	if _, e = svc.RevalidateOwn(b.ctx, r.Access, built); e != nil {
		t.Fatal("current native revalidation", e)
	}
	if !reflect.DeepEqual(before, policyNativeSnapshot(t, f.place.private, true)) {
		t.Fatal("read-only runtime created knowledge or lifecycle state")
	}
	for name, change := range map[string]func(*acb.Request){
		"otherGrant": func(r *acb.Request) { r.PurposeGrantID = f.task.ID }, "otherField": func(r *acb.Request) { r.ProfileFields = []string{"agentNotes"} }, "unapprovedPlace": func(r *acb.Request) { r.PlaceIDs = []string{f.task.ID} },
		"unapprovedQuery": func(r *acb.Request) { r.CurrentQuery = "未批准的新查询" }, "laterDeadline": func(r *acb.Request) { r.PurposeDeadlineAt = r.PurposeDeadlineAt.Add(time.Minute) },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := r
			change(&candidate)
			if _, e := svc.Build(b.ctx, candidate); e == nil {
				t.Fatal("unapproved selection inherited machine grant")
			}
		})
	}
	// The native service treats a same-value PUT as an idempotent replay. Make
	// two actual A->B->A native edits, using each real returned CAS revision.
	currentVersion := built.Bundle.Memories[0].Version
	changed, e := b.store.PutOwnMemory(b.ctx, r.Access, r.MemoryIDs[0], agentmemory.PutInput{ExpectedVersion: currentVersion, MemoryType: agentmemory.TypePreference, MemoryKey: "builder.purpose.selected", Summary: "本次实际修改后的临时明确偏好", StructuredValue: json.RawMessage(`{"declared":true}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: f.now.Add(time.Hour)})
	if e != nil || changed.Version != currentVersion+1 {
		t.Fatal(e)
	}
	restored, e := b.store.PutOwnMemory(b.ctx, r.Access, r.MemoryIDs[0], agentmemory.PutInput{ExpectedVersion: changed.Version, MemoryType: agentmemory.TypePreference, MemoryKey: "builder.purpose.selected", Summary: "本人明确记录的周末羽毛球偏好", StructuredValue: json.RawMessage(`{"declared":true}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: f.now.Add(time.Hour)})
	if e != nil || restored.Version != changed.Version+1 {
		t.Fatal("native source ABA not executed", e)
	}
	if _, e = svc.RevalidateOwn(b.ctx, r.Access, built); e == nil {
		t.Fatal("source ABA restored old machine context")
	}
}
func TestContextBuilderMachineNativeRevokeAndNoHumanControlInheritance(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	r := contextBuilderPurposeRequest(t, f)
	svc, _ := acb.NewService(b.store)
	built, e := svc.Build(b.ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.RevokeOwnContextPurpose(b.ctx, r.Access, r.PurposeGrantID, 1); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.RevalidateOwn(b.ctx, r.Access, built); e == nil {
		t.Fatal("revoke did not invalidate sealed context")
	}
	if _, e = svc.Build(b.ctx, r); e == nil {
		t.Fatal("revoked grant still built")
	}
	h := acb.Request{Access: r.Access, Agent: r.Agent, RequestID: "human-control", Mode: acb.HumanSelfReview, Selection: acb.ExactSelfReview, ProfileFields: r.ProfileFields, DeadlineAt: r.DeadlineAt}
	human, e := svc.Build(b.ctx, h)
	if e != nil {
		t.Fatal(e)
	}
	human.Request = r
	if _, e = svc.RevalidateOwn(b.ctx, r.Access, human); e == nil {
		t.Fatal("human control became a machine grant")
	}
}

func contextBuilderNative(t *testing.T) *contextBuilderNativeFixture {
	t.Helper()
	f := &contextBuilderNativeFixture{place: placeMemoryNativeFixture(t)}
	b := f.place.private.base
	if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&f.now); e != nil {
		t.Fatal(e)
	}
	b.exec(`INSERT INTO city_contexts(city_id) VALUES($1) ON CONFLICT(city_id) DO NOTHING`, f.place.city)
	task, e := b.store.SaveTask(b.ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: b.person.ID, ActingUserID: b.person.ID, CityID: f.place.city, Query: "公开羽毛球活动", Intent: agentworkspace.FindActivity, Status: agentworkspace.TaskActive, Filters: map[string]string{"currentQuery": "公开羽毛球活动", "category": "badminton", "timePreference": "anytime"}, Conversation: []agentworkspace.Message{{Role: "user", Text: "不要把全部历史带入Context"}}})
	if e != nil {
		t.Fatal(e)
	}
	f.task = task
	t.Cleanup(func() {
		for _, sql := range []string{`DELETE FROM agent_tasks WHERE owner_account_id=ANY($1::uuid[])`, `DELETE FROM activities WHERE created_by_account_id=ANY($1::uuid[])`, `DELETE FROM person_contexts WHERE person_account_id=ANY($1::uuid[])`} {
			if _, e := b.pool.Exec(context.Background(), sql, b.accounts); e != nil {
				t.Error("owned ContextBuilder cleanup", e)
			}
		}
		for _, sql := range []string{`DELETE FROM contexts WHERE city_id=$1`, `DELETE FROM city_contexts WHERE city_id=$1`} {
			if _, e := b.pool.Exec(context.Background(), sql, f.place.city); e != nil {
				t.Error(e)
			}
		}
	})
	for _, entry := range []struct {
		visibility string
		id         *string
	}{{"public", &f.public}, {"invite_only", &f.private}} {
		activity, e := b.store.CreateSocialDraft(b.ctx, b.other.ID, activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: b.other.ID}, CityID: f.place.city, PlaceID: f.place.place, Title: "合成本地Context羽毛球 " + entry.visibility, Summary: "PUBLIC_DESCRIPTION_CANARY", Description: "UNREQUESTED_ACTIVITY_BODY_CANARY", CategoryCode: "badminton", Visibility: entry.visibility, StartsAt: f.now.Add(time.Hour), EndsAt: f.now.Add(2 * time.Hour), TimeZone: "Europe/London"})
		if e != nil {
			t.Fatal(e)
		}
		*entry.id = activity.ID
		if _, e = b.store.PublishSocialActivity(b.ctx, b.other.ID, activity.ID); e != nil {
			t.Fatal(e)
		}
		if entry.visibility != "public" {
			if e = b.store.InviteActivityPerson(b.ctx, b.other.ID, activity.ID, b.person.ID); e != nil {
				t.Fatal(e)
			}
		}
	}
	return f
}
func (f *contextBuilderNativeFixture) request(t *testing.T, selection acb.Selection) acb.Request {
	t.Helper()
	b := f.place.private.base
	ref, e := b.store.ResolveOwnContextAgent(b.ctx, f.place.private.owner)
	if e != nil {
		t.Fatal(e)
	}
	r := acb.Request{Access: f.place.private.owner, Agent: ref, TaskID: f.task.ID, TaskUpdatedAt: f.task.UpdatedAt, RequestID: "LOCAL_SYNTHETIC_NATIVE_CONTEXT", CityID: f.place.city, CurrentQuery: f.task.Filters["currentQuery"], Selection: selection, Mode: acb.RulesPublicQuery, DeadlineAt: f.now.Add(10 * time.Minute)}
	if selection == acb.ActivitySearch {
		r.ActivityIDs = []string{f.public}
	} else {
		r.PlaceIDs = []string{f.place.place}
	}
	return r
}
func contextBuilderRequireEmpty(t *testing.T, out acb.BuiltContext, e error) {
	t.Helper()
	if e == nil || !reflect.DeepEqual(out, acb.BuiltContext{}) {
		t.Fatalf("failed closed e=%v empty=%t", e, reflect.DeepEqual(out, acb.BuiltContext{}))
	}
	for _, secret := range []string{"SELECT", "CANARY", "token_sha256"} {
		if strings.Contains(e.Error(), secret) {
			t.Fatal("private detail leaked")
		}
	}
}
func TestContextBuilderNativePublicAndSelectedHumanReview(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	savePrivateCanaries(t, f.place.private)
	memoryIDs := []string{}
	for i := 0; i < 6; i++ {
		id := agentMemoryID(t, f.place.private)
		_, e := b.store.PutOwnMemory(b.ctx, f.place.private.owner, id, agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: fmt.Sprintf("builder.explicit.%d", i), Summary: fmt.Sprintf("EXPLICIT_PRIVATE_MEMORY_CANARY_%d", i), StructuredValue: json.RawMessage(`{"declared":true}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: f.now.Add(time.Hour)})
		if e != nil {
			t.Fatal(e)
		}
		memoryIDs = append(memoryIDs, id)
	}
	if _, e := b.store.PutOwnPolicy(b.ctx, f.place.private.owner, agentpolicysettings.Autonomy, policyNativeInput(agentpolicysettings.Autonomy, 0, f.now.Add(time.Hour))); e != nil {
		t.Fatal(e)
	}
	before := policyNativeSnapshot(t, f.place.private, true)
	for _, selection := range []acb.Selection{acb.ActivitySearch, acb.PlaceSearch} {
		t.Run(string(selection), func(t *testing.T) {
			r := f.request(t, selection)
			svc, _ := acb.NewService(b.store)
			out, e := svc.Build(b.ctx, r)
			if e != nil || acb.ValidateBuilt(r, out) != nil {
				t.Fatal("actual public Builder", e)
			}
			encoded, _ := json.Marshal(out.Bundle)
			for _, secret := range []string{"CANARY", "latitude", "longitude", "summary", "description", "Conversation", "agentNotes"} {
				if strings.Contains(string(encoded), secret) {
					t.Fatal("irrelevant private/extended content copied", secret)
				}
			}
			if len(out.Bundle.Sources) != 3 || out.Bundle.ModelAccess != "UNAVAILABLE" {
				t.Fatal("no real source or model gate")
			}
			again, e := svc.RevalidateOwn(b.ctx, r.Access, out)
			if e != nil || !again.Bundle.ExpiresAt.Equal(out.Bundle.ExpiresAt) {
				t.Fatal("native lease/version revalidation", e)
			}
		})
	}
	t.Run("ExactHumanProjection", func(t *testing.T) {
		r := f.request(t, acb.ActivitySearch)
		r.Mode = acb.HumanSelfReview
		r.Selection = acb.ExactSelfReview
		r.TaskID = ""
		r.TaskUpdatedAt = time.Time{}
		r.CurrentQuery = ""
		r.CityID = ""
		r.ActivityIDs = nil
		r.ProfileFields = []string{"availability"}
		r.MemoryIDs = memoryIDs[:3]
		r.PolicyFamilies = []agentpolicysettings.Family{agentpolicysettings.Autonomy}
		svc, _ := acb.NewService(b.store)
		out, e := svc.Build(b.ctx, r)
		if e != nil || len(out.Bundle.Memories) != 3 || len(out.Bundle.Profile) != 1 || len(out.Bundle.Policies) != 1 {
			t.Fatal("bounded human selection", e)
		}
		raw, _ := json.Marshal(out.Bundle)
		if strings.Contains(string(raw), "CANARY_3") || strings.Contains(string(raw), "agentNotes") || strings.Contains(string(raw), "socialPreferences") {
			t.Fatal("unselected private content read/returned")
		}
		for _, source := range out.Bundle.Sources {
			if source.Version.Revision <= 0 || source.RowToken == "" {
				t.Fatal("fake source version")
			}
		}
		bad := r
		bad.Mode = acb.RulesPublicQuery
		bad.Selection = acb.ActivitySearch
		got, e := svc.Build(b.ctx, bad)
		contextBuilderRequireEmpty(t, got, e)
	})
	if before != policyNativeSnapshot(t, f.place.private, true) {
		t.Fatal("read mutated authoritative Profile/Memory/Policy/grants")
	}
}
func TestContextBuilderNativeNegativeSourcesAndIdentity(t *testing.T) {
	for _, name := range []string{"PeerSession", "Organization", "DormantBusiness", "WrongAgent", "MissingMetadata", "PrivateActivity", "DraftActivity", "CancelledActivity", "EndedActivity", "ExpiredActivity", "ActivityFutureClock", "Blocked", "HiddenCity", "InactiveCity", "ExpiredCity", "FutureCity", "HiddenPlace", "ExpiredPlace", "FuturePlace", "TaskCompleted", "TaskWrongOwner", "TaskDifferentQuery", "TaskChangedVersion", "PrivateMemoryWanted", "PrivateProfileWanted", "PrivatePolicyWanted", "RelationshipWanted", "ZeroSession", "DeadlineExpired"} {
		t.Run(name, func(t *testing.T) {
			f := contextBuilderNative(t)
			b := f.place.private.base
			r := f.request(t, acb.ActivitySearch)
			switch name {
			case "PeerSession":
				r.Access = f.place.private.peer
			case "Organization":
				r.Access = f.place.private.org
			case "DormantBusiness":
				r.Access = f.place.private.biz
			case "WrongAgent":
				r.Agent.AgentID = b.otherID
			case "MissingMetadata":
				b.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, b.personID)
			case "PrivateActivity":
				r.ActivityIDs = []string{f.private}
			case "DraftActivity":
				b.exec(`UPDATE activities SET publication_status='draft' WHERE id=$1`, f.public)
			case "CancelledActivity":
				b.exec(`UPDATE activities SET cancelled_at=clock_timestamp() WHERE id=$1`, f.public)
			case "EndedActivity":
				b.exec(`UPDATE activities SET starts_at=clock_timestamp()-interval '2 hours',ends_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, f.public)
			case "ExpiredActivity":
				b.exec(`UPDATE activities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.public)
			case "ActivityFutureClock":
				b.exec(`UPDATE activities SET updated_at=clock_timestamp()+interval '1 day' WHERE id=$1`, f.public)
			case "Blocked":
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, b.person.ID, b.other.ID)
			case "HiddenCity":
				b.exec(`UPDATE cities SET publication_status='draft' WHERE id=$1`, f.place.city)
			case "InactiveCity":
				b.exec(`UPDATE city_contexts SET status='paused' WHERE city_id=$1`, f.place.city)
			case "ExpiredCity":
				b.exec(`UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.place.city)
			case "FutureCity":
				b.exec(`UPDATE cities SET updated_at=clock_timestamp()+interval '1 day' WHERE id=$1`, f.place.city)
			case "HiddenPlace":
				r.Selection = acb.PlaceSearch
				r.ActivityIDs = nil
				r.PlaceIDs = []string{f.place.place}
				b.exec(`UPDATE places SET publication_status='draft' WHERE id=$1`, f.place.place)
			case "ExpiredPlace":
				r.Selection = acb.PlaceSearch
				r.ActivityIDs = nil
				r.PlaceIDs = []string{f.place.place}
				b.exec(`UPDATE places SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.place.place)
			case "FuturePlace":
				r.Selection = acb.PlaceSearch
				r.ActivityIDs = nil
				r.PlaceIDs = []string{f.place.place}
				b.exec(`UPDATE places SET updated_at=clock_timestamp()+interval '1 day' WHERE id=$1`, f.place.place)
			case "TaskCompleted":
				b.exec(`UPDATE agent_tasks SET status='COMPLETED' WHERE id=$1`, f.task.ID)
			case "TaskWrongOwner":
				r.TaskID = agentMemoryID(t, f.place.private)
			case "TaskDifferentQuery":
				r.CurrentQuery = "旧查询不是当前请求"
			case "TaskChangedVersion":
				b.exec(`UPDATE agent_tasks SET updated_at=clock_timestamp() WHERE id=$1`, f.task.ID)
			case "PrivateMemoryWanted":
				r.MemoryIDs = []string{f.task.ID}
			case "PrivateProfileWanted":
				r.ProfileFields = []string{"agentNotes"}
			case "PrivatePolicyWanted":
				r.PolicyFamilies = []agentpolicysettings.Family{agentpolicysettings.Social}
			case "RelationshipWanted":
				r.Relationships = true
			case "ZeroSession":
				r.Access.SessionDigest = [32]byte{}
			case "DeadlineExpired":
				r.DeadlineAt = f.now.Add(-time.Second)
			}
			got, e := b.store.BuildOwnAgentContext(b.ctx, r)
			contextBuilderRequireEmpty(t, got, e)
		})
	}
}
func TestContextBuilderNativeRevalidationAndRestart(t *testing.T) {
	for _, name := range []string{"Normal", "NewService", "SuspendRestoreAccount", "RetireRestoreAgent", "SourceSameTimeEdit", "TaskChange", "TamperedMemoryPayload", "MissingMetadata", "CityExpiry", "OtherSession", "TamperedQuery", "TamperedMode"} {
		t.Run(name, func(t *testing.T) {
			f := contextBuilderNative(t)
			b := f.place.private.base
			r := f.request(t, acb.PlaceSearch)
			svc, _ := acb.NewService(b.store)
			out, e := svc.Build(b.ctx, r)
			if e != nil {
				t.Fatal(e)
			}
			access := r.Access
			switch name {
			case "NewService":
				svc, _ = acb.NewService(New(b.pool, false))
			case "SuspendRestoreAccount":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
				b.exec(`UPDATE accounts SET status='active' WHERE id=$1`, b.person.ID)
			case "RetireRestoreAgent":
				b.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, b.personID)
				b.exec(`UPDATE agents SET status='active' WHERE id=$1`, b.personID)
			case "SourceSameTimeEdit":
				b.exec(`UPDATE places SET name=name||'修改',updated_at=updated_at WHERE id=$1`, f.place.place)
			case "TaskChange":
				b.exec(`UPDATE agent_tasks SET query=query||'修改',updated_at=clock_timestamp() WHERE id=$1`, f.task.ID)
			case "TamperedMemoryPayload":
				out.Bundle.Memories = []acb.ReviewMemory{{ID: f.task.ID}}
			case "MissingMetadata":
				b.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, b.personID)
			case "CityExpiry":
				b.exec(`UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.place.city)
			case "OtherSession":
				access = f.place.private.peer
			case "TamperedQuery":
				out.Bundle.CurrentQuery = "伪造"
			case "TamperedMode":
				out.Bundle.Mode = acb.HumanSelfReview
			}
			got, e := svc.RevalidateOwn(b.ctx, access, out)
			if name == "Normal" {
				if e != nil || !got.Bundle.ExpiresAt.Equal(out.Bundle.ExpiresAt) {
					t.Fatal("renewed native source", e)
				}
			} else {
				contextBuilderRequireEmpty(t, got, e)
			}
		})
	}
}
func TestContextBuilderNativeLateCurrentAccountBarrier(t *testing.T) {
	for _, name := range []string{"Revoke", "AbsoluteExpiry", "IdleExpiry", "RequestExpiry", "CancelledContext", "DefaultRRLateSuspend"} {
		t.Run(name, func(t *testing.T) {
			f := contextBuilderNative(t)
			b := f.place.private.base
			r := f.request(t, acb.PlaceSearch)
			cfg := b.pool.Config().Copy()
			cfg.ConnConfig.RuntimeParams["application_name"] = "builder033-" + b.person.ID
			cfg.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"
			pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			store := New(pool, false)
			lock, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer lock.Rollback(context.Background())
			if _, e = lock.Exec(b.ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, b.person.ID); e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithCancel(b.ctx)
			defer cancel()
			if name == "RequestExpiry" {
				var at time.Time
				if b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&at) != nil {
					t.Fatal("PG clock")
				}
				r.DeadlineAt = at.Add(150 * time.Millisecond)
			}
			done := make(chan struct {
				out acb.BuiltContext
				err error
			}, 1)
			go func() {
				out, e := store.BuildOwnAgentContext(ctx, r)
				done <- struct {
					out acb.BuiltContext
					err error
				}{out, e}
			}()
			until := time.Now().Add(4 * time.Second)
			waiting := false
			for time.Now().Before(until) {
				if b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock')`, "builder033-"+b.person.ID).Scan(&waiting) != nil {
					t.Fatal("actual wait not observed")
				}
				if waiting {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if !waiting {
				t.Fatal("no actual native lock wait")
			}
			switch name {
			case "Revoke":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.place.private.ownerSession)
			case "AbsoluteExpiry":
				b.exec(`UPDATE sessions SET created_at=clock_timestamp()-interval '1 hour',expires_at=clock_timestamp()-interval '1 second',idle_expires_at=clock_timestamp()-interval '2 seconds' WHERE id=$1`, f.place.private.ownerSession)
			case "IdleExpiry":
				b.exec(`UPDATE sessions SET created_at=clock_timestamp()-interval '1 hour',idle_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.place.private.ownerSession)
			case "RequestExpiry":
				time.Sleep(200 * time.Millisecond)
			case "CancelledContext":
				cancel()
			case "DefaultRRLateSuspend":
				if _, e = lock.Exec(b.ctx, `UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID); e != nil {
					t.Fatal(e)
				}
			}
			if e = lock.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case got := <-done:
				contextBuilderRequireEmpty(t, got.out, got.err)
			case <-time.After(5 * time.Second):
				t.Fatal("native barrier stuck")
			}
		})
	}
}
func TestContextBuilderNativeNilAndUnavailableNoPanic(t *testing.T) {
	f := contextBuilderNative(t)
	r := f.request(t, acb.ActivitySearch)
	out, e := f.place.private.base.store.BuildOwnAgentContext(nil, r)
	contextBuilderRequireEmpty(t, out, e)
	if !errors.Is(e, acb.ErrUnavailable) {
		t.Fatal(e)
	}
	var nilStore *Store
	out, e = nilStore.BuildOwnAgentContext(context.Background(), r)
	contextBuilderRequireEmpty(t, out, e)
}

func TestContextBuilderNativeFinalClockAfterSourceWait(t *testing.T) {
	for _, name := range []string{"RequestExpired", "SessionExpired", "PlaceExpired", "CancelledContext", "PhantomBlock"} {
		t.Run(name, func(t *testing.T) {
			f := contextBuilderNative(t)
			b := f.place.private.base
			r := f.request(t, acb.PlaceSearch)
			cfg := b.pool.Config().Copy()
			cfg.ConnConfig.RuntimeParams["application_name"] = "builder033-final-" + b.person.ID
			pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			store := New(pool, false)
			var clock time.Time
			if e = b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&clock); e != nil {
				t.Fatal(e)
			}
			if name == "RequestExpired" {
				r.DeadlineAt = clock.Add(250 * time.Millisecond)
			}
			if name == "SessionExpired" {
				b.exec(`UPDATE sessions SET expires_at=$2,idle_expires_at=$2 WHERE id=$1`, f.place.private.ownerSession, clock.Add(250*time.Millisecond))
			}
			if name == "PlaceExpired" {
				b.exec(`UPDATE places SET expires_at=$2 WHERE id=$1`, f.place.place, clock.Add(250*time.Millisecond))
			}
			lock, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer lock.Rollback(context.Background())
			q := `SELECT id FROM places WHERE id=$1 FOR UPDATE`
			id := f.place.place
			if name == "PhantomBlock" {
				q = `SELECT id FROM activities WHERE id=$1 FOR UPDATE`
				id = f.public
				r.Selection = acb.ActivitySearch
				r.PlaceIDs = nil
				r.ActivityIDs = []string{f.public}
			}
			if _, e = lock.Exec(b.ctx, q, id); e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithCancel(b.ctx)
			defer cancel()
			done := make(chan struct {
				out acb.BuiltContext
				err error
			}, 1)
			go func() {
				out, e := store.BuildOwnAgentContext(ctx, r)
				done <- struct {
					out acb.BuiltContext
					err error
				}{out, e}
			}()
			waiting := false
			until := time.Now().Add(4 * time.Second)
			for time.Now().Before(until) {
				if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock')`, "builder033-final-"+b.person.ID).Scan(&waiting); e != nil {
					t.Fatal(e)
				}
				if waiting {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if !waiting {
				t.Fatal("actual source lock wait not observed")
			}
			if name == "PhantomBlock" {
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, b.person.ID, b.other.ID)
			} else if name == "CancelledContext" {
				cancel()
			} else {
				time.Sleep(300 * time.Millisecond)
			}
			if e = lock.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case got := <-done:
				contextBuilderRequireEmpty(t, got.out, got.err)
			case <-time.After(5 * time.Second):
				t.Fatal("source wait did not finish")
			}
		})
	}
}

func TestContextBuilderNativeHumanSelectedSourcesChangedOrDeleted(t *testing.T) {
	for _, name := range []string{"MemoryDelete", "MemoryEdit", "ProfileClear", "PolicyEdit", "MemoryExpired", "PrivateFutureUpdated"} {
		t.Run(name, func(t *testing.T) {
			f := contextBuilderNative(t)
			b := f.place.private.base
			savePrivateCanaries(t, f.place.private)
			id := agentMemoryID(t, f.place.private)
			m, e := b.store.PutOwnMemory(b.ctx, f.place.private.owner, id, agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "builder.human.selected", Summary: "合成明确声明", StructuredValue: json.RawMessage(`{"declared":true}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: f.now.Add(time.Hour)})
			if e != nil {
				t.Fatal(e)
			}
			p, e := b.store.PutOwnPolicy(b.ctx, f.place.private.owner, agentpolicysettings.Autonomy, policyNativeInput(agentpolicysettings.Autonomy, 0, f.now.Add(time.Hour)))
			if e != nil {
				t.Fatal(e)
			}
			r := f.request(t, acb.ActivitySearch)
			r.Mode = acb.HumanSelfReview
			r.Selection = acb.ExactSelfReview
			r.TaskID = ""
			r.TaskUpdatedAt = time.Time{}
			r.CurrentQuery = ""
			r.CityID = ""
			r.ActivityIDs = nil
			r.ProfileFields = []string{"availability"}
			r.MemoryIDs = []string{id}
			r.PolicyFamilies = []agentpolicysettings.Family{agentpolicysettings.Autonomy}
			svc, _ := acb.NewService(b.store)
			out, e := svc.Build(b.ctx, r)
			if e != nil {
				t.Fatal(e)
			}
			switch name {
			case "MemoryDelete":
				_, e = b.store.DeleteOwnMemory(b.ctx, r.Access, id, m.Version)
			case "MemoryEdit":
				_, e = b.store.PutOwnMemory(b.ctx, r.Access, id, agentmemory.PutInput{ExpectedVersion: m.Version, MemoryType: m.MemoryType, MemoryKey: m.MemoryKey, Summary: "新的明确声明", StructuredValue: json.RawMessage(`{"declared":false}`), Visibility: m.Visibility, ValidUntil: m.ValidUntil})
			case "ProfileClear":
				old, x := b.store.ReadOwnAgentPrivateProfile(b.ctx, r.Access)
				if x != nil {
					t.Fatal(x)
				}
				_, e = b.store.ReplaceOwnAgentPrivateProfile(b.ctx, r.Access, agentprofile.ReplacePrivateInput{ExpectedVersion: old.Profile.ProfileVersion, Fields: agentprofile.PrivateFields{}})
			case "PolicyEdit":
				_, e = b.store.PutOwnPolicy(b.ctx, r.Access, agentpolicysettings.Autonomy, policyNativeInput(agentpolicysettings.Autonomy, p.Autonomy.NativeRevision, f.now.Add(2*time.Hour)))
			case "MemoryExpired":
				b.exec(`UPDATE agent_memories SET version=version+1,valid_until=clock_timestamp()+interval '100 milliseconds',updated_at=clock_timestamp() WHERE id=$1`, id)
				time.Sleep(120 * time.Millisecond)
			case "PrivateFutureUpdated":
				b.exec(`UPDATE agent_profiles SET profile_version=profile_version+1,updated_at=clock_timestamp() WHERE agent_id=$1`, r.Agent.AgentID)
				b.exec(`UPDATE agent_private_profiles SET written_profile_version=(SELECT profile_version FROM agent_profiles WHERE agent_id=$1),updated_at=clock_timestamp()+interval '1 day' WHERE agent_id=$1`, r.Agent.AgentID)
			}
			if e != nil {
				t.Fatal("actual human source mutation failed", e)
			}
			got, e := svc.RevalidateOwn(b.ctx, r.Access, out)
			contextBuilderRequireEmpty(t, got, e)
		})
	}
}
