package modelgateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func assertModelDiagnostic(t *testing.T, err error, stage, kind string, status int, reason string) {
	t.Helper()
	var diagnostic *modelFailureDiagnostic
	if err == nil || !errors.As(err, &diagnostic) || diagnostic.stageName() != stage || diagnostic.kindName() != kind || diagnostic.status != status || diagnostic.reasonName() != reason {
		t.Fatal("fixed model diagnostic did not identify the expected local boundary")
	}
	summary, ok := SafeModelFailureSummary(err)
	if !ok || summary != diagnostic.Error() {
		t.Fatal("safe model diagnostic summary was lost")
	}
}

func TestTencentFailureDiagnosticPreservesHTTPStatusAndProviderClass(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{{403, "AUTHENTICATION"}, {400, "INVALID_REQUEST"}, {429, "RATE_LIMIT"}, {503, "TEMPORARY"}} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			calls := 0
			adapter := tencentAdapter(t, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
				calls++
				return tencentResponse("PRIVATE_PROVIDER_BODY PRIVATE_QUERY PRIVATE_KEY", tc.status), nil
			}))
			raw, err := adapter.Complete(context.Background(), tencentRequest())
			assertModelDiagnostic(t, err, "TENCENT_HTTP", "HTTP", tc.status, "unknown")
			var provider ProviderError
			if raw != nil || calls != 1 || !errors.As(err, &provider) || provider.Code != tc.code {
				t.Fatal("HTTP diagnostics changed single request/provider error identity")
			}
			for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d", "%w"} {
				if strings.Contains(fmt.Sprintf(format, err), "PRIVATE") {
					t.Fatal("HTTP diagnostic formatted a provider response")
				}
			}
		})
	}
}

func TestTencentFailureDiagnosticIdentifies200ResponseContractBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, body, stage string
	}{
		{"envelope", `{"PRIVATE_BODY":"PRIVATE_KEY"}`, "TENCENT_ENVELOPE"},
		{"model_binding", strings.Replace(tencentSuccess(), `"model":"hy3"`, `"model":"PRIVATE_MODEL"`, 1), "TENCENT_ID_MODEL"},
		{"choices", strings.Replace(tencentSuccess(), `"choices":[{`, `"choices":[null,{`, 1), "TENCENT_CHOICES"},
		{"tools", strings.Replace(tencentSuccess(), `"role":"assistant"`, `"role":"assistant","tool_calls":[{"PRIVATE_QUERY":"PRIVATE_KEY"}]`, 1), "TENCENT_TOOLS"},
		{"finish_enum", strings.Replace(tencentSuccess(), `"finish_reason":"stop"`, `"finish_reason":"PRIVATE_FINISH"`, 1), "TENCENT_FINISH"},
		{"content", strings.Replace(tencentSuccess(), `"content":"当前地点需要核验来源。"`, `"content":null`, 1), "TENCENT_CONTENT"},
		{"usage", strings.Replace(tencentSuccess(), `"total_tokens":43`, `"total_tokens":999`, 1), "TENCENT_USAGE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := tencentAdapter(t, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
				return tencentResponse(tc.body, 200), nil
			}))
			raw, err := adapter.Complete(context.Background(), tencentRequest())
			assertModelDiagnostic(t, err, tc.stage, "RESPONSE_SHAPE", 200, "unknown")
			if raw != nil || !errors.Is(err, ErrAdapter) || strings.Contains(fmt.Sprintf("%#v", err), "PRIVATE") {
				t.Fatal("invalid 200 body released content or changed adapter error identity")
			}
		})
	}
	adapter := tencentAdapter(t, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
		response := tencentResponse("PRIVATE_BODY", 200)
		response.Header.Set("Content-Type", "text/plain; PRIVATE_KEY")
		return response, nil
	}))
	_, err := adapter.Complete(context.Background(), tencentRequest())
	assertModelDiagnostic(t, err, "TENCENT_RESPONSE", "CONTENT_TYPE", 200, "unknown")
}

func TestTencentFailureDiagnosticTransportNeverFormatsURLCause(t *testing.T) {
	adapter := tencentAdapter(t, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "PRIVATE_OP", URL: "https://private.example/?query=PRIVATE_QUERY&key=PRIVATE_KEY", Err: errors.New("PRIVATE_TRANSPORT")}
	}))
	_, err := adapter.Complete(context.Background(), tencentRequest())
	assertModelDiagnostic(t, err, "TENCENT_TRANSPORT", "TRANSPORT", 0, "unknown")
	var provider ProviderError
	if !errors.As(err, &provider) || provider.Code != "UNKNOWN" {
		t.Fatal("transport diagnostic changed original normalized ProviderError")
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d", "%w"} {
		text := fmt.Sprintf(format, err)
		if strings.Contains(text, "PRIVATE") || strings.Contains(text, "private.example") || strings.Contains(text, "tokenhub.tencentmaas.com") {
			t.Fatal("transport diagnostic leaked a URL, query, key or original error")
		}
	}
}

func TestTencentFailureDiagnosticNativeGatewayRetainsSafeAdapterCauseAndReason(t *testing.T) {
	for _, tc := range []struct {
		name, body, stage, kind, code string
		status                        int
	}{
		{"http403", "PRIVATE_PROVIDER_BODY", "TENCENT_HTTP", "HTTP", "provider_AUTHENTICATION", 403},
		{"http400", "PRIVATE_PROVIDER_BODY", "TENCENT_HTTP", "HTTP", "provider_INVALID_REQUEST", 400},
		{"invalid200", `{"PRIVATE_BODY":"PRIVATE_KEY"}`, "TENCENT_ENVELOPE", "RESPONSE_SHAPE", "provider_UNKNOWN", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			port := &liveTestPort{}
			calls := 0
			gateway, _ := liveTestGateway(t, port, tencentRoundTripper(func(req *http.Request) (*http.Response, error) {
				calls++
				return tencentResponse(tc.body, tc.status), nil
			}))
			result, err := gateway.Complete(context.Background(), liveTestRequest())
			assertModelDiagnostic(t, err, tc.stage, tc.kind, tc.status, tc.code)
			assertNoLiveText(t, result, err)
			if calls != 1 || port.releases.Load() != 1 || port.settles.Load() != 1 || result.ReasonCode != tc.code || result.Usage.CostStatus != "UNKNOWN" {
				t.Fatal("diagnostic changed once-only wire or UNKNOWN settlement")
			}
			if tc.status == 200 && !errors.Is(err, ErrAdapter) {
				t.Fatal("gateway normalization discarded the safe original adapter identity")
			}
		})
	}
}

func TestTencentFailureDiagnosticNativeBrakeAndSettlementRemainClosed(t *testing.T) {
	t.Run("before_release", func(t *testing.T) {
		port := &liveTestPort{}
		port.denied.Store(true)
		gateway, _ := liveTestGateway(t, port, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
			t.Fatal("native brake diagnostic dispatched")
			return nil, nil
		}))
		result, err := gateway.Complete(context.Background(), liveTestRequest())
		assertModelDiagnostic(t, err, "MODEL_CURRENT", "NATIVE_CURRENT", 0, "live_current_authority_unavailable")
		assertNoLiveText(t, result, err)
		if !errors.Is(err, ErrUnavailable) || port.releases.Load() != 0 || port.settles.Load() != 0 {
			t.Fatal("native denial became a release or settlement")
		}
	})
	t.Run("settlement", func(t *testing.T) {
		port := &liveTestPort{}
		port.onSettle = func(context.Context, Request, PreparedTencentWire) error {
			return errors.New("PRIVATE_NATIVE_SETTLEMENT https://private.example/?key=PRIVATE_KEY")
		}
		calls := 0
		gateway, _ := liveTestGateway(t, port, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
			calls++
			return tencentResponse(tencentSuccess(), 200), nil
		}))
		result, err := gateway.Complete(context.Background(), liveTestRequest())
		assertModelDiagnostic(t, err, "MODEL_RELEASE_SETTLEMENT", "NATIVE_RELEASE_SETTLEMENT", 0, "live_release_or_settlement_unknown")
		assertNoLiveText(t, result, err)
		if calls != 1 || port.settles.Load() != 1 || !errors.Is(err, ErrUnavailable) {
			t.Fatal("settlement diagnostic retried or released an uncertain result")
		}
	})
}

func TestTencentFailureDiagnosticAllowlistAndFormatAreClosed(t *testing.T) {
	cause := errors.New("PRIVATE_CAUSE https://private.example/?query=PRIVATE_QUERY&key=PRIVATE_KEY")
	diagnostic := modelFailure("PRIVATE_STAGE", "PRIVATE_KIND", 999, cause).(*modelFailureDiagnostic)
	diagnostic.stage, diagnostic.kind, diagnostic.reason, diagnostic.status = 255, 255, 255, -1
	err := WithLiveFailureReason(diagnostic, "provider_PRIVATE_CODE")
	if !errors.Is(err, cause) || modelFailure("MODEL_GATEWAY", "UNKNOWN", 0, nil) != nil || WithLiveFailureReason(nil, "PRIVATE") != nil {
		t.Fatal("diagnostic changed original cause or nil result")
	}
	assertModelDiagnostic(t, err, "MODEL_GATEWAY", "UNKNOWN", 0, "unknown")
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d", "%w"} {
		text := fmt.Sprintf(format, err)
		if strings.Contains(text, "PRIVATE") || strings.Contains(text, "private.example") || (format != "%w" && !strings.Contains(text, "status=0")) {
			t.Fatal("diagnostic format boundary", format, "privateValuePresent", strings.Contains(text, "PRIVATE"), "statusPresent", strings.Contains(text, "status=0"))
		}
	}
	if wrapped := fmt.Errorf("%w", err); !errors.Is(wrapped, cause) || !strings.Contains(wrapped.Error(), "status=0") || strings.Contains(wrapped.Error(), "PRIVATE") {
		t.Fatal("valid error wrapping lost cause identity or safe diagnostics")
	}
	for _, status := range []int{0, -1, 99, 600, 999} {
		if normalizedModelHTTPStatus(status) != 0 {
			t.Fatal("out-of-range status entered diagnostics")
		}
	}
	for _, status := range []int{100, 200, 403, 599} {
		if normalizedModelHTTPStatus(status) != status {
			t.Fatal("valid HTTP status was lost")
		}
	}
	if _, ok := SafeModelFailureSummary(cause); ok {
		t.Fatal("arbitrary cause was classified from its private text")
	}
	if summary, ok := SafeModelFailureSummary(ProviderError{Code: "PRIVATE_CODE"}); !ok || strings.Contains(summary, "PRIVATE") || !strings.Contains(summary, "provider_UNKNOWN") {
		t.Fatal("arbitrary provider code entered server diagnostics")
	}
}

func TestTencentFailureDiagnosticNativeWireGuardIsNotProviderTransport(t *testing.T) {
	adapter := tencentAdapter(t, nil)
	prepared, err := adapter.Prepare(tencentRequest())
	if err != nil {
		t.Fatal("synthetic local preparation failed")
	}
	var attempts, wires atomic.Int32
	guard := liveWireTransport{prepared: prepared, attempts: &attempts, wires: &wires, before: func() error { return nil },
		next: tencentRoundTripper(func(*http.Request) (*http.Response, error) {
			t.Fatal("tampered local wire was dispatched")
			return nil, nil
		})}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, tencentChatEndpoint, bytes.NewReader(append(prepared.ExactWire(), ' ')))
	if err != nil {
		t.Fatal("synthetic request failed")
	}
	req.GetBody, req.Close = nil, true
	_, err = guard.RoundTrip(req)
	assertModelDiagnostic(t, err, "MODEL_WIRE", "NATIVE_WIRE", 0, "unknown")
	wrapped := &url.Error{Op: "PRIVATE_OP", URL: "https://private.example/?query=PRIVATE_QUERY&key=PRIVATE_KEY", Err: err}
	diagnostic := nativeModelTransportDiagnostic(wrapped)
	assertModelDiagnostic(t, diagnostic, "MODEL_WIRE", "NATIVE_WIRE", 0, "unknown")
	if !errors.Is(diagnostic, ErrLivePreparation) || wires.Load() != 0 || strings.Contains(fmt.Sprintf("%#v", diagnostic), "PRIVATE") {
		t.Fatal("local guard classification lost its identity, leaked URL or sent tampered wire")
	}
	if nativeModelTransportDiagnostic(&url.Error{Err: ProviderError{Code: "UNKNOWN"}}) != nil {
		t.Fatal("arbitrary provider transport was promoted to a native wire guard")
	}
}

func TestTencentFailureDiagnosticNativeBodyGuardKeepsOriginalCancellationPriority(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	adapter := tencentAdapter(t, tencentRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		body := &nativeLiveRequestBody{ctx: req.Context(), reader: strings.NewReader("synthetic-wire"), before: func() error {
			cancel()
			return ErrUnavailable
		}}
		_, guardErr := body.Read(make([]byte, 1))
		if nativeModelTransportDiagnostic(guardErr) == nil {
			t.Fatal("synthetic cancellation did not exercise native body guard diagnostics")
		}
		return nil, &url.Error{Op: "PRIVATE_OP", URL: "https://private.example/?key=PRIVATE_KEY", Err: guardErr}
	}))
	raw, err := adapter.Complete(ctx, tencentRequest())
	if raw != nil || calls != 1 || !errors.Is(err, context.Canceled) || strings.Contains(fmt.Sprintf("%#v", err), "PRIVATE") {
		t.Fatal("native guard metadata changed original cancellation priority, retried or leaked transport data")
	}
}

func TestTencentHTTPDiagnosticKnownCodeAndExplicitWireParam(t *testing.T) {
	for _, tc := range []struct {
		name, code string
	}{
		{"string", `"400002"`}, {"number", `400002`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, param := range tencentHTTPParams[1:] {
				t.Run(param, func(t *testing.T) {
					body := `{"error":{"code":` + tc.code + `,"type":"gateway_error","source":"gateway","param":"` + param + `","upstream_status":400,"message":"PRIVATE_BODY https://private.example/?key=PRIVATE_KEY","message_zh":"PRIVATE_QUERY","request_id":"PRIVATE_ID","upstream_code":"PRIVATE_CODE","arbitrary":"PRIVATE_VALUE"}}`
					calls := 0
					adapter := tencentAdapter(t, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
						calls++
						return tencentResponse(body, 400), nil
					}))
					raw, err := adapter.Complete(context.Background(), tencentRequest())
					assertModelDiagnostic(t, err, "TENCENT_HTTP", "HTTP", 400, "unknown")
					var diagnostic *modelFailureDiagnostic
					var provider ProviderError
					if raw != nil || calls != 1 || !errors.As(err, &diagnostic) || !errors.As(err, &provider) || provider.Code != "INVALID_REQUEST" ||
						modelDiagnosticName(uint8(diagnostic.http.code), tencentHTTPCodes[:]) != "400002" ||
						modelDiagnosticName(uint8(diagnostic.http.param), tencentHTTPParams[:]) != param || diagnostic.http.upstreamStatus != 400 {
						t.Fatal("known metadata changed dispatch/provider class or lost its explicit parameter")
					}
					wrapped := WithLiveFailureReason(err, "provider_INVALID_REQUEST")
					if !errors.Is(wrapped, err) || !errors.As(wrapped, &provider) || !strings.Contains(wrapped.Error(), "business=400002") || !strings.Contains(wrapped.Error(), "param="+param) {
						t.Fatal("native reason wrapper lost original HTTP identity or safe metadata")
					}
					assertTencentHTTPDiagnosticRedacted(t, wrapped)
				})
			}
		})
	}
	for _, code := range tencentHTTPCodes[1:] {
		metadata := readTencentHTTPMetadata(strings.NewReader(`{"error":{"code":"` + code + `"}}`))
		if modelDiagnosticName(uint8(metadata.code), tencentHTTPCodes[:]) != code {
			t.Fatal("reviewed Tencent business code was lost")
		}
	}
	metadata := readTencentHTTPMetadata(strings.NewReader(`{"error":{"code":"401006"}}`))
	err := tencentHTTPFailure(400, ProviderError{Code: "INVALID_REQUEST"}, metadata)
	if !strings.Contains(err.Error(), "business=401006") || !strings.Contains(err.Error(), "status=400") {
		t.Fatal("official invalid-endpoint business code incorrectly dictated the HTTP status")
	}
}

func assertTencentHTTPDiagnosticRedacted(t *testing.T, err error) {
	t.Helper()
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d", "%w"} {
		text := fmt.Sprintf(format, err)
		if strings.Contains(text, "PRIVATE") || strings.Contains(text, "private.example") || strings.Contains(text, "tokenhub.tencentmaas.com") {
			t.Fatal("HTTP diagnostic formatted unselected provider data", format)
		}
	}
}

type tencentHTTPDiagnosticReader struct {
	reader io.Reader
	err    error
	read   int
	onRead func()
}

func (r *tencentHTTPDiagnosticReader) Read(p []byte) (int, error) {
	if r.onRead != nil {
		r.onRead()
	}
	n, err := r.reader.Read(p)
	r.read += n
	if r.err != nil {
		return n, r.err
	}
	return n, err
}

func TestTencentHTTPDiagnosticUnknownMalformedAndBoundedBodyKeepOriginalHTTPError(t *testing.T) {
	base := `{"error":{"code":"400002","param":"messages","padding":""}}`
	atBound := strings.Replace(base, `"padding":""`, `"padding":"`+strings.Repeat("x", maxTencentHTTPDiagnosticBytes-len(base))+`"`, 1)
	for _, tc := range []struct {
		name, body string
		readErr    error
		known      bool
	}{
		{"unknown_code", `{"error":{"code":"PRIVATE_KEY https://private.example/?query=PRIVATE_QUERY","param":"model"}}`, nil, false},
		{"unlisted_numeric_code", `{"error":{"code":400099,"param":"model"}}`, nil, false},
		{"fractional_code", `{"error":{"code":400002.5}}`, nil, false},
		{"malformed", `{"error":{"code":"400002","message":"PRIVATE_KEY"}`, nil, false},
		{"trailing_json", `{"error":{"code":"400002"}} {"PRIVATE_KEY":true}`, nil, false},
		{"code_object", `{"error":{"code":{"PRIVATE_KEY":"PRIVATE_QUERY"}}}`, nil, false},
		{"read_failure", base, errors.New("PRIVATE_READ_ERROR https://private.example/?key=PRIVATE_KEY"), false},
		{"exact_8k", atBound, nil, true},
		{"over_8k", atBound + " ", nil, false},
		{"huge_body", atBound + strings.Repeat("PRIVATE_KEY", 5000), nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &tencentHTTPDiagnosticReader{reader: strings.NewReader(tc.body), err: tc.readErr}
			calls := 0
			adapter := tencentAdapter(t, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
				calls++
				response := tencentResponse("", 400)
				response.Body = io.NopCloser(reader)
				return response, nil
			}))
			raw, err := adapter.Complete(context.Background(), tencentRequest())
			assertModelDiagnostic(t, err, "TENCENT_HTTP", "HTTP", 400, "unknown")
			var provider ProviderError
			var diagnostic *modelFailureDiagnostic
			if raw != nil || calls != 1 || !errors.As(err, &provider) || provider != (ProviderError{Code: "INVALID_REQUEST"}) || !errors.As(err, &diagnostic) ||
				(diagnostic.http.code != 0) != tc.known || reader.read > maxTencentHTTPDiagnosticBytes+1 {
				t.Fatal("invalid diagnostic body changed original HTTP error, retried or exceeded its read bound")
			}
			if !tc.known && err.Error() != "MODEL stage=TENCENT_HTTP kind=HTTP status=400 reason=unknown" {
				t.Fatal("unknown diagnostic body changed the original safe HTTP summary")
			}
			assertTencentHTTPDiagnosticRedacted(t, err)
		})
	}
}

func TestTencentHTTPDiagnosticEnumsAndExplicitParamAreClosed(t *testing.T) {
	metadata := readTencentHTTPMetadata(strings.NewReader(`{"error":{"code":"400002","type":"PRIVATE_TYPE","source":"PRIVATE_SOURCE","param":"PRIVATE_PARAM","upstream_status":999,"message":"The request parameter messages is invalid or missing. Please check the value of this parameter.","message_zh":"PRIVATE_KEY"}}`))
	if metadata.code == 0 || metadata.typ != 0 || metadata.source != 0 || metadata.param != 0 || metadata.upstreamStatus != 0 {
		t.Fatal("arbitrary fields or message template became typed diagnostics")
	}
	err := tencentHTTPFailure(400, ProviderError{Code: "INVALID_REQUEST"}, metadata)
	if !strings.Contains(err.Error(), "param=unknown") {
		t.Fatal("parameter name was inferred from untrusted message text")
	}
	assertTencentHTTPDiagnosticRedacted(t, err)
	for _, param := range []string{"max_tokens", "thinking.type", "Messages", "messages[0].content", "model PRIVATE_KEY"} {
		metadata := readTencentHTTPMetadata(strings.NewReader(`{"error":{"code":"400002","param":"` + param + `"}}`))
		if metadata.param != 0 {
			t.Fatal("unselected parameter alias entered diagnostics")
		}
	}
	for _, source := range tencentHTTPSources[1:] {
		metadata := readTencentHTTPMetadata(strings.NewReader(`{"error":{"code":"400002","source":"` + source + `"}}`))
		if modelDiagnosticName(uint8(metadata.source), tencentHTTPSources[:]) != source {
			t.Fatal("known source enum was lost")
		}
	}
	for _, typ := range tencentHTTPTypes[1:] {
		metadata := readTencentHTTPMetadata(strings.NewReader(`{"error":{"code":"400002","type":"` + typ + `"}}`))
		if modelDiagnosticName(uint8(metadata.typ), tencentHTTPTypes[:]) != typ {
			t.Fatal("known type enum was lost")
		}
	}
	for _, status := range []int{0, 100, 400, 599, -1, 99, 600} {
		metadata := readTencentHTTPMetadata(strings.NewReader(fmt.Sprintf(`{"error":{"code":"400002","upstream_status":%d}}`, status)))
		if metadata.upstreamStatus != normalizedModelHTTPStatus(status) {
			t.Fatal("upstream status escaped its numeric HTTP bound")
		}
	}
}

func TestTencentHTTPDiagnosticNativeGatewayKeepsBusinessMetadataAndUnknownSettlement(t *testing.T) {
	port := &liveTestPort{}
	calls := 0
	gateway, _ := liveTestGateway(t, port, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
		calls++
		return tencentResponse(`{"error":{"code":"400002","type":"gateway_error","param":"model","message":"PRIVATE_KEY"}}`, 400), nil
	}))
	result, err := gateway.Complete(context.Background(), liveTestRequest())
	assertModelDiagnostic(t, err, "TENCENT_HTTP", "HTTP", 400, "provider_INVALID_REQUEST")
	assertNoLiveText(t, result, err)
	var provider ProviderError
	if !errors.As(err, &provider) || provider.Code != "INVALID_REQUEST" || calls != 1 || port.releases.Load() != 1 || port.settles.Load() != 1 ||
		result.ReasonCode != "provider_INVALID_REQUEST" || result.Usage.CostStatus != "UNKNOWN" || !strings.Contains(err.Error(), "business=400002") || !strings.Contains(err.Error(), "param=model") {
		t.Fatal("business metadata changed provider identity, request count or UNKNOWN settlement")
	}
	assertTencentHTTPDiagnosticRedacted(t, err)
	if summary, ok := SafeModelFailureSummary(err); ok {
		t.Log("synthetic safe HTTP summary:", summary)
	} else {
		t.Fatal("known synthetic HTTP metadata lost its safe summary")
	}
}

func TestTencentHTTPDiagnosticBodyReadKeepsOriginalCancellationPriority(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &tencentHTTPDiagnosticReader{reader: strings.NewReader(`{"error":{"code":"400002","param":"model"}}`), onRead: cancel}
	calls := 0
	adapter := tencentAdapter(t, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
		calls++
		response := tencentResponse("", 400)
		response.Body = io.NopCloser(reader)
		return response, nil
	}))
	raw, err := adapter.Complete(ctx, tencentRequest())
	if raw != nil || calls != 1 || !errors.Is(err, context.Canceled) {
		t.Fatal("bounded HTTP body diagnostic replaced original cancellation or retried")
	}
}
