package agenttool

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
)

// Actual Go presentation DTO and validator coverage; no native authorization,
// SQL fixture, provider response or paid call is claimed by these units.
func TestCurrentReadonlyRegistryDescribesValidatedSourcedAnswerWire(t *testing.T) {
	for _, kind := range []string{"person", "place"} {
		t.Run(kind, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Microsecond)
			task := agentworkspace.Task{ID: currentReadTask, Query: "原当前问题", Filters: map[string]string{"currentQuery": "原当前问题"},
				Conversation: []agentworkspace.Message{{Role: "user", Text: "原当前问题"}}, UpdatedAt: now}
			ref := arp.Ref{Type: kind, ID: currentReadEntity}
			result := agentworkspace.WithContract(agentworkspace.Results{Query: task.Query, CityID: "city_fixture", NativeProjection: true,
				ProjectionItems: []arp.Item{{Entity: ref, Title: "原对象", Summary: "原领域明确可见", Scope: arp.AuthorizedView}}}, task, "request_fixture")
			queryDigest, e := agentworkspace.SourcedAnswerQueryDigest(task)
			if e != nil {
				t.Fatal(e)
			}
			taskDigest, e := agentworkspace.SourcedAnswerTaskDigest(task)
			if e != nil {
				t.Fatal(e)
			}
			binding := agentworkspace.SourcedAnswerBinding{TaskID: task.ID, RequestID: "request_fixture", CurrentQueryDigest: queryDigest,
				TaskSnapshotDigest: taskDigest, SourceEvidenceDigest: strings.Repeat("a", 64), RunID: "original-run", GeneratedAt: now, ValidUntil: now.Add(time.Minute)}
			sources := []agentworkspace.AnswerSource{{ID: "source-1", Title: "来源标题", URL: "https://example.test/current", Site: "公开官网", PublishedAt: "2026-10-08"}}
			answer, e := agentworkspace.NewSourcedAnswer(binding, "来源说明。[1]", sources)
			if e != nil {
				t.Fatal("actual typed sourced-answer construction", e)
			}
			result, e = agentworkspace.ApplySourcedAnswer(result, task, "request_fixture", answer, now)
			if e != nil {
				t.Fatal("actual same-task result decoration", e)
			}
			d, _ := Lookup(CurrentSearchTool(kind))
			var schema map[string]any
			var wire any
			if json.Unmarshal(d.OutputSchema, &schema) != nil {
				t.Fatal("invalid original output metadata")
			}
			raw, e := json.Marshal(result.ResultSet)
			if e != nil || json.Unmarshal(raw, &wire) != nil {
				t.Fatal("actual decorated wire", e)
			}
			checkCurrentReadDeclaredWire(t, schema, wire, "ResultSet")
			if result.ResultSet.AnswerBinding == nil || !reflect.DeepEqual(result.ResultSet.Sources, sources) ||
				len(result.ResultSet.Items) != 1 || result.ResultSet.Items[0].Entity != ref {
				t.Fatal("metadata validation changed the original result set")
			}
		})
	}
}

func TestCurrentReadonlyRegistrySourcedAnswerMetadataStaysClosedAndDescriptive(t *testing.T) {
	for _, tool := range []string{PlaceSearch, PersonSearch} {
		d, _ := Lookup(tool)
		var schema map[string]any
		if json.Unmarshal(d.OutputSchema, &schema) != nil {
			t.Fatal("invalid output metadata")
		}
		props := schema["properties"].(map[string]any)
		sources := props["sources"].(map[string]any)
		if sources["type"] != "array" || sources["maxItems"] != float64(10) {
			t.Fatal("unbounded source references")
		}
		for _, item := range []struct {
			name string
			data map[string]any
			keys []string
		}{
			{"source", sources["items"].(map[string]any), []string{"id", "title", "url", "site", "publishedAt"}},
			{"answer binding", props["answerBinding"].(map[string]any), []string{"taskId", "requestId", "currentQueryDigest", "taskSnapshotDigest", "sourceEvidenceDigest", "runId", "generatedAt", "validUntil"}},
		} {
			fields := item.data["properties"].(map[string]any)
			if item.data["additionalProperties"] != false || len(fields) != len(item.keys) {
				t.Fatal("new presentation metadata is not closed", item.name)
			}
			for _, key := range item.keys {
				if _, ok := fields[key]; !ok {
					t.Fatal("actual DTO field missing", item.name, key)
				}
			}
		}
		purpose := map[string]string{PlaceSearch: "HUMAN_READ_CURRENT_PUBLIC_PLACE", PersonSearch: "HUMAN_READ_CURRENT_PUBLIC_PERSON"}[tool]
		if d.Operation != "OBSERVE" || d.Purpose != purpose {
			t.Fatal("read metadata changed authority", d.Operation, d.Purpose)
		}
	}
	for _, tool := range []string{agentplanner.ActivitySearch, agentplanner.ActivityDetail} {
		d, _ := Lookup(tool)
		if strings.Contains(string(d.OutputSchema), "sources") || strings.Contains(string(d.OutputSchema), "answerBinding") ||
			!strings.Contains(string(d.OutputSchema), `"visibility":{"const":"public"}`) {
			t.Fatal("human presentation metadata broadened original model activity output")
		}
	}
}
