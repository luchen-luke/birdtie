package postgres

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

// No database or provider calls: only the concrete adapter's fake transport
// and original safe native error projection are used in these unit fixtures.
type modelDiagnosticTestTransport func(*http.Request) (*http.Response, error)

func (f modelDiagnosticTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestModelLiveSourceDiagnosticsPreserveModelHTTPAndErrorsAs(t *testing.T) {
	config, err := modelgateway.ParseTencentTokenHubConfig([]byte(`{"provider":"tencent_tokenhub","baseUrl":"https://tokenhub.tencentmaas.com/v1","model":"hy3","maxOutputTokens":768,"apiKey":"synthetic-unit-placeholder"}`))
	if err != nil {
		t.Fatal("synthetic adapter config invalid")
	}
	adapter, err := modelgateway.NewTencentTokenHubAdapter(config, modelDiagnosticTestTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Header: http.Header{"Content-Type": {"application/json"}}, Body: http.NoBody}, nil
	}))
	if err != nil {
		t.Fatal("synthetic adapter invalid")
	}
	_, err = adapter.Complete(context.Background(), modelgateway.ProviderRequest{
		TaskKind: modelgateway.ActivityQuery, PromptVersion: "unit.diagnostic.v1", OutputMode: modelgateway.Text,
		OutputSchemaVersion: "air.answer.v1", MaxOutputTokens: 128, DeadlineAt: time.Now().Add(time.Minute),
		Messages: []modelgateway.Message{{Role: "user", Content: "synthetic public question"}},
	})
	err = nativeLiveStage("model-gateway-complete", modelgateway.WithLiveFailureReason(err, "provider_AUTHENTICATION"))
	var provider modelgateway.ProviderError
	if !errors.As(err, &provider) || provider.Code != "AUTHENTICATION" {
		t.Fatal("native diagnostic lost original typed model provider error")
	}
	for _, text := range []string{"model-gateway-complete", "MODEL", "TENCENT_HTTP", "kind=HTTP", "status=403", "reason=provider_AUTHENTICATION"} {
		if !strings.Contains(err.Error(), text) {
			t.Fatal("native server summary lost fixed model diagnostic", text)
		}
	}
}

func TestModelLiveSourceDiagnosticsKeepOriginalWSABudgetAndContextClassification(t *testing.T) {
	for _, tc := range []struct {
		cause error
		code  string
	}{
		{agenttool.TencentWSAProviderError{Code: "AUTHENTICATION"}, "WSA_AUTHENTICATION"},
		{agenttool.TencentWSAProviderError{Code: "PRIVATE_PROVIDER_CODE"}, "WSA_PROVIDER_UNKNOWN"},
		{modelegressbudget.ErrBudget, "BUDGET"}, {modelegressbudget.ErrDenied, "DENIED"},
		{context.Canceled, "CANCELED"}, {context.DeadlineExceeded, "DEADLINE"},
	} {
		err := nativeLiveStage("search-provider", tc.cause)
		if !errors.Is(err, tc.cause) || nativeLiveFailureSummary(tc.cause) != tc.code {
			t.Fatal("new model diagnostic changed an original WSA/native class")
		}
	}
}

func TestModelLiveSourceDiagnosticsFormatNeverProjectsPrivateErrorText(t *testing.T) {
	private := errors.New("PRIVATE_CAUSE https://private.example/?query=PRIVATE_QUERY&key=PRIVATE_KEY")
	model := modelgateway.WithLiveFailureReason(errors.Join(modelgateway.ErrAdapter, private), "provider_PRIVATE_CODE")
	err := nativeLiveStage("model-gateway-complete", errors.Join(model, nativeLiveStage("search-wire-current", private)))
	if !errors.Is(err, private) || !errors.Is(err, modelgateway.ErrAdapter) {
		t.Fatal("fixed native diagnostics discarded original cause identity")
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d", "%w"} {
		text := fmt.Sprintf(format, err)
		if strings.Contains(text, "PRIVATE") || strings.Contains(text, "private.example") || strings.Contains(text, "tokenhub.tencentmaas.com") {
			t.Fatal("native error formatting exposed raw error, query, key or URL")
		}
		if format != "%w" && (!strings.Contains(text, "reason=unknown") || !strings.Contains(text, "status=0") || !strings.Contains(text, "MODEL")) {
			t.Fatal("fixed model reason/status were lost in native formatting")
		}
	}
	if wrapped := fmt.Errorf("%w", err); !errors.Is(wrapped, private) || strings.Contains(wrapped.Error(), "PRIVATE") {
		t.Fatal("native error wrapping lost safe cause identity")
	}
}

func TestModelLiveSourceDiagnosticsAIServiceAvailabilitySurvivesOriginalNativeWrapper(t *testing.T) {
	// Minimal synthetic body from the old 402/403004 receipt; original packet
	// was NOT_CAPTURED. This selected-model transport never leaves this test.
	config, e := modelgateway.ParseTencentTokenHubConfig([]byte(`{"provider":"tencent_tokenhub","baseUrl":"https://tokenhub.tencentmaas.com/v1","model":"deepseek-v4-pro-0813","maxOutputTokens":768,"apiKey":"synthetic-unit-placeholder"}`))
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	adapter, e := modelgateway.NewTencentTokenHubAdapter(config, modelDiagnosticTestTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 402, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"403004","source":"gateway","message":"PRIVATE_BODY_SENTINEL"}}`))}, nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	_, err := adapter.Complete(context.Background(), modelgateway.ProviderRequest{TaskKind: modelgateway.ActivityQuery, PromptVersion: "unit.diagnostic.v1", OutputMode: modelgateway.Text, OutputSchemaVersion: "air.answer.v1", MaxOutputTokens: 128, DeadlineAt: time.Now().Add(time.Minute), Messages: []modelgateway.Message{{Role: "system", Content: "Synthetic public context"}, {Role: "user", Content: "Synthetic public question"}}})
	wrapped := nativeLiveStage("model-gateway-complete", modelgateway.WithLiveFailureReason(err, "provider_UNKNOWN"))
	var provider modelgateway.ProviderError
	if calls != 1 || !errors.As(wrapped, &provider) || provider.Code != "UNKNOWN" || provider.Retryable {
		t.Fatal("native wrapper changed dispatch/retry/provider class")
	}
	if !modelgateway.IsAIServiceUnavailable(wrapped) {
		t.Fatal("original native diagnostic wrapper hid AI service failure")
	}
	for _, blocked := range []error{context.Canceled, context.DeadlineExceeded, modelgateway.ErrUnavailable} {
		if modelgateway.IsAIServiceUnavailable(nativeLiveStage("model-gateway-complete", errors.Join(wrapped, blocked))) {
			t.Fatal("availability replaced original cancellation or native failure")
		}
	}
	for _, reason := range []string{"live_release_or_settlement_unknown", "live_current_authority_unavailable", "live_wire_unavailable"} {
		if modelgateway.IsAIServiceUnavailable(nativeLiveStage("model-gateway-complete", modelgateway.WithLiveFailureReason(wrapped, reason))) {
			t.Fatal("availability ignored original native reason")
		}
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%w"} {
		if text := fmt.Sprintf(format, wrapped); strings.Contains(text, "PRIVATE") || strings.Contains(text, "synthetic-unit-placeholder") {
			t.Fatal("native service diagnostic exposed provider body/key")
		}
	}
}
