package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
)

func replyResultsHTTPEntry(t *testing.T, result agentworkspace.Results, index int, refs []arp.Ref) agentworkspace.MessageResult {
	t.Helper()
	if result.Task == nil || index < 0 || index >= len(result.Task.Conversation) {
		t.Fatal("missing original stored assistant turn")
	}
	membership := result.Task.Conversation[index].ResultMembership
	if membership == nil || agentworkspace.ValidateReplyMembership(*result.Task, index) != nil {
		t.Fatal("original membership did not survive registered response", index)
	}
	var entry agentworkspace.MessageResult
	found := 0
	for _, candidate := range result.MessageResults {
		if candidate.MessageIndex == index {
			entry = candidate
			found++
		}
	}
	if found != 1 || !reflect.DeepEqual(entry.ResultSet.Entities, refs) || !reflect.DeepEqual(arp.Refs(entry.ResultSet.Items), refs) {
		t.Fatalf("turn %d acquired another result: count=%d refs=%v want=%v", index, found, entry.ResultSet.Entities, refs)
	}
	if entry.TurnDigest != membership.TurnDigest || entry.ResultSet.ID != membership.ResultSetID ||
		entry.ResultSet.TaskID != result.Task.ID || entry.ResultSet.CityID != result.Task.CityID ||
		entry.ResultSet.Schema != arp.Schema || arp.ValidateItems(entry.ResultSet.Items) != nil ||
		!entry.ValidUntil.After(entry.ResultSet.GeneratedAt) || entry.ValidUntil.Sub(entry.ResultSet.GeneratedAt) > 30*time.Second ||
		entry.MapEffects.Camera != "preserve" || !reflect.DeepEqual(entry.MapEffects.PinEntityIDs, arp.PinIDs(entry.ResultSet.Items)) {
		t.Fatalf("turn %d lost its original binding/current native lease: %+v", index, entry)
	}
	if entry.ResultSet.AnswerBinding != nil || entry.ResultSet.PublicFieldEvidence != nil || len(entry.ResultSet.Sources) != 0 ||
		entry.ResultSet.Query != "" || len(entry.ResultSet.Filters) != 0 {
		t.Fatal("history read revived answer/model context", index)
	}
	return entry
}

func TestAgentReplyResultsHTTPRegisteredRulesRestartKeepsOriginalTurns(t *testing.T) {
	f := contextBuilderHTTPNative(t)
	// No WithNowLiveAnswers dependency is installed. These are the registered
	// ordinary rules paths and original native stores, with no provider calls.
	f.handler = contextBuilderHTTPNew(f.store, f.store, f.store)
	path := "/v1/cities/" + f.city + "/agent/tasks"
	first := resultHTTPReply(t, f.request(t, f.handler, http.MethodPost, path, `{"query":"找地点"}`, f.tokens[0], http.StatusOK, nil))
	resultHTTPFind(t, first, "place", f.place)
	if len(first.Task.Conversation) != 2 || len(first.MessageResults) != 1 || first.Mode != "rules" {
		t.Fatal("first native rules reply was not one bound stored turn", first)
	}
	firstRefs := append([]arp.Ref{}, first.ResultSet.Entities...)
	firstEntry := replyResultsHTTPEntry(t, first, 1, firstRefs)
	firstText := first.Task.Conversation[1].Text
	for _, ref := range firstRefs {
		if ref.Type != "place" {
			t.Fatal("place turn contains another source kind", ref)
		}
	}
	secondBody := fmt.Sprintf(`{"taskId":%q,"query":"帮我找羽毛球活动"}`, first.Task.ID)
	second := resultHTTPReply(t, f.request(t, f.handler, http.MethodPost, path, secondBody, f.tokens[0], http.StatusOK, nil))
	resultHTTPFind(t, second, "activity", f.public)
	resultHTTPFind(t, second, "activity", f.private)
	if second.Task.ID != first.Task.ID || len(second.Task.Conversation) != 4 || len(second.MessageResults) != 2 || second.Mode != "rules" {
		t.Fatal("registered follow-up created another task or lost a turn", second)
	}
	secondRefs := append([]arp.Ref{}, second.ResultSet.Entities...)
	secondEntry := replyResultsHTTPEntry(t, second, 3, secondRefs)
	if reflect.DeepEqual(firstRefs, secondRefs) || second.Task.Conversation[1].Text != firstText ||
		second.ResultSet.ID != secondEntry.ResultSet.ID || !strings.Contains(second.Task.Conversation[3].Text, "当前可见活动") {
		t.Fatal("native reply/card identity or invited activity wording diverged")
	}
	replyResultsHTTPEntry(t, second, 1, firstRefs)

	// A new Store/server has no process-local original reply snapshots. Restore
	// must use each stored membership and the original current native reader.
	restarted := postgres.New(f.pool, false)
	restartedHandler := contextBuilderHTTPNew(restarted, restarted, restarted)
	getPath := "/v1/me/agent-tasks/" + first.Task.ID
	restored := resultHTTPReply(t, f.request(t, restartedHandler, http.MethodGet, getPath, "", f.tokens[0], http.StatusOK, nil))
	old := replyResultsHTTPEntry(t, restored, 1, firstRefs)
	current := replyResultsHTTPEntry(t, restored, 3, secondRefs)
	if len(restored.MessageResults) != 2 || old.ResultSet.ID != firstEntry.ResultSet.ID || current.ResultSet.ID != secondEntry.ResultSet.ID ||
		restored.ResultSet.ID != current.ResultSet.ID || !reflect.DeepEqual(restored.ResultSet.Entities, secondRefs) ||
		restored.Task.Conversation[1].Text != firstText {
		t.Fatal("restart attached current activity projection to earlier place reply", restored)
	}

	starts := time.Now().UTC().Add(time.Hour)
	third, err := f.store.CreateSocialDraft(f.ctx, f.accountIDs[1], activitypublish.Input{
		Organizer: activitypublish.Organizer{Type: "PERSON", ID: f.accountIDs[1]}, CityID: f.city, PlaceID: f.place,
		Title: "合成羽毛球 reply 后新增活动", Summary: "仅用于证明新匹配不能加入旧轮次", CategoryCode: "badminton",
		Visibility: "public", StartsAt: starts, EndsAt: starts.Add(time.Hour), TimeZone: "Europe/London",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.PublishSocialActivity(f.ctx, f.accountIDs[1], third.ID); err != nil {
		t.Fatal(err)
	}
	// Prove C really matches the current registered query rather than relying on
	// an irrelevant new entity that a fresh search would not return anyway.
	freshQuery := resultHTTPReply(t, f.request(t, restartedHandler, http.MethodPost, path, `{"query":"帮我找羽毛球活动"}`, f.tokens[0], http.StatusOK, nil))
	resultHTTPFind(t, freshQuery, "activity", third.ID)
	afterPublish := resultHTTPReply(t, f.request(t, restartedHandler, http.MethodGet, getPath, "", f.tokens[0], http.StatusOK, nil))
	replyResultsHTTPEntry(t, afterPublish, 1, firstRefs)
	last := replyResultsHTTPEntry(t, afterPublish, 3, secondRefs)
	if afterPublish.ResultSet.ID != secondEntry.ResultSet.ID || !reflect.DeepEqual(afterPublish.ResultSet.Entities, secondRefs) ||
		!reflect.DeepEqual(afterPublish.ResultSet.Items, last.ResultSet.Items) {
		t.Fatal("main result stopped using the saved latest membership", afterPublish.ResultSet)
	}
	for _, ref := range afterPublish.ResultSet.Entities {
		if ref.ID == third.ID {
			t.Fatal("new matching activity joined saved latest reply")
		}
	}

	// Seed an old citation-only representation by removing only membership.
	// Role/text remain unchanged, so the later turn's digest must stay valid.
	legacyTask, err := restarted.GetTask(f.ctx, f.accountIDs[0], first.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	legacyTask.Conversation[1].ResultMembership = nil
	legacyTask.Conversation[1].Sources = []agentworkspace.AnswerSource{{ID: "legacy-public-source", Title: "原回答官网来源", URL: "https://example.org/original-answer"}}
	legacyTask.Conversation[1].SourceRunID = "legacy-source-run"
	legacyTask.Conversation[1].SourceEvidenceDigest = strings.Repeat("a", 64)
	if _, err = restarted.UpdateTask(f.ctx, legacyTask); err != nil {
		t.Fatal(err)
	}
	legacyRestart := postgres.New(f.pool, false)
	legacyHandler := contextBuilderHTTPNew(legacyRestart, legacyRestart, legacyRestart)
	legacy := resultHTTPReply(t, f.request(t, legacyHandler, http.MethodGet, getPath, "", f.tokens[0], http.StatusOK, nil))
	if len(legacy.MessageResults) != 1 || legacy.MessageResults[0].MessageIndex != 3 ||
		legacy.Task.Conversation[1].ResultMembership != nil || legacy.Task.Conversation[1].Text != firstText ||
		!reflect.DeepEqual(legacy.Task.Conversation[1].Sources, legacyTask.Conversation[1].Sources) ||
		legacy.Task.Conversation[1].SourceRunID != "legacy-source-run" || legacy.Task.Conversation[1].SourceEvidenceDigest != strings.Repeat("a", 64) {
		t.Fatal("legacy text/citation was retrofitted with current native result", legacy)
	}
	replyResultsHTTPEntry(t, legacy, 3, secondRefs)
	f.request(t, legacyHandler, http.MethodGet, getPath, "", f.tokens[1], http.StatusNotFound, nil)
	t.Logf("LOCAL_DISPOSABLE_SYNTHETIC_ONLY nil_live_provider=true same_task=%s original_place_refs=%d original_activity_refs=%d new_matching_activity=%s", first.Task.ID, len(firstRefs), len(secondRefs), third.ID)
}

type replyResultsHTTPFinalBarrier struct {
	*postgres.Store
	before        func()
	reads         int
	revalidations int
}

var _ agentworkspace.HumanReplyResultsPort = (*replyResultsHTTPFinalBarrier)(nil)

func (b *replyResultsHTTPFinalBarrier) ReadOwnMessageResults(ctx context.Context, access arp.Access) (agentworkspace.MessageResultsRead, error) {
	b.reads++
	read, err := b.Store.ReadOwnMessageResults(ctx, access)
	if err != nil {
		return nil, err
	}
	return &replyResultsHTTPBufferedRead{read: read, barrier: b}, nil
}

type replyResultsHTTPBufferedRead struct {
	read    agentworkspace.MessageResultsRead
	barrier *replyResultsHTTPFinalBarrier
}

func (h *replyResultsHTTPBufferedRead) Results() []agentworkspace.MessageResult {
	return h.read.Results()
}

func (h *replyResultsHTTPBufferedRead) Revalidate(ctx context.Context) error {
	h.barrier.revalidations++
	if h.barrier.before != nil {
		h.barrier.before()
	}
	return h.read.Revalidate(ctx)
}

func TestAgentReplyResultsHTTPFinalBufferedNativeRevokeRejectsEntityBody(t *testing.T) {
	f := contextBuilderHTTPNative(t)
	f.handler = contextBuilderHTTPNew(f.store, f.store, f.store)
	first := resultHTTPReply(t, f.request(t, f.handler, http.MethodPost, "/v1/cities/"+f.city+"/agent/tasks", `{"query":"找地点"}`, f.tokens[0], http.StatusOK, nil))
	place := resultHTTPFind(t, first, "place", f.place)
	if len(first.MessageResults) != 1 {
		t.Fatal("missing original native place membership")
	}
	bridge := &replyResultsHTTPFinalBarrier{Store: postgres.New(f.pool, false)}
	bridge.before = func() {
		// This hook runs inside the returned private handle's final revalidation,
		// after the registered responder has encoded its success body to a buffer.
		f.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place)
	}
	handler := New(f.store, f.store, nil, f.store, bridge, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil)
	denied := f.request(t, handler, http.MethodGet, "/v1/me/agent-tasks/"+first.Task.ID, "", f.tokens[0], http.StatusConflict, nil)
	if bridge.reads != 1 || bridge.revalidations != 1 {
		t.Fatal("registered GET bypassed returned history final barrier", bridge.reads, bridge.revalidations)
	}
	var failure struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(denied.Body.Bytes(), &failure) != nil || failure.Error.Code != "result_source_changed" {
		t.Fatal("late native revocation lost its typed failure", denied.Body.String())
	}
	for _, stale := range []string{f.place, place.Title, `"data"`, `"items"`, `"messageResults"`, `"resultMembership"`} {
		if strings.Contains(denied.Body.String(), stale) {
			t.Fatal("encoded historical entity escaped final revalidation", stale, denied.Body.String())
		}
	}
}

// Preserve the original native final-barrier fixture's mutation assertions on
// the added history interface. Its original tests and assertions are unchanged.
func (b *resultHTTPFinalBarrier) ReadOwnMessageResults(ctx context.Context, a arp.Access) (agentworkspace.MessageResultsRead, error) {
	read, e := b.Store.ReadOwnMessageResults(ctx, a)
	if e != nil {
		return nil, e
	}
	var task agentworkspace.Task
	if json.Unmarshal(a.ExpectedTask, &task) != nil {
		return nil, arp.ErrDenied
	}
	q := arp.Query{CityID: task.CityID, Kind: nativeResultKind(task.Intent), SearchTerm: task.Filters["searchTerm"], Category: task.Filters["category"], TimePreference: task.Filters["timePreference"], Closer: task.Filters["distancePreference"] == "closer", CompareIDs: []string{}}
	bounds, e := agentworkspace.BoundsFromFilters(task.Filters)
	if e != nil {
		return nil, e
	}
	if bounds != nil {
		q.Bounds = &arp.Bounds{West: bounds.West, South: bounds.South, East: bounds.East, North: bounds.North}
	}
	receipt, e := b.Store.ReadAgentResultProjection(ctx, a, q)
	if e != nil {
		return nil, e
	}
	return &originalResultHTTPHistoryBarrier{parent: b, read: read, access: a, query: q, receipt: receipt}, nil
}

type originalResultHTTPHistoryBarrier struct {
	parent  *resultHTTPFinalBarrier
	read    agentworkspace.MessageResultsRead
	access  arp.Access
	query   arp.Query
	receipt arp.Receipt
}

func (h *originalResultHTTPHistoryBarrier) Results() []agentworkspace.MessageResult {
	return h.read.Results()
}
func (h *originalResultHTTPHistoryBarrier) Revalidate(ctx context.Context) error {
	// Same fixed source/ABA/session mutation, now inside the actual new tail.
	if e := h.parent.RevalidateAgentResultProjection(ctx, h.access, h.query, h.receipt); e != nil {
		return e
	}
	return h.read.Revalidate(ctx)
}
