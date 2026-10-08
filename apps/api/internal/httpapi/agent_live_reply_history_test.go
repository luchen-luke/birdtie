package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// Explicit HTTP/presentation doubles. These tests do not prove native SQL,
// real model/retrieval, supplier billing or phone history restoration.
type liveReplyHistoryHTTPPort struct {
	*httpActionSafetyTaskStore
	entries []agentworkspace.MessageResult
	reads   int
	err     error
	handle  *liveReplyHistoryHTTPRead
}

type liveReplyHistoryHTTPRead struct {
	entries []agentworkspace.MessageResult
	err     error
	checks  int
}

func (p *liveReplyHistoryHTTPPort) CaptureOwnHumanReply(context.Context, arp.Access) (agentworkspace.Task, error) {
	return agentworkspace.Task{}, errors.New("unit live history must not create rules reply")
}
func (p *liveReplyHistoryHTTPPort) ReadOwnMessageResults(_ context.Context, a arp.Access) (agentworkspace.MessageResultsRead, error) {
	p.reads++
	raw, _ := json.Marshal(agentworkspace.SanitizeTaskForResponse(p.task))
	if a.Actor.ID != p.task.PrincipalID || a.SessionDigest == ([32]byte{}) || a.TaskID != p.task.ID || string(raw) != string(a.ExpectedTask) {
		return nil, arp.ErrDenied
	}
	if p.err != nil {
		return nil, p.err
	}
	p.handle = &liveReplyHistoryHTTPRead{entries: p.entries}
	return p.handle, nil
}
func (p *liveReplyHistoryHTTPRead) Results() []agentworkspace.MessageResult {
	raw, _ := json.Marshal(p.entries)
	var out []agentworkspace.MessageResult
	_ = json.Unmarshal(raw, &out)
	return out
}
func (p *liveReplyHistoryHTTPRead) Revalidate(context.Context) error { p.checks++; return p.err }

func liveReplyHistoryHTTPFixture(t *testing.T) (*server, *liveReplyHistoryHTTPPort, agentworkspace.Results, *nowHTTPFakeReply) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Second)
	task := agentworkspace.Task{ID: httpActionSafetyTaskID, PrincipalType: "person", PrincipalID: httpActionSafetyOwner, ActingUserID: httpActionSafetyOwner, CityID: "aberdeen-gb", ContextType: "CITY", Status: agentworkspace.TaskCompleted, Intent: agentworkspace.FindPlace, Query: "找地点 A", Filters: map[string]string{"currentQuery": "找地点 B", "searchTerm": "B"}, UpdatedAt: now, Conversation: []agentworkspace.Message{{Role: "user", Text: "找地点 A"}, {Role: "assistant", Text: "UNIT old model reply", Sources: []agentworkspace.AnswerSource{{ID: "source-A", Title: "UNIT source A", URL: "https://example.com/A"}}, SourceRunID: "unit-old-run", SourceEvidenceDigest: strings.Repeat("b", 64)}, {Role: "user", Text: "找地点 B"}, {Role: "assistant", Text: nowHTTPPrivateAnswer, Sources: []agentworkspace.AnswerSource{{ID: "source-B", Title: "UNIT source B", URL: "https://example.com/B"}}, SourceRunID: "unit-http-run", SourceEvidenceDigest: strings.Repeat("a", 64)}}}
	refs := []arp.Ref{{Type: "place", ID: "33333333-3333-4333-8333-333333333333"}, {Type: "place", ID: "44444444-4444-4444-8444-444444444444"}}
	for i, index := range []int{1, 3} {
		member, e := agentworkspace.NewReplyMembership(task, index, "place", []arp.Ref{refs[i]})
		if e != nil {
			t.Fatal(e)
		}
		task.Conversation[index].ResultMembership = member
	}
	entries := []agentworkspace.MessageResult{}
	for i, index := range []int{1, 3} {
		member := task.Conversation[index].ResultMembership
		item := arp.Item{Entity: refs[i], Scope: "AUTHORIZED_VIEW", Title: "UNIT native " + refs[i].ID, Summary: "", Anchor: &arp.Anchor{CoordinateSystem: "wgs84", Precision: "point", Latitude: 57.15 + float64(i)*0.01, Longitude: -2.1}, Detail: &refs[i]}
		r := agentworkspace.WithContract(agentworkspace.Results{CityID: task.CityID, NativeProjection: true, ProjectionItems: []arp.Item{item}}, task, "UNIT_HISTORY")
		r.ResultSet.ID = member.ResultSetID
		r.ResultSet.GeneratedAt = now.Add(500 * time.Millisecond)
		entries = append(entries, agentworkspace.MessageResult{MessageIndex: index, TurnDigest: member.TurnDigest, ValidUntil: now.Add(30 * time.Second), ResultSet: r.ResultSet, MapEffects: r.MapEffects})
	}
	port := &liveReplyHistoryHTTPPort{httpActionSafetyTaskStore: &httpActionSafetyTaskStore{task: task}, entries: entries}
	s := &server{agent: port}
	unrelated := arp.Ref{Type: "place", ID: "55555555-5555-4555-8555-555555555555"}
	result := agentworkspace.WithContract(agentworkspace.Results{Task: &task, CityID: task.CityID, NativeProjection: true, ProjectionItems: []arp.Item{{Entity: unrelated, Scope: arp.AuthorizedView, Title: "UNIT newly matched C", Detail: &unrelated, Anchor: &arp.Anchor{CoordinateSystem: "wgs84", Precision: "point", Latitude: 57.4, Longitude: -2.1}}}}, task, "UNIT_LIVE_HISTORY_REQUEST")
	reply := &nowHTTPFakeReply{task: task, sources: task.Conversation[3].Sources}
	return s, port, result, reply
}

func TestAgentLiveReplyHistoryPOSTUsesExactMembershipBeforeDecoration(t *testing.T) {
	s, port, initial, reply := liveReplyHistoryHTTPFixture(t)
	r := httptest.NewRequest(http.MethodPost, "/unit-live-history", nil)
	r = r.WithContext(context.WithValue(r.Context(), nowLiveReplyKey{}, agentworkspace.LiveReply(reply)))
	got, read, primary, e := s.prepareAgentMessageResults(r, initial, [32]byte{1}, identity.Actor{ID: httpActionSafetyOwner, AccountType: "person"})
	if e != nil || !primary || read == nil || port.reads != 1 || len(got.MessageResults) != 2 || got.ResultSet.ID != initial.Task.Conversation[3].ResultMembership.ResultSetID || !reflect.DeepEqual(got.ResultSet.Entities, initial.Task.Conversation[3].ResultMembership.Refs) {
		t.Fatal("live POST failed exact current/history native set", e)
	}
	if !got.ResultSet.GeneratedAt.Equal(initial.Task.UpdatedAt) || !got.MessageResults[1].ResultSet.GeneratedAt.Equal(port.entries[1].ResultSet.GeneratedAt) {
		t.Fatal("live binding timestamp changed historical observation")
	}
	got, e = decorateCurrentNowReply(r, got)
	if e != nil || got.Mode != "live" || got.Message != nowHTTPPrivateAnswer || got.ResultSet.AnswerBinding == nil || !reflect.DeepEqual(got.ResultSet.Sources, reply.sources) || !reflect.DeepEqual(got.MapEffects, port.entries[1].MapEffects) {
		t.Fatal("live sourced decoration diverged from native saved reply", e)
	}
	if reply.checks != 0 || len(got.MessageResults[0].ResultSet.Sources) != 0 || got.MessageResults[0].ResultSet.AnswerBinding != nil {
		t.Fatal("history read revived a model grant or grafted latest source")
	}
}

func TestAgentLiveReplyHistoryGETKeepsModelProvenanceAndFreshLifetime(t *testing.T) {
	s, port, initial, reply := liveReplyHistoryHTTPFixture(t)
	reply.invalid = true // Expired dispatch is irrelevant to authenticated history.
	r := httptest.NewRequest(http.MethodGet, "/unit-live-history", nil)
	got, read, primary, e := s.prepareAgentMessageResults(r, initial, [32]byte{1}, identity.Actor{ID: httpActionSafetyOwner, AccountType: "person"})
	if e != nil || !primary || read == nil || got.Mode != "live" || got.Message != nowHTTPPrivateAnswer || got.ResultSet.AnswerBinding != nil || !got.ResultSet.GeneratedAt.Equal(port.entries[1].ResultSet.GeneratedAt) || !reflect.DeepEqual(got.ResultSet.Sources, reply.sources) || reply.checks != 0 {
		t.Fatal("historical model provenance was replaced by rules or dispatch", e)
	}
	if strings.Contains(got.Note, "规则") || got.MessageResults[0].ResultSet.Entities[0] == got.MessageResults[1].ResultSet.Entities[0] {
		t.Fatal("older native membership acquired latest turn")
	}
	port.handle.err = arp.ErrChanged
	if !errors.Is(read.Revalidate(r.Context()), arp.ErrChanged) {
		t.Fatal("fresh historical source withdrawal was ignored")
	}
}

func TestAgentLiveReplyHistoryRulesAndMissingMembershipRemainCompatible(t *testing.T) {
	for _, mode := range []string{"rules", "missingMembership", "sourceMissing", "digestMissing", "runMissing", "unsealedPOST", "readError"} {
		t.Run(mode, func(t *testing.T) {
			s, port, initial, _ := liveReplyHistoryHTTPFixture(t)
			r := httptest.NewRequest(http.MethodGet, "/unit-live-history", nil)
			switch mode {
			case "rules":
				m := &port.task.Conversation[3]
				m.Sources, m.SourceRunID, m.SourceEvidenceDigest = nil, "", ""
				initial.Task = &port.task
			case "missingMembership":
				port.task.Conversation[3].ResultMembership = nil
				port.entries = port.entries[:1]
				initial.Task = &port.task
			case "sourceMissing", "digestMissing", "runMissing":
				m := &port.task.Conversation[3]
				m.ResultMembership = nil
				switch mode {
				case "sourceMissing":
					m.Sources = nil
				case "digestMissing":
					m.SourceEvidenceDigest = ""
				case "runMissing":
					m.SourceRunID = ""
				}
				port.entries = port.entries[:1]
				initial.Task = &port.task
			case "unsealedPOST":
				r = httptest.NewRequest(http.MethodPost, "/unit-live-history", nil)
			case "readError":
				port.err = arp.ErrChanged
			}
			got, read, primary, e := s.prepareAgentMessageResults(r, initial, [32]byte{1}, identity.Actor{ID: httpActionSafetyOwner, AccountType: "person"})
			switch mode {
			case "rules":
				if e != nil || !primary || got.Mode != "rules" || len(got.ResultSet.Sources) != 0 {
					t.Fatal("rules compatibility changed", e)
				}
			case "missingMembership":
				if e != nil || !primary || read == nil || len(got.MessageResults) != 1 || len(got.ResultSet.Items) != 0 || len(got.ResultSet.Entities) != 0 || len(got.MapEffects.PinEntityIDs) != 0 || got.Message != nowHTTPPrivateAnswer || len(got.ResultSet.Sources) != 1 || got.ResultSet.AnswerBinding != nil || got.Note != "历史回答的地点结果未保存，请重新检索。" {
					t.Fatal("old model round was retrospectively given latest membership or lost original citations", e)
				}
			case "sourceMissing", "digestMissing", "runMissing":
				if e != nil || primary || read == nil || !reflect.DeepEqual(got.ResultSet, initial.ResultSet) || got.Mode == "live" {
					t.Fatal("incomplete provenance was classified as an old model reply", e)
				}
			case "unsealedPOST":
				if e != nil || primary || read != nil || port.reads != 0 {
					t.Fatal("ordinary unsealed POST acquired history branch", e)
				}
			case "readError":
				if !errors.Is(e, arp.ErrChanged) || primary || read != nil {
					t.Fatal("native withdrawal became successful empty history", e)
				}
			}
		})
	}
}
