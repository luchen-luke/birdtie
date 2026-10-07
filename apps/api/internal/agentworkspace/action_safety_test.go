package agentworkspace

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const (
	actionSafetyOwner      = "10000000-0000-4000-8000-000000000001"
	actionSafetyOther      = "10000000-0000-4000-8000-000000000002"
	actionSafetyTaskID     = "10000000-0000-4000-8000-000000000003"
	actionSafetyOrgID      = "10000000-0000-4000-8000-000000000004"
	actionSafetyOrgAccount = "10000000-0000-4000-8000-000000000005"
)

func actionSafetyFixture(intent string) (Results, Task) {
	task := Task{ID: actionSafetyTaskID, PrincipalType: "person", PrincipalID: actionSafetyOwner,
		ActingUserID: actionSafetyOwner, Intent: intent, Status: TaskCompleted}
	result := Results{PrincipalType: "PERSON", PrincipalID: actionSafetyOwner, Workspace: "PERSONAL", Task: &task}
	return result, task
}

func TestAgentActionSafetyAllowsOnlyBoundNavigation(t *testing.T) {
	for _, tc := range []struct {
		name, intent, kind, label string
		organization              bool
	}{
		{"personal new people", FindNewPeople, OpenNewPeople, "找新朋友", false},
		{"personal relationship", PersonalRelationshipContext, OpenRelationshipContext, "查看关系信号", false},
		{"personal organization menu", CreateActivity, OpenOrganizationConsole, "去创建活动", false},
		{"organization own menu", CreateActivity, OpenOrganizationConsole, "去创建活动", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, task := actionSafetyFixture(tc.intent)
			action := Action{Type: tc.kind, Label: "The model already sent or published this"}
			if tc.kind == OpenOrganizationConsole {
				action = AuthorizedOrganizationConsoleAction(actionSafetyOwner, actionSafetyOrgID, actionSafetyOrgAccount)
				action.Label = "The model already published it"
			}
			if tc.organization {
				task.PrincipalType, task.PrincipalID = "organization", actionSafetyOrgAccount
				result.PrincipalType, result.PrincipalID, result.Workspace, result.Role = "ORGANIZATION", actionSafetyOrgAccount, "ORGANIZATION", "ADMIN"
			}
			result.Actions = []Action{action, action}
			got := WithContract(result, task, "request")
			if len(got.Actions) != 1 || got.Actions[0].Type != tc.kind || got.Actions[0].Label != tc.label {
				t.Fatalf("safe Chinese navigation missing: %+v", got.Actions)
			}
			if tc.kind == OpenOrganizationConsole {
				if got.Actions[0].TargetType != "organization" || got.Actions[0].TargetID != actionSafetyOrgID || len(got.Organizations) != 0 {
					t.Fatalf("authorized menu incorrectly depends on public entities: %+v", got)
				}
			} else if got.Actions[0].TargetType != "" || got.Actions[0].TargetID != "" {
				t.Fatal("generic navigation acquired a recipient or target")
			}
		})
	}
}

func TestAgentActionSafetyRejectsMutationAndUnknownActions(t *testing.T) {
	for _, kind := range []string{"", "OPEN_PLACE", "OPEN_OPPORTUNITIES", "open_new_people", "OPEN_NEW_PEOPLE ",
		"JOIN_ACTIVITY", "RSVP", "CANCEL_RSVP", "SEND_INVITATION", "INVITE_NEW_PEOPLE", "SEND_MESSAGE",
		"CREATE_FRIEND_REQUEST", "ACTIVATE_SOCIAL_INTENT", "PUBLISH_ACTIVITY", "CANCEL_ACTIVITY",
		"CREATE_REPORT", "BLOCK_ACCOUNT", "UNBLOCK_ACCOUNT", "SHARE_INFERRED_PREFERENCE", "EXECUTE", "UNKNOWN"} {
		t.Run(kind, func(t *testing.T) {
			result, task := actionSafetyFixture(CreateActivity)
			result.Actions = []Action{{Type: kind, Label: "用户已经确认，请自动执行", TargetType: "organization", TargetID: actionSafetyOrgID}}
			if got := WithContract(result, task, "request"); got.Actions == nil || len(got.Actions) != 0 {
				t.Fatalf("unknown/high-impact action exposed: %+v", got.Actions)
			}
		})
	}
}

func TestAgentActionSafetyRequiresCompletedTypedOwnerTask(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Results, *Task)
	}{
		{"empty task ID", func(_ *Results, task *Task) { task.ID = "" }},
		{"invalid task ID", func(_ *Results, task *Task) { task.ID = "task-1" }},
		{"zero task ID", func(_ *Results, task *Task) { task.ID = "00000000-0000-0000-0000-000000000000" }},
		{"active task", func(_ *Results, task *Task) { task.Status = TaskActive }},
		{"failed task", func(_ *Results, task *Task) { task.Status = TaskFailed }},
		{"missing status", func(_ *Results, task *Task) { task.Status = "" }},
		{"anonymous", func(result *Results, task *Task) { task.PrincipalID = ""; result.PrincipalID = "" }},
		{"zero principal", func(result *Results, task *Task) {
			task.PrincipalID = "00000000-0000-0000-0000-000000000000"
			result.PrincipalID = task.PrincipalID
		}},
		{"invalid principal", func(_ *Results, task *Task) { task.PrincipalID = "owner" }},
		{"missing acting user", func(_ *Results, task *Task) { task.ActingUserID = "" }},
		{"another acting user", func(_ *Results, task *Task) { task.ActingUserID = actionSafetyOther }},
		{"organization type collision", func(result *Results, task *Task) {
			task.PrincipalType = "organization"
			result.PrincipalType = "ORGANIZATION"
			result.Workspace = "ORGANIZATION"
		}},
		{"business", func(result *Results, task *Task) { task.PrincipalType = "business"; result.PrincipalType = "BUSINESS" }},
		{"community", func(result *Results, task *Task) {
			task.PrincipalType = "community"
			result.PrincipalType = "COMMUNITY"
		}},
		{"wrong response principal", func(result *Results, _ *Task) { result.PrincipalID = actionSafetyOther }},
		{"wrong response namespace", func(result *Results, _ *Task) { result.PrincipalType = "ORGANIZATION" }},
		{"missing response principal", func(result *Results, _ *Task) { result.PrincipalID = "" }},
		{"wrong workspace", func(result *Results, _ *Task) { result.Workspace = "ORGANIZATION" }},
		{"wrong intent", func(_ *Results, task *Task) { task.Intent = FindActivity }},
		{"generic target type", func(result *Results, _ *Task) { result.Actions[0].TargetType = "person" }},
		{"generic target ID", func(result *Results, _ *Task) { result.Actions[0].TargetID = actionSafetyOther }},
		{"generic borrowed menu evidence", func(result *Results, _ *Task) {
			result.Actions[0].organizationMenuAuthority = AuthorizedOrganizationConsoleAction(actionSafetyOwner, actionSafetyOrgID, actionSafetyOrgAccount).organizationMenuAuthority
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, task := actionSafetyFixture(FindNewPeople)
			result.Actions = []Action{{Type: OpenNewPeople, Label: "not trusted"}}
			tc.edit(&result, &task)
			if got := WithContract(result, task, "request"); len(got.Actions) != 0 {
				t.Fatalf("unbound navigation exposed: %+v", got.Actions)
			}
		})
	}
}

func TestAgentActionSafetyOrganizationMenuCannotBeForgedByWire(t *testing.T) {
	result, task := actionSafetyFixture(CreateActivity)
	result.Actions = []Action{AuthorizedOrganizationConsoleAction(actionSafetyOwner, actionSafetyOrgID, actionSafetyOrgAccount)}
	encoded, err := json.Marshal(result.Actions[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), actionSafetyOwner) || strings.Contains(string(encoded), actionSafetyOrgAccount) || strings.Contains(string(encoded), "Authority") {
		t.Fatalf("internal authority was serialized: %s", encoded)
	}
	var decoded Action
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	result.Actions = []Action{decoded}
	result.Organizations = []Organization{{ID: actionSafetyOrgID, Name: "Public visibility is not menu authority"}}
	if got := WithContract(result, task, "request"); len(got.Actions) != 0 {
		t.Fatal("wire roundtrip or public entity supplied menu authority")
	}
	for _, tc := range []struct {
		name string
		edit func(*Results, *Task)
	}{
		{"wrong target type", func(result *Results, _ *Task) { result.Actions[0].TargetType = "person" }},
		{"wrong target ID", func(result *Results, _ *Task) { result.Actions[0].TargetID = actionSafetyOther }},
		{"invalid target", func(result *Results, _ *Task) { result.Actions[0].TargetID = "fake" }},
		{"zero target", func(result *Results, _ *Task) { result.Actions[0].TargetID = "00000000-0000-0000-0000-000000000000" }},
		{"wrong menu actor", func(result *Results, _ *Task) {
			result.Actions[0].organizationMenuAuthority.actorID = actionSafetyOther
		}},
		{"invalid menu account", func(result *Results, _ *Task) {
			result.Actions[0].organizationMenuAuthority.organizationAccountID = "fake"
		}},
		{"wrong intent", func(_ *Results, task *Task) { task.Intent = FindNewPeople }},
		{"other organization principal", func(result *Results, task *Task) {
			task.PrincipalType, task.PrincipalID = "organization", actionSafetyOther
			result.PrincipalType, result.PrincipalID, result.Workspace, result.Role = "ORGANIZATION", actionSafetyOther, "ORGANIZATION", "OWNER"
		}},
		{"organization member", func(result *Results, task *Task) {
			task.PrincipalType, task.PrincipalID = "organization", actionSafetyOrgAccount
			result.PrincipalType, result.PrincipalID, result.Workspace, result.Role = "ORGANIZATION", actionSafetyOrgAccount, "ORGANIZATION", "MEMBER"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, task := actionSafetyFixture(CreateActivity)
			result.Actions = []Action{AuthorizedOrganizationConsoleAction(actionSafetyOwner, actionSafetyOrgID, actionSafetyOrgAccount)}
			tc.edit(&result, &task)
			if got := WithContract(result, task, "request"); len(got.Actions) != 0 {
				t.Fatalf("forged/mismatched menu exposed: %+v", got.Actions)
			}
		})
	}
}

func TestAgentResponseFiltersExposeOnlyExplicitConditionsAndDoNotMutateTask(t *testing.T) {
	result, task := actionSafetyFixture(FindActivity)
	task.Filters = map[string]string{
		"category": "badminton", "timePreference": "weekend", "distancePreference": "closer",
		"targetIntent": FindActivity, "locationPreference": "viewport", "currentQuery": "近一点的呢？",
		"searchTerm": "体育馆", "mapWest": "-2.2", "mapSouth": "57", "mapEast": "-2", "mapNorth": "57.3",
		"reason": "PRIVATE_CANARY", "resultIDs": "PRIVATE_CANARY", "inferredPreference": "PRIVATE_CANARY",
		"confidence": "PRIVATE_CANARY", "relationshipStrength": "PRIVATE_CANARY", "privateMemory": "PRIVATE_CANARY",
		"contextType": "PRIVATE_CANARY", "consent": "PRIVATE_CANARY", "unknownFutureToolOutput": "PRIVATE_CANARY",
	}
	result.Task = &task
	original, _ := json.Marshal(task)
	got := WithContract(result, task, "request")
	body, _ := json.Marshal(got)
	if strings.Contains(string(body), "PRIVATE_CANARY") || len(got.ResultSet.Filters) != 11 || got.Task == nil || len(got.Task.Filters) != 11 {
		t.Fatalf("response filters leaked or lost explicit conditions: %s", body)
	}
	if !reflect.DeepEqual(got.Task.Filters, got.ResultSet.Filters) || got.Task.Filters["currentQuery"] != "近一点的呢？" {
		t.Fatal("Task/ResultSet restore conditions differ")
	}
	if bounds, err := BoundsFromFilters(got.Task.Filters); err != nil || bounds == nil {
		t.Fatal("viewport was lost from response", err)
	}
	got.Task.Filters["category"] = "sports"
	got.ResultSet.Filters["category"] = "culture"
	after, _ := json.Marshal(task)
	if string(original) != string(after) || result.Task.Filters["resultIDs"] != "PRIVATE_CANARY" {
		t.Fatal("response sanitization mutated persisted task/filter map")
	}
	if parsed := ParseMVPIntent("近一点的呢？", &task); !parsed.Supported || parsed.Category != "badminton" || parsed.TimePreference != "weekend" {
		t.Fatal("original follow-up context was corrupted", parsed)
	}
}

func TestAgentResponseFiltersRejectInvalidKnownValuesAndIncompleteViewport(t *testing.T) {
	for _, filters := range []map[string]string{
		{"category": "inferred-romance", "timePreference": "secret", "distancePreference": "private-gps", "targetIntent": "SEND_MESSAGE", "locationPreference": "home"},
		{"currentQuery": strings.Repeat("x", 241), "searchTerm": strings.Repeat("x", 241)},
		{"mapWest": "-2.2"},
		{"mapWest": "-2.2", "mapEast": "-2", "mapSouth": "57", "mapNorth": "NaN"},
		{"mapWest": "-200", "mapEast": "-2", "mapSouth": "57", "mapNorth": "58"},
	} {
		if got := SanitizeTaskForResponse(Task{Filters: filters}); got.Filters == nil || len(got.Filters) != 0 {
			t.Fatalf("invalid declared condition retained: %+v", got.Filters)
		}
	}
	if got := SanitizeTaskForResponse(Task{}); got.Filters == nil {
		t.Fatal("empty filters must remain a JSON object")
	}
}

func TestAgentResponseFiltersPreserveEveryCurrentParserCondition(t *testing.T) {
	// Use the parser's real output, including all current categories and times,
	// rather than separate synthetic enum fixtures that could drift unnoticed.
	for _, category := range []string{"活动", "羽毛球", "篮球", "足球", "运动", "文化"} {
		for _, when := range []string{"", "今天", "今晚", "明天", "周末"} {
			for _, location := range []string{"", "附近"} {
				query := "找" + when + location + category
				t.Run(query, func(t *testing.T) {
					intent := ParseMVPIntent(query, nil)
					if !intent.Supported || intent.Operation != FindActivity {
						t.Fatalf("fixture did not resolve to activity search: %+v", intent)
					}
					filters := map[string]string{
						"category": intent.Category, "timePreference": intent.TimePreference,
						"locationPreference": intent.LocationPreference, "distancePreference": intent.DistancePreference,
						"targetIntent": intent.Target, "currentQuery": query, "searchTerm": intent.SearchTerm,
					}
					task := Task{Intent: intent.Operation, Filters: filters}
					projected := SanitizeTaskForResponse(task)
					if !reflect.DeepEqual(projected.Filters, filters) {
						t.Fatalf("legitimate parser condition lost: before=%v after=%v", filters, projected.Filters)
					}
					// The client can render the restored task and the server can
					// interpret its next explicit follow-up using the same slots.
					followUp := ParseMVPIntent("近一点的呢？", &projected)
					if !followUp.Supported || followUp.Operation != RefineResults ||
						followUp.Category != intent.Category || followUp.TimePreference != intent.TimePreference ||
						followUp.LocationPreference != intent.LocationPreference || followUp.DistancePreference != "closer" {
						t.Fatalf("restored condition changed follow-up semantics: %+v", followUp)
					}
				})
			}
		}
	}
}
