package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

func externalReplyFixture() (agentworkspace.Task, agentworkspace.OnlinePlaceSearch, time.Time) {
	_, search, at := externalCacheFixture()
	t := agentworkspace.Task{ID: currentReadonlyPGTask, PrincipalType: "person", PrincipalID: currentReadonlyPGOwner, ActingUserID: currentReadonlyPGOwner, CityID: search.CityID, ContextType: "CITY", Intent: agentworkspace.FindPlace, Status: agentworkspace.TaskActive, Query: search.Query, Filters: map[string]string{"currentQuery": search.Query, "category": "restaurant"}, Conversation: []agentworkspace.Message{{Role: "user", Text: search.Query}}}
	return t, search, at
}
func TestExternalPlaceReplyStoresRealAnswerAndOriginalBoundSnapshot(t *testing.T) {
	task, search, at := externalReplyFixture()
	got, changed, e := bindExternalPlaceReply(task, search, "可以先看看这家餐厅。", at)
	if e != nil || !changed || got.Status != agentworkspace.TaskCompleted || len(got.Conversation) != 2 {
		t.Fatal(got, changed, e)
	}
	m := got.Conversation[1]
	p := m.ExternalPlaceReply
	if m.Text != "可以先看看这家餐厅。" || p == nil || p.TaskID != task.ID || p.MessageIndex != 1 || p.TurnDigest != agentworkspace.OnlinePlaceTurnDigest(search.Query) || p.Places[0].ID != search.Places[0].ID || p.Query != search.Query {
		t.Fatal("reply lost exact round/place identity", m)
	}
	*search.Places[0].Location.Latitude = 1
	if *p.Places[0].Location.Latitude == 1 || len(task.Conversation) != 1 {
		t.Fatal("caller mutation changed persisted historical snapshot")
	}
	search, _ = copyExternalPlaceSearch(*p)
	retry, wrote, e := bindExternalPlaceReply(got, search, m.Text, at.Add(30*24*time.Hour))
	if e != nil || wrote || !reflect.DeepEqual(retry, got) {
		t.Fatal("completed retry duplicated/expired a retained reply", retry, wrote, e)
	}
}
func TestExternalPlaceReplyRejectsCrossOwnerCityQueryAndOldUserTurn(t *testing.T) {
	for _, change := range []func(*agentworkspace.Task, *agentworkspace.OnlinePlaceSearch){
		func(t *agentworkspace.Task, s *agentworkspace.OnlinePlaceSearch) { t.ActingUserID = "other" },
		func(t *agentworkspace.Task, s *agentworkspace.OnlinePlaceSearch) { t.PrincipalType = "organization" },
		func(t *agentworkspace.Task, s *agentworkspace.OnlinePlaceSearch) { s.CityID = "other-city" },
		func(t *agentworkspace.Task, s *agentworkspace.OnlinePlaceSearch) { s.TaskID = "other-task" },
		func(t *agentworkspace.Task, s *agentworkspace.OnlinePlaceSearch) { s.Query = "another query" },
		func(t *agentworkspace.Task, s *agentworkspace.OnlinePlaceSearch) {
			t.Conversation[0].Text = "another query"
		},
		func(t *agentworkspace.Task, s *agentworkspace.OnlinePlaceSearch) {
			t.Conversation[0].Role = "assistant"
		},
		func(t *agentworkspace.Task, s *agentworkspace.OnlinePlaceSearch) { s.SnapshotID = "" },
		func(t *agentworkspace.Task, s *agentworkspace.OnlinePlaceSearch) { s.TurnDigest = "wrong-digest" },
		func(t *agentworkspace.Task, s *agentworkspace.OnlinePlaceSearch) { s.MessageIndex = 99 },
		func(t *agentworkspace.Task, s *agentworkspace.OnlinePlaceSearch) { s.Origin = "HISTORY" },
		func(t *agentworkspace.Task, s *agentworkspace.OnlinePlaceSearch) { s.Freshness = "VERIFIED" },
	} {
		task, search, at := externalReplyFixture()
		change(&task, &search)
		if _, _, e := bindExternalPlaceReply(task, search, "结果", at); e == nil {
			t.Fatal("invalid private round accepted", task, search)
		}
	}
	task, search, at := externalReplyFixture()
	got, _, e := bindExternalPlaceReply(task, search, "原回答", at)
	if e != nil {
		t.Fatal(e)
	}
	search.Places[0].Name = "revised provider place"
	if _, _, e := bindExternalPlaceReply(got, search, "原回答", at); !errors.Is(e, arp.ErrChanged) {
		t.Fatal("old snapshot silently acquired refreshed source facts")
	}
}

type externalReplyAuthorityUnitTx struct {
	pgx.Tx
	session, current bool
	calls            int
}

func (tx *externalReplyAuthorityUnitTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	tx.calls++
	return currentReadonlyUnitRow{values: []any{tx.session, tx.current}}
}
func TestExternalPlaceReplyFinalNativeAuthorityRejectsRevocation(t *testing.T) {
	task, _, _ := externalReplyFixture()
	a := arp.Access{Actor: identity.Actor{ID: task.PrincipalID, AccountType: "person"}, SessionDigest: [32]byte{1}, TaskID: task.ID}
	for _, tc := range []struct {
		session, current bool
		want             error
	}{{true, true, nil}, {false, true, identity.ErrUnauthorized}, {true, false, arp.ErrChanged}} {
		tx := &externalReplyAuthorityUnitTx{session: tc.session, current: tc.current}
		e := finalExternalPlaceReplyAuthority(context.Background(), tx, a, task, "agent-unit", false)
		if !errors.Is(e, tc.want) || tx.calls != 1 {
			t.Fatal("late revoke/source change released owned snapshot", e, tc)
		}
	}
}

func TestExternalPlaceReplyNativeExpectedTaskCASRejectsLateCapturedTurnBeforeWrite(t *testing.T) {
	captured, _, at := externalReplyFixture()
	captured.CreatedAt, captured.UpdatedAt = at, at
	raw, e := json.Marshal(agentworkspace.SanitizeTaskForResponse(captured))
	if e != nil {
		t.Fatal(e)
	}
	a := arp.Access{Actor: identity.Actor{ID: captured.PrincipalID, AccountType: "person"}, SessionDigest: [32]byte{1}, TaskID: captured.ID, ExpectedTask: raw}
	current := captured
	current.Filters = map[string]string{"currentQuery": "推荐附近的餐厅", "category": "restaurant", "locationPreference": "viewport"}
	current.Conversation = append(append([]agentworkspace.Message(nil), current.Conversation...), agentworkspace.Message{Role: "user", Text: "推荐附近的餐厅"})
	current.UpdatedAt = at.Add(time.Second)
	before, e := json.Marshal(current)
	if e != nil {
		t.Fatal(e)
	}
	tx := &currentReadonlySearchTx{task: current}
	// This is the exact original native selector used by the new completion
	// port before bindExternalPlaceReply or updateTaskInTx can be reached.
	if _, e = ownReplyTaskTx(context.Background(), tx, a, true); !errors.Is(e, arp.ErrChanged) {
		t.Fatal("late provider result accepted an obsolete captured Task", e)
	}
	after, _ := json.Marshal(tx.task)
	if string(before) != string(after) || !reflect.DeepEqual(tx.calls, []string{"task"}) {
		t.Fatal("stale expected Task changed the current private turn or reached a write", tx.calls)
	}
	// Even a same-text edit changes the owned snapshot: ABA and a timestamp
	// change must not be collapsed into an authorized query-string match.
	tx = &currentReadonlySearchTx{task: captured}
	tx.task.UpdatedAt = at.Add(time.Second)
	if _, e = ownReplyTaskTx(context.Background(), tx, a, true); !errors.Is(e, arp.ErrChanged) || !reflect.DeepEqual(tx.calls, []string{"task"}) {
		t.Fatal("same-text new Task version borrowed the captured completion", e, tx.calls)
	}
}

func TestExternalPlaceReplyFollowupPreparesAndBindsBothMessagesWithoutChangingOriginal(t *testing.T) {
	task, search, at := externalReplyFixture()
	search.CacheVersion = 1
	completed, _, err := bindExternalPlaceReply(task, search, "原餐厅回答", at)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(completed)
	query := "第一家地址"
	nextSearch, _ := copyExternalPlaceSearch(search)
	nextSearch.Query, nextSearch.TurnDigest, nextSearch.SnapshotID, nextSearch.Origin = query, agentworkspace.OnlinePlaceTurnDigest(query), "45e55ac7-7107-465c-a05d-74c22c0578bd", "CACHE"
	active, err := prepareExternalPlaceFollowup(completed, query, nextSearch)
	if err != nil || active.Status != agentworkspace.TaskActive || len(active.Conversation) != 3 || active.Conversation[2].Text != query || active.Filters["currentQuery"] != query {
		t.Fatal("followup did not prepare the exact new user turn", active, err)
	}
	got, changed, err := bindExternalPlaceReply(active, nextSearch, "来源地址回答", at.Add(time.Minute))
	if err != nil || !changed || got.Status != agentworkspace.TaskCompleted || len(got.Conversation) != 4 || got.Conversation[3].ExternalPlaceReply.MessageIndex != 3 || got.Conversation[3].ExternalPlaceReply.Query != query || got.Conversation[1].Text != "原餐厅回答" || got.Conversation[1].ExternalPlaceReply.SnapshotID != search.SnapshotID {
		t.Fatal("two-message atomic followup lost original round binding", got, err)
	}
	after, _ := json.Marshal(completed)
	if string(before) != string(after) {
		t.Fatal("preparation mutated caller's captured old Task before native CAS")
	}
}

func TestExternalPlaceReplyFollowupRejectsReplacementScopeVersionFieldsAndQuery(t *testing.T) {
	for name, change := range map[string]func(*agentworkspace.Task, *agentworkspace.OnlinePlaceSearch){
		"active task": func(t *agentworkspace.Task, p *agentworkspace.OnlinePlaceSearch) {
			t.Status = agentworkspace.TaskActive
		},
		"different context": func(t *agentworkspace.Task, p *agentworkspace.OnlinePlaceSearch) { t.ContextType = "ONLINE" },
		"new activity": func(t *agentworkspace.Task, p *agentworkspace.OnlinePlaceSearch) {
			t.Intent = agentworkspace.FindActivity
		},
		"different actor": func(t *agentworkspace.Task, p *agentworkspace.OnlinePlaceSearch) { t.ActingUserID = "other" },
		"old query":       func(t *agentworkspace.Task, p *agentworkspace.OnlinePlaceSearch) { p.Query = "another question" },
		"wrong city":      func(t *agentworkspace.Task, p *agentworkspace.OnlinePlaceSearch) { p.CityID = "other-city" },
		"version changed": func(t *agentworkspace.Task, p *agentworkspace.OnlinePlaceSearch) { p.CacheVersion++ },
		"scope changed":   func(t *agentworkspace.Task, p *agentworkspace.OnlinePlaceSearch) { p.Scope = "viewport" },
		"name changed":    func(t *agentworkspace.Task, p *agentworkspace.OnlinePlaceSearch) { p.Places[0].Name = "new name" },
		"new coordinates": func(t *agentworkspace.Task, p *agentworkspace.OnlinePlaceSearch) { *p.Places[0].Location.Latitude = 1 },
		"observation changed": func(t *agentworkspace.Task, p *agentworkspace.OnlinePlaceSearch) {
			p.ObservedAt = p.ObservedAt.Add(time.Second)
		},
		"old message binding": func(t *agentworkspace.Task, p *agentworkspace.OnlinePlaceSearch) {
			t.Conversation[1].ExternalPlaceReply.MessageIndex = 99
		},
	} {
		t.Run(name, func(t *testing.T) {
			task, search, at := externalReplyFixture()
			search.CacheVersion = 1
			completed, _, err := bindExternalPlaceReply(task, search, "原回答", at)
			if err != nil {
				t.Fatal(err)
			}
			query := "第一家地址"
			search.Query, search.TurnDigest = query, agentworkspace.OnlinePlaceTurnDigest(query)
			change(&completed, &search)
			before, _ := json.Marshal(completed)
			if _, err := prepareExternalPlaceFollowup(completed, query, search); !errors.Is(err, arp.ErrChanged) {
				t.Fatal("changed owned context acquired a new completed reply", err)
			}
			after, _ := json.Marshal(completed)
			if string(before) != string(after) {
				t.Fatal("rejected followup mutated original history")
			}
		})
	}
}
