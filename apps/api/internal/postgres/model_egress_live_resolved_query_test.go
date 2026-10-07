package postgres

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

func resolvedNativeTestContext() modelegressbudget.LiveResolvedPublicSearchContext {
	return modelegressbudget.LiveResolvedPublicSearchContext{SelectedCity: modelegressbudget.LiveSelectedCity{ID: "aberdeen-gb", Name: "Aberdeen", CountryCode: "GB"}, ResolvedSlots: modelegressbudget.LiveResolvedSlots{Operation: agentworkspace.RefineResults, Target: agentworkspace.FindActivity, Category: "badminton", TimePreference: "weekend", DistancePreference: "closer"}}
}

func TestLiveResolvedNativeOnlyProjectsCurrentTaskWhitelist(t *testing.T) {
	task := agentworkspace.Task{Intent: agentworkspace.RefineResults, Filters: map[string]string{"targetIntent": agentworkspace.FindActivity, "category": "badminton", "timePreference": "weekend", "distancePreference": "closer", "searchTerm": "sports venue", "resultIDs": "PRIVATE_CANARY", "privateProfile": "PRIVATE_CANARY", "mapWest": "PRIVATE_CANARY"}, Conversation: []agentworkspace.Message{{Role: "user", Text: "PRIVATE_CANARY"}}}
	slots, e := resolvedPublicTaskSlots(task)
	if e != nil || slots.Category != "badminton" || slots.TimePreference != "weekend" || slots.DistancePreference != "closer" {
		t.Fatal("resolved current Task slots lost", e)
	}
	raw, _ := json.Marshal(slots)
	if bytes.Contains(raw, []byte("PRIVATE_CANARY")) || bytes.Contains(raw, []byte("resultIDs")) || bytes.Contains(raw, []byte("mapWest")) {
		t.Fatal("generic private Task fields entered the public projection")
	}
	for _, operation := range []string{"PENDING", "SEND_MESSAGE", agentworkspace.PersonalRelationshipContext, agentworkspace.CreateActivity} {
		task.Intent = operation
		if _, e = resolvedPublicTaskSlots(task); e == nil {
			t.Fatal("non-search operation admitted public slots", operation)
		}
	}
	task.Intent = agentworkspace.FindPlace
	if _, e = resolvedPublicTaskSlots(task); e == nil {
		t.Fatal("stale activity target silently became a place search")
	}
}

func TestLiveResolvedNativeCityGenerationsArePrivateAndRetained(t *testing.T) {
	c := resolvedNativeTestContext()
	first, e := resolvedPublicContextEvidence(c, []byte(`{"cityRow":"10","contextRow":"11","stateRow":"12"}`))
	second, e2 := resolvedPublicContextEvidence(c, []byte(`{"cityRow":"10","contextRow":"11","stateRow":"13"}`))
	if e != nil || e2 != nil || first == second {
		t.Fatal("city pause/restore generation did not retire the proof")
	}
	compiled, e := modelegressbudget.CompileLiveResolvedPublicSearchQuery("近一点的呢？", c)
	if e != nil {
		t.Fatal(e)
	}
	p := PreparedLiveEgress{price: modelegressbudget.LivePrice{Kind: modelegressbudget.LiveCall}, query: "近一点的呢？", resolved: nativeResolvedPublicSearch{context: c, compiledQuery: compiled, evidence: first, valid: true}}
	if p.SearchQuery() == p.query || !strings.Contains(p.SearchQuery(), "Aberdeen GB activities badminton weekend") || !strings.Contains(p.SearchQuery(), "closer to city centre") || bytes.Contains(p.SearchWire(), []byte("cityRow")) {
		t.Fatal("raw question and actual public search wire were conflated")
	}
	if liveModelSourceScope(p) != modelegressbudget.LiveResolvedSourceScope {
		t.Fatal("resolved source scope lost")
	}
	p.resolved = nativeResolvedPublicSearch{}
	if p.SearchQuery() != p.query || liveModelSourceScope(p) != modelegressbudget.LivePublicSearchScope {
		t.Fatal("old literal-query branch changed")
	}
}

func TestLiveResolvedNativeWirePreservesRawQueryAndUniversalUpper(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	query := "  近一点的呢？  "
	c := resolvedNativeTestContext()
	compiled, _ := modelegressbudget.CompileLiveResolvedPublicSearchQuery(query, c)
	proof := nativeResolvedPublicSearch{context: c, compiledQuery: compiled, evidence: strings.Repeat("d", 64), valid: true}
	sources := liveSourceTestSources()
	payload, e := modelegressbudget.LiveResolvedPublicSearchPayload(query, c, sources)
	if e != nil {
		t.Fatal(e)
	}
	r := liveLedgerRequest(now, query)
	r.Messages[1].Content = payload
	price, adapter := liveLedgerPrice(now, modelegressbudget.LiveToken), liveLedgerAdapter(t)
	w, q, digest, amount, e := prepareResolvedLiveSourceWire(query, proof, sources, r, price, adapter, now)
	if e != nil || q != liveDigestBytes("birdtie.live-current-query.v1", []byte(query)) || q == liveDigestBytes("birdtie.live-current-query.v1", []byte(compiled)) || digest != w.WireDigest() || amount.Amount.InputTokens != 196608 || amount.Amount.OutputTokens != 768 || amount.Amount.CostMicros != 199680 {
		t.Fatal("resolved wire changed raw-query identity or universal hold", e)
	}
	if _, _, _, _, e = prepareLiveSourceWire(query, sources, r, price, adapter, now); e == nil {
		t.Fatal("new resolved envelope entered original v1 scope")
	}
	if _, _, _, _, e = prepareLiveWire(query, r, price, adapter, now); e == nil {
		t.Fatal("new resolved envelope entered literal-query scope")
	}
	for _, name := range []string{"unsealed", "wrong_context", "lower_bound", "extra_history", "mutable_projector"} {
		t.Run(name, func(t *testing.T) {
			p, request, tariff := proof, r, price
			request.Messages = append([]modelgateway.Message(nil), r.Messages...)
			var projector LiveWireProjector = adapter
			switch name {
			case "unsealed":
				p.valid = false
			case "wrong_context":
				p.context.ResolvedSlots.Category = "basketball"
			case "lower_bound":
				tariff.Base.InputTokenCeiling--
			case "extra_history":
				request.Messages = append(request.Messages, modelgateway.Message{Role: "user", Content: "PRIVATE_CANARY"})
			case "mutable_projector":
				projector = liveLedgerProjector(func(provider modelgateway.ProviderRequest) (modelgateway.PreparedTencentWire, error) {
					provider.Messages[1].Content = "PRIVATE_CANARY"
					return adapter.Prepare(provider)
				})
			}
			if _, _, _, _, e := prepareResolvedLiveSourceWire(query, p, sources, request, tariff, projector, now); e == nil {
				t.Fatal("changed resolved native contract accepted")
			}
		})
	}
}
