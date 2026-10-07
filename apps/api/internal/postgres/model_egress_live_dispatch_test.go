package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

type panicNilLiveGate struct{}

func (p *panicNilLiveGate) InferenceEnabled(context.Context) bool {
	if p == nil {
		panic("typed nil gate must not be invoked")
	}
	return true
}

func TestNativeLiveDispatchMissingBoundaryCannotInvoke(t *testing.T) {
	var s *Store
	var h *nativeLiveModelDispatch
	if nativeLiveGatePresent(nil) || nativeLiveGatePresent((*panicNilLiveGate)(nil)) {
		t.Fatal("missing gate accepted")
	}
	if !nativeLiveGatePresent(&panicNilLiveGate{}) {
		t.Fatal("trusted live gate missing")
	}
	if _, _, err := s.NewOwnLiveModelDispatch(context.Background(), agentevent.Access{}, "not-an-operation", nil, nil, nil); err == nil {
		t.Fatal("missing native store granted dispatch")
	}
	if h.CheckCurrent(context.Background(), modelgateway.Request{}, modelgateway.PreparedTencentWire{}) == nil {
		t.Fatal("missing native model port accepted request")
	}
	called := false
	if _, err := h.ReleaseOnce(context.Background(), modelgateway.Request{}, modelgateway.PreparedTencentWire{}, func(context.Context) ([]byte, error) {
		called = true
		return nil, nil
	}); err == nil || called {
		t.Fatal("missing native model port invoked provider")
	}
	if h.SettleUnknown(context.Background(), modelgateway.Request{}, modelgateway.PreparedTencentWire{}) == nil {
		t.Fatal("missing hold reported settled")
	}
	if _, err := s.ExecuteOwnLiveSearch(context.Background(), agentevent.Access{}, "not-an-operation", nil, nil, nil); err == nil {
		t.Fatal("missing native store granted search")
	}
}

func TestNativeLiveQueryOnlyRequestComparison(t *testing.T) {
	r := modelgateway.Request{RunID: "run-a", Messages: []modelgateway.Message{{Role: "user", Content: "query-a"}}}
	copy := r
	copy.Messages = append([]modelgateway.Message(nil), r.Messages...)
	if !nativeLiveRequestEqual(r, copy) {
		t.Fatal("equal wire selectors rejected")
	}
	copy.Messages[0].Content = "query-b"
	if nativeLiveRequestEqual(r, copy) {
		t.Fatal("changed question matched original operation")
	}
	copy = r
	copy.BudgetRef = "another-root"
	if nativeLiveRequestEqual(r, copy) {
		t.Fatal("changed budget root matched original operation")
	}
	copy = r
	copy.Messages = []modelgateway.Message{{Role: "user", Content: strings.Repeat("x", modelgateway.MaxRequestBytes)}}
	if nativeLiveRequestEqual(copy, copy) {
		t.Fatal("overlarge request matched")
	}
}

func TestNativeLiveSearchOutputIsUntrustedCopiedData(t *testing.T) {
	out := OwnLiveSearchOutput{query: "private-current-query-canary", result: agenttool.TencentWSAResult{
		RequestID: "provider-id", CashStatus: "UNKNOWN", Sources: []agenttool.TencentWSASource{{Title: "source", URL: "https://example.com/source", Passage: "untrusted public snippet"}},
	}}
	first := out.Result()
	first.Sources[0].Title = "changed"
	if out.Result().Sources[0].Title != "source" {
		t.Fatal("mutable caller source alias")
	}
	if _, err := json.Marshal(out); err == nil {
		t.Fatal("source data became reconstructable grant")
	}
	var decoded OwnLiveSearchOutput
	if json.Unmarshal([]byte(`{"query":"injected"}`), &decoded) == nil {
		t.Fatal("client input reconstructed native source bundle")
	}
	if strings.Contains(fmt.Sprintf("%+v", out), "private-current-query-canary") {
		t.Fatal("private query exposed by diagnostic formatting")
	}
}
