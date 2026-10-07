package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

func TestModelLiveSourceRunNilDependenciesAndSealedMetadata(t *testing.T) {
	var h *nativeLiveSourceAnswerRun
	if _, e := h.Read(context.Background()); e == nil {
		t.Fatal("nil read")
	}
	if e := h.Stop(context.Background()); e == nil {
		t.Fatal("nil stop")
	}
	if _, e := h.ExecuteSourceSearch(context.Background(), nil); e == nil {
		t.Fatal("nil search")
	}
	if _, e := h.CompleteSourceModel(context.Background(), nil); e == nil {
		t.Fatal("nil inference")
	}
	if _, e := h.ReserveSource(context.Background()); e == nil {
		t.Fatal("nil reserve")
	}
	if _, e := h.PreviewSourceModel(context.Background(), OwnLiveSourceBatch{}); e == nil {
		t.Fatal("nil preview")
	}
	if e := h.ApproveSourceModel(context.Background(), "", ""); e == nil {
		t.Fatal("nil approve")
	}
	if _, e := h.BindSourceModel(context.Background(), "", "", 1); e == nil {
		t.Fatal("nil bind")
	}
	for _, v := range []any{&nativeLiveSourceAnswerRun{}, &nativeLiveSourceRunAssociation{}} {
		if _, e := json.Marshal(v); !errors.Is(e, modelegressbudget.ErrServerOnly) {
			t.Fatal("sealed JSON marshal", e)
		}
		if fmt.Sprint(v) == "" {
			t.Fatal("missing redacted format")
		}
	}
	var association nativeLiveSourceRunAssociation
	if e := json.Unmarshal([]byte(`{}`), &association); !errors.Is(e, modelegressbudget.ErrServerOnly) {
		t.Fatal("sealed association decode", e)
	}
}
func TestModelLiveSourceRunAssociationRejectsNoNativeAuthorityBeforeSQL(t *testing.T) {
	for _, association := range []*nativeLiveSourceRunAssociation{nil, {}, {handle: &nativeLiveSourceAnswerRun{}}} {
		if e := validateLiveSourceRunAssociationTx(context.Background(), nil, agentevent.Access{}, association, OwnLiveSourceBatch{}); !errors.Is(e, modelegressbudget.ErrDenied) {
			t.Fatal("metadata created linked authority", e)
		}
	}
	p := &nativeLiveSourceModelDispatch{}
	if e := p.CheckCurrent(context.Background(), modelgateway.Request{}, modelgateway.PreparedTencentWire{}); e == nil {
		t.Fatal("nil bridge")
	}
	if _, e := p.ReleaseOnce(context.Background(), modelgateway.Request{}, modelgateway.PreparedTencentWire{}, nil); e == nil {
		t.Fatal("nil callback bridge")
	}
	if e := p.SettleUnknown(context.Background(), modelgateway.Request{}, modelgateway.PreparedTencentWire{}); e == nil {
		t.Fatal("unused bridge settlement")
	}
}

func TestModelLiveSourceRunCompletedTextCopyOwnsUsage(t *testing.T) {
	input, output := int64(41), int64(12)
	result := modelgateway.Result{Text: "UNIT validated native text", Usage: modelgateway.Usage{Status: "KNOWN", CostStatus: "UNKNOWN", InputTokens: &input, OutputTokens: &output}}
	saved := cloneValidatedLiveSourceTextResult(result)
	if saved == nil || saved.Text != result.Text || saved.Usage.InputTokens == result.Usage.InputTokens || saved.Usage.OutputTokens == result.Usage.OutputTokens {
		t.Fatal("validated completion shares mutable usage")
	}
	*result.Usage.InputTokens, *result.Usage.OutputTokens = 99, 100
	result.Text = "caller mutation"
	if saved.Text != "UNIT validated native text" || *saved.Usage.InputTokens != 41 || *saved.Usage.OutputTokens != 12 || saved.Usage.CostStatus != "UNKNOWN" || saved.Answer != nil || saved.Candidate != nil || len(saved.ToolProposals) != 0 {
		t.Fatal("returned result changed private completion")
	}
	withoutUsage := cloneValidatedLiveSourceTextResult(modelgateway.Result{Text: "UNIT unknown usage"})
	if withoutUsage.Usage.InputTokens != nil || withoutUsage.Usage.OutputTokens != nil {
		t.Fatal("copy invented usage")
	}
}

func TestModelLiveSourceRunStagesPreserveOriginalErrorIdentity(t *testing.T) {
	err := nativeLiveStage("bind-source-replay", modelegressbudget.ErrDenied)
	if !errors.Is(err, modelegressbudget.ErrDenied) || nativeLiveStage("check-current", nil) != nil {
		t.Fatal("stage changed original failure identity")
	}
	var staged *nativeLiveRunStageError
	if !errors.As(err, &staged) || staged.stage != "bind-source-replay" {
		t.Fatal("stage omitted native failed boundary")
	}
}

func TestModelLiveSourceRunFailureStagesRedactDataAndKeepEveryCause(t *testing.T) {
	private := errors.New("https://private.example/?query=PRIVATE_QUERY&key=PRIVATE_KEY")
	joined := errors.Join(nativeLiveStage("search-provider", private), nativeLiveStage("search-after-current", modelegressbudget.ErrDenied), nativeLiveStage("search-settle-unknown", modelegressbudget.ErrConflict), nativeLiveStage("search-wire-current", context.DeadlineExceeded))
	err := nativeLiveStage("search-call-result", joined)
	for _, cause := range []error{private, modelegressbudget.ErrDenied, modelegressbudget.ErrConflict, context.DeadlineExceeded} {
		if !errors.Is(err, cause) {
			t.Fatal("source failure discarded an original cause")
		}
	}
	message := err.Error()
	if strings.Contains(message, "PRIVATE") || strings.Contains(message, "private.example") {
		t.Fatal("server stage exposed arbitrary transport text")
	}
	for _, stage := range []string{"search-provider", "search-after-current", "search-settle-unknown", "search-wire-current"} {
		if !strings.Contains(message, stage) {
			t.Fatal("missing independent source failure stage", stage)
		}
	}
	for _, code := range []string{"AUTHENTICATION", "QUERY_REJECTED", "PRIVATE_PROVIDER_CANARY"} {
		cause := agenttool.TencentWSAProviderError{Code: code}
		staged := nativeLiveStage("search-provider", cause)
		var original agenttool.TencentWSAProviderError
		if !errors.As(staged, &original) || original.Code != code || strings.Contains(staged.Error(), "PRIVATE") {
			t.Fatal("provider stage leaked text or lost typed cause")
		}
		if code != "PRIVATE_PROVIDER_CANARY" && !strings.Contains(staged.Error(), "WSA_"+code) {
			t.Fatal("safe provider class disappeared")
		}
	}
}
