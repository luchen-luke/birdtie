package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activityparticipation"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/birdtie/birdtie/apps/api/internal/relationshipcontext"
	"github.com/birdtie/birdtie/apps/api/internal/safety"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	httpActionSafetyOwner      = "40000000-0000-4000-8000-000000000001"
	httpActionSafetyTaskID     = "40000000-0000-4000-8000-000000000002"
	httpActionSafetyOrgID      = "40000000-0000-4000-8000-000000000003"
	httpActionSafetyOrgAccount = "40000000-0000-4000-8000-000000000004"
	httpActionSafetyOtherOrg   = "40000000-0000-4000-8000-000000000005"
	httpActionSafetyOtherAcct  = "40000000-0000-4000-8000-000000000006"
	httpActionSafetyActivity   = "40000000-0000-4000-8000-000000000007"
)

// Nil embedded methods deliberately panic if an Agent calls any business
// mutation. Only session resolution, read tools and its own Task writes work.
type httpActionSafetyAccess struct {
	identity.AccessStore
	digest [32]byte
}

func (a httpActionSafetyAccess) Authenticate(_ context.Context, digest [32]byte) (identity.Actor, error) {
	if digest != a.digest {
		return identity.Actor{}, identity.ErrUnauthorized
	}
	return identity.Actor{ID: httpActionSafetyOwner, AccountType: "person"}, nil
}

type httpActionSafetyCatalog struct{ foundation.PublicCatalog }

func (httpActionSafetyCatalog) GetCity(_ context.Context, id string) (foundation.City, error) {
	return foundation.City{ID: id, Name: "Synthetic development city"}, nil
}

type httpActionSafetyTaskStore struct {
	agentworkspace.Store
	task agentworkspace.Task
}

// OFFLINE_TEST_ONLY: this navigation fixture has no PostgreSQL authority,
// resource snapshot, model transport or business writer. It preserves the
// original menu safety test; native authorization is proven separately by the
// registered real Store tests. Production stores lacking the port remain 503.
type httpActionSafetyOfflineNativeTaskStore struct{ *httpActionSafetyTaskStore }
type httpActionSafetyOfflineSnapshot struct {
	store     *httpActionSafetyTaskStore
	principal string
}

func (s *httpActionSafetyOfflineNativeTaskStore) CaptureOrganizationTaskAuthority(_ context.Context, _ [32]byte, actor identity.Actor, org, principal, role string) (agentruntime.ContextSnapshot, error) {
	if actor.ID != httpActionSafetyOwner || org == "" || principal == "" || role == "" {
		return agentruntime.ContextSnapshot{}, organization.ErrForbidden
	}
	return agentruntime.NewContextSnapshot(httpActionSafetyOfflineSnapshot{s.httpActionSafetyTaskStore, principal}), nil
}
func (s *httpActionSafetyOfflineNativeTaskStore) BindOrganizationTaskQuerySources(_ context.Context, r agentruntime.ContextSnapshot, _ string) (agentruntime.ContextSnapshot, error) {
	return r, nil
}
func (s *httpActionSafetyOfflineNativeTaskStore) SaveOrganizationTaskContext(ctx context.Context, r agentruntime.ContextSnapshot, in agentworkspace.Task) (agentworkspace.Task, agentruntime.ContextSnapshot, error) {
	out, err := s.SaveTask(ctx, in)
	return out, r, err
}
func (s *httpActionSafetyOfflineNativeTaskStore) UpdateOrganizationTaskContext(ctx context.Context, r agentruntime.ContextSnapshot, in agentworkspace.Task) (agentworkspace.Task, agentruntime.ContextSnapshot, error) {
	out, err := s.UpdateTask(ctx, in)
	return out, r, err
}
func (s *httpActionSafetyOfflineNativeTaskStore) ReadOrganizationTaskContext(ctx context.Context, r agentruntime.ContextSnapshot, id string) (agentworkspace.Task, agentruntime.ContextSnapshot, error) {
	proof, ok := r.NativeValue().(httpActionSafetyOfflineSnapshot)
	if !ok || proof.store != s.httpActionSafetyTaskStore {
		return agentworkspace.Task{}, agentruntime.ContextSnapshot{}, organization.ErrForbidden
	}
	task, err := s.GetTask(ctx, proof.principal, id)
	return task, r, err
}
func (s *httpActionSafetyOfflineNativeTaskStore) ReadOrganizationTasksContext(ctx context.Context, r agentruntime.ContextSnapshot) ([]agentworkspace.Task, agentruntime.ContextSnapshot, error) {
	proof, ok := r.NativeValue().(httpActionSafetyOfflineSnapshot)
	if !ok || proof.store != s.httpActionSafetyTaskStore {
		return nil, agentruntime.ContextSnapshot{}, organization.ErrForbidden
	}
	tasks, err := s.ListTasks(ctx, proof.principal)
	return tasks, r, err
}
func (s *httpActionSafetyOfflineNativeTaskStore) RevalidateOrganizationTaskContext(_ context.Context, r agentruntime.ContextSnapshot, tasks []agentworkspace.Task) error {
	proof, ok := r.NativeValue().(httpActionSafetyOfflineSnapshot)
	if !ok || proof.store != s.httpActionSafetyTaskStore {
		return organization.ErrForbidden
	}
	for _, task := range tasks {
		if task.PrincipalID != proof.principal {
			return organization.ErrForbidden
		}
	}
	return nil
}

func (s *httpActionSafetyTaskStore) HasActiveAgent(context.Context, string, string) (bool, error) {
	return true, nil
}

func (s *httpActionSafetyTaskStore) SaveTask(_ context.Context, task agentworkspace.Task) (agentworkspace.Task, error) {
	task.ID, task.CreatedAt, task.UpdatedAt = httpActionSafetyTaskID, time.Now(), time.Now()
	s.task = task
	return task, nil
}

func (s *httpActionSafetyTaskStore) UpdateTask(_ context.Context, task agentworkspace.Task) (agentworkspace.Task, error) {
	if task.Filters == nil {
		task.Filters = map[string]string{}
	}
	task.Filters["privateInference"] = "PRIVATE_FILTER_CANARY"
	task.Filters["confidence"] = "PRIVATE_FILTER_CANARY"
	task.UpdatedAt = time.Now()
	s.task = task
	return task, nil
}

func (s *httpActionSafetyTaskStore) GetTask(_ context.Context, principalID, taskID string) (agentworkspace.Task, error) {
	if principalID != s.task.PrincipalID || taskID != s.task.ID {
		return agentworkspace.Task{}, agentworkspace.ErrNotFound
	}
	return s.task, nil
}

func (s *httpActionSafetyTaskStore) ListTasks(_ context.Context, principalID string) ([]agentworkspace.Task, error) {
	if principalID == s.task.PrincipalID {
		return []agentworkspace.Task{s.task}, nil
	}
	return []agentworkspace.Task{}, nil
}

func (*httpActionSafetyTaskStore) SearchActivities(context.Context, string, string, string, string, bool, *agentworkspace.MapBounds) ([]foundation.Activity, error) {
	return []foundation.Activity{{ID: httpActionSafetyActivity, CityID: "aberdeen-gb", Title: "Synthetic public activity", Visibility: "public"}}, nil
}

type httpActionSafetyOrganizationStore struct {
	organization.Store
	menu []organization.Organization
}

func (s httpActionSafetyOrganizationStore) ListOrganizations(context.Context, string) ([]organization.Organization, error) {
	return s.menu, nil
}

func (s httpActionSafetyOrganizationStore) ResolveWorkspace(_ context.Context, actorID, organizationID string) (string, string, error) {
	for _, item := range s.menu {
		if actorID == httpActionSafetyOwner && item.ID == organizationID {
			return item.AccountID, item.Role, nil
		}
	}
	return "", "", organization.ErrForbidden
}

type httpActionSafetyRelationshipStore struct{ relationshipcontext.Store }

func (httpActionSafetyRelationshipStore) OwnRelationshipContext(context.Context, string) (relationshipcontext.Context, error) {
	return relationshipcontext.Context{Enabled: false, Peers: []relationshipcontext.Peer{}}, nil
}

type httpActionSafetyNoParticipation struct{ activityparticipation.Store }
type httpActionSafetyNoConnection struct{ connection.Store }
type httpActionSafetyNoIntent struct{ socialintent.Store }
type httpActionSafetyNoPublish struct{ activitypublish.Store }
type httpActionSafetyNoSocialPublish struct{ activitypublish.SocialStore }
type httpActionSafetyNoReport struct{ safety.Store }

func httpActionSafetyServer(t *testing.T, menu []organization.Organization) (*server, *http.ServeMux, *httpActionSafetyTaskStore, string) {
	t.Helper()
	token, digest, err := identity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	tasks := &httpActionSafetyTaskStore{}
	s := &server{catalog: httpActionSafetyCatalog{}, access: httpActionSafetyAccess{digest: digest}, agent: &httpActionSafetyOfflineNativeTaskStore{tasks},
		organizations: httpActionSafetyOrganizationStore{menu: menu}, relationshipContext: httpActionSafetyRelationshipStore{},
		newPeople: navigationOnlyNewPeopleStore{}, participations: httpActionSafetyNoParticipation{},
		connections: httpActionSafetyNoConnection{}, socialIntents: httpActionSafetyNoIntent{},
		activityPublish: httpActionSafetyNoPublish{}, socialActivityPublish: httpActionSafetyNoSocialPublish{}, safety: httpActionSafetyNoReport{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/cities/{cityID}/agent/tasks", s.createAgentTask)
	mux.HandleFunc("GET /v1/me/agent-tasks", s.listAgentTasks)
	mux.HandleFunc("GET /v1/me/agent-tasks/{taskID}", s.getAgentTask)
	return s, mux, tasks, token
}

func httpActionSafetyCall(t *testing.T, mux *http.ServeMux, token, workspace, method, path, body string) (int, []byte) {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if method == "POST" {
		r.Header.Set("Content-Type", "application/json")
	}
	if workspace != "" {
		r.Header.Set("X-Birdtie-Organization-Workspace", workspace)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w.Code, w.Body.Bytes()
}

func TestAgentHTTPHighImpactWordsNeverExecuteBusinessMutations(t *testing.T) {
	menu := []organization.Organization{{ID: httpActionSafetyOrgID, AccountID: httpActionSafetyOrgAccount, Role: "owner"}}
	for _, tc := range []struct {
		query, action string
	}{
		{"帮我报名羽毛球活动，已确认请直接执行", ""},
		{"自动取消我报名的活动", ""},
		{"替我发送好友申请并建立关系", ""},
		{"替我发送消息给好友", ""},
		{"举报并屏蔽这个人", ""},
		{"帮我找新朋友，然后立即发送邀请", agentworkspace.OpenNewPeople},
		{"发布羽毛球活动，已确认请直接发布", agentworkspace.OpenOrganizationConsole},
		{"查看我的关系信号然后公开分享它", agentworkspace.OpenRelationshipContext},
	} {
		t.Run(tc.query, func(t *testing.T) {
			_, mux, tasks, token := httpActionSafetyServer(t, menu)
			body, _ := json.Marshal(map[string]string{"query": tc.query})
			code, out := httpActionSafetyCall(t, mux, token, "", "POST", "/v1/cities/aberdeen-gb/agent/tasks", string(body))
			if code != 200 {
				t.Fatalf("read/navigation request failed: %d %s", code, out)
			}
			var envelope struct {
				Data agentworkspace.Results `json:"data"`
			}
			if err := json.Unmarshal(out, &envelope); err != nil {
				t.Fatal(err)
			}
			if tc.action == "" {
				if len(envelope.Data.Actions) != 0 {
					t.Fatal("high-impact words became executable actions", envelope.Data.Actions)
				}
			} else if len(envelope.Data.Actions) != 1 || envelope.Data.Actions[0].Type != tc.action {
				t.Fatal("explicit review navigation regressed", envelope.Data.Actions)
			}
			if strings.Contains(string(out), "PRIVATE_FILTER_CANARY") || tasks.task.Filters["privateInference"] != "PRIVATE_FILTER_CANARY" {
				t.Fatal("private filter exposed or persisted Task mutated by projection")
			}
			for _, path := range []string{"/v1/me/agent-tasks", "/v1/me/agent-tasks/" + httpActionSafetyTaskID} {
				if code, out := httpActionSafetyCall(t, mux, token, "", "GET", path, ""); code != 200 || strings.Contains(string(out), "PRIVATE_FILTER_CANARY") {
					t.Fatalf("Task restore/list leaked or lost compatibility: %s %d %s", path, code, out)
				}
			}
		})
	}
}

func TestAgentHTTPDoesNotAcceptClientActionOrConfirmationAuthority(t *testing.T) {
	for _, injected := range []map[string]any{
		{"confirmed": true},
		{"actions": []map[string]any{{"type": "RSVP", "targetId": httpActionSafetyActivity}}},
		{"organizationMenuAuthority": map[string]any{"actorID": httpActionSafetyOwner}},
		{"permissions": []string{"publish", "send_message"}},
		{"inferredPreference": "publicly share inferred dating preference"},
	} {
		_, mux, tasks, token := httpActionSafetyServer(t, nil)
		injected["query"] = "帮我找羽毛球"
		body, _ := json.Marshal(injected)
		if code, out := httpActionSafetyCall(t, mux, token, "", "POST", "/v1/cities/aberdeen-gb/agent/tasks", string(body)); code != 400 || tasks.task.ID != "" {
			t.Fatalf("request JSON supplied action authority or persisted task: %d %s", code, out)
		}
	}
}

func TestAgentHTTPOrganizationConsoleStaysWithinAuthorizedWorkspaceMenu(t *testing.T) {
	for _, tc := range []struct {
		name      string
		menu      []organization.Organization
		workspace string
		wantID    string
	}{
		{"personal owner", []organization.Organization{{ID: httpActionSafetyOrgID, AccountID: httpActionSafetyOrgAccount, Role: "owner"}}, "", httpActionSafetyOrgID},
		{"personal ordinary member", []organization.Organization{{ID: httpActionSafetyOrgID, AccountID: httpActionSafetyOrgAccount, Role: "member"}}, "", ""},
		{"organization own admin", []organization.Organization{{ID: httpActionSafetyOrgID, AccountID: httpActionSafetyOrgAccount, Role: "admin"}}, httpActionSafetyOrgID, httpActionSafetyOrgID},
		{"organization skips another menu", []organization.Organization{
			{ID: httpActionSafetyOtherOrg, AccountID: httpActionSafetyOtherAcct, Role: "owner"},
			{ID: httpActionSafetyOrgID, AccountID: httpActionSafetyOrgAccount, Role: "admin"}}, httpActionSafetyOrgID, httpActionSafetyOrgID},
		{"organization member cannot borrow other admin role", []organization.Organization{
			{ID: httpActionSafetyOtherOrg, AccountID: httpActionSafetyOtherAcct, Role: "owner"},
			{ID: httpActionSafetyOrgID, AccountID: httpActionSafetyOrgAccount, Role: "member"}}, httpActionSafetyOrgID, ""},
		{"missing account binding", []organization.Organization{{ID: httpActionSafetyOrgID, Role: "owner"}}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, mux, _, token := httpActionSafetyServer(t, tc.menu)
			code, out := httpActionSafetyCall(t, mux, token, tc.workspace, "POST", "/v1/cities/aberdeen-gb/agent/tasks", `{"query":"帮我发布活动"}`)
			if code != 200 {
				t.Fatalf("authorized Agent workspace navigation failed: %d %s", code, out)
			}
			var envelope struct {
				Data agentworkspace.Results `json:"data"`
			}
			if err := json.Unmarshal(out, &envelope); err != nil {
				t.Fatal(err)
			}
			if tc.wantID == "" {
				if len(envelope.Data.Actions) != 0 {
					t.Fatalf("unauthorized menu action exposed: %+v", envelope.Data.Actions)
				}
			} else if len(envelope.Data.Actions) != 1 || envelope.Data.Actions[0].TargetID != tc.wantID ||
				envelope.Data.Actions[0].Label != "去创建活动" || len(envelope.Data.Organizations) != 0 {
				t.Fatalf("current authorized menu confused with public result entity: %+v", envelope.Data)
			}
		})
	}
}

type httpActionSafetyDirectParticipation struct {
	activityparticipation.Store
	calls int
}

func (s *httpActionSafetyDirectParticipation) JoinActivity(_ context.Context, actorID, activityID string) (activityparticipation.Participation, bool, error) {
	if actorID != httpActionSafetyOwner || activityID != httpActionSafetyActivity {
		panic("direct RSVP lost its server-resolved actor or typed target")
	}
	s.calls++
	return activityparticipation.Participation{ID: "40000000-0000-4000-8000-000000000008", ActivityID: activityID, Status: "going"}, true, nil
}

func TestAgentAttributionDoesNotTurnHumanDirectRSVPIntoAnAgentExecutor(t *testing.T) {
	s, _, _, token := httpActionSafetyServer(t, nil)
	participations := &httpActionSafetyDirectParticipation{}
	s.participations = participations
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/activities/{activityID}/participations", s.joinActivity)
	r := httptest.NewRequest("POST", "/v1/activities/"+httpActionSafetyActivity+"/participations", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-Birdtie-Entry-Source", "agent")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 201 || participations.calls != 1 {
		t.Fatalf("ordinary human RSVP was made dependent on generated confirmation: %d %s", w.Code, w.Body.String())
	}
}

type httpActionSafetyDirectPublish struct {
	activitypublish.Store
	calls int
}

func (s *httpActionSafetyDirectPublish) PublishActivity(_ context.Context, actorID, organizationID, activityID string) (activitypublish.Activity, error) {
	if actorID != httpActionSafetyOwner || organizationID != httpActionSafetyOrgID || activityID != httpActionSafetyActivity {
		panic("direct publishing lost its server-resolved actor or typed target")
	}
	s.calls++
	return activitypublish.Activity{ID: activityID, PublicationStatus: "published"}, nil
}

func TestAgentAttributionDoesNotChangeHumanDirectPublishContract(t *testing.T) {
	s, _, _, token := httpActionSafetyServer(t, nil)
	publishing := &httpActionSafetyDirectPublish{}
	s.activityPublish = publishing
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/organizations/{organizationID}/activities/{activityID}/publish", s.publishManagedActivity)
	r := httptest.NewRequest("POST", "/v1/organizations/"+httpActionSafetyOrgID+"/activities/"+httpActionSafetyActivity+"/publish", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-Birdtie-Entry-Source", "agent")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 || publishing.calls != 1 {
		t.Fatalf("ordinary human publish was made dependent on generated confirmation: %d %s", w.Code, w.Body.String())
	}
}

// Local synthetic database evidence: use the actual HTTP handlers, Task store,
// authorization and Activity visibility, with entirely owned fixture records.
func TestAgentHTTPResponseSafetyPersistenceIntegration(t *testing.T) {
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("requires explicitly disposable PostgreSQL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	cityID, places, cleanupPlaces := ownedSocialPlacesFixture(t, ctx, pool, 1)
	defer cleanupPlaces()
	people, cleanupPeople := ownedSocialPeopleFixture(t, ctx, pool, 3)
	defer cleanupPeople()
	store := postgres.New(pool, false)
	var organizationID, organizationAccountID string
	var activityIDs []string
	var sessionDigests [][]byte
	defer func() {
		principals := append([]string(nil), people...)
		if organizationAccountID != "" {
			principals = append(principals, organizationAccountID)
		}
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM agent_tasks WHERE owner_account_id=ANY($1::uuid[])`, principals)
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, principals)
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM activities WHERE id=ANY($1::uuid[])`, activityIDs)
		if organizationID != "" {
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM admin_audit_events WHERE organization_id=$1`, organizationID)
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM organizations WHERE id=$1`, organizationID)
		}
		for _, digest := range sessionDigests {
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM sessions WHERE token_sha256=$1`, digest)
		}
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM agents WHERE principal_account_id=ANY($1::uuid[])`, principals)
		if organizationAccountID != "" {
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM accounts WHERE id=$1`, organizationAccountID)
		}
	}()
	tokens := make([]string, len(people))
	for i, person := range people {
		if _, err := pool.Exec(ctx, `INSERT INTO agents(agent_type,principal_account_id) VALUES('personal',$1)`, person); err != nil {
			t.Fatal(err)
		}
		token, digest, err := identity.NewToken()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)
			VALUES($1,$2,'test',now()+interval '1 hour',now()+interval '30 minutes')`, person, digest[:]); err != nil {
			t.Fatal(err)
		}
		tokens[i] = token
		sessionDigests = append(sessionDigests, append([]byte(nil), digest[:]...))
	}
	org, err := store.CreateOrganization(ctx, people[0], organization.CreateInput{OrganizationType: "club", Name: "Synthetic response safety menu"})
	if err != nil {
		t.Fatal(err)
	}
	organizationID, organizationAccountID = org.ID, org.AccountID
	for _, visibility := range []string{"public", "invite_only"} {
		activity, err := store.CreateSocialDraft(ctx, people[1], activitypublish.Input{
			Organizer: activitypublish.Organizer{Type: "PERSON", ID: people[1]}, CityID: cityID,
			PlaceID: places[0], Title: "Synthetic response safety badminton " + visibility,
			Summary: "Local development fixture only", CategoryCode: "badminton", Visibility: visibility,
			StartsAt: time.Now().Add(time.Hour), EndsAt: time.Now().Add(2 * time.Hour), TimeZone: "Europe/London",
		})
		if err != nil {
			t.Fatal(err)
		}
		activityIDs = append(activityIDs, activity.ID)
		if _, err := store.PublishSocialActivity(ctx, people[1], activity.ID); err != nil {
			t.Fatal(err)
		}
		if visibility == "invite_only" {
			if err := store.InviteActivityPerson(ctx, people[1], activity.ID, people[0]); err != nil {
				t.Fatal(err)
			}
		}
	}
	s := &server{catalog: store, access: store, agent: store, organizations: store,
		newPeople: store, relationshipContext: store, participations: store, connections: store,
		socialIntents: store, activityPublish: store, socialActivityPublish: store, safety: store}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/cities/{cityID}/agent/tasks", s.createAgentTask)
	mux.HandleFunc("GET /v1/me/agent-tasks/{taskID}", s.getAgentTask)
	mux.HandleFunc("GET /v1/me/agent-tasks", s.listAgentTasks)
	call := func(token, workspace, method, path, body string, want int) []byte {
		t.Helper()
		code, response := httpActionSafetyCall(t, mux, token, workspace, method, path, body)
		if code != want {
			t.Fatalf("%s %s: %d want %d: %s", method, path, code, want, response)
		}
		if strings.Contains(string(response), "PRIVATE_FILTER_CANARY") {
			t.Fatalf("Task response leaked private filters: %s", response)
		}
		return response
	}
	decode := func(body []byte) agentworkspace.Results {
		t.Helper()
		var envelope struct {
			Data agentworkspace.Results `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data
	}
	contains := func(result agentworkspace.Results, id string) bool {
		for _, activity := range result.Activities {
			if activity.ID == id {
				return true
			}
		}
		return false
	}
	counts := func() [9]int {
		t.Helper()
		var count [9]int
		queries := []string{
			`SELECT count(*) FROM activity_participations WHERE participant_account_id=ANY($1::uuid[])`,
			`SELECT count(*) FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`,
			`SELECT count(*) FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[])`,
			`SELECT count(*) FROM incident_reports WHERE reporter_account_id=ANY($1::uuid[])`,
			`SELECT count(*) FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`,
			`SELECT count(*) FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`,
			`SELECT count(*) FROM activity_invitations WHERE invitee_account_id=ANY($1::uuid[]) OR invited_by_account_id=ANY($1::uuid[])`,
			`SELECT count(*) FROM activities WHERE created_by_account_id=ANY($1::uuid[]) OR host_account_id=ANY($1::uuid[])`,
			`SELECT count(*) FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`,
		}
		for i, query := range queries {
			if err := pool.QueryRow(ctx, query, people).Scan(&count[i]); err != nil {
				t.Fatal(err)
			}
		}
		return count
	}
	activityState := func() string {
		t.Helper()
		var state string
		if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('id',id,'status',publication_status,
			'visibility',visibility,'revision',revision,'cancelledAt',cancelled_at) ORDER BY id)::text,'[]')
			FROM activities WHERE id=ANY($1::uuid[])`, activityIDs).Scan(&state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	before := counts()
	beforeActivityState := activityState()
	path := "/v1/cities/" + cityID + "/agent/tasks"
	query := `{"query":"帮我找羽毛球活动"}`
	public := decode(call("", "", "POST", path, query, 200))
	if !contains(public, activityIDs[0]) || contains(public, activityIDs[1]) || public.Task != nil || len(public.Actions) != 0 {
		t.Fatal("anonymous public search changed visibility or acquired private Task/action", public)
	}
	own := decode(call(tokens[0], "", "POST", path, query, 200))
	if own.Task == nil || own.TaskID == "" || !contains(own, activityIDs[0]) || !contains(own, activityIDs[1]) {
		t.Fatal("own authorized public/private Activity search incompatible", own)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_tasks SET filters=filters ||
		'{"privateMemory":"PRIVATE_FILTER_CANARY","inferredPreference":"PRIVATE_FILTER_CANARY","reason":"PRIVATE_FILTER_CANARY"}'::jsonb WHERE id=$1`, own.TaskID); err != nil {
		t.Fatal(err)
	}
	getPath := "/v1/me/agent-tasks/" + own.TaskID
	restored := decode(call(tokens[0], "", "GET", getPath, "", 200))
	if restored.Task == nil || restored.Task.Filters["currentQuery"] != "帮我找羽毛球活动" ||
		!contains(restored, activityIDs[0]) || !contains(restored, activityIDs[1]) {
		t.Fatal("own Task restore lost explicit conditions or authorized private supply", restored)
	}
	call(tokens[2], "", "GET", getPath, "", 404)
	call("", "", "GET", getPath, "", 401)
	call(tokens[0], organizationID, "GET", getPath, "", 404)
	call(tokens[0], "", "GET", "/v1/me/agent-tasks", "", 200)
	var internal string
	if err := pool.QueryRow(ctx, `SELECT filters::text FROM agent_tasks WHERE id=$1`, own.TaskID).Scan(&internal); err != nil ||
		!strings.Contains(internal, "PRIVATE_FILTER_CANARY") || !strings.Contains(internal, "resultIDs") {
		t.Fatal("read-only response sanitizer rewrote internal restore/comparison data", internal, err)
	}
	body, _ := json.Marshal(map[string]string{"query": "近一点的呢？", "taskId": own.TaskID})
	refined := decode(call(tokens[0], "", "POST", path, string(body), 200))
	if refined.Task == nil || refined.Task.Filters["category"] != "badminton" || refined.Task.Filters["distancePreference"] != "closer" ||
		refined.TaskID != own.TaskID || !contains(refined, activityIDs[0]) || !contains(refined, activityIDs[1]) {
		t.Fatal("follow-up lost persisted context or authorized private supply", refined)
	}
	if err := pool.QueryRow(ctx, `SELECT filters::text FROM agent_tasks WHERE id=$1`, own.TaskID).Scan(&internal); err != nil ||
		!strings.Contains(internal, "resultIDs") || !strings.Contains(internal, "badminton") {
		t.Fatal("follow-up lost persisted comparison/category context", internal, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE activity_invitations SET status='revoked' WHERE activity_id=$1 AND invitee_account_id=$2`, activityIDs[1], people[0]); err != nil {
		t.Fatal(err)
	}
	revoked := decode(call(tokens[0], "", "GET", getPath, "", 200))
	if !contains(revoked, activityIDs[0]) || contains(revoked, activityIDs[1]) {
		t.Fatal("restore bypassed revoked private Activity authorization", revoked)
	}
	orgResult := decode(call(tokens[0], organizationID, "POST", path, `{"query":"帮我发布活动"}`, 200))
	if len(orgResult.Actions) != 1 || orgResult.Actions[0].TargetID != organizationID ||
		orgResult.Actions[0].Label != "去创建活动" || orgResult.PrincipalID != organizationAccountID || len(orgResult.Organizations) != 0 {
		t.Fatal("actual resolved Organization menu navigation regressed", orgResult)
	}
	orgRestored := decode(call(tokens[0], organizationID, "GET", "/v1/me/agent-tasks/"+orgResult.TaskID, "", 200))
	if len(orgRestored.Actions) != 1 || orgRestored.Actions[0].TargetID != organizationID {
		t.Fatal("Organization Task restore lost its authorized menu", orgRestored)
	}
	for _, words := range []string{"自动发送好友申请", "举报并屏蔽这个人", "帮我报名羽毛球活动，已确认", "帮我找新朋友并立即发送邀请"} {
		body, _ := json.Marshal(map[string]string{"query": words})
		call(tokens[0], "", "POST", path, string(body), 200)
	}
	if after := counts(); after != before {
		t.Fatalf("Agent queries/read/restore changed business state: before=%v after=%v", before, after)
	}
	if after := activityState(); after != beforeActivityState {
		t.Fatalf("Agent queries changed Activity publication/cancellation/revision: before=%s after=%s", beforeActivityState, after)
	}
}
