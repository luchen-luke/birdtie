package agentworkspace

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
)

func answerFixture(t *testing.T) (Task, Results, SourcedAnswerBinding, []AnswerSource, time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 8, 1, 0, 0, 0, time.UTC)
	task := Task{ID: "original-task", Query: "找地点", Status: TaskCompleted,
		PrincipalType: "PERSON", PrincipalID: "owner", ActingUserID: "owner", CityID: "original-city",
		Filters: map[string]string{"currentQuery": "St. Mary's 的开放时间"},
		Conversation: []Message{{Role: "user", Text: "找地点"}, {Role: "assistant", Text: "原结果"},
			{Role: "user", Text: "St. Mary's 的开放时间"}, {Role: "assistant", Text: "原领域回答"}}, UpdatedAt: now}
	ref := arp.Ref{Type: "place", ID: "original-place"}
	result := WithContract(Results{NativeProjection: true, Query: task.Filters["currentQuery"],
		Message: "原领域回答", Task: &task, TaskID: task.ID,
		ProjectionItems: []arp.Item{{Entity: ref, Title: "St. Mary's", Scope: arp.AuthorizedView, Detail: &ref}},
	}, task, "original-request")
	query, err := SourcedAnswerQueryDigest(task)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := SourcedAnswerTaskDigest(task)
	if err != nil {
		t.Fatal(err)
	}
	binding := SourcedAnswerBinding{TaskID: task.ID, RequestID: result.RequestID,
		CurrentQueryDigest: query, TaskSnapshotDigest: digest, SourceEvidenceDigest: strings.Repeat("a", 64),
		RunID: "original-run", GeneratedAt: now, ValidUntil: now.Add(time.Minute)}
	return task, result, binding, []AnswerSource{{ID: "source-1", Title: "St. Mary's — Opening times",
		URL: "https://official.example/opening", Site: "Official site", PublishedAt: "2026-10-08"}}, now
}

func TestApplySourcedAnswerPreservesNativeProjectionAndTask(t *testing.T) {
	task, result, binding, sources, now := answerFixture(t)
	beforeTask, _ := json.Marshal(result.Task)
	beforeItems, _ := json.Marshal(result.ResultSet.Items)
	beforeMap, _ := json.Marshal(result.MapEffects)
	answer, err := NewSourcedAnswer(binding, "官网列出的开放时间如下。[1]", sources)
	if err != nil {
		t.Fatal(err)
	}
	sources[0].Title = "mutated caller title"
	got, err := ApplySourcedAnswer(result, task, result.RequestID, answer, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if got.Message != "官网列出的开放时间如下。[1]" || got.ResultSet.Sources[0].Title != "St. Mary's — Opening times" ||
		got.ResultSet.AnswerBinding == nil || got.ResultSet.AnswerBinding.RunID != "original-run" {
		t.Fatal("sourced reply lost its binding")
	}
	afterTask, _ := json.Marshal(got.Task)
	afterItems, _ := json.Marshal(got.ResultSet.Items)
	afterMap, _ := json.Marshal(got.MapEffects)
	if string(beforeTask) != string(afterTask) || string(beforeItems) != string(afterItems) || string(beforeMap) != string(afterMap) ||
		!reflect.DeepEqual(result.ResultSet.Entities, got.ResultSet.Entities) || len(result.ResultSet.Sources) != 0 {
		t.Fatal("answer replaced or mutated native task/entity/map state")
	}
	got.ResultSet.Sources[0].Title = "mutated result"
	got.ResultSet.AnswerBinding.RunID = "mutated binding"
	again, err := ApplySourcedAnswer(result, task, result.RequestID, answer, now.Add(time.Second))
	if err != nil || again.ResultSet.Sources[0].Title != "St. Mary's — Opening times" || again.ResultSet.AnswerBinding.RunID != "original-run" {
		t.Fatal("response shared mutable process-local source state")
	}
}

func TestSourcedAnswerDeniesMismatchedOrExpiredBinding(t *testing.T) {
	for _, name := range []string{"task", "request", "query", "snapshot", "principal", "city", "expired", "future", "zero-now", "missing-set", "unsupported", "wrong-conversation", "legacy-projection", "wrong-result-turn"} {
		t.Run(name, func(t *testing.T) {
			task, result, binding, sources, now := answerFixture(t)
			answer, err := NewSourcedAnswer(binding, "有来源的回答。[1]", sources)
			if err != nil {
				t.Fatal(err)
			}
			request := result.RequestID
			switch name {
			case "task":
				task.ID = "other"
			case "request":
				request = "other"
			case "query":
				task.Filters = map[string]string{"currentQuery": "另一问题"}
			case "snapshot":
				task.UpdatedAt = task.UpdatedAt.Add(time.Nanosecond)
			case "principal":
				task.PrincipalID = "other"
			case "city":
				task.CityID = "other"
			case "expired":
				now = binding.ValidUntil
			case "future":
				now = now.Add(-time.Nanosecond)
			case "zero-now":
				now = time.Time{}
			case "missing-set":
				result.ResultSet.ID = ""
			case "unsupported":
				result.ResultSet.Status = "unsupported"
			case "wrong-conversation":
				result.ConversationID = "other"
			case "legacy-projection":
				result.NativeProjection = false
			case "wrong-result-turn":
				result.ResultSet.GeneratedAt = result.ResultSet.GeneratedAt.Add(time.Nanosecond)
			}
			originalMessage := result.Message
			got, err := ApplySourcedAnswer(result, task, request, answer, now)
			if err != ErrSourcedAnswer || got.Message != originalMessage || got.ResultSet.AnswerBinding != nil || len(got.ResultSet.Sources) != 0 {
				t.Fatal("invalid binding modified reply")
			}
		})
	}
}

func TestSourcedAnswerCannotBeJSONAuthority(t *testing.T) {
	_, _, binding, sources, _ := answerFixture(t)
	answer, err := NewSourcedAnswer(binding, "回答。[1]", sources)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = json.Marshal(answer); err == nil {
		t.Fatal("process-local answer serialized")
	}
	if err = json.Unmarshal([]byte(`{"valid":true,"message":"forged"}`), &SourcedAnswer{}); err == nil {
		t.Fatal("JSON rebuilt answer")
	}
	task, result, _, _, now := answerFixture(t)
	for _, invalid := range []*SourcedAnswer{nil, {}, {valid: false, binding: binding}} {
		if _, err := ApplySourcedAnswer(result, task, result.RequestID, invalid, now); err == nil {
			t.Fatal("zero answer accepted")
		}
	}
}

func TestSourcedAnswerRejectsUnboundedAndNonHTTPReferences(t *testing.T) {
	for _, value := range []string{"javascript:alert(1)", "dev-seed://place", "/relative", "https://user:secret@example.org/", "https://example.org/with space", "https://example.org/\\path", "https://example.org/\x00"} {
		t.Run(value, func(t *testing.T) {
			_, _, binding, sources, _ := answerFixture(t)
			sources[0].URL = value
			if _, err := NewSourcedAnswer(binding, "回答", sources); err == nil {
				t.Fatal("unsafe URL accepted")
			}
		})
	}
	_, _, binding, sources, _ := answerFixture(t)
	for _, list := range [][]AnswerSource{nil, append(sources, sources[0]), make([]AnswerSource, 11)} {
		if _, err := NewSourcedAnswer(binding, "回答", list); err == nil {
			t.Fatal("unbounded/duplicate sources accepted")
		}
	}
	for _, message := range []string{"", "  ", "answer\x00", strings.Repeat("界", 2800)} {
		if _, err := NewSourcedAnswer(binding, message, sources); err == nil {
			t.Fatal("unbounded answer accepted")
		}
	}
	binding.SourceEvidenceDigest = strings.Repeat("0", 64)
	if _, err := NewSourcedAnswer(binding, "回答", sources); err == nil {
		t.Fatal("zero source evidence accepted")
	}
}

func TestSourcedAnswerCurrentQueryDigestDoesNotCarryHistory(t *testing.T) {
	task, _, _, _, _ := answerFixture(t)
	want, _ := SourcedAnswerQueryDigest(task)
	task.Conversation[0].Text = "unrelated private history"
	got, err := SourcedAnswerQueryDigest(task)
	if err != nil || got != want {
		t.Fatal("old history exported as query")
	}
	task.Conversation[2].Text = "different current question"
	if _, err := SourcedAnswerQueryDigest(task); err == nil {
		t.Fatal("mismatched current question accepted")
	}
}

func TestWithContractDoesNotPreserveForgedSourceFields(t *testing.T) {
	task, result, _, sources, _ := answerFixture(t)
	result.ResultSet.Sources = sources
	result.ResultSet.AnswerBinding = &SourcedAnswerBinding{RunID: "forged"}
	got := WithContract(result, task, result.RequestID)
	if got.ResultSet.AnswerBinding != nil || got.ResultSet.Sources == nil || len(got.ResultSet.Sources) != 0 {
		t.Fatal("unbound sources survived projection")
	}
}

func TestSourcedAnswerNormalizesTimesToUTC(t *testing.T) {
	_, _, binding, sources, _ := answerFixture(t)
	binding.GeneratedAt = binding.GeneratedAt.In(time.FixedZone("offset", 8*60*60))
	binding.ValidUntil = binding.ValidUntil.In(time.FixedZone("offset", 8*60*60))
	answer, err := NewSourcedAnswer(binding, "回答", sources)
	if err != nil {
		t.Fatal(err)
	}
	if answer.binding.GeneratedAt.Location() != time.UTC || answer.binding.ValidUntil.Location() != time.UTC {
		t.Fatal("binding times are not interoperable UTC values")
	}
}
