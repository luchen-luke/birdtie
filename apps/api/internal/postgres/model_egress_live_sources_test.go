package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

func liveSourceTestSources() []modelegressbudget.LivePublicSource {
	return []modelegressbudget.LivePublicSource{{Title: "Aberdeen Maritime Museum", URL: "https://www.aberdeencity.gov.uk/museum", Passage: "Official visitor information"}}
}

func liveSourceTestData(now time.Time) (liveSourceBatchData, string) {
	sources := liveSourceTestSources()
	selection, _ := modelegressbudget.LivePublicSourcesDigest(sources)
	v := modelegressbudget.LiveSourceEvidence{OperationID: "11000000-0000-4000-8000-000000000001", PreviewID: "11000000-0000-4000-8000-000000000002", ProviderRequestID: "11000000-0000-4000-8000-000000000003", SourceRequestDigest: strings.Repeat("a", 64), SourcePayloadDigest: strings.Repeat("b", 64), QueryEvidenceDigest: liveDigestBytes("birdtie.live-current-query.v1", []byte("current query")), ArtifactSHA256: strings.Repeat("c", 64), SelectedSourcesDigest: selection, ObservedAt: now.Add(-time.Second), DeadlineAt: now.Add(time.Minute)}
	h, _ := modelegressbudget.LiveSourceEvidenceDigest(v, now)
	return liveSourceBatchData{Evidence: v, Sources: sources}, h
}

func TestLiveSourceLedgerTypedWireKeepsQueryOnlyClosed(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	query := "current query"
	sources := liveSourceTestSources()
	payload, err := modelegressbudget.LivePublicSearchPayload(query, sources)
	if err != nil {
		t.Fatal(err)
	}
	r := liveLedgerRequest(now, query)
	r.Messages[1].Content = payload
	price := liveLedgerPrice(now, modelegressbudget.LiveToken)
	w, q, h, amount, err := prepareLiveSourceWire(query, sources, r, price, liveLedgerAdapter(t), now)
	if err != nil || amount.Amount.InputTokens != 196608 || amount.Amount.OutputTokens != 768 || amount.Amount.CostMicros != 199680 || amount.Requests != 1 || q != liveDigestBytes("birdtie.live-current-query.v1", []byte(query)) || h != w.WireDigest() || !w.Matches(liveProviderRequest(r), now) {
		t.Fatal("exact typed source wire failed", err)
	}
	if _, _, _, _, err := prepareLiveWire(query, r, price, liveLedgerAdapter(t), now); err == nil {
		t.Fatal("source context entered the original query-only scope")
	}
	r.Messages[1].Content = query
	if _, _, _, _, err := prepareLiveSourceWire(query, sources, r, price, liveLedgerAdapter(t), now); err == nil {
		t.Fatal("source scope accepted query-only payload")
	}
	if _, _, _, _, err := prepareLiveWire(query, r, price, liveLedgerAdapter(t), now); err != nil {
		t.Fatal("old scope stopped working", err)
	}
}

func TestLiveSourceLedgerCanonicalPayloadPreservesExactScalarEvidence(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	query := "  current query  "
	sources := liveSourceTestSources()
	payload, err := modelegressbudget.LivePublicSearchPayload(strings.TrimSpace(query), sources)
	if err != nil {
		t.Fatal(err)
	}
	r := liveLedgerRequest(now, query)
	r.Messages[1].Content = payload
	price := liveLedgerPrice(now, modelegressbudget.LiveToken)
	_, queryDigest, _, _, err := prepareLiveSourceWire(query, sources, r, price, liveLedgerAdapter(t), now)
	if err != nil || queryDigest != liveDigestBytes("birdtie.live-current-query.v1", []byte(query)) || queryDigest == liveDigestBytes("birdtie.live-current-query.v1", []byte(strings.TrimSpace(query))) {
		t.Fatal("canonical payload lost original current scalar evidence", err)
	}
}

func TestLiveSourceLedgerRejectsChangedWireAndUnapprovedExtras(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	a := liveLedgerAdapter(t)
	sources := liveSourceTestSources()
	payload, _ := modelegressbudget.LivePublicSearchPayload("current query", sources)
	for name, change := range map[string]func(*modelgateway.Request, *modelegressbudget.LivePrice, *LiveWireProjector){
		"extra_history": func(r *modelgateway.Request, _ *modelegressbudget.LivePrice, _ *LiveWireProjector) {
			r.Messages = append(r.Messages, modelgateway.Message{Role: "user", Content: "prior private conversation"})
		},
		"context_role": func(r *modelgateway.Request, _ *modelegressbudget.LivePrice, _ *LiveWireProjector) {
			r.Messages[1].Role = "context"
		},
		"tool_allowlist": func(r *modelgateway.Request, _ *modelegressbudget.LivePrice, _ *LiveWireProjector) {
			r.ToolAllowlist = []string{"activity.detail"}
		},
		"tool_output": func(r *modelgateway.Request, _ *modelegressbudget.LivePrice, _ *LiveWireProjector) {
			r.OutputMode = modelgateway.ToolProposals
		},
		"wrong_source_body": func(r *modelgateway.Request, _ *modelegressbudget.LivePrice, _ *LiveWireProjector) {
			r.Messages[1].Content += " "
		},
		"lower_input_bound": func(_ *modelgateway.Request, p *modelegressbudget.LivePrice, _ *LiveWireProjector) {
			p.Base.InputTokenCeiling--
		},
		"CALL_tariff": func(_ *modelgateway.Request, p *modelegressbudget.LivePrice, _ *LiveWireProjector) {
			*p = liveLedgerPrice(now, modelegressbudget.LiveCall)
		},
		"nil_projector": func(_ *modelgateway.Request, _ *modelegressbudget.LivePrice, p *LiveWireProjector) { *p = nil },
		"changed_projection": func(_ *modelgateway.Request, _ *modelegressbudget.LivePrice, p *LiveWireProjector) {
			*p = liveLedgerProjector(func(r modelgateway.ProviderRequest) (modelgateway.PreparedTencentWire, error) {
				r.Messages[1].Content = "unapproved source"
				return a.Prepare(r)
			})
		},
		"mutated_prompt": func(_ *modelgateway.Request, _ *modelegressbudget.LivePrice, p *LiveWireProjector) {
			*p = liveLedgerProjector(func(r modelgateway.ProviderRequest) (modelgateway.PreparedTencentWire, error) {
				r.Messages[0].Content = "different prompt"
				return a.Prepare(r)
			})
		},
		"expired": func(r *modelgateway.Request, _ *modelegressbudget.LivePrice, _ *LiveWireProjector) {
			r.DeadlineAt = now
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := liveLedgerRequest(now, "current query")
			r.Messages[1].Content = payload
			price := liveLedgerPrice(now, modelegressbudget.LiveToken)
			var projector LiveWireProjector = a
			change(&r, &price, &projector)
			w, _, _, upper, err := prepareLiveSourceWire("current query", sources, r, price, projector, now)
			if err == nil || len(w.ExactWire()) != 0 || upper.Amount.CostMicros != 0 {
				t.Fatal("changed source input received usable wire")
			}
		})
	}
}

func TestLiveSourceLedgerEvidenceAndSelectedBodyRevalidate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	data, digest := liveSourceTestData(now)
	if err := validateLiveSourceBatchData(data, digest, now); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*liveSourceBatchData){
		"title":       func(d *liveSourceBatchData) { d.Sources[0].Title = "changed" },
		"url":         func(d *liveSourceBatchData) { d.Sources[0].URL += "?changed=1" },
		"passage":     func(d *liveSourceBatchData) { d.Sources[0].Passage = "changed" },
		"operation":   func(d *liveSourceBatchData) { d.Evidence.OperationID = "11000000-0000-4000-8000-000000000004" },
		"artifact":    func(d *liveSourceBatchData) { d.Evidence.ArtifactSHA256 = strings.Repeat("d", 64) },
		"expired":     func(d *liveSourceBatchData) { d.Evidence.DeadlineAt = now },
		"observation": func(d *liveSourceBatchData) { d.Evidence.ObservedAt = now.Add(time.Second) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := data
			changed.Sources = append([]modelegressbudget.LivePublicSource(nil), data.Sources...)
			change(&changed)
			if err := validateLiveSourceBatchData(changed, digest, now); err == nil {
				t.Fatal("modified or expired evidence accepted")
			}
		})
	}
	if err := validateLiveSourceBatchData(data, digest, now.Add(time.Minute)); err == nil {
		t.Fatal("original expiry extended")
	}
	raw, _ := json.Marshal(data)
	decoded, err := readLiveSourceBatchData(raw)
	if err != nil || validateLiveSourceBatchData(decoded, digest, now) != nil {
		t.Fatal("stored bounded data did not decode", err)
	}
	for name, raw := range map[string][]byte{"empty": nil, "trailing": append(raw, []byte("{}")...), "unknown": []byte(`{"evidence":{},"sources":[],"grant":true}`), "extra_source": []byte(`{"evidence":{},"sources":[{"title":"x","url":"https://example.org/x","passage":"x","latitude":1}]}`), "too_large": []byte(strings.Repeat("x", 16385))} {
		t.Run(name, func(t *testing.T) {
			if _, err := readLiveSourceBatchData(raw); err == nil {
				t.Fatal("non-closed storage data accepted")
			}
		})
	}
}

func TestLiveSourceLedgerServerOnlyBatchCopiesAndNilStore(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	data, digest := liveSourceTestData(now)
	b := OwnLiveSourceBatch{data: data, evidenceDigest: digest, valid: true}
	copy := b.Sources()
	copy[0].Passage = "changed"
	if b.Sources()[0].Passage == "changed" {
		t.Fatal("source accessor mutated authenticated data")
	}
	if strings.Contains(fmt.Sprintf("%#v", b), "Museum") || strings.Contains(fmt.Sprintf("%+v", b), digest) {
		t.Fatal("batch leaked via formatting")
	}
	if _, err := json.Marshal(b); !errors.Is(err, modelegressbudget.ErrServerOnly) {
		t.Fatal("batch marshaled", err)
	}
	if err := json.Unmarshal([]byte(`{"valid":true}`), &b); !errors.Is(err, modelegressbudget.ErrServerOnly) || b.valid || b.evidenceDigest != "" {
		t.Fatal("JSON reconstructed batch authority")
	}
	var s *Store
	if _, err := s.AuthenticateOwnLiveSearchSources(context.Background(), agentevent.Access{}, OwnLiveSearchOutput{}); !errors.Is(err, modelegressbudget.ErrUnavailable) {
		t.Fatal("nil store minted sources", err)
	}
	if _, err := s.PreviewOwnLiveSourceEgress(context.Background(), agentevent.Access{}, modelegressbudget.PreviewInput{}, b, nil); !errors.Is(err, modelegressbudget.ErrUnavailable) {
		t.Fatal("nil store approved source export", err)
	}
	if _, err := s.revalidateLiveSourceBatchTx(context.Background(), nil, agentevent.Access{}, b, nil); !errors.Is(err, modelegressbudget.ErrDenied) {
		t.Fatal("JSON/zero batch reached native transaction", err)
	}
	if _, err := s.prepareStoredLiveEgressTx(context.Background(), nil, agentevent.Access{}, storedLiveEgressPreview{scope: "FULL_HISTORY"}, nil); !errors.Is(err, modelegressbudget.ErrDenied) {
		t.Fatal("unrecognized scope reached native transaction", err)
	}
	if _, err := s.prepareStoredLiveEgressTx(context.Background(), nil, agentevent.Access{}, storedLiveEgressPreview{scope: modelegressbudget.Scope, sourceBatch: []byte(`{}`)}, nil); !errors.Is(err, modelegressbudget.ErrDenied) {
		t.Fatal("old scope accepted extra source data", err)
	}
	if _, err := oneLiveSourceRunAssociation([]*nativeLiveSourceRunAssociation{nil, nil}); !errors.Is(err, modelegressbudget.ErrDenied) {
		t.Fatal("multiple associations accepted")
	}
	if a, err := oneLiveSourceRunAssociation(nil); err != nil || a != nil {
		t.Fatal("public default changed")
	}
}

func TestLiveSourceLedgerStoredPreviewScopeAndEvidenceAreExact(t *testing.T) {
	p := PreparedLiveEgress{owner: "owner", session: "session", binding: "binding", source: "source", authority: "authority", digest: "digest", queryDigest: "query", payloadDigest: "wire", price: modelegressbudget.LivePrice{Kind: modelegressbudget.LiveToken}, request: liveLedgerRequest(time.Now(), "query")}
	v := storedLiveEgressPreview{storedEgressPreview: storedEgressPreview{owner: p.owner, session: p.session, agent: p.request.Agent.AgentID, binding: p.binding, source: p.source, authority: p.authority, digest: p.digest}, kind: p.price.Kind, queryDigest: p.queryDigest, payloadDigest: p.payloadDigest, purpose: modelegressbudget.Purpose, scope: modelegressbudget.Scope}
	if !matchLivePreview(v, p) {
		t.Fatal("old scope failed exact match")
	}
	p.sourceBatch = OwnLiveSourceBatch{valid: true, evidenceDigest: "actual source proof digest"}
	if matchLivePreview(v, p) {
		t.Fatal("source export reused query-only approval")
	}
	v.scope = modelegressbudget.LivePublicSearchScope
	v.sourceEvidenceDigest = p.sourceBatch.evidenceDigest
	if !matchLivePreview(v, p) {
		t.Fatal("exact source scope failed")
	}
	v.sourceEvidenceDigest = "changed"
	if matchLivePreview(v, p) {
		t.Fatal("different source batch reused approval")
	}
}

func TestLiveSourceLedgerSuccessfulOutputIsStillUnapprovedData(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	access := agentevent.Access{SessionDigest: [32]byte{1}}
	p := PreparedLiveEgress{input: modelegressbudget.PreviewInput{RootTraceID: "11000000-0000-4000-8000-000000000004", TaskID: "11000000-0000-4000-8000-000000000005", DeadlineAt: now.Add(time.Minute)}, price: liveLedgerPrice(now, modelegressbudget.LiveCall), query: "current query", queryDigest: liveDigestBytes("birdtie.live-current-query.v1", []byte("current query")), payloadDigest: strings.Repeat("b", 64), digest: strings.Repeat("a", 64), owner: "owner", session: "session", binding: "binding", source: "source", authority: "authority"}
	output := OwnLiveSearchOutput{prepared: p, query: "current query", operation: "11000000-0000-4000-8000-000000000001", access: access, observedAt: now.Add(-time.Second), result: agenttool.TencentWSAResult{RequestID: "11000000-0000-4000-8000-000000000003", Version: "standard", CashStatus: "UNKNOWN", Sources: []agenttool.TencentWSASource{{Title: "Aberdeen Maritime Museum", URL: "https://www.aberdeencity.gov.uk/museum", Passage: "Official visitor information", Site: "Aberdeen City", Date: "2026-10-08"}}}}
	b, err := sourceBatchFromOutput(access, output, now)
	if err != nil || b.valid || b.evidenceDigest != "" || b.data.Evidence.PreviewID != "" || len(b.Sources()) != 1 {
		t.Fatal("data projection minted native permission", err)
	}
	for name, edit := range map[string]func(*OwnLiveSearchOutput){
		"wrong_session":      func(o *OwnLiveSearchOutput) { o.access.SessionDigest = [32]byte{2} },
		"different_query":    func(o *OwnLiveSearchOutput) { o.query = "other query" },
		"no_request_id":      func(o *OwnLiveSearchOutput) { o.result.RequestID = "" },
		"cash_receipt_claim": func(o *OwnLiveSearchOutput) { o.result.CashStatus = "FREE" },
		"no_operation":       func(o *OwnLiveSearchOutput) { o.operation = "" },
		"MODEL_instead_CALL": func(o *OwnLiveSearchOutput) { o.prepared.price.Kind = modelegressbudget.LiveToken },
		"expired_original":   func(o *OwnLiveSearchOutput) { o.prepared.input.DeadlineAt = now },
	} {
		t.Run(name, func(t *testing.T) {
			changed := output
			edit(&changed)
			if _, err := sourceBatchFromOutput(access, changed, now); err == nil {
				t.Fatal("invalid output accepted")
			}
		})
	}
	oldArtifact := b.data.Evidence.ArtifactSHA256
	output.result.Sources[0].Site = "changed site excluded from model payload"
	changed, err := sourceBatchFromOutput(access, output, now)
	if err != nil || changed.data.Evidence.ArtifactSHA256 == oldArtifact || changed.data.Evidence.SelectedSourcesDigest != b.data.Evidence.SelectedSourcesDigest {
		t.Fatal("full artifact proof or selected projection conflated", err)
	}
}
