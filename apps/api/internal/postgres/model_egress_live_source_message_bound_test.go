package postgres

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

func sourceMessageBoundPrepared(t *testing.T, now time.Time, resolved bool) PreparedLiveEgress {
	t.Helper()
	query := "  Find public museums  "
	p := PreparedLiveEgress{input: modelegressbudget.PreviewInput{RootTraceID: "11000000-0000-4000-8000-000000000004", TaskID: "11000000-0000-4000-8000-000000000005", DeadlineAt: now.Add(time.Minute)}, price: liveLedgerPrice(now, modelegressbudget.LiveCall), request: liveLedgerRequest(now, query), query: strings.TrimSpace(query), queryDigest: liveDigestBytes("birdtie.live-current-query.v1", []byte(query)), payloadDigest: strings.Repeat("b", 64), digest: strings.Repeat("a", 64), owner: "owner", session: "session", binding: "binding", source: "source", authority: "authority"}
	if resolved {
		c := resolvedNativeTestContext()
		compiled, e := modelegressbudget.CompileLiveResolvedPublicSearchQuery(query, c)
		if e != nil {
			t.Fatal(e)
		}
		p.resolved = nativeResolvedPublicSearch{context: c, compiledQuery: compiled, evidence: strings.Repeat("d", 64), valid: true}
	}
	return p
}

func sourceMessageBoundWire(t *testing.T, p PreparedLiveEgress, sources []modelegressbudget.LivePublicSource, now time.Time) (modelgateway.PreparedTencentWire, modelegressbudget.LiveAmount, error) {
	t.Helper()
	payload, e := liveSourceModelPayload(p, sources)
	if e != nil {
		return modelgateway.PreparedTencentWire{}, modelegressbudget.LiveAmount{}, e
	}
	r := p.request
	r.Messages = append([]modelgateway.Message(nil), p.request.Messages...)
	r.Messages[1].Content = payload
	price := liveLedgerPrice(now, modelegressbudget.LiveToken)
	var wire modelgateway.PreparedTencentWire
	var amount modelegressbudget.LiveAmount
	if p.resolved.valid {
		wire, _, _, amount, e = prepareResolvedLiveSourceWire(p.request.Messages[1].Content, p.resolved, sources, r, price, liveLedgerAdapter(t), now)
	} else {
		wire, _, _, amount, e = prepareLiveSourceWire(p.request.Messages[1].Content, sources, r, price, liveLedgerAdapter(t), now)
	}
	return wire, amount, e
}

// This recreates the pre-fix selector, proving the contract mismatch without
// using recorded provider content, contacting a provider or changing a ledger.
func TestLiveSourceMessageBoundOriginalSelectionExceedsModelContract(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, resolved := range []bool{false, true} {
		t.Run(fmt.Sprintf("resolved_%t", resolved), func(t *testing.T) {
			p := sourceMessageBoundPrepared(t, now, resolved)
			sources := make([]modelegressbudget.LivePublicSource, 10)
			for i := range sources {
				sources[i] = modelegressbudget.LivePublicSource{Title: "Public museum", URL: fmt.Sprintf("https://example.org/museum/%d", i), Passage: strings.Repeat("x", 1024)}
			}
			oldSelected, e := modelegressbudget.SelectLivePublicSources(sources)
			if e != nil || len(oldSelected) != 10 {
				t.Fatal("original source selector did not admit the representative input", e)
			}
			oldPayload, e := liveSourceModelPayload(p, oldSelected)
			if e != nil || len(oldPayload) <= liveSourceModelMessageMaxBytes || len(oldPayload) > modelegressbudget.LivePublicSearchMaxPayloadBytes {
				t.Fatal("did not reproduce the admitted source envelope above the original message limit", e)
			}
			wire, amount, e := sourceMessageBoundWire(t, p, oldSelected, now)
			if !errors.Is(e, modelegressbudget.ErrDenied) || len(wire.ExactWire()) != 0 || amount.Amount.CostMicros != 0 || !strings.Contains(e.Error(), "source-wire-request-validation") {
				t.Fatal("oversized original selection received a model wire or lost the safe stage", e)
			}
			selected, e := selectLiveModelMessageSources(p, sources)
			if e != nil || len(selected) < 1 || len(selected) >= len(sources) {
				t.Fatal("bounded prefix was not selected", e)
			}
			payload, e := liveSourceModelPayload(p, selected)
			if e != nil || len(payload) > liveSourceModelMessageMaxBytes {
				t.Fatal("selected encoded envelope exceeded the model bound", e)
			}
			for i := range selected {
				if selected[i] != oldSelected[i] {
					t.Fatal("selection reordered or changed an admitted public source")
				}
			}
			nextPayload, e := liveSourceModelPayload(p, oldSelected[:len(selected)+1])
			if e != nil || len(nextPayload) <= liveSourceModelMessageMaxBytes {
				t.Fatal("selection was not the largest fitting prefix", e)
			}
			wire, amount, e = sourceMessageBoundWire(t, p, selected, now)
			if e != nil || len(wire.ExactWire()) == 0 || amount.Amount.InputTokens != 196608 || amount.Amount.OutputTokens != 768 || amount.Amount.CostMicros != 199680 {
				t.Fatal("bounded exact envelope did not pass the unchanged concrete model formatter and universal hold", e)
			}
			t.Logf("originalEncodedBytes=%d selectedSources=%d selectedEncodedBytes=%d", len(oldPayload), len(selected), len(payload))
		})
	}
}

func TestLiveSourceMessageBoundUsesJSONBytesAndPreservesUTF8(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, resolved := range []bool{false, true} {
		for _, passage := range []string{strings.Repeat("中😀", 500), strings.Repeat("<>&\t\n", 180), strings.Repeat(`"\`, 512)} {
			t.Run(fmt.Sprintf("resolved_%t/encoding_%x", resolved, liveDigestBytes("unit", []byte(passage))[:8]), func(t *testing.T) {
				p := sourceMessageBoundPrepared(t, now, resolved)
				sources := make([]modelegressbudget.LivePublicSource, 10)
				for i := range sources {
					sources[i] = modelegressbudget.LivePublicSource{Title: "中文 proper name 😀", URL: fmt.Sprintf("https://example.org/%d?a=1&b=2", i), Passage: passage}
				}
				selected, e := selectLiveModelMessageSources(p, sources)
				canonical, canonicalErr := modelegressbudget.SelectLivePublicSources(sources)
				if canonicalErr != nil {
					t.Fatal(canonicalErr)
				}
				firstPayload, payloadErr := liveSourceModelPayload(p, canonical[:1])
				if payloadErr != nil {
					t.Fatal(payloadErr)
				}
				if len(firstPayload) > 4096 {
					if !errors.Is(e, modelegressbudget.ErrDenied) || len(selected) != 0 {
						t.Fatal("single escaped result above the encoded limit received fake/empty success", e)
					}
					return
				}
				if e != nil || len(selected) == 0 {
					t.Fatal(e)
				}
				payload, e := liveSourceModelPayload(p, selected)
				if e != nil || len(payload) > 4096 || !utf8.ValidString(payload) {
					t.Fatal("UTF8 or JSON encoded size changed", e)
				}
				for _, source := range selected {
					if !utf8.ValidString(source.Passage) || source.Title != sources[0].Title || len(source.Passage) > 1024 {
						t.Fatal("source was split within UTF8 or proper name changed")
					}
				}
				if _, _, e = sourceMessageBoundWire(t, p, selected, now); e != nil {
					t.Fatal("selected escaped payload failed the original model message contract", e)
				}
			})
		}
	}
}

func TestLiveSourceMessageBoundExactLimitAndOversizedFirstSource(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, resolved := range []bool{false, true} {
		t.Run(fmt.Sprintf("resolved_%t", resolved), func(t *testing.T) {
			p := sourceMessageBoundPrepared(t, now, resolved)
			source := modelegressbudget.LivePublicSource{Title: strings.Repeat(`"`, 512), URL: "https://example.org/a", Passage: strings.Repeat("x", 1024)}
			payload, e := liveSourceModelPayload(p, []modelegressbudget.LivePublicSource{source})
			if e != nil {
				t.Fatal(e)
			}
			source.URL += strings.Repeat("x", 4096-len(payload))
			payload, e = liveSourceModelPayload(p, []modelegressbudget.LivePublicSource{source})
			if e != nil || len(payload) != 4096 {
				t.Fatal("exact boundary fixture invalid", e)
			}
			selected, e := selectLiveModelMessageSources(p, []modelegressbudget.LivePublicSource{source})
			if e != nil || len(selected) != 1 || selected[0] != source {
				t.Fatal("exact boundary source rejected or modified", e)
			}
			if _, _, e = sourceMessageBoundWire(t, p, selected, now); e != nil {
				t.Fatal(e)
			}
			source.URL += "x"
			selected, e = selectLiveModelMessageSources(p, []modelegressbudget.LivePublicSource{source})
			if !errors.Is(e, modelegressbudget.ErrDenied) || len(selected) != 0 || !strings.Contains(e.Error(), "source-payload-message-bound") {
				t.Fatal("oversized first result became fake/empty success or lost its stage", e)
			}
		})
	}
}

func TestLiveSourceMessageBoundFullArtifactAndActualSelectionStaySeparate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, resolved := range []bool{false, true} {
		t.Run(fmt.Sprintf("resolved_%t", resolved), func(t *testing.T) {
			p := sourceMessageBoundPrepared(t, now, resolved)
			access := agentevent.Access{SessionDigest: [32]byte{1}}
			output := OwnLiveSearchOutput{prepared: p, access: access, operation: "11000000-0000-4000-8000-000000000001", query: p.SearchQuery(), observedAt: now.Add(-time.Millisecond), result: agenttool.TencentWSAResult{RequestID: "11000000-0000-4000-8000-000000000003", Version: "standard", CashStatus: "UNKNOWN"}}
			for i := 0; i < 10; i++ {
				output.result.Sources = append(output.result.Sources, agenttool.TencentWSASource{Title: "Public museum", URL: fmt.Sprintf("https://example.org/%d", i), Passage: strings.Repeat("x", 1024), Site: "Public site metadata"})
			}
			batch, e := sourceBatchFromOutput(access, output, now)
			if e != nil || batch.valid || batch.evidenceDigest != "" || batch.data.Evidence.PreviewID != "" || len(batch.Sources()) >= 10 {
				t.Fatal("data projection minted authority or lost bounded selection", e)
			}
			artifact, _ := json.Marshal(output.result)
			selectedDigest, _ := modelegressbudget.LivePublicSourcesDigest(batch.Sources())
			if batch.data.Evidence.ArtifactSHA256 != liveDigestBytes("birdtie.public-search-original-artifact.v1", artifact) || batch.data.Evidence.SelectedSourcesDigest != selectedDigest || batch.data.Evidence.QueryEvidenceDigest != p.queryDigest {
				t.Fatal("full original artifact, selected sources and literal query digests were conflated")
			}
			output.result.Sources[9].Passage = "unselected changed public source"
			changed, e := sourceBatchFromOutput(access, output, now)
			if e != nil || changed.data.Evidence.ArtifactSHA256 == batch.data.Evidence.ArtifactSHA256 || changed.data.Evidence.SelectedSourcesDigest != selectedDigest {
				t.Fatal("unselected original source was omitted from full artifact identity", e)
			}
			payload, _ := liveSourceModelPayload(p, batch.Sources())
			if resolved {
				var dto struct {
					CurrentQuery string `json:"currentQuery"`
				}
				if json.Unmarshal([]byte(payload), &dto) != nil || dto.CurrentQuery != p.request.Messages[1].Content {
					t.Fatal("literal current question changed during bounded selection")
				}
			}
			copySources := batch.Sources()
			copySources[0].Title = "changed"
			if batch.Sources()[0].Title == "changed" {
				t.Fatal("source selection returned mutable batch storage")
			}
		})
	}
}

func TestLiveSourceMessageBoundRetainsClosedSourceAndRequestGuards(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	p := sourceMessageBoundPrepared(t, now, true)
	valid := liveSourceTestSources()[0]
	for name, sources := range map[string][]modelegressbudget.LivePublicSource{
		"empty": nil, "duplicate": {valid, valid},
		"private_url":  {{Title: "private", URL: "http://127.0.0.1/private", Passage: "data"}},
		"invalid_utf8": {{Title: string([]byte{0xff}), URL: valid.URL, Passage: "data"}},
		"eleven":       make([]modelegressbudget.LivePublicSource, 11),
	} {
		t.Run(name, func(t *testing.T) {
			if selected, e := selectLiveModelMessageSources(p, sources); e == nil || len(selected) != 0 {
				t.Fatal("source selection weakened a closed public-source guard")
			}
		})
	}
	// Validation covers the entire provider projection before choosing a
	// prefix. A rejected source after the fitting prefix cannot be hidden by it.
	for _, defect := range []string{"private_url", "duplicate_url"} {
		t.Run("unselected_"+defect, func(t *testing.T) {
			sources := make([]modelegressbudget.LivePublicSource, 10)
			for i := range sources {
				sources[i] = modelegressbudget.LivePublicSource{Title: "Public museum", URL: fmt.Sprintf("https://example.org/%d", i), Passage: strings.Repeat("x", 1024)}
			}
			if defect == "private_url" {
				sources[9].URL = "http://127.0.0.1/private"
			} else {
				sources[9].URL = sources[0].URL
			}
			if selected, e := selectLiveModelMessageSources(p, sources); e == nil || len(selected) != 0 {
				t.Fatal("prefix concealed an invalid unselected source")
			}
		})
	}
	r := p.request
	r.Messages = append([]modelgateway.Message(nil), r.Messages...)
	r.Messages[1].Content = strings.Repeat("x", 4097)
	if modelgateway.ValidateRequest(r, now) == nil {
		t.Fatal("original message validator was enlarged")
	}
	if _, e := liveLedgerAdapter(t).Prepare(liveProviderRequest(r)); e == nil {
		t.Fatal("concrete provider formatter accepted an oversized generic message")
	}
	for _, request := range []modelgateway.Request{{}, {Messages: []modelgateway.Message{{Role: "user", Content: "invented"}}}} {
		p.request = request
		if selected, e := selectLiveModelMessageSources(p, []modelegressbudget.LivePublicSource{valid}); e == nil || len(selected) != 0 {
			t.Fatal("resolved literal question was synthesized from missing native messages")
		}
	}
}
