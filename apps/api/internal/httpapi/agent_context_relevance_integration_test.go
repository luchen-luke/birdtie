package httpapi

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	aca "github.com/birdtie/birdtie/apps/api/internal/agentcontextadapter"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentcontextrelevance"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"strings"
	"testing"
	"time"
)

func TestRelevanceHTTPNativeRegisteredRuntimeFiltersApprovedAndUnselected(t *testing.T) {
	f := contextPurposeHTTPNative(t)
	// Existing explicit memory is relevant. Add a second explicitly selected
	// unrelated record and one unselected same-topic canary before preview.
	var unrelated, unselected string
	for _, target := range []*string{&unrelated, &unselected} {
		if e := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text`).Scan(target); e != nil {
			t.Fatal(e)
		}
	}
	until := time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)
	for id, summary := range map[string]string{unrelated: "意大利面_SELECTED_IRRELEVANT", unselected: "羽毛球_UNSELECTED_RELEVANCE_CANARY"} {
		raw, _ := json.Marshal(agentmemory.PutInput{ExpectedVersion: 0, MemoryType: agentmemory.TypePreference, MemoryKey: "relevance." + id, Summary: summary, StructuredValue: json.RawMessage(`{}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: until})
		f.request(t, f.handler, "PUT", memoryHTTPList+"/"+id, string(raw), f.tokens[0], 200, nil)
	}
	f.selection.MemoryIDs = append(f.selection.MemoryIDs, unrelated)
	p := f.preview(t)
	g := f.approve(t, p)
	before := f.readonlyRows(t)
	w := f.runtime(t, g, 200)
	var data struct {
		Data aca.View `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &data); e != nil {
		t.Fatal(e)
	}
	if len(data.Data.Relationships) != 0 || data.Data.AdapterVersion != aca.Version || len(data.Data.Activities) != 1 || data.Data.MemoryPromotionAllowed || data.Data.ModelAccess != "UNAVAILABLE" {
		t.Fatal("registered Runtime did not consume relevance then adapter", data.Data)
	}
	for _, secret := range []string{"SELECTED_IRRELEVANT", "UNSELECTED_RELEVANCE_CANARY", unrelated, unselected, f.tie, "STRUCTURED_VALUE_NOT_IN_RUNTIME", "rowToken"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("unrelated/unselected/internal projection leaked", secret)
		}
	}
	if !strings.Contains(w.Body.String(), contextPurposeHTTPMemory) || before != f.readonlyRows(t) {
		t.Fatal("relevant content lost or runtime wrote source")
	}
	// The unrelated selected source is absent from output but remains in the
	// original concrete approval. An actual native versioned edit invalidates it.
	raw, _ := json.Marshal(agentmemory.PutInput{ExpectedVersion: 1, MemoryType: agentmemory.TypePreference, MemoryKey: "relevance." + unrelated, Summary: "实际更改后的无关记录", StructuredValue: json.RawMessage(`{}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: until})
	f.request(t, f.handler, "PUT", memoryHTTPList+"/"+unrelated, string(raw), f.tokens[0], 200, nil)
	f.runtime(t, g, 403)
}

func TestRelevanceHTTPNativeExplicitSixFamilyQueryUsesOnlyApprovedSources(t *testing.T) {
	f := contextPurposeHTTPNative(t)
	// A new real Task revision explicitly asks about each selected value. It
	// needs its own concrete Preview/approval, not a broadened old grant.
	f.task.Query = "我的好友互动；羽毛球，周末，中文，合成 Context 地点"
	f.task.Filters["currentQuery"] = f.task.Query
	var e error
	f.task, e = f.store.UpdateTask(f.ctx, f.task)
	if e != nil {
		t.Fatal(e)
	}
	// UpdateTask preserves the original historical query column. The current
	// native request is filters.currentQuery; use that exact current value for
	// this test's human Preview rather than the old initial query.
	f.task.Query = f.task.Filters["currentQuery"]
	f.selection.CurrentQuery = f.task.Query
	f.selection.TaskUpdatedAt = f.task.UpdatedAt
	p := f.preview(t)
	g := f.approve(t, p)
	before := f.readonlyRows(t)
	w := f.runtime(t, g, 200)
	var result struct {
		Data aca.View `json:"data"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	bounded := result.Data
	encoded, e := json.Marshal(bounded)
	if e != nil || bounded.Budget.Limit != aca.DefaultBudget().MaxEncodedBytes || bounded.Budget.Used != len(encoded) || len(encoded) > bounded.Budget.Limit || bounded.FieldEvidenceSet == nil {
		t.Fatal("registered default runtime lost actual bounded field evidence", e)
	}
	if len(bounded.Activities) == 0 && (bounded.Sections["activities"] != aca.OmittedBudget || bounded.Budget.Omitted["activities"] != 1) {
		t.Fatal("selected activity omission falsely represented as absent source", bounded)
	}
	// Verify the complete six-family relevance projection with the same actual
	// approved native sources, at an explicit internal budget. No client budget
	// knob or runtime default is broadened to make every selected source fit.
	_, built, _ := adapterNativeBuild(t, f, g)
	relevance, e := agentcontextrelevance.NewService(f.store)
	if e != nil { t.Fatal(e) }
	related, e := relevance.Retrieve(f.ctx, built.Request)
	if e != nil { t.Fatal("actual six-family relevant projection", e) }
	r, e := consumeRelatedBudgetedTaskContext(related.ProjectionBundle(), aca.Budget{MaxEncodedBytes: aca.MaxEncodedBytes}, related.ExcludedCounts())
	if e != nil { t.Fatal("complete approved native families", e) }
	if len(r.Facts) != 4 || len(r.Places) != 1 || r.Places[0].ID != f.place || len(r.Activities) != 1 || r.Activities[0].ID != f.public || len(r.Relationships) != 1 || r.Relationships[0].ID != f.tie || r.Relationships[0].PeerAccountID != f.accountIDs[1] || r.Relationships[0].State != "ACCEPTED" {
		t.Fatal("explicit relevant native families absent", r)
	}
	for _, value := range []string{"周末下午", "中文", contextPurposeHTTPMemory, "LEVEL_0_OBSERVE", "合成 Context 地点"} {
		completeRaw, _ := json.Marshal(r)
		if !strings.Contains(string(completeRaw), value) {
			t.Fatal("explicit requested content absent", value)
		}
	}
	if before != f.readonlyRows(t) {
		t.Fatal("relevance runtime wrote native sources")
	}
	for _, p := range r.Provenance {
		if p.Source.NativeTime.IsZero() || p.Source.RowToken != "" {
			t.Fatal("fake/internal source provenance")
		}
	}
}

func TestRelevanceHTTPNativeEmptySelectedFieldUnknownAndNoUnselectedFallback(t *testing.T) {
	for _, name := range []string{"selectedRelatedUnknown", "selectedUnrelated", "notSelected"} {
		t.Run(name, func(t *testing.T) {
			f := contextPurposeHTTPNative(t)
			a := agentprofile.PrivateAccess{SessionDigest: contextPurposeHTTPDigest(t, f.tokens[0]), WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[0]}}
			current, e := f.store.ReadOwnAgentPrivateProfile(f.ctx, a)
			if e != nil {
				t.Fatal(e)
			}
			fields := current.Fields
			fields.LanguagePreferences = []string{}
			if _, e = f.store.ReplaceOwnAgentPrivateProfile(f.ctx, a, agentprofile.ReplacePrivateInput{ExpectedVersion: current.Profile.ProfileVersion, Fields: fields}); e != nil {
				t.Fatal(e)
			}
			query := "我的语言偏好是什么"
			if name == "selectedUnrelated" {
				query = "帮我找羽毛球"
			}
			f.task.Filters["currentQuery"] = query
			f.task, e = f.store.UpdateTask(f.ctx, f.task)
			if e != nil {
				t.Fatal(e)
			}
			f.selection.CurrentQuery = query
			f.selection.TaskUpdatedAt = f.task.UpdatedAt
			f.selection.ProfileFields = []string{"languagePreferences"}
			if name == "notSelected" {
				f.selection.ProfileFields = []string{}
			}
			w := f.request(t, f.handler, "POST", contextPurposeHTTPBase+"/previews", contextPurposeHTTPSelectionBody(t, f.selection), f.tokens[0], 200, nil)
			var p struct {
				Data acb.PurposePreview `json:"data"`
			}
			if e = json.Unmarshal(w.Body.Bytes(), &p); e != nil {
				t.Fatal(e)
			}
			if name != "notSelected" && string(p.Data.Review.Profile["languagePreferences"]) != "[]" {
				t.Fatal("human explicit empty field preview missing")
			}
			g := f.approve(t, p.Data)
			before := f.readonlyRows(t)
			w = f.runtime(t, g, 200)
			var out struct {
				Data aca.View `json:"data"`
			}
			if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil {
				t.Fatal(e)
			}
			unknown := 0
			for _, fact := range out.Data.Facts {
				if fact.Field == "languagePreferences" {
					if fact.ValueStatus != "UNKNOWN" || !strings.Contains(fact.Text, "尚未填写") {
						t.Fatal("empty selected field guessed", fact)
					}
					unknown++
				}
			}
			want := "UNKNOWN"
			if name == "selectedUnrelated" {
				want = "NOT_RELEVANT"
			}
			if name == "notSelected" {
				want = "NOT_REQUESTED"
			}
			if out.Data.Sections["profile"] != want || (name == "selectedRelatedUnknown" && unknown != 1) || (name != "selectedRelatedUnknown" && unknown != 0) {
				t.Fatal("unknown/unrelated/unrequested collapsed", name, out.Data)
			}
			if before != f.readonlyRows(t) {
				t.Fatal("empty-field retrieval altered authoritative sources")
			}
		})
	}
}
